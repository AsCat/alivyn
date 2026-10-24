package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/AsCat/alivyn/internal/store"
)

func workChannelContact(ctx context.Context, h *store.History, zone *time.Location, language string, available func() bool, send func(context.Context, string, string) error, out io.Writer) {
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
