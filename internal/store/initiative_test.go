package store

import (
	"context"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

func seedInitiative(t *testing.T, h *History, now time.Time) {
	t.Helper()
	ctx := context.Background()
	for i, text := range []string{"我最近在练习画画", "想画一张家门口的旧书店"} {
		at := now.Add(-3*time.Hour + time.Duration(i)*time.Hour)
		if err := h.AddUserMessage(ctx, text, at, time.UTC); err != nil {
			t.Fatal(err)
		}
		if err := h.Add(ctx, "assistant", "门口那块旧招牌挺适合画进去的。", at.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
}

func proposedContact() character.Initiative {
	return character.Initiative{Action: "contact", Kind: "follow_up", Reason: "想到画旧书店时可以保留的一个细节", EvidenceIDs: []string{"chat:3"}, AfterMinutes: 5}
}

func TestInitiativeCanConsiderAfterCompletedQuietTurn(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	seedInitiative(t, h, now)
	at := now.Add(-time.Hour)
	if err := h.AddUserMessage(ctx, "嗯", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	entries, err := h.Recent(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	id := entries[0].ID
	if c, err := h.ClaimInitiative(ctx, now, time.UTC); err != nil || c.Ready {
		t.Fatalf("unhandled user turn treated as quiet: %+v %v", c, err)
	}
	if err := h.SaveCharacterTurn(ctx, id, character.Turn{Mode: "wait"}, at); err != nil {
		t.Fatal(err)
	}
	if err := h.CommitCharacterReply(ctx, id, "", at); err != nil {
		t.Fatal(err)
	}
	if c, err := h.ClaimInitiative(ctx, now, time.UTC); err != nil || !c.Ready {
		t.Fatalf("deliberate quiet permanently blocked consideration: %+v %v", c, err)
	}
}

func TestInitiativeRestartCompositionDeliveryAndNoUnansweredChasing(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	seedInitiative(t, h, now)
	c, err := h.ClaimInitiative(ctx, now, time.UTC)
	if err != nil || !c.Ready {
		t.Fatalf("claim: %+v %v", c, err)
	}
	queued, err := h.FinishInitiative(ctx, c, proposedContact(), now, time.UTC)
	if err != nil || !queued {
		t.Fatalf("queue: %t %v", queued, err)
	}
	if events, err := h.ProcessDue(ctx, now.Add(6*time.Minute), time.UTC, "zh-CN"); err != nil || len(events) != 0 {
		t.Fatalf("legacy outbox sent placeholder: %+v %v", events, err)
	}
	first, ready, err := h.NextChannelContact(ctx, now.Add(6*time.Minute), time.UTC, "zh-CN")
	if err != nil || !ready || !first.FirstTry || first.Body != "" {
		t.Fatalf("prepare: %+v %t %v", first, ready, err)
	}
	h.Close()
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	retry, ready, err := h.NextChannelContact(ctx, now.Add(7*time.Minute), time.UTC, "zh-CN")
	if err != nil || !ready || !retry.FirstTry || retry.Key != first.Key {
		t.Fatalf("restart lost composition: %+v %v", retry, err)
	}
	body := "忽然想到，招牌上掉漆的地方也可以留下。"
	if ok, err := h.FinalizeChannelContact(ctx, retry.IntentID, body, now.Add(7*time.Minute), time.UTC); err != nil || !ok {
		t.Fatalf("finalize: %t %v", ok, err)
	}
	retry, ready, err = h.NextChannelContact(ctx, now.Add(8*time.Minute), time.UTC, "zh-CN")
	if err != nil || !ready || retry.FirstTry || retry.Body != body {
		t.Fatalf("delivery retry changed text: %+v %v", retry, err)
	}
	if err := h.MarkChannelContactSent(ctx, retry.IntentID, now.Add(8*time.Minute)); err != nil {
		t.Fatal(err)
	}
	c, err = h.ClaimInitiative(ctx, now.Add(2*time.Hour), time.UTC)
	if err != nil || c.Ready || c.Reason != "waiting for user to return" {
		t.Fatalf("chased unanswered contact: %+v %v", c, err)
	}
}

func TestInitiativeRechecksIncomingMessagePauseAndForget(t *testing.T) {
	for _, action := range []string{"incoming", "pause", "forget"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
			h, err := OpenHistory(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			seedInitiative(t, h, now)
			c, err := h.ClaimInitiative(ctx, now, time.UTC)
			if err != nil || !c.Ready {
				t.Fatal(c, err)
			}
			if action == "pause" {
				err = h.SetPaused(ctx, true)
			} else {
				msg := "我回来啦，刚才那张画不用再聊了"
				if action == "forget" {
					msg = "忘记我的名字"
				}
				err = h.AddUserMessage(ctx, msg, now.Add(time.Second), time.UTC)
			}
			if err != nil {
				t.Fatal(err)
			}
			queued, err := h.FinishInitiative(ctx, c, proposedContact(), now.Add(2*time.Second), time.UTC)
			if err != nil || queued {
				t.Fatalf("stale proposal survived %s: %t %v", action, queued, err)
			}
			items, err := h.ContactIntents(ctx, 10)
			if err != nil || len(items) != 0 {
				t.Fatalf("stale intent saved: %+v %v", items, err)
			}
		})
	}
}

func TestInitiativeFinalSendRecheck(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	seedInitiative(t, h, now)
	c, err := h.ClaimInitiative(ctx, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := h.FinishInitiative(ctx, c, proposedContact(), now, time.UTC); err != nil || !ok {
		t.Fatal(ok, err)
	}
	item, ready, err := h.NextChannelContact(ctx, now.Add(6*time.Minute), time.UTC, "en")
	if err != nil || !ready {
		t.Fatal(ready, err)
	}
	if err := h.AddUserMessage(ctx, "换个话题吧", now.Add(6*time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if ok, err := h.FinalizeChannelContact(ctx, item.IntentID, "stale draft", now.Add(7*time.Minute), time.UTC); err != nil || ok {
		t.Fatalf("overtook new message: %t %v", ok, err)
	}
}

func TestInitiativeCadenceBudgetAndQuietHoursPersist(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	seedInitiative(t, h, now)
	if c, err := h.ClaimInitiative(ctx, time.Date(2026, 10, 26, 23, 0, 0, 0, time.UTC), time.UTC); err != nil || c.Ready || c.Reason != "quiet hours" {
		t.Fatalf("quiet: %+v %v", c, err)
	}
	for i := 0; i < 4; i++ {
		at := now.Add(time.Duration(i) * 46 * time.Minute)
		c, err := h.ClaimInitiative(ctx, at, time.UTC)
		if err != nil || !c.Ready {
			t.Fatalf("budget claim %d: %+v %v", i, c, err)
		}
		if _, err := h.FinishInitiative(ctx, c, character.Initiative{Action: "wait", Reason: "no new reason"}, at, time.UTC); err != nil {
			t.Fatal(err)
		}
		if c, err := h.ClaimInitiative(ctx, at.Add(time.Second), time.UTC); err != nil || c.Ready {
			t.Fatalf("rapid reconsideration: %+v %v", c, err)
		}
	}
	h.Close()
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if c, err := h.ClaimInitiative(ctx, now.Add(5*time.Hour), time.UTC); err != nil || c.Ready || c.Reason != "daily consideration budget" {
		t.Fatalf("restart reset budget: %+v %v", c, err)
	}
}
