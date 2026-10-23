package store

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/contact"
)

func contactTestClock(t *testing.T) (time.Time, *time.Location) {
	t.Helper()
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, 10, 26, 10, 0, 0, 0, zone), zone
}

func TestDurableCorrectionAndAtMostOnceOutbox(t *testing.T) {
	ctx := context.Background()
	first, zone := contactTestClock(t)
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "周三下午有面试", first, zone); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "面试改到周五了", first.Add(26*time.Hour), zone); err != nil {
		t.Fatal(err)
	}
	intents, err := h.ContactIntents(ctx, 10)
	if err != nil || len(intents) != 2 || intents[0].State != "pending" || intents[1].State != "canceled" {
		t.Fatalf("correction did not replace pending intent: %+v, %v", intents, err)
	}
	wed := time.Date(2026, 10, 28, 16, 0, 0, 0, zone)
	if events, err := h.ProcessDue(ctx, wed, zone, "zh-CN"); err != nil || len(events) != 0 {
		t.Fatalf("old Wednesday intent ran: %+v, %v", events, err)
	}
	fri := time.Date(2026, 10, 30, 16, 0, 0, 0, zone)
	events, err := h.ProcessDue(ctx, fri, zone, "zh-CN")
	if err != nil || len(events) != 1 || events[0].Outcome != "sent" {
		t.Fatalf("Friday follow-up not sent: %+v, %v", events, err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if events, err := h.ProcessDue(ctx, fri.Add(time.Minute), zone, "zh-CN"); err != nil || len(events) != 0 {
		t.Fatalf("restart caused repeat: %+v, %v", events, err)
	}
	outbox, err := h.Outbox(ctx, 10)
	if err != nil || len(outbox) != 1 || outbox[0].Topic != "interview" || outbox[0].MessageID < 1 {
		t.Fatalf("outbox was not exactly once: %+v, %v", outbox, err)
	}
	history, err := h.Recent(ctx, 10)
	if err != nil || len(history) == 0 || history[len(history)-1].ID != outbox[0].MessageID || history[len(history)-1].Role != "assistant" {
		t.Fatalf("proactive message did not join continuous conversation: %+v, %v", history, err)
	}
	audit, err := h.ContactEvents(ctx, 10)
	if err != nil || len(audit) != 2 || audit[0].Reason != "event follow-up" || audit[1].Reason != "superseded intent" {
		t.Fatalf("missing decision audit: %+v, %v", audit, err)
	}
	if err := h.AddUserMessage(ctx, "忘记面试", fri.Add(24*time.Hour), zone); err != nil {
		t.Fatal(err)
	}
	outbox, err = h.Outbox(ctx, 10)
	if err != nil || len(outbox) != 1 || outbox[0].Body != "[forgotten memory]" || outbox[0].Topic != "forgotten" {
		t.Fatalf("forgotten topic remained in outbox: %+v, %v", outbox, err)
	}
	intents, err = h.ContactIntents(ctx, 10)
	if err != nil || len(intents) != 2 || intents[0].Topic != "forgotten" || intents[0].Reason != "forgotten memory" {
		t.Fatalf("forgotten topic remained in intent status: %+v, %v", intents, err)
	}
	var dated int
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM memory_revisions WHERE topic = 'interview' AND happens_at IS NOT NULL").Scan(&dated); err != nil || dated != 0 {
		t.Fatalf("forgotten date remained in revisions: %d, %v", dated, err)
	}
}

