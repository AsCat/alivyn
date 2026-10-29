package session

import (
	"context"
	"fmt"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/life"
	"github.com/AsCat/alivyn/internal/persona"
)

func (s Session) lifeEnabled() bool {
	pack, _ := persona.Get(s.Config.EffectivePersonaPack())
	return s.Config.EffectiveEngine() == "character_v2" && !pack.Standalone
}

// ExploreLife performs one source-backed private reading. It returns an empty
// Reading if today's activity has already happened or the day is outside the
// ordinary activity window. Force bypasses only the time-of-day gate.
func (s Session) ExploreLife(ctx context.Context, now time.Time, force bool) (life.Reading, error) {
	if !s.lifeEnabled() {
		return life.Reading{}, nil
	}
	zone, err := s.Config.Location()
	if err != nil {
		return life.Reading{}, err
	}
	local := now.In(zone)
	generation, claimed, err := s.History.ClaimLifeReading(ctx, now, zone, force)
	if err != nil || !claimed {
		return life.Reading{}, err
	}
	defer s.History.ReleaseLifeReading(context.Background(), generation, local.Format("2006-01-02"))
	candidates, err := s.readingCandidates(ctx, now)
	if err != nil {
		return life.Reading{}, err
	}
	prior, err := s.History.LifeReadingsBefore(ctx, now, 40)
	if err != nil {
		return life.Reading{}, err
	}
	seen := map[string]bool{}
	for _, r := range prior {
		seen[r.URL] = true
	}
	var fresh []life.Candidate
	for _, item := range candidates {
		if !seen[item.URL] {
			fresh = append(fresh, item)
		}
	}
	if len(fresh) == 0 {
		return life.Reading{}, fmt.Errorf("no unread public excerpts available")
	}
	reading, err := life.Choose(ctx, s.Client, s.Config.Model, s.characterPersona(), s.Config.Language, local, prior, fresh)
	if err != nil {
		return life.Reading{}, err
	}
	return s.History.SaveLifeReading(ctx, generation, reading)
}

// StartLifeLoop is independent of incoming messages and of the contact send
// worker. Model latency here never blocks a direct reply.
func (s Session) StartLifeLoop(ctx context.Context, report func(error)) func() {
	if !s.lifeEnabled() {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		next := time.Time{}
		for {
			now := time.Now()
			if !now.Before(next) {
				if _, err := s.ExploreLife(ctx, now, false); err != nil && ctx.Err() == nil {
					if report != nil {
						report(err)
					}
					next = now.Add(15 * time.Minute)
				} else {
					next = now.Add(2 * time.Minute)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}

func readingEvidence(r life.Reading) []actor.Evidence {
	base := fmt.Sprintf("reading:%d", r.ID)
	return []actor.Evidence{
		{ID: base + ":source", Provenance: "external_excerpt", At: r.At.Format(time.RFC3339),
			Text: fmt.Sprintf("On %s the character read a public feed excerpt, not the full article. Title: %s. URL: %s. Actual excerpt: %s", r.Date, r.Title, r.URL, r.Excerpt)},
		{ID: base + ":thought", Provenance: "character_reflection", At: r.At.Format(time.RFC3339),
			Text: fmt.Sprintf("Her private, tentative reaction to that excerpt: %s. Feeling: %s. Open question: %s", r.Thought, r.Feeling, r.Question)},
	}
}
