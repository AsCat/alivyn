package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/display"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/persona"
	"github.com/AsCat/alivyn/internal/store"
)

func (s Session) characterContext(ctx context.Context, now time.Time, sourceID int64) (character.Context, error) {
	f, err := s.actorFrame(ctx, now, sourceID)
	if err != nil {
		return character.Context{}, err
	}
	entries, err := s.History.ActorTranscriptThrough(ctx, 40, f.SourceMessageID)
	if err != nil {
		return character.Context{}, err
	}
	f.Recent = nil
	inDialogue := map[string]bool{}
	lastChat := ""
	var previousAt time.Time
	var sources []actor.Evidence
	for _, e := range entries {
		if e.Content == store.ForgetAcknowledgement {
			inDialogue[fmt.Sprintf("chat:%d", e.ID)] = true
			f.Recent = append(f.Recent, model.Message{Role: "user", Content: "I requested forgetting a memory and the local store has completed that operation. Please just acknowledge it without recalling the removed information."})
			continue
		}
		if e.Content == "[forgotten memory]" {
			if e.ID == f.SourceMessageID {
				f.Recent = append(f.Recent, model.Message{Role: "system", Content: "The user's current message requested forgetting a memory. The local store has already removed that memory and redacted related messages. Acknowledge briefly without reconstructing what was removed."})
			}
			continue
		}
		inDialogue[fmt.Sprintf("chat:%d", e.ID)] = true
		if !previousAt.IsZero() && e.CreatedAt.Sub(previousAt) >= 2*time.Hour {
			f.Recent = append(f.Recent, model.Message{Role: "system", Content: fmt.Sprintf("Conversation time boundary: %.1f hours passed since the preceding message. The next message arrived at %s. Earlier activities and goodbyes are historical; use the current availability state and local time, rather than treating this as one uninterrupted exchange.", e.CreatedAt.Sub(previousAt).Hours(), e.CreatedAt.Format(time.RFC3339))})
		}
		previousAt = e.CreatedAt
		text := e.Content
		if r := []rune(text); len(r) > 2000 {
			text = string(r[:2000]) + "…"
		}
		f.Recent = append(f.Recent, model.Message{Role: e.Role, Content: text})
		sources = append(sources, actor.Evidence{ID: fmt.Sprintf("chat:%d", e.ID), Provenance: "observed_chat", At: e.CreatedAt.Format(time.RFC3339), Text: e.Role + ": " + text})
		if e.ID != f.SourceMessageID {
			lastChat = e.CreatedAt.Format(time.RFC3339)
		}
	}
	var records []actor.Evidence
	worldCount := 0
	// World events arrive newest first. A few recent developments give life
	// continuity without turning every exchange into an archive status report.
	for _, e := range f.Evidence {
		if e.Provenance == "character_identity" || inDialogue[e.ID] {
			continue
		}
		if e.Provenance == "simulated_world" {
			worldCount++
			if worldCount > 3 {
				continue
			}
		}
		records = append(records, e)
	}
	f.Evidence = records
	// A queued reply may be composed after a later reading. It may only know
	// about activities that had happened when this user message arrived.
	if s.lifeEnabled() {
		cutoff, parseErr := time.Parse(time.RFC3339, f.Latest.At)
		if parseErr != nil {
			return character.Context{}, parseErr
		}
		readings, readErr := s.History.LifeReadingsBefore(ctx, cutoff, 3)
		if readErr != nil {
			return character.Context{}, readErr
		}
		for _, reading := range readings {
			f.Evidence = append(f.Evidence, readingEvidence(reading)...)
		}
	}
	if s.Config.EffectiveWorldPack() == "tidehaven" {
		f.Canon = character.Setting(s.Config.Language)
	}
	characterProfile := s.characterPersona()
	moment, threads, at, err := s.characterAffect(ctx, f.SourceMessageID, now)
	if err != nil {
		return character.Context{}, err
	}
	threadSources, err := s.History.OpenThreadSources(ctx, threads)
	if err != nil {
		return character.Context{}, err
	}
	sources = append(sources, threadSources...)
	c := character.Context{Frame: f, Persona: characterProfile, Moment: moment, LastChatAt: lastChat, OpenThreads: threads, Sources: sources}
	selected, _ := persona.Get(s.Config.EffectivePersonaPack())
	c.DirectPersona = selected.Standalone
	c.AllowBubbles = s.Config.EffectivePersonaPack() == "vivid" || selected.Standalone
	c.SelfFacts, c.Episodes, err = s.characterMemory(ctx, f.SourceMessageID, now)
	if err != nil {
		return character.Context{}, err
	}
	c.Routine = s.Config.Routine
	if strings.TrimSpace(c.Routine) == "" {
		c.Routine = character.DefaultRoutine(s.Config.Language)
	}
	localNow, err := time.Parse(time.RFC3339, f.Now)
	if err != nil {
		return character.Context{}, err
	}
	c.CanLeave, c.Availability, c.ReceivedAt, err = s.History.AvailabilityContext(ctx, f.SourceMessageID, localNow)
	if err != nil {
		return character.Context{}, err
	}
	if !at.IsZero() {
		c.MomentAt = at.Format(time.RFC3339)
	}
	closedAt, err := s.History.RecentClosedExchange(ctx, f.SourceMessageID, now)
	if err != nil {
		return character.Context{}, err
	}
	if !closedAt.IsZero() {
		c.ClosedAt = closedAt.In(localNow.Location()).Format(time.RFC3339)
	}
	return c, nil
}

