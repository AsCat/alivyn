package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/AsCat/alivyn/internal/world"
)

func (s Session) composeActorReply(ctx context.Context, now time.Time, sourceMessageID int64) (string, error) {
	frame, err := s.actorFrame(ctx, now, sourceMessageID)
	if err != nil {
		return "", err
	}
	decision, found, err := s.History.ActorDecision(ctx, frame.SourceMessageID)
	if err != nil {
		return "", err
	}
	if found {
		raw, _ := json.Marshal(decision)
		if _, err := actor.ParseDecision(string(raw), frame); err != nil {
			found = false // Forgotten or superseded evidence cannot be reused.
		}
	}
	if !found {
		decision, err = actor.Plan(ctx, s.Client, s.Config.Model, frame)
		if err != nil {
			return "", fmt.Errorf("actor planning: %w", err)
		}
		if err := s.History.SaveActorDecision(ctx, frame.SourceMessageID, decision, now); err != nil {
			return "", err
		}
	}
	reply, err := actor.Realize(ctx, s.Client, s.Config.Model, frame, decision, "")
	revised := false
	if errors.Is(err, actor.ErrInvalidReply) {
		revised = true
		reply, err = actor.Realize(ctx, s.Client, s.Config.Model, frame, decision, "The draft was empty or too long. Write a concise complete message.")
	}
	if err != nil {
		return "", err
	}
	review, err := actor.Check(ctx, s.Client, s.Config.Model, frame, decision, reply)
	if err != nil {
		return "", fmt.Errorf("actor review: %w", err)
	}
	if review.Pass {
		return reply, nil
	}
	if revised {
		return "", fmt.Errorf("actor reply did not pass grounded review: %s", review.Issue)
	}
	reply, err = actor.Realize(ctx, s.Client, s.Config.Model, frame, decision, review.Issue)
	if err != nil {
		return "", err
	}
	review, err = actor.Check(ctx, s.Client, s.Config.Model, frame, decision, reply)
	if err != nil {
		return "", fmt.Errorf("actor revision review: %w", err)
	}
	if !review.Pass {
		return "", fmt.Errorf("actor reply did not pass grounded review: %s", review.Issue)
	}
	return reply, nil
}

