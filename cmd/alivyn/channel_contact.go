package main

import (
	"context"
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
	for ctx.Err() == nil {
		now := time.Now()
		if now.Sub(lastHeartbeat) >= time.Minute {
			if s.Config.EffectiveWorldPack() == "tidehaven" {
				if _, err := h.AdvanceWorld(ctx, now, zone); err != nil {
					fmt.Fprintf(out, "World clock failed: %v\n", err)
				}
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
		item, ready, err := h.NextChannelContact(ctx, now, zone, language)
		if err != nil && ctx.Err() == nil {
			fmt.Fprintf(out, "Contact decision failed: %v\n", err)
		}
		if ready {
			if item.FirstTry {
				if generated, err := s.ComposeContact(ctx, item.Topic); err == nil {
					if clean := strings.TrimSpace(display.Clean(generated)); clean != "" {
						item.Body = clean
					}
				} else if ctx.Err() == nil {
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
