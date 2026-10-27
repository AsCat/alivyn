package store

import (
	"context"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/contact"
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

func TestHighInitiativeCanFollowUpSeveralTimesAndResetsAfterReply(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	seedInitiative(t, h, now)
	if err := h.SetInitiative(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err := h.SetContactPolicy(ctx, contact.Policy{QuietStart: "23:00", QuietEnd: "07:00", DailyLimit: 6}); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 4; round++ {
		at := now.Add(time.Duration(round) * 40 * time.Minute)
		check, err := h.ClaimInitiative(ctx, at, time.UTC)
		if err != nil || !check.Ready || check.Unanswered != round {
			t.Fatalf("round %d could not consider: %+v %v", round, check, err)
		}
		if queued, err := h.FinishInitiative(ctx, check, proposedContact(), at, time.UTC); err != nil || !queued {
			t.Fatalf("round %d did not queue: %t %v", round, queued, err)
		}
		item, ready, err := h.NextChannelContact(ctx, at.Add(6*time.Minute), time.UTC, "zh-CN")
		if err != nil || !ready {
			t.Fatalf("round %d not due: %+v %t %v", round, item, ready, err)
		}
		if ok, err := h.FinalizeChannelContact(ctx, item.IntentID, "我又想到那幅画。", at.Add(6*time.Minute), time.UTC); err != nil || !ok {
			t.Fatalf("round %d could not finalize: %t %v", round, ok, err)
		}
		if err := h.MarkChannelContactSent(ctx, item.IntentID, at.Add(7*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if check, err := h.ClaimInitiative(ctx, now.Add(160*time.Minute), time.UTC); err != nil || check.Ready || check.Reason != "waiting for user to return" {
		t.Fatalf("unanswered cap failed: %+v %v", check, err)
	}
	if err := h.AddUserMessage(ctx, "回来啦", now.Add(180*time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.Add(ctx, "assistant", "嗯，画还在。", now.Add(180*time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	check, err := h.ClaimInitiative(ctx, now.Add(215*time.Minute), time.UTC)
	if err != nil || !check.Ready || check.Unanswered != 0 {
		t.Fatalf("user reply did not reset outreach: %+v %v", check, err)
	}
}

func TestInitiativeTuningCannotBypassQuietHoursOrDailyLimit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	seedInitiative(t, h, now)
	if err := h.SetInitiative(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if c, err := h.ClaimInitiative(ctx, now, time.UTC); err != nil || c.Ready || c.Reason != "initiative disabled" {
		t.Fatalf("disabled: %+v %v", c, err)
	}
	if err := h.SetInitiative(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err := h.SetContactPolicy(ctx, contact.Policy{QuietStart: "11:00", QuietEnd: "13:00", DailyLimit: 1}); err != nil {
		t.Fatal(err)
	}
	if c, err := h.ClaimInitiative(ctx, now, time.UTC); err != nil || c.Ready || c.Reason != "quiet hours" {
		t.Fatalf("quiet hours: %+v %v", c, err)
	}
	if err := h.SetContactPolicy(ctx, contact.Policy{QuietStart: "23:00", QuietEnd: "07:00", DailyLimit: 1}); err != nil {
		t.Fatal(err)
	}
	c, err := h.ClaimInitiative(ctx, now, time.UTC)
	if err != nil || !c.Ready {
		t.Fatal(c, err)
	}
	if queued, err := h.FinishInitiative(ctx, c, proposedContact(), now, time.UTC); err != nil || !queued {
		t.Fatal(queued, err)
	}
	item, ready, err := h.NextChannelContact(ctx, now.Add(6*time.Minute), time.UTC, "zh-CN")
	if err != nil || !ready {
		t.Fatal(item, ready, err)
	}
	if ok, err := h.FinalizeChannelContact(ctx, item.IntentID, "我想起那张画。", now.Add(6*time.Minute), time.UTC); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := h.MarkChannelContactSent(ctx, item.IntentID, now.Add(7*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if c, err := h.ClaimInitiative(ctx, now.Add(40*time.Minute), time.UTC); err != nil || c.Ready || c.Reason != "daily contact limit" {
		t.Fatalf("daily limit: %+v %v", c, err)
	}
}

func TestTuningCommandDoesNotBecomeAConversationOrResetUnansweredContact(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	seedInitiative(t, h, now)
	if err := h.SetContactPolicy(ctx, contact.Policy{QuietStart: "23:00", QuietEnd: "07:00", DailyLimit: 5}); err != nil {
		t.Fatal(err)
	}
	c, err := h.ClaimInitiative(ctx, now, time.UTC)
	if err != nil || !c.Ready {
		t.Fatal(c, err)
	}
	if queued, err := h.FinishInitiative(ctx, c, proposedContact(), now, time.UTC); err != nil || !queued {
		t.Fatal(queued, err)
	}
	item, ready, err := h.NextChannelContact(ctx, now.Add(6*time.Minute), time.UTC, "zh-CN")
	if err != nil || !ready {
		t.Fatal(item, ready, err)
	}
	if ok, err := h.FinalizeChannelContact(ctx, item.IntentID, "我想起那张画。", now.Add(6*time.Minute), time.UTC); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := h.MarkChannelContactSent(ctx, item.IntentID, now.Add(7*time.Minute)); err != nil {
		t.Fatal(err)
	}
	controlTestMessage(t, h, "/主动性 100", now.Add(8*time.Minute))
	check, err := h.ClaimInitiative(ctx, now.Add(40*time.Minute), time.UTC)
	if err != nil || !check.Ready || check.Unanswered != 1 || check.SourceID != c.SourceID {
		t.Fatalf("command hid an unanswered contact: %+v %v", check, err)
	}
	if queued, err := h.FinishInitiative(ctx, check, proposedContact(), now.Add(40*time.Minute), time.UTC); err != nil || !queued {
		t.Fatalf("follow-up after command: %t %v", queued, err)
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
	now := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
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
		at := now.Add(time.Duration(i) * (2*time.Hour + time.Minute))
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
		if i == 0 {
			if c, err := h.ClaimInitiative(ctx, at.Add(time.Hour), time.UTC); err != nil || c.Ready || c.Reason != "consideration cooldown" {
				t.Fatalf("unchanged conversation reconsidered within an hour: %+v %v", c, err)
			}
		}
	}
	h.Close()
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if c, err := h.ClaimInitiative(ctx, now.Add(7*time.Hour), time.UTC); err != nil || c.Ready || c.Reason != "daily consideration budget" {
		t.Fatalf("restart reset budget: %+v %v", c, err)
	}
	for i := 0; i < 2; i++ {
		at := now.Add(time.Duration(8+i*2)*time.Hour + 5*time.Minute)
		if err := h.AddUserMessage(ctx, "我刚又找到一张旧照片", at.Add(-time.Hour), time.UTC); err != nil {
			t.Fatal(err)
		}
		if err := h.Add(ctx, "assistant", "照片里是那家书店？", at.Add(-time.Hour+time.Second)); err != nil {
			t.Fatal(err)
		}
		c, err := h.ClaimInitiative(ctx, at, time.UTC)
		if err != nil || !c.Ready {
			t.Fatalf("fresh conversation could not be reconsidered: %+v %v", c, err)
		}
		if _, err := h.FinishInitiative(ctx, c, character.Initiative{Action: "wait", Reason: "no new reason"}, at, time.UTC); err != nil {
			t.Fatal(err)
		}
		if c, err := h.ClaimInitiative(ctx, at.Add(time.Minute), time.UTC); err != nil || c.Ready || c.Reason != "daily consideration budget" {
			t.Fatalf("same new conversation exceeded budget: %+v %v", c, err)
		}
	}
	if c, err := h.ClaimInitiative(ctx, now.Add(12*time.Hour+10*time.Minute), time.UTC); err != nil || c.Ready || c.Reason != "daily consideration budget" {
		t.Fatalf("fresh-context allowance exceeded hard cap: %+v %v", c, err)
	}
}
