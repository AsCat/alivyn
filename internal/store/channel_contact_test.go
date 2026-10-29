package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/contact"
)

func TestProactiveBurstRetriesOnlyUnsentPartsAndCountsOnce(t *testing.T) {
	ctx := context.Background()
	at, zone := contactTestClock(t)
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	seedChannelIntent(t, h, at, "burst-retry")
	item, ready, err := h.NextChannelContact(ctx, at, zone, "zh-CN")
	if err != nil || !ready {
		t.Fatal(item, ready, err)
	}
	parts := []string{"你去哪儿了？", "算了，先忙你的。"}
	err = h.DeliverContactParts(ctx, item.IntentID, at, zone, "你去哪儿了？\n\n算了，先忙你的。", parts, func(index, _ int, _ string) error {
		if index == 1 {
			return errors.New("temporary send failure")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected second send failure")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	item, ready, err = h.NextChannelContact(ctx, at.Add(time.Minute), zone, "zh-CN")
	if err != nil || !ready {
		t.Fatal(item, ready, err)
	}
	var retried []int
	if err := h.DeliverContactParts(ctx, item.IntentID, at.Add(time.Minute), zone, item.Body, parts, func(index, _ int, _ string) error {
		retried = append(retried, index)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(retried) != 1 || retried[0] != 1 {
		t.Fatalf("retried parts: %v", retried)
	}
	entries, err := h.Recent(ctx, 10)
	if err != nil || len(entries) != 2 || entries[0].Content != parts[0] || entries[1].Content != parts[1] {
		t.Fatalf("visible transcript: %+v %v", entries, err)
	}
	var deliveries int
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM contact_delivery WHERE state='sent'").Scan(&deliveries); err != nil || deliveries != 1 {
		t.Fatal(deliveries, err)
	}
}

func TestUserReplyStopsRemainingProactiveBurst(t *testing.T) {
	ctx := context.Background()
	at, zone := contactTestClock(t)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	seedChannelIntent(t, h, at, "burst-interrupted")
	item, ready, err := h.NextChannelContact(ctx, at, zone, "zh-CN")
	if err != nil || !ready {
		t.Fatal(item, ready, err)
	}
	parts := []string{"你去哪儿了？", "好吧，等你。"}
	err = h.DeliverContactParts(ctx, item.IntentID, at, zone, "你去哪儿了？\n\n好吧，等你。", parts, func(index, _ int, _ string) error {
		if index == 0 {
			return nil
		}
		return errors.New("second bubble not sent")
	})
	if err == nil {
		t.Fatal("expected second send failure")
	}
	if err := h.AddUserMessage(ctx, "我在呢", at.Add(time.Minute), zone); err != nil {
		t.Fatal(err)
	}
	if err := h.ReconcilePartialContacts(ctx, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.NextChannelContact(ctx, at.Add(2*time.Minute), zone, "zh-CN"); err != nil || ready {
		t.Fatal(ready, err)
	}
	entries, err := h.Recent(ctx, 10)
	if err != nil || len(entries) != 2 || entries[0].Content != parts[0] || entries[1].Content != "我在呢" {
		t.Fatalf("interrupted transcript: %+v %v", entries, err)
	}
	var body, state string
	if err := h.db.QueryRowContext(ctx, "SELECT body,state FROM contact_delivery WHERE intent_id=?", item.IntentID).Scan(&body, &state); err != nil || body != parts[0] || state != "sent" {
		t.Fatal(body, state, err)
	}
}

func seedChannelIntent(t *testing.T, h *History, at time.Time, key string) {
	t.Helper()
	if err := h.SetContactPolicy(context.Background(), contact.Policy{QuietStart: "22:00", QuietEnd: "08:00", DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	_, err := h.db.ExecContext(context.Background(), `INSERT INTO contact_intents(topic, due_ns, expires_ns, idempotency_key, created_ns, updated_ns) VALUES ('time_gap', ?, ?, ?, ?, ?)`, at.UnixNano(), at.Add(6*time.Hour).UnixNano(), key, at.UnixNano(), at.UnixNano())
	if err != nil {
		t.Fatal(err)
	}
}

func TestFailedContactCompositionMustComposeAgain(t *testing.T) {
	ctx := context.Background()
	at, zone := contactTestClock(t)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	seedChannelIntent(t, h, at, "compose-failure")
	first, ready, err := h.NextChannelContact(ctx, at, zone, "en")
	if err != nil || !ready {
		t.Fatalf("not prepared: %t %v", ready, err)
	}
	if err := h.RetryChannelComposition(ctx, first.IntentID); err != nil {
		t.Fatal(err)
	}
	retry, ready, err := h.NextChannelContact(ctx, at.Add(2*time.Minute), zone, "en")
	if err != nil || !ready || !retry.FirstTry || retry.Key != first.Key {
		t.Fatalf("placeholder could be sent: %+v %t %v", retry, ready, err)
	}
}

func TestChannelContactPolicyRetryAndRestart(t *testing.T) {
	ctx := context.Background()
	at, zone := contactTestClock(t)
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	seedChannelIntent(t, h, at, "channel-test")
	if err := h.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.NextChannelContact(ctx, at, zone, "zh-CN"); err != nil || ready {
		t.Fatalf("paused contact attempted: %t %v", ready, err)
	}
	if err := h.SetContactPolicy(ctx, contact.Policy{QuietStart: "10:00", QuietEnd: "11:00", DailyLimit: 1}); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.NextChannelContact(ctx, at.Add(time.Minute), zone, "zh-CN"); err != nil || ready {
		t.Fatalf("quiet contact attempted: %t %v", ready, err)
	}
	if err := h.SetContactPolicy(ctx, contact.Policy{QuietStart: "22:00", QuietEnd: "08:00", DailyLimit: 1}); err != nil {
		t.Fatal(err)
	}
	first, ready, err := h.NextChannelContact(ctx, at.Add(2*time.Minute), zone, "zh-CN")
	if err != nil || !ready || first.Key != "channel-test" || first.Body == "" {
		t.Fatalf("contact not prepared: %+v %t %v", first, ready, err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, ready, err := h.NextChannelContact(ctx, at.Add(2*time.Minute+time.Second), zone, "zh-CN"); err != nil || ready {
		t.Fatalf("immediate restart retried: %t %v", ready, err)
	}
	retry, ready, err := h.NextChannelContact(ctx, at.Add(3*time.Minute), zone, "zh-CN")
	if err != nil || !ready || !first.FirstTry || retry.FirstTry || retry.IntentID != first.IntentID || retry.Key != first.Key || retry.Topic != first.Topic || retry.Body != first.Body {
		t.Fatalf("retry changed message or key: %+v %+v %t %v", first, retry, ready, err)
	}
	if err := h.MarkChannelContactSent(ctx, first.IntentID, at.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.NextChannelContact(ctx, at.Add(4*time.Minute), zone, "zh-CN"); err != nil || ready {
		t.Fatalf("sent contact repeated: %t %v", ready, err)
	}
	events, err := h.ContactEvents(ctx, 10)
	if err != nil || len(events) != 3 || events[0].Outcome != "sent" {
		t.Fatalf("audit: %+v %v", events, err)
	}
}

func TestChannelContactStopsAfterDeduplicationWindow(t *testing.T) {
	ctx := context.Background()
	at, zone := contactTestClock(t)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	seedChannelIntent(t, h, at, "uncertain")
	first, ready, err := h.NextChannelContact(ctx, at, zone, "en")
	if err != nil || !ready {
		t.Fatalf("first attempt: %+v %t %v", first, ready, err)
	}
	if _, ready, err := h.NextChannelContact(ctx, at.Add(56*time.Minute), zone, "en"); err != nil || ready {
		t.Fatalf("late duplicate attempt: %t %v", ready, err)
	}
	intents, err := h.ContactIntents(ctx, 10)
	if err != nil || len(intents) != 1 || intents[0].State != "failed" {
		t.Fatalf("uncertain intent: %+v %v", intents, err)
	}
}

func TestGeneratedContactIsPersistedAndPauseWinsBeforeSend(t *testing.T) {
	ctx := context.Background()
	at, zone := contactTestClock(t)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	seedChannelIntent(t, h, at, "generated")
	first, ready, err := h.NextChannelContact(ctx, at, zone, "zh-CN")
	if err != nil || !ready || !first.FirstTry {
		t.Fatalf("first attempt: %+v %t %v", first, ready, err)
	}
	if err := h.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	allowed, err := h.FinalizeChannelContact(ctx, first.IntentID, "有一阵没说话了。", at.Add(time.Second), zone)
	if err != nil || allowed {
		t.Fatalf("pause did not stop generated contact: %t %v", allowed, err)
	}
	if err := h.SetPaused(ctx, false); err != nil {
		t.Fatal(err)
	}
	allowed, err = h.FinalizeChannelContact(ctx, first.IntentID, "有一阵没说话了。", at.Add(2*time.Second), zone)
	if err != nil || !allowed {
		t.Fatalf("generated contact not finalized: %t %v", allowed, err)
	}
	retry, ready, err := h.NextChannelContact(ctx, at.Add(time.Minute), zone, "zh-CN")
	if err != nil || !ready || retry.FirstTry || retry.Body != "有一阵没说话了。" || retry.Key != first.Key {
		t.Fatalf("retry lost durable body: %+v %t %v", retry, ready, err)
	}
}

func TestChannelContactCorrectionCancelsPendingDelivery(t *testing.T) {
	ctx := context.Background()
	first, zone := contactTestClock(t)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "周三下午有面试", first, zone); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "面试改到周五了", first.Add(26*time.Hour), zone); err != nil {
		t.Fatal(err)
	}
	wednesday := time.Date(2026, 10, 28, 16, 0, 0, 0, zone)
	if _, ready, err := h.NextChannelContact(ctx, wednesday, zone, "zh-CN"); err != nil || ready {
		t.Fatalf("superseded date sent: %t %v", ready, err)
	}
	friday := time.Date(2026, 10, 30, 16, 0, 0, 0, zone)
	item, ready, err := h.NextChannelContact(ctx, friday, zone, "zh-CN")
	if err != nil || !ready || item.Body == "" {
		t.Fatalf("corrected date did not send: %+v %t %v", item, ready, err)
	}
	if err := h.AddUserMessage(ctx, "忘记面试", friday.Add(time.Minute), zone); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.NextChannelContact(ctx, friday.Add(2*time.Minute), zone, "zh-CN"); err != nil || ready {
		t.Fatalf("forgotten topic retried: %t %v", ready, err)
	}
	var retained string
	if err := h.db.QueryRowContext(ctx, "SELECT body FROM contact_delivery WHERE intent_id = ?", item.IntentID).Scan(&retained); err != nil || retained != "[forgotten memory]" {
		t.Fatalf("forgotten channel body remained: %q %v", retained, err)
	}
}
