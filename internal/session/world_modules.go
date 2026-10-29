package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/AsCat/alivyn/pkg/extension"
)

func (s Session) simulatedSceneEvidence(ctx context.Context, before time.Time) ([]actor.Evidence, error) {
	if s.Config.EffectiveWorldPack() != "tidehaven" {
		return nil, nil
	}
	visible, err := s.History.VisibleSimulatedScenes(ctx, "tidehaven", actorSubject, before, 3)
	if err != nil {
		return nil, err
	}
	var evidence []actor.Evidence
	for _, event := range visible {
		var scene store.Scene
		if err := json.Unmarshal(event.Payload, &scene); err != nil {
			return nil, err
		}
		text := scene.En
		if s.Config.Language == "zh-CN" {
			text = scene.Zh
		}
		parent := ""
		if len(event.ParentIDs) > 0 {
			parent = event.ParentIDs[0]
		}
		evidence = append(evidence, actor.Evidence{ID: event.ID, Provenance: "simulated_world", At: event.At.Format(time.RFC3339), Text: text, ObjectID: scene.Place, ParentID: parent, Action: event.Kind})
	}
	return evidence, nil
}

// AdvanceWorldModule runs an explicitly selected simulator. The simulator
// receives only world events, not the user's private chat. Returned scenes
// are proposals; the store validates and commits each one with a stable key.
func (s Session) AdvanceWorldModule(ctx context.Context, worldID string, now time.Time) ([]string, error) {
	if s.Extensions == nil {
		return nil, nil
	}
	selected, ok := s.Extensions.Active("world."+worldID, extension.Simulate)
	if !ok {
		return nil, nil
	}
	events, err := s.History.RecentWorldEvents(ctx, worldID, 100)
	if err != nil {
		return nil, err
	}
	world, err := json.Marshal(events)
	if err != nil {
		return nil, err
	}
	proposals, err := selected.Simulator.Advance(ctx, extension.SimulationRequest{SubjectID: actorSubject, WorldID: worldID, AsOf: now, World: world})
	if err != nil {
		return nil, err
	}
	if len(proposals) > 3 {
		return nil, errors.New("simulator proposed too many events in one tick")
	}
	var committed []string
	for _, proposal := range proposals {
		if proposal.ProposalID == "" || len(proposal.ProposalID) > 120 {
			return committed, errors.New("simulator proposal needs a stable ID")
		}
		id, err := s.History.CommitWorldProposal(ctx, worldID, fmt.Sprintf("%s:%s:%s", selected.Manifest.ID, worldID, proposal.ProposalID), proposal, now)
		if err != nil {
			return committed, err
		}
		committed = append(committed, id)
	}
	return committed, nil
}

// StartWorldSimulation considers one scene per local day while the channel is
// running. A failed model call may be retried later; a successful skip stays
// quiet until the next day in this process. Restarted attempts remain fenced
// by the simulator's stable daily proposal ID and the store's unique key.
func (s Session) StartWorldSimulation(ctx context.Context, report func(error)) func() {
	if s.Extensions == nil || s.Config.EffectiveEngine() != "character_v2" || s.Config.EffectiveWorldPack() != "tidehaven" {
		return func() {}
	}
	if _, ok := s.Extensions.Active("world.tidehaven", extension.Simulate); !ok {
		return func() {}
	}
	zone, err := s.Config.Location()
	if err != nil {
		if report != nil {
			report(err)
		}
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Minute)
		defer ticker.Stop()
		attemptedDay := ""
		next := time.Time{}
		for {
			now := time.Now()
			local := now.In(zone)
			day := local.Format("2006-01-02")
			if local.Hour() >= 11 && local.Hour() < 21 && attemptedDay != day && !now.Before(next) {
				if _, err := s.AdvanceWorldModule(ctx, "tidehaven", now); err != nil {
					if report != nil && ctx.Err() == nil {
						report(err)
					}
					next = now.Add(time.Hour)
				} else {
					attemptedDay = day
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
