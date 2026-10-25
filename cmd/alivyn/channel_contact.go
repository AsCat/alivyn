package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/display"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
)

func workChannelContact(ctx context.Context, h *store.History, s session.Session, zone *time.Location, language string, available func() bool, send func(context.Context, string, string) error, out io.Writer) {
	if err := h.ResumeRuntime(ctx, time.Now()); err != nil {
		fmt.Fprintf(out, "Contact runtime could not resume: %v\n", err)
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastHeartbeat := time.Time{}
	nextCompositionAttempt := time.Time{}
	nextInitiativeCheck := time.Time{}
	for ctx.Err() == nil {
		now := time.Now()
		if now.Sub(lastHeartbeat) >= time.Minute {
			if err := advanceConfiguredWorld(ctx, h, s.Config, now, zone); err != nil {
				fmt.Fprintf(out, "World clock failed: %v\n", err)
			}
			if err := h.Heartbeat(ctx, now); err != nil {
				fmt.Fprintf(out, "Contact heartbeat failed: %v\n", err)
			}
			lastHeartbeat = now
		}
		if !available() {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			}
		}
		if now.Before(nextCompositionAttempt) {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			}
		}
		if !now.Before(nextInitiativeCheck) {
			if _, err := s.ConsiderInitiative(ctx, now); err != nil && ctx.Err() == nil {
				fmt.Fprintf(out, "Initiative consideration deferred: %v\n", err)
			}
			nextInitiativeCheck = time.Now().Add(time.Minute)
		}
		item, ready, err := h.NextChannelContact(ctx, now, zone, language)
		if err != nil && ctx.Err() == nil {
			fmt.Fprintf(out, "Contact decision failed: %v\n", err)
		}
		if ready {
			if item.FirstTry {
				if generated, err := s.ComposeContactFor(ctx, item.Topic, item.Key); err == nil {
					if clean := strings.TrimSpace(display.Clean(generated)); clean != "" {
						item.Body = clean
					}
				} else if errors.Is(err, session.ErrContactWait) {
					if err := h.CancelChannelContact(ctx, item.IntentID, time.Now()); err != nil {
						fmt.Fprintf(out, "Contact cancellation failed: %v\n", err)
					}
					continue
				} else if ctx.Err() == nil {
					if item.Topic == "world_update" || s.Config.EffectiveEngine() == "character_v2" {
						fmt.Fprintf(out, "Contact composition delayed: %v\n", err)
						if resetErr := h.RetryChannelComposition(ctx, item.IntentID); resetErr != nil {
							fmt.Fprintf(out, "Contact composition reset failed: %v\n", resetErr)
							return
						}
						nextCompositionAttempt = time.Now().Add(2 * time.Minute)
						continue // A generic fallback would lose the source event.
					}
					fmt.Fprintf(out, "Contact composition failed; using fallback: %v\n", err)
				}
			}
			allowed, err := h.FinalizeChannelContact(ctx, item.IntentID, item.Body, time.Now(), zone)
			if err != nil && ctx.Err() == nil {
				fmt.Fprintf(out, "Contact recheck failed: %v\n", err)
			}
			if !allowed || err != nil {
				continue
			}
			if err := send(ctx, item.Body, item.Key); err != nil {
				if ctx.Err() == nil {
					fmt.Fprintf(out, "Contact delivery pending; will retry: %v\n", err)
				}
			} else if err := h.MarkChannelContactSent(ctx, item.IntentID, time.Now()); err != nil && ctx.Err() == nil {
				fmt.Fprintf(out, "Contact delivery confirmed, local record pending: %v\n", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
