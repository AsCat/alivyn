package session

import (
	"context"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

// WakeInnerLife advances attention even when outbound contact is quiet or
// paused. A wake only saves a sourced inner-state summary; it never sends.
func (s Session) WakeInnerLife(ctx context.Context, now time.Time) (character.InnerState, time.Time, bool, error) {
	if s.Config.EffectiveEngine() != "character_v2" {
		return character.InnerState{}, time.Time{}, false, nil
	}
	zone, err := s.Config.Location()
	if err != nil {
		return character.InnerState{}, time.Time{}, false, err
	}
	claim, err := s.History.ClaimInnerWake(ctx, now, zone)
	if err != nil {
		return character.InnerState{}, time.Time{}, false, err
	}
	if !claim.Ready {
		return s.History.LatestInnerState(ctx)
	}
	previous, _, exists, err := s.History.LatestInnerState(ctx)
	if err != nil {
		s.History.FailInnerWake(ctx, claim.ID, now)
		return character.InnerState{}, time.Time{}, false, err
	}
	c, refs, err := s.characterContactContext(ctx, now)
	if err != nil {
		s.History.FailInnerWake(ctx, claim.ID, now)
		return character.InnerState{}, time.Time{}, false, err
	}
	var prior *character.InnerState
	if exists && character.ValidateInnerState(previous, refs) == nil {
		prior = &previous
	}
	started := time.Now()
	state, err := character.FormInnerState(ctx, s.Client, s.Config.Model, c, refs, prior)
	if err != nil {
		s.History.FailInnerWake(ctx, claim.ID, now)
		return character.InnerState{}, time.Time{}, false, err
	}
	committed, err := s.History.FinishInnerWake(ctx, claim, state, now.Add(time.Since(started)))
	if err != nil {
		return character.InnerState{}, time.Time{}, false, err
	}
	if !committed {
		return character.InnerState{}, time.Time{}, false, nil
	}
	// A choice to attend to her own fictional life can advance the selected
	// world simulator. Its separate ledger still validates and deduplicates the
	// event; no private chat text is passed to that module.
	if state.Focus == "own_life" && s.Config.EffectiveWorldPack() == "tidehaven" {
		if _, err := s.AdvanceWorldModule(ctx, "tidehaven", now); err != nil {
			return state, now, true, err
		}
	}
	return state, now, true, nil
}

// StartInnerLife runs independently of the chat transport and contact queue.
// The durable clock makes frequent ticks cheap and bounds model calls.
func (s Session) StartInnerLife(ctx context.Context, report func(error)) func() {
	if s.Config.EffectiveEngine() != "character_v2" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			if _, _, _, err := s.WakeInnerLife(ctx, time.Now()); err != nil && ctx.Err() == nil && report != nil {
				report(err)
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
