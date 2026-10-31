package store

import (
	"context"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/pkg/extension"
)

func TestInnerWakePersistsAcrossRestartAndIgnoresContactPause(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "昨晚那首歌还在脑子里", now.Add(-time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	claim, err := h.ClaimInnerWake(ctx, now, time.UTC)
	if err != nil || !claim.Ready {
		t.Fatalf("contact pause also stopped private wake: %+v %v", claim, err)
	}
	state := character.InnerState{Focus: "rest", Reason: "想安静一会儿", RevisitIn: 120}
	if ok, err := h.FinishInnerWake(ctx, claim, state, now); err != nil || !ok {
		t.Fatalf("finish wake: %t %v", ok, err)
	}
	events, err := h.ReadEvents(ctx, extension.EventQuery{SubjectID: "default", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		found = found || event.Kind == "person.inner_wake" && event.Domain == "inference" && event.Visibility == "owner"
	}
	if !found {
		t.Fatalf("inner wake missing from experience ledger: %+v", events)
	}
	h.Close()
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	got, at, exists, err := h.LatestInnerState(ctx)
	if err != nil || !exists || got.Focus != "rest" || !at.Equal(now) {
		t.Fatalf("saved attention did not survive restart: %+v %s %t %v", got, at, exists, err)
	}
	if next, err := h.ClaimInnerWake(ctx, now.Add(time.Hour), time.UTC); err != nil || next.Ready {
		t.Fatalf("durable wake cadence lost: %+v %v", next, err)
	}
	if next, err := h.ClaimInnerWake(ctx, now.Add(2*time.Hour), time.UTC); err != nil || !next.Ready {
		t.Fatalf("wake never revisited saved attention: %+v %v", next, err)
	}
}

func TestInnerWakeDiscardedWhenConversationChanges(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "在吗", now.Add(-time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	claim, err := h.ClaimInnerWake(ctx, now, time.UTC)
	if err != nil || !claim.Ready {
		t.Fatal(claim, err)
	}
	if err := h.AddUserMessage(ctx, "我换个话题", now.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if ok, err := h.FinishInnerWake(ctx, claim, character.InnerState{Focus: "rest", Reason: "想安静", RevisitIn: 120}, now.Add(time.Minute)); err != nil || ok {
		t.Fatalf("stale wake committed: %t %v", ok, err)
	}
	if _, _, exists, err := h.LatestInnerState(ctx); err != nil || exists {
		t.Fatalf("superseded inner state became current: %t %v", exists, err)
	}
}