func (s Session) characterPersona() string {
	selected, _ := persona.Get(s.Config.EffectivePersonaPack())
	if selected.Standalone {
		return persona.Text(selected.ID, s.Config.Language)
	}
	base := strings.TrimSpace(s.Config.Background)
	if base == "" {
		base = character.DefaultPersona(s.Config.Language)
	}
	if extra := persona.Text(s.Config.EffectivePersonaPack(), s.Config.Language); extra != "" {
		return base + "\n\n" + extra
	}
	return base
}

func (s Session) composeCharacterReply(ctx context.Context, now time.Time, sourceID int64) (string, error) {
	if sourceID == 0 {
		entries, err := s.History.RecentForModel(ctx, 1)
		if err != nil {
			return "", err
		}
		if len(entries) != 1 || entries[0].Role != "user" {
			return "", errors.New("character reply requires a latest user turn")
		}
		sourceID = entries[0].ID
	}
	if saved, found, err := s.History.CharacterTurn(ctx, sourceID); err != nil {
		return "", err
	} else if found {
		return saved.Reply, nil
	}
	c, err := s.characterContext(ctx, now, sourceID)
	if err != nil {
		return "", err
	}
	turn, err := character.GenerateChecked(ctx, s.Client, s.Config.Model, c)
	if err != nil {
		return "", fmt.Errorf("character dialogue: %w", err)
	}
	if c.AllowBubbles && turn.Mode == "say" && turn.Absence == nil {
		turn.Reply = display.ComposeChat(turn.Reply, c.Frame.Recent, c.Frame.Language)
	}
	if err := s.History.SaveCharacterTurn(ctx, sourceID, turn, now); err != nil {
		return "", err
	}
	return turn.Reply, nil
}

func (s Session) composeCharacterContact(ctx context.Context, occasion string) (string, error) {
	return s.composeCharacterContactAt(ctx, occasion, time.Now())
}

func (s Session) composeCharacterContactAt(ctx context.Context, occasion string, now time.Time) (string, error) {
	c, _, err := s.characterContactContext(ctx, now)
	if err != nil {
		return "", err
	}
	tuning, err := s.History.CharacterTuning(ctx)
	if err != nil {
		return "", err
	}
	c.InitiativeLevel = tuning.Initiative
	c.Occasion = occasion
	t, err := character.GenerateChecked(ctx, s.Client, s.Config.Model, c)
	if err != nil {
		return "", err
	}
	if t.Mode == "wait" {
		return "", ErrContactWait
	}
	if c.AllowBubbles && t.Mode == "say" && t.Absence == nil {
		t.Reply = display.ComposeChat(t.Reply, c.Frame.Recent, c.Frame.Language)
	}
	if len([]rune(t.Reply)) > 180 {
		return "", errors.New("character contact is too long")
	}
	return t.Reply, nil
}

