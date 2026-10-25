package session

import (
	"context"
	"errors"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

var ErrContactWait = errors.New("character chose not to initiate contact")

// ConsiderInitiative runs independently of incoming messages. A cheap durable
// gate precedes the model; the proposal is rechecked after it returns.
func (s Session) ConsiderInitiative(ctx context.Context, now time.Time) (string, error) {
	if s.Config.EffectiveEngine() != "character_v2" {
		return "engine disabled", nil
	}
	zone, err := s.Config.Location()
	if err != nil {
		return "", err
	}
	check, err := s.History.ClaimInitiative(ctx, now, zone)
	if err != nil || !check.Ready {
		return check.Reason, err
	}
	started := time.Now()
	c, refs, err := s.characterContactContext(ctx, now)
	if err != nil {
		s.History.FailInitiative(ctx, check.ID)
		return "context failed", err
	}
	p, err := character.ConsiderContact(ctx, s.Client, s.Config.Model, c, refs)
	if err != nil {
		s.History.FailInitiative(ctx, check.ID)
		return "consideration failed", err
	}
	queued, err := s.History.FinishInitiative(ctx, check, p, now.Add(time.Since(started)), zone)
	if err != nil {
		return "", err
	}
	if queued {
		return "queued", nil
	}
	if p.Action == "wait" {
		return "wait", nil
	}
	return "superseded or duplicate", nil
}
