package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/contact"
	"github.com/AsCat/alivyn/internal/relationship"
)

func TestResetClearsLifeAndQueuedWorkButPreservesPolicyAndReceipts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	seedInitiative(t, h, at)
	check, err := h.ClaimInitiative(ctx, at, time.UTC)
	if err != nil || !check.Ready {
		t.Fatal(check, err)
	}
	if ok, err := h.FinishInitiative(ctx, check, proposedContact(), at, time.UTC); err != nil || !ok {
		t.Fatal(ok, err)
	}
	item, ready, err := h.NextChannelContact(ctx, at.Add(6*time.Minute), time.UTC, "en")
	if err != nil || !ready {
		t.Fatal(ready, err)
	}
	for _, day := range []time.Time{at.Add(-48 * time.Hour), at.Add(-24 * time.Hour), at} {
		if _, err := h.AdvanceGoalWorld(ctx, day, time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.EnqueueFeishu(ctx, "old", "chat", "m1", "我叫林晓", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	old, _, _ := h.NextFeishu(ctx)
	turn := character.Turn{Mode: "say", Reply: "你好，林晓。", Moment: character.Moment{Intention: "记得这段交流"}}
	if err := h.SaveCharacterTurn(ctx, old.UserMessageID, turn, at); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFeishuReply(ctx, "old", turn.Reply, at); err != nil {
		t.Fatal(err)
	}
	policy := contact.Policy{QuietStart: "21:30", QuietEnd: "09:00", DailyLimit: 1, Paused: true}
	if err := h.SetContactPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnqueueFeishu(ctx, "reset", "chat", "m2", "/清除记忆", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"character_turns", "character_self_facts", "personal_memory", "memory_revisions", "relationship_snapshots", "world_state", "world_events", "goal_world_state", "goal_world_events", "initiative_checks", "contact_intents", "contact_delivery", "assistant_turns"} {
		var n int
		if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s survived reset: %d %v", table, n, err)
		}
	}
	if state, err := h.Relationship(ctx); err != nil || state.Stage != relationship.Stranger {
		t.Fatal(state, err)
	}
	if p, err := h.ContactPolicy(ctx); err != nil || p != policy {
		t.Fatal(p, err)
	}
	if entries, err := h.RecentForModel(ctx, 100); err != nil || len(entries) != 0 {
		t.Fatalf("reset entered model context: %+v %v", entries, err)
	}
	sent := false
	if err := h.DeliverFeishu(ctx, "old", func() error { sent = true; return nil }); err != nil || sent {
		t.Fatal("cleared prepared reply sent", err)
	}
	if err := h.DeliverContact(ctx, item.IntentID, at, time.UTC, "old draft", func() error { sent = true; return nil }); err != nil || sent {
		t.Fatal("cleared contact sent", err)
	}
	if err := h.SaveCharacterTurn(ctx, old.UserMessageID, turn, at); !errors.Is(err, ErrTurnSuperseded) {
		t.Fatal("in-flight generation resurrected", err)
	}
	if err := h.CommitCharacterReply(ctx, old.UserMessageID, turn.Reply, at); !errors.Is(err, ErrTurnSuperseded) {
		t.Fatal("old reply committed", err)
	}
	turn.StateUpdated = true
	if err := h.SaveCharacterReflection(ctx, old.UserMessageID, turn); err != nil {
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
	if added, err := h.EnqueueFeishu(ctx, "old", "chat", "m1", "我叫林晓", at, time.UTC); err != nil || added {
		t.Fatal("old delivery replayed after reset", added, err)
	}
	if _, err := h.EnqueueFeishu(ctx, "new", "chat", "m3", "你好", at.Add(2*time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if added, err := h.EnqueueFeishu(ctx, "reset", "chat", "m2", "/清除记忆", at, time.UTC); err != nil || added {
		t.Fatal("reset replayed", added, err)
	}
	reset, ok, err := h.NextFeishuBatch(ctx, at.Add(3*time.Minute))
	if err != nil || !ok || reset.EventID != "reset" {
		t.Fatal("command merged with greeting", reset, ok, err)
	}
	reply, found, err := h.CommandReply(ctx, reset.UserMessageID, "zh-CN")
	if err != nil || !found {
		t.Fatal(reply, found, err)
	}
	if err := h.SetFeishuReply(ctx, reset.EventID, reply, at); err != nil {
		t.Fatal(err)
	}
	if err := h.MarkFeishuSent(ctx, reset.EventID); err != nil {
		t.Fatal(err)
	}
	entries, err := h.ActorTranscriptThrough(ctx, 100, reset.UserMessageID+1)
	if err != nil || len(entries) != 1 || entries[0].Content != "你好" {
		t.Fatalf("command acknowledgement leaked: %+v %v", entries, err)
	}
	if strings.Contains(reply, "林晓") {
		t.Fatal("ack recalled deleted name")
	}
}

func TestResetCommandMustBeTheWholeUserInput(t *testing.T) {
	for _, text := range []string{"她说 /清除记忆", "请解释 /reset", "`/reset`", "/reset now"} {
		if IsResetCommand(text) {
			t.Fatalf("prose treated as destructive command: %q", text)
		}
	}
}
