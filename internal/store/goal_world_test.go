package store

import (
	"context"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
)

func TestGoalWorldPersistsCausalChainAndCatchup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	if _, err := h.AdvanceGoalWorld(ctx, start, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	state, err := h.AdvanceGoalWorld(ctx, start.AddDate(0, 0, 2), time.UTC)
	if err != nil || state.Day != 3 || state.Batch != 1 {
		t.Fatalf("goal world failed to resume: %+v err=%v", state, err)
	}
	events, err := h.GoalWorldEvents(ctx, 10)
	if err != nil || len(events) != 3 || events[1].ParentID != events[0].ID || events[2].ParentID != events[1].ID {
		t.Fatalf("causal chain missing: %+v err=%v", events, err)
	}
	if events[0].ObjectID != events[1].ObjectID || !events[1].Catchup || events[2].Catchup {
		t.Fatalf("object or catchup provenance changed: %+v", events)
	}
	if _, err := h.AdvanceGoalWorld(ctx, start.AddDate(0, 0, 2), time.UTC); err != nil {
		t.Fatal(err)
	}
	again, err := h.GoalWorldEvents(ctx, 10)
	if err != nil || len(again) != 3 {
		t.Fatalf("same-day advance duplicated events: %+v err=%v", again, err)
	}
	far := start.AddDate(0, 0, 30)
	state, err = h.AdvanceGoalWorld(ctx, far, time.UTC)
	if err != nil || state.Day != 10 || state.Date != far.Format("2006-01-02") {
		t.Fatalf("long-gap catchup was not bounded: %+v err=%v", state, err)
	}
	again, err = h.GoalWorldEvents(ctx, 20)
	if err != nil || len(again) != 10 || !again[8].Catchup || again[9].Catchup {
		t.Fatalf("catchup provenance was not recorded: %+v err=%v", again, err)
	}
}

func TestSharedWorldProgressCreatesOneCancellableContactCandidate(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	start := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	if _, err := h.AdvanceGoalWorld(ctx, start, time.UTC); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"那盒录音是谁留下的？", "我觉得先核对出处比较好。"} {
		if err := h.AddUserMessage(ctx, text, start.Add(time.Hour), time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	decision := actor.Decision{Act: "answer", Focus: "recording source", EvidenceIDs: []string{"worldv2:1"}, Brevity: "short"}
	if err := h.SaveActorDecision(ctx, 2, decision, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.ConfirmActorDecision(ctx, 2, decision, actor.Review{Pass: true, UsedWorldIDs: []string{"worldv2:1"}}, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	next := start.AddDate(0, 0, 1)
	if _, err := h.AdvanceGoalWorld(ctx, next, time.UTC); err != nil {
		t.Fatal(err)
	}
	intents, err := h.ContactIntents(ctx, 10)
	if err != nil || len(intents) != 1 || intents[0].Topic != "world_update" || intents[0].IdempotencyKey != "world:2" || !intents[0].Due.Equal(next.Add(45*time.Minute)) {
		t.Fatalf("world progress did not create one sourced candidate: %+v %v", intents, err)
	}
	if _, err := h.AdvanceGoalWorld(ctx, next, time.UTC); err != nil {
		t.Fatal(err)
	}
	if intents, err = h.ContactIntents(ctx, 10); err != nil || len(intents) != 1 {
		t.Fatalf("same-day progress duplicated contact: %+v %v", intents, err)
	}
	if err := h.AddUserMessage(ctx, "那盒录音现在怎么样了？", next.Add(10*time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.NextChannelContact(ctx, next.Add(time.Hour), time.UTC, "zh-CN"); err != nil || ready {
		t.Fatalf("user resumed before world contact but it remained sendable: %t %v", ready, err)
	}
	intents, err = h.ContactIntents(ctx, 10)
	if err != nil || intents[0].State != "canceled" {
		t.Fatalf("world contact cancellation was not durable: %+v %v", intents, err)
	}
}

func TestGoalWorldSeedsOldStoryWithoutRepeatingDayOne(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	start := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	for day := 0; day < 3; day++ {
		if _, err := h.AdvanceWorld(ctx, start.AddDate(0, 0, day), time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	state, err := h.AdvanceGoalWorld(ctx, start.AddDate(0, 0, 2), time.UTC)
	if err != nil || state.Day != 3 || state.Batch != 1 || !state.AudioRecovered {
		t.Fatalf("legacy story did not become goal state: %+v err=%v", state, err)
	}
	events, err := h.GoalWorldEvents(ctx, 10)
	if err != nil || len(events) != 3 || events[0].Provenance != "authored_v1" || events[2].ParentID != events[1].ID {
		t.Fatalf("legacy events were not linked: %+v err=%v", events, err)
	}
	if _, err := h.AdvanceGoalWorld(ctx, start.AddDate(0, 0, 2), time.UTC); err != nil {
		t.Fatal(err)
	}
	events, err = h.GoalWorldEvents(ctx, 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("legacy migration duplicated events: %+v err=%v", events, err)
	}
	state, err = h.AdvanceGoalWorld(ctx, start.AddDate(0, 0, 3), time.UTC)
	if err != nil || state.Day != 4 {
		t.Fatalf("goal world did not continue after migration: %+v err=%v", state, err)
	}
}

func TestOnlyGroundedWorldAttentionChangesNextAction(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, want      string
		worldDecision   bool
		confirmDecision bool
		worldUsed       bool
		forget          bool
		wantRelated     bool
	}{
		{"unrelated chat", "repair", false, false, false, false, false},
		{"unaccepted plan", "repair", true, false, false, false, false},
		{"unrelated reply with world in plan", "repair", true, true, false, false, false},
		{"world conversation", "inspect", true, true, true, false, true},
		{"forgotten conversation", "repair", true, true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := OpenHistory(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			if _, err := h.AdvanceGoalWorld(ctx, start, time.UTC); err != nil {
				t.Fatal(err)
			}
			if err := h.AddUserMessage(ctx, "今天聊了很多。", start.Add(time.Hour), time.UTC); err != nil {
				t.Fatal(err)
			}
			if tc.worldDecision {
				decision := actor.Decision{Act: "answer", Focus: "recording box", EvidenceIDs: []string{"worldv2:1"}, Brevity: "short"}
				if err := h.SaveActorDecision(ctx, 1, decision, start.Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				if tc.confirmDecision {
					review := actor.Review{Pass: true}
					if tc.worldUsed {
						review.UsedWorldIDs = []string{"worldv2:1"}
					}
					if err := h.ConfirmActorDecision(ctx, 1, decision, review, start.Add(time.Hour)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.forget {
				if err := h.AddUserMessage(ctx, "忘记我的名字", start.Add(2*time.Hour), time.UTC); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := h.AdvanceGoalWorld(ctx, start.AddDate(0, 0, 1), time.UTC); err != nil {
				t.Fatal(err)
			}
			events, err := h.GoalWorldEvents(ctx, 2)
			if err != nil || len(events) != 2 || events[1].Action != tc.want || events[1].RelatedToUser != tc.wantRelated {
				t.Fatalf("world changed for wrong reason: %+v err=%v", events, err)
			}
		})
	}
}
