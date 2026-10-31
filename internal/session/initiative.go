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
	// The private wake is an independent loop. A temporary failure there must
	// not disable the established contact decision path.
	inner, innerAt, hasInner, _ := s.WakeInnerLife(ctx, now)
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
	c.InitiativeLevel = check.Level
	c.UnansweredOutreach = check.Unanswered
	c.TestRequest = check.TestRequest
	lastUserAt, _ := time.Parse(time.RFC3339, c.LastUserAt)
	useInner := hasInner && innerAt.After(lastUserAt) && now.Sub(innerAt) >= 0 && now.Sub(innerAt) <= 6*time.Hour && character.ValidateInnerState(inner, refs) == nil
	if useInner {
		c.Impulses = append([]character.Impulse{}, inner.Impulses...)
		c.InnerFocus = inner.Focus
		c.InnerReason = inner.Reason
	}
	var p character.Initiative
	if useInner && len(c.Impulses) == 0 && inner.Focus != "connect" {
		p = character.Initiative{Action: "wait", Reason: inner.Reason, ReconsiderIn: inner.RevisitIn, Impulses: c.Impulses}
	} else {
		p, err = s.characterInitiativeDecision(ctx, now, c, refs)
	}
	if err != nil {
		s.History.FailInitiative(ctx, check.ID)
		return "consideration failed", err
	}
	p.Unanswered = check.Unanswered
	if p.Action == "contact" {
		p.Subject = character.ChosenSubject(p, p.Impulses)
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