func (s Session) characterContactContext(ctx context.Context, now time.Time) (character.Context, []actor.Evidence, error) {
	entries, err := s.History.RecentForModel(ctx, 40)
	if err != nil {
		return character.Context{}, nil, err
	}
	if len(entries) > 0 {
		entries, err = s.History.ActorTranscriptThrough(ctx, 40, entries[len(entries)-1].ID)
		if err != nil {
			return character.Context{}, nil, err
		}
	}
	var sourceID int64
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Role == "user" {
			sourceID = entries[i].ID
			break
		}
	}
	if sourceID == 0 {
		return character.Context{}, nil, errors.New("proactive character contact needs prior conversation")
	}
	c, err := s.characterContext(ctx, now, sourceID)
	if err != nil {
		return character.Context{}, nil, err
	}
	// Proactive contact includes the last response, unlike a source-bound reply.
	c.Frame.Recent = nil
	// A proactive turn happens after the last inbound message. New activities
	// in that gap are visible here even though ordinary replies are source-bound.
	if s.lifeEnabled() {
		readings, readErr := s.History.LifeReadingsBefore(ctx, now, 3)
		if readErr != nil {
			return character.Context{}, nil, readErr
		}
		seen := map[string]bool{}
		for _, evidence := range c.Frame.Evidence {
			seen[evidence.ID] = true
		}
		for _, reading := range readings {
			for _, evidence := range readingEvidence(reading) {
				if !seen[evidence.ID] {
					c.Frame.Evidence = append(c.Frame.Evidence, evidence)
				}
			}
		}
	}
	references := append(append([]actor.Evidence(nil), c.Frame.Evidence...), c.Sources...)
	// A character can initiate from an established preference or her own
	// recorded state, without pretending a new external event happened.
	if profile := strings.TrimSpace(c.Persona); profile != "" {
		runes := []rune(profile)
		if len(runes) > 1100 {
			profile = string(runes[:1100]) + "…"
		}
		references = append(references, actor.Evidence{ID: "self:character", Provenance: "authored_character", Text: profile})
	}
	if c.Moment.Feeling != "" || c.Moment.Intention != "" || c.Moment.Attention != "" {
		references = append(references, actor.Evidence{ID: "self:moment", Provenance: "character_state", At: c.MomentAt,
			Text: fmt.Sprintf("feeling=%s; attention=%s; intention=%s; connection=%s", c.Moment.Feeling, c.Moment.Attention, c.Moment.Intention, c.Moment.Connection)})
	}
	for i, e := range entries {
		if e.Content != "[forgotten memory]" {
			content := e.Content
			if runes := []rune(content); len(runes) > 2000 {
				content = string(runes[:2000]) + "…"
			}
			c.Frame.Recent = append(c.Frame.Recent, model.Message{Role: e.Role, Content: content})
			c.LastChatAt = e.CreatedAt.Format(time.RFC3339)
			if e.Role == "user" {
				c.LastUserAt = e.CreatedAt.Format(time.RFC3339)
			}
			if i >= len(entries)-12 {
				references = append(references, actor.Evidence{ID: fmt.Sprintf("chat:%d", e.ID), Provenance: "observed_chat", At: e.CreatedAt.Format(time.RFC3339), Text: e.Role + ": " + content})
			}
		}
	}
	c.OutreachHistory, err = s.History.RecentOutreach(ctx, now, 8)
	if err != nil {
		return c, nil, err
	}
	moment, threads, momentAt, err := s.characterAffect(ctx, sourceID+1, now)
	c.Moment = moment
	c.OpenThreads = threads
	if err == nil {
		more, sourceErr := s.History.OpenThreadSources(ctx, threads)
		if sourceErr != nil {
			return c, nil, sourceErr
		}
		references = append(references, more...)
	}
	if !momentAt.IsZero() {
		c.MomentAt = momentAt.Format(time.RFC3339)
	}
	if err == nil {
		c.SelfFacts, c.Episodes, err = s.characterMemory(ctx, sourceID+1, now)
	}
	return c, references, err
}

// ReflectCharacter maintains continuity independently of message generation.
func (s Session) ReflectCharacter(ctx context.Context, now time.Time) error {
	if s.Config.EffectiveEngine() != "character_v2" {
		return nil
	}
	id, delivered, ready, err := s.History.NextCharacterReflection(ctx, now)
	if err != nil || !ready {
		return err
	}
	c, err := s.characterContext(ctx, now, id)
	if err != nil {
		return err
	}
	tuning, err := s.History.CharacterTuning(ctx)
	if err != nil {
		return err
	}
	c.InitiativeLevel = tuning.Initiative
	if source, found, err := s.History.CharacterReplySource(ctx, id); err != nil {
		return err
	} else if found {
		c.Sources = append(c.Sources, source)
	}
	snapshot, err := character.Reflect(ctx, s.Client, s.Config.Model, c, delivered.Reply)
	if err != nil {
		return err
	}
	return s.History.SaveCharacterReflection(ctx, id, snapshot)
}

// StartCharacterReflection owns one bounded worker. Stop it before closing the
// store. Reflection failures leave the transcript and previous state intact.
func (s Session) StartCharacterReflection(ctx context.Context) func() {
	if s.Config.EffectiveEngine() != "character_v2" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		next := time.Time{}
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if now.Before(next) {
					continue
				}
				if err := s.ReflectCharacter(ctx, now); err != nil {
					next = now.Add(time.Minute)
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}