func (s Session) actorFrame(ctx context.Context, now time.Time, sourceMessageID int64) (actor.Frame, error) {
	zone, err := s.Config.Location()
	if err != nil {
		return actor.Frame{}, err
	}
	var all []store.Entry
	if sourceMessageID > 0 {
		all, err = s.History.ActorTranscriptThrough(ctx, 100, sourceMessageID)
	} else {
		all, err = s.History.RecentForModel(ctx, 100)
	}
	if err != nil {
		return actor.Frame{}, err
	}
	if len(all) == 0 || all[len(all)-1].Role != "user" {
		return actor.Frame{}, errors.New("actor reply requires a latest user message")
	}
	latest := all[len(all)-1]
	if sourceMessageID > 0 && latest.ID != sourceMessageID {
		return actor.Frame{}, errors.New("source user message is unavailable for actor reply")
	}
	state, err := s.History.RelationshipAt(ctx, latest.ID)
	if err != nil {
		return actor.Frame{}, err
	}
	priorUserTurns, err := s.History.PriorUserTurns(ctx, latest.ID)
	if err != nil {
		return actor.Frame{}, err
	}
	frame := actor.Frame{
		SourceMessageID: latest.ID,
		Reality:         "An AI character met through this chat. Its world is simulated. It cannot have met the user offline, and events in its setting cannot serve as real-world alibis.",
		Name:            s.Config.Name, Language: s.Config.Language,
		Style: s.Config.EffectiveStyle(), Background: s.Config.EffectiveBackground(),
		Relationship: string(state.Stage), Now: now.In(zone).Format(time.RFC3339),
		PriorUserTurns: priorUserTurns,
		Latest:         actor.Evidence{ID: fmt.Sprintf("chat:%d", latest.ID), Provenance: "observed_chat", At: latest.CreatedAt.In(zone).Format(time.RFC3339), Text: latest.Content},
	}
	if s.Config.EffectiveWorldPack() == "tidehaven" {
		if s.Config.Language == "zh-CN" {
			frame.Values = world.ValuesZh
			frame.Abilities = world.AbilitiesZh
			frame.Canon = world.CanonZh
		} else {
			frame.Values = world.ValuesEn
			frame.Abilities = world.AbilitiesEn
			frame.Canon = world.CanonEn
		}
	}
	if state.Reserved {
		frame.Relationship += "; user explicitly requested distance"
	}
	for i := len(all) - 2; i >= 0; i-- {
		if all[i].Role != "user" {
			continue
		}
		previous, found, err := s.History.ActorDecision(ctx, all[i].ID)
		if err != nil {
			return actor.Frame{}, err
		}
		if found {
			frame.Previous = &previous
		}
		break
	}
	if s.Config.EffectiveWorldPack() == "tidehaven" {
		_, err := s.History.AdvanceWorld(ctx, now, zone)
		if err != nil {
			return actor.Frame{}, err
		}
		events, err := s.History.WorldEvents(ctx, 5)
		if err != nil {
			return actor.Frame{}, err
		}
		for _, event := range events {
			content := event.En
			if s.Config.Language == "zh-CN" {
				content = event.Zh
			}
			frame.Evidence = append(frame.Evidence, actor.Evidence{ID: fmt.Sprintf("world:%d", event.Day), Provenance: "simulated_world", At: event.Date, Text: content})
		}
	}
	facts, err := s.History.ActiveFacts(ctx)
	if err != nil {
		return actor.Frame{}, err
	}
	for _, fact := range facts {
		if fact.SourceMessageID > latest.ID {
			continue
		}
		frame.Evidence = append(frame.Evidence, actor.Evidence{ID: fmt.Sprintf("fact:%d", fact.ID), Provenance: fact.Provenance, At: fact.SourceAt.In(zone).Format(time.RFC3339), Text: fmt.Sprintf("%s: %s (%s)", fact.Topic, fact.HappensAt.In(zone).Format("2006-01-02 15:04"), fact.Precision)})
	}
	personal, err := s.History.ActivePersonal(ctx)
	if err != nil {
		return actor.Frame{}, err
	}
	for _, fact := range personal {
		if fact.SourceMessageID > latest.ID {
			continue
		}
		frame.Evidence = append(frame.Evidence, actor.Evidence{ID: fmt.Sprintf("personal:%d", fact.SourceMessageID), Provenance: "user_stated", At: fact.SourceAt.In(zone).Format(time.RFC3339), Text: fact.Value})
	}
	// Older user messages are episodic evidence. Do not treat old assistant
	// wording as a style guide, and do not retrieve forgotten transcript lines.
	var prior []store.Entry
	for i := len(all) - 2; i >= 0 && len(prior) < 8; i-- {
		entry := all[i]
		if entry.Role != "user" || entry.Content == "[forgotten memory]" || strings.HasPrefix(entry.Content, "Please acknowledge that I asked you to forget") {
			continue
		}
		prior = append(prior, entry)
	}
	for i := len(prior) - 1; i >= 0; i-- {
		entry := prior[i]
		content := entry.Content
		if runes := []rune(content); len(runes) > 240 {
			content = string(runes[:240]) + "…"
		}
		frame.Evidence = append(frame.Evidence, actor.Evidence{ID: fmt.Sprintf("chat:%d", entry.ID), Provenance: "observed_chat", At: entry.CreatedAt.In(zone).Format(time.RFC3339), Text: content})
	}
	start := len(all) - 6
	if start < 0 {
		start = 0
	}
	for _, entry := range all[start:] {
		frame.Recent = append(frame.Recent, model.Message{Role: entry.Role, Content: entry.Content})
	}
	return frame, nil
}
