package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/pkg/extension"
)

func TestExperienceLedgerLinksSourcesAndResetRemovesTheirContent(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "今天想听你说说汐湾", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := h.AdvanceGoalWorld(ctx, at, time.UTC); err != nil {
		t.Fatal(err)
	}
	first, err := h.ReadEvents(ctx, extension.EventQuery{After: 0, Limit: 20})
	if err != nil || len(first) < 2 || first[0].Kind != "chat.message" || first[1].Kind != "world.action" {
		t.Fatalf("sources were not indexed atomically: %+v %v", first, err)
	}
	if err := h.AddUserMessage(ctx, "/清除记忆", at.Add(time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	after, err := h.ReadEvents(ctx, extension.EventQuery{After: 0, Limit: 20})
	if err != nil || len(after) != 0 {
		t.Fatalf("reset leaked old events: %+v %v", after, err)
	}
}

func TestSimulatedSceneIsIdempotentAndObserverScoped(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	payload, _ := json.Marshal(Scene{Place: "archive", Zh: "她在档案室听见一段旧录音。", En: "She heard an old recording in the archive."})
	p := extension.EventProposal{Kind: "world.scene", SubjectID: "default", At: at, ObserverIDs: []string{"default"}, Payload: payload, Schema: "alivyn.world.scene/v1"}
	id, err := h.CommitWorldProposal(ctx, "tidehaven", "demo:one", p, at)
	if err != nil || id != "worldsim:1" {
		t.Fatal(id, err)
	}
	again, err := h.CommitWorldProposal(ctx, "tidehaven", "demo:one", p, at)
	if err != nil || again != id {
		t.Fatal(again, err)
	}
	changed := p
	changed.ObserverIDs = nil
	if _, err := h.CommitWorldProposal(ctx, "tidehaven", "demo:one", changed, at); err == nil {
		t.Fatal("idempotency key accepted different event")
	}
	all, err := h.ReadEvents(ctx, extension.EventQuery{After: 0, Limit: 10})
	if err != nil || len(all) != 1 || all[0].ID != id {
		t.Fatal(all, err)
	}
	visible, err := h.VisibleSimulatedScenes(ctx, "tidehaven", "default", at, 3)
	if err != nil || len(visible) != 1 {
		t.Fatal(visible, err)
	}
	unseen := p
	unseen.ObserverIDs = nil
	unseen.At = at.Add(time.Minute)
	if _, err := h.CommitWorldProposal(ctx, "tidehaven", "demo:unseen", unseen, unseen.At); err != nil {
		t.Fatal(err)
	}
	visible, err = h.VisibleSimulatedScenes(ctx, "tidehaven", "default", unseen.At, 3)
	if err != nil || len(visible) != 1 {
		t.Fatalf("unseen scene entered character view: %+v %v", visible, err)
	}
}

func TestExperienceBackfillAndForgottenSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, "DROP TRIGGER experience_message_insert"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "我叫阿意", at, time.UTC); err != nil {
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
	events, err := h.ReadEvents(ctx, extension.EventQuery{After: 0, Limit: 10})
	if err != nil || len(events) != 1 || events[0].ID != "chat:1" {
		t.Fatalf("old source was not backfilled: %+v %v", events, err)
	}
	if err := h.AddUserMessage(ctx, "忘记我的名字", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	events, err = h.ReadEvents(ctx, extension.EventQuery{After: 0, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.Contains(string(event.Payload), "我叫阿意") {
			t.Fatal("forgotten content remained in experience view")
		}
	}
}

func TestWitnessedSceneCanCreateSourcedContactCandidate(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	for _, text := range []string{"你在修那批录音吗？", "先确认授权再公开吧。"} {
		if err := h.AddUserMessage(ctx, text, at.Add(-time.Hour), time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	payload, _ := json.Marshal(Scene{Place: "archive", Zh: "她决定先核对授权。", En: "She decided to verify consent first."})
	id, err := h.CommitWorldProposal(ctx, "tidehaven", "test:contact", extension.EventProposal{Kind: "world.scene", SubjectID: "default", At: at, ObserverIDs: []string{"default"}, Payload: payload, Schema: "alivyn.world.scene/v1"}, at)
	if err != nil {
		t.Fatal(err)
	}
	intents, err := h.ContactIntents(ctx, 10)
	if err != nil || len(intents) != 1 || intents[0].Topic != "world_scene" || intents[0].IdempotencyKey != id {
		t.Fatalf("scene contact was not sourced: %+v %v", intents, err)
	}
	if _, err := h.CommitWorldProposal(ctx, "tidehaven", "test:contact", extension.EventProposal{Kind: "world.scene", SubjectID: "default", At: at, ObserverIDs: []string{"default"}, Payload: payload, Schema: "alivyn.world.scene/v1"}, at); err != nil {
		t.Fatal(err)
	}
	intents, err = h.ContactIntents(ctx, 10)
	if err != nil || len(intents) != 1 {
		t.Fatalf("duplicate scene created a second contact: %+v %v", intents, err)
	}
	// A new user turn makes the scheduled outreach stale, even though the
	// underlying scene remains a valid experience for the next reply.
	if err := h.AddUserMessage(ctx, "你今天在忙什么？", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.NextChannelContact(ctx, at.Add(16*time.Minute), time.UTC, "zh-CN"); err != nil || ready {
		t.Fatalf("stale world scene contact became ready: ready=%v err=%v", ready, err)
	}
	intents, err = h.ContactIntents(ctx, 10)
	if err != nil || len(intents) != 1 || intents[0].State != "canceled" || intents[0].Reason != "conversation resumed after world scene" {
		t.Fatalf("stale world scene contact was not canceled: %+v %v", intents, err)
	}
}

func TestRecentWorldEventsSurviveBusyConversation(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	payload, _ := json.Marshal(Scene{Place: "archive", Zh: "她整理了一段录音。", En: "She sorted a recording."})
	if _, err := h.CommitWorldProposal(ctx, "tidehaven", "busy:world", extension.EventProposal{Kind: "world.scene", SubjectID: "default", At: at, ObserverIDs: []string{"default"}, Payload: payload, Schema: "alivyn.world.scene/v1"}, at); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		if err := h.Add(ctx, "user", "一条后来的普通消息", at.Add(time.Duration(i+1)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	events, err := h.RecentWorldEvents(ctx, "tidehaven", 5)
	if err != nil || len(events) != 1 || events[0].ID != "worldsim:1" {
		t.Fatalf("world event was lost behind chat volume: %+v %v", events, err)
	}
}

func TestExperienceReopenPreservesCursorAndRecentRowsHandleGaps(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	if err := h.Add(ctx, "user", "第一条", at); err != nil {
		t.Fatal(err)
	}
	first, err := h.EventSequence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		h, err = OpenHistory(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.EventSequence(ctx)
		if err != nil || got != first {
			t.Fatalf("reopening changed the event cursor: got=%d want=%d err=%v", got, first, err)
		}
		if err := h.Close(); err != nil {
			t.Fatal(err)
		}
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.db.ExecContext(ctx, "UPDATE sqlite_sequence SET seq=5000 WHERE name='experience_events'"); err != nil {
		t.Fatal(err)
	}
	if err := h.Add(ctx, "user", "第二条", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	cursor, err := h.RecentEventCursor(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	events, err := h.ReadEvents(ctx, extension.EventQuery{After: cursor, Limit: 2})
	if err != nil || len(events) != 2 || events[0].ID != "chat:1" || events[1].ID != "chat:2" {
		t.Fatalf("recent event view lost events across cursor gap: %+v %v", events, err)
	}
}
