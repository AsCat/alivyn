package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWorldRunsSevenDaysAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	zone := time.FixedZone("test", 8*3600)
	start := time.Date(2026, 10, 24, 10, 0, 0, 0, zone)
	for day := 0; day < 7; day++ {
		at := start.AddDate(0, 0, day)
		state, err := h.AdvanceWorld(ctx, at, zone)
		if err != nil || state.Day != day+1 {
			t.Fatalf("day %d: state=%+v err=%v", day+1, state, err)
		}
		if day == 2 {
			if err := h.AddUserMessage(ctx, "我最近在整理旧照片，发现很多人的名字记不起来了。", at.Add(time.Hour), zone); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	state, err := h.AdvanceWorld(ctx, start.AddDate(0, 0, 6).Add(8*time.Hour), zone)
	if err != nil || state.Day != 7 || state.Progress != 2 {
		t.Fatalf("reopened state=%+v err=%v", state, err)
	}
	events, err := h.WorldEvents(ctx, 10)
	if err != nil || len(events) != 7 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	if !events[3].RelatedToUser || events[3].Kind != "reflection" || !strings.Contains(events[3].Zh, "用户") {
		t.Fatalf("conversation did not change day four: %+v", events[3])
	}
	if events[2].RelatedToUser || events[6].RelatedToUser {
		t.Fatal("one encounter leaked into unrelated days")
	}
}

func TestWorldCatchesUpWithoutFutureEvents(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	zone := time.UTC
	first := time.Date(2026, 1, 1, 23, 0, 0, 0, zone)
	if _, err := h.AdvanceWorld(ctx, first, zone); err != nil {
		t.Fatal(err)
	}
	state, err := h.AdvanceWorld(ctx, first.Add(30*24*time.Hour), zone)
	if err != nil {
		t.Fatal(err)
	}
	if state.Day != 8 || state.Date != "2026-01-31" {
		t.Fatalf("unexpected capped catch-up: %+v", state)
	}
	events, err := h.WorldEvents(ctx, 30)
	if err != nil || len(events) != 8 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	if events[len(events)-1].Date != state.Date {
		t.Fatal("last event does not match current local day")
	}
}
