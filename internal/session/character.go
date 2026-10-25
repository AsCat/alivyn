package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/model"
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
	if s.Config.EffectiveWorldPack() == "tidehaven" {
		f.Canon = character.Setting(s.Config.Language)
	}
	persona := s.Config.Background
	if strings.TrimSpace(persona) == "" {
		persona = character.DefaultPersona(s.Config.Language)
	}
	state, at, err := s.History.CharacterStateBefore(ctx, f.SourceMessageID, now)
	if err != nil {
		return character.Context{}, err
	}
	threadSources, err := s.History.OpenThreadSources(ctx, state.OpenThreads)
	if err != nil {
		return character.Context{}, err
	}
	sources = append(sources, threadSources...)
	c := character.Context{Frame: f, Persona: persona, Moment: state.Moment, LastChatAt: lastChat, OpenThreads: state.OpenThreads, Sources: sources}
	if !at.IsZero() {
		c.MomentAt = at.Format(time.RFC3339)
	}
	return c, nil
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
	turn, err := character.Generate(ctx, s.Client, s.Config.Model, c)
	if err != nil {
		return "", fmt.Errorf("character dialogue: %w", err)
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
	c.Occasion = occasion
	t, err := character.Generate(ctx, s.Client, s.Config.Model, c)
	if err != nil {
		return "", err
	}
	if t.Mode == "wait" {
		return "", ErrContactWait
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
	references := append(append([]actor.Evidence(nil), c.Frame.Evidence...), c.Sources...)
	for i, e := range entries {
		if e.Content != "[forgotten memory]" {
			content := e.Content
			if runes := []rune(content); len(runes) > 2000 {
				content = string(runes[:2000]) + "…"
			}
			c.Frame.Recent = append(c.Frame.Recent, model.Message{Role: e.Role, Content: content})
			c.LastChatAt = e.CreatedAt.Format(time.RFC3339)
			if i >= len(entries)-12 {
				references = append(references, actor.Evidence{ID: fmt.Sprintf("chat:%d", e.ID), Provenance: "observed_chat", At: e.CreatedAt.Format(time.RFC3339), Text: e.Role + ": " + content})
			}
		}
	}
	state, momentAt, err := s.History.CharacterStateBefore(ctx, sourceID+1, now)
	c.Moment = state.Moment
	c.OpenThreads = state.OpenThreads
	if err == nil {
		more, sourceErr := s.History.OpenThreadSources(ctx, state.OpenThreads)
		if sourceErr != nil {
			return c, nil, sourceErr
		}
		references = append(references, more...)
	}
	if !momentAt.IsZero() {
		c.MomentAt = momentAt.Format(time.RFC3339)
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