func TestPauseQuietAndExpiryDecisions(t *testing.T) {
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
	if err := h.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 10, 28, 16, 0, 0, 0, zone)
	events, err := h.ProcessDue(ctx, due, zone, "en")
	if err != nil || len(events) != 1 || events[0].Reason != "paused" {
		t.Fatalf("pause did not skip: %+v, %v", events, err)
	}
	if repeated, err := h.ProcessDue(ctx, due.Add(30*time.Second), zone, "en"); err != nil || len(repeated) != 0 {
		t.Fatalf("unchanged pause wrote duplicate audit event: %+v, %v", repeated, err)
	}
	if err := h.SetContactPolicy(ctx, contact.Policy{QuietStart: "15:00", QuietEnd: "17:00", DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	events, err = h.ProcessDue(ctx, due.Add(time.Minute), zone, "en")
	if err != nil || len(events) != 1 || events[0].Reason != "quiet hours" {
		t.Fatalf("quiet hours did not skip: %+v, %v", events, err)
	}
	events, err = h.ProcessDue(ctx, due.Add(5*time.Hour), zone, "en")
	if err != nil || len(events) != 1 || events[0].Reason != "expired intent is not backfilled" {
		t.Fatalf("expired intent was backfilled: %+v, %v", events, err)
	}
	if outbox, err := h.Outbox(ctx, 10); err != nil || len(outbox) != 0 {
		t.Fatalf("suppressed message reached outbox: %+v, %v", outbox, err)
	}
}

func TestDailyLimitAndTimeGapCooldown(t *testing.T) {
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
	if err := h.SetContactPolicy(ctx, contact.Policy{QuietStart: "22:00", QuietEnd: "08:00", DailyLimit: 1}); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 10, 28, 16, 0, 0, 0, zone)
	if err := h.Heartbeat(ctx, due.Add(-8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.ResumeRuntime(ctx, due); err != nil {
		t.Fatal(err)
	}
	events, err := h.ProcessDue(ctx, due, zone, "en")
	if err != nil || len(events) != 2 || events[0].Outcome != "sent" || events[1].Reason != "daily contact limit" {
		t.Fatalf("daily limit was not enforced: %+v, %v", events, err)
	}
	if err := h.ResumeRuntime(ctx, due.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if intents, err := h.ContactIntents(ctx, 10); err != nil || len(intents) != 2 {
		t.Fatalf("time gap was duplicated: %+v, %v", intents, err)
	}
}

type failingSink struct{}

func (failingSink) Deliver(context.Context, *sql.Tx, ContactIntent, string, time.Time) error {
	return errors.New("simulated transport outage")
}

func TestFailedAttemptIsAuditedAndNotRetried(t *testing.T) {
	ctx := context.Background()
	first, zone := contactTestClock(t)
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "周三下午有面试", first, zone); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 10, 28, 16, 0, 0, 0, zone)
	events, err := h.processDue(ctx, due, zone, "en", failingSink{})
	if err != nil || len(events) != 1 || events[0].Outcome != "failed" || events[0].Reason == "" {
		t.Fatalf("failure was not recorded: %+v, %v", events, err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if events, err := h.ProcessDue(ctx, due.Add(time.Minute), zone, "en"); err != nil || len(events) != 0 {
		t.Fatalf("failed attempt was retried: %+v, %v", events, err)
	}
	if outbox, err := h.Outbox(ctx, 10); err != nil || len(outbox) != 0 {
		t.Fatalf("failed delivery leaked into outbox: %+v, %v", outbox, err)
	}
}

func TestTimeGapYieldsToUserActivity(t *testing.T) {
	ctx := context.Background()
	_, zone := contactTestClock(t)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, zone)
	if err := h.ResumeRuntime(ctx, start); err != nil {
		t.Fatal(err)
	}
	if err := h.ResumeRuntime(ctx, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if intents, err := h.ContactIntents(ctx, 10); err != nil || len(intents) != 0 {
		t.Fatalf("short gap created intent: %+v, %v", intents, err)
	}
	wake := start.Add(9 * time.Hour)
	if err := h.ResumeRuntime(ctx, wake); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "你好，我回来了", wake.Add(time.Minute), zone); err != nil {
		t.Fatal(err)
	}
	if events, err := h.ProcessDue(ctx, wake.Add(2*time.Minute), zone, "zh-CN"); err != nil || len(events) != 0 {
		t.Fatalf("time gap contacted active user: %+v, %v", events, err)
	}
	intents, err := h.ContactIntents(ctx, 10)
	if err != nil || len(intents) != 1 || intents[0].State != "canceled" || intents[0].Reason != "user already active" {
		t.Fatalf("user activity did not cancel gap: %+v, %v", intents, err)
	}
}

func TestDeliveryRevalidatesLatestSource(t *testing.T) {
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
	facts, err := h.ActiveFacts(ctx)
	if err != nil || len(facts) != 1 {
		t.Fatalf("missing source fact: %+v, %v", facts, err)
	}
	// Simulate an external correction arriving without the eager intent update.
	// The send gate must still reject the stale source revision.
	_, err = h.db.ExecContext(ctx, `INSERT INTO memory_revisions
  (topic, action, provenance, source_message_id, source_at, supersedes_id)
  VALUES ('interview', 'cancel', 'user_stated', ?, ?, ?)`,
		facts[0].SourceMessageID, first.UTC().Format(time.RFC3339Nano), facts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 10, 28, 16, 0, 0, 0, zone)
	events, err := h.ProcessDue(ctx, due, zone, "en")
	if err != nil || len(events) != 1 || events[0].Reason != "superseded intent" {
		t.Fatalf("stale source was not revalidated: %+v, %v", events, err)
	}
	if outbox, err := h.Outbox(ctx, 10); err != nil || len(outbox) != 0 {
		t.Fatalf("stale source reached outbox: %+v, %v", outbox, err)
	}
}

func TestTwoRuntimeReadersStillWriteOneDelivery(t *testing.T) {
	ctx := context.Background()
	first, zone := contactTestClock(t)
	dir := t.TempDir()
	left, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer left.Close()
	right, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer right.Close()
	if err := left.AddUserMessage(ctx, "周三下午有面试", first, zone); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 10, 28, 16, 0, 0, 0, zone)
	var wg sync.WaitGroup
	errorsCh := make(chan error, 2)
	for _, h := range []*History{left, right} {
		wg.Add(1)
		go func(h *History) {
			defer wg.Done()
			_, err := h.ProcessDue(ctx, due, zone, "en")
			errorsCh <- err
		}(h)
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	outbox, err := left.Outbox(ctx, 10)
	if err != nil || len(outbox) != 1 {
		t.Fatalf("concurrent runtimes wrote duplicate messages: %+v, %v", outbox, err)
	}
}
