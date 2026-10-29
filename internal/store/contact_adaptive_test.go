package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/contact"
)

func seedSentContact(t *testing.T, h *History, key string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	result, err := h.db.ExecContext(ctx, `INSERT INTO contact_intents(topic,due_ns,expires_ns,state,idempotency_key,created_ns,updated_ns)
VALUES('interview',?,?, 'sent',?,?,?)`, at.UnixNano(), at.Add(time.Hour).UnixNano(), key, at.UnixNano(), at.UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.db.ExecContext(ctx, `INSERT INTO contact_delivery(intent_id,body,first_attempt_ns,last_attempt_ns,attempts,state,sent_ns)
VALUES(?,'hello',?,?,1,'sent',?)`, id, at.UnixNano(), at.UnixNano(), at.UnixNano())
	if err != nil {
		t.Fatal(err)
	}
}

func seedPendingContact(t *testing.T, h *History, key string, due time.Time) {
	t.Helper()
	_, err := h.db.ExecContext(context.Background(), `INSERT INTO contact_intents(topic,due_ns,expires_ns,idempotency_key,created_ns,updated_ns)
VALUES('interview',?,?,?,?,?)`, due.UnixNano(), due.Add(time.Hour).UnixNano(), key, due.UnixNano(), due.UnixNano())
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdaptiveContactAllowsFreshConversationBeyondTwo(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	if err := h.Add(ctx, "user", "早上好", at); err != nil {
		t.Fatal(err)
	}
	seedSentContact(t, h, "sent:1", at.Add(time.Hour))
	seedSentContact(t, h, "sent:2", at.Add(2*time.Hour))
	if err := h.Add(ctx, "user", "刚忙完", at.Add(2*time.Hour+30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	seedPendingContact(t, h, "pending:3", at.Add(3*time.Hour))
	if err := h.SetContactPolicy(ctx, contact.Policy{QuietStart: "22:00", QuietEnd: "08:00", DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.NextChannelContact(ctx, at.Add(3*time.Hour), time.UTC, "en"); err != nil || ready {
		t.Fatalf("explicit fixed limit was not honored: ready=%v err=%v", ready, err)
	}
	if err := h.SetAdaptiveContactPolicy(ctx); err != nil {
		t.Fatal(err)
	}
	item, ready, err := h.NextChannelContact(ctx, at.Add(3*time.Hour), time.UTC, "en")
	if err != nil || !ready || item.Key != "pending:3" {
		t.Fatalf("adaptive cadence stayed stuck at two: %+v ready=%v err=%v", item, ready, err)
	}
}

func TestAdaptiveContactWaitsForReplyButOutgoingPersonaMayFollowUp(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	if err := h.Add(ctx, "user", "今天画画", at); err != nil {
		t.Fatal(err)
	}
	seedSentContact(t, h, "sent:one", at.Add(time.Hour))
	seedPendingContact(t, h, "pending:followup", at.Add(2*time.Hour))
	if _, ready, err := h.NextChannelContact(ctx, at.Add(2*time.Hour), time.UTC, "en"); err != nil || ready {
		t.Fatalf("default persona chased unanswered contact: ready=%v err=%v", ready, err)
	}
	if err := h.SetInitiative(ctx, 100); err != nil {
		t.Fatal(err)
	}
	item, ready, err := h.NextChannelContact(ctx, at.Add(2*time.Hour), time.UTC, "en")
	if err != nil || !ready || item.Key != "pending:followup" {
		t.Fatalf("outgoing persona could not consider follow-up: %+v ready=%v err=%v", item, ready, err)
	}
}

func TestLegacyTwoContactDefaultMigratesToAdaptive(t *testing.T) {
	for _, tc := range []struct {
		name      string
		old, want int
		adaptive  bool
	}{{"default two", 2, 12, true}, {"explicit five", 5, 5, false}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, "alivyn.db")
			file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			file.Close()
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `CREATE TABLE contact_settings(id INTEGER PRIMARY KEY,paused INTEGER NOT NULL DEFAULT 0,
quiet_start TEXT NOT NULL DEFAULT '22:00',quiet_end TEXT NOT NULL DEFAULT '08:00',daily_limit INTEGER NOT NULL DEFAULT 2);
			INSERT INTO contact_settings(id,daily_limit) VALUES(1,?)`, tc.old); err != nil {
				t.Fatal(err)
			}
			db.Close()
			h, err := OpenHistory(ctx, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			p, err := h.ContactPolicy(ctx)
			if err != nil || p.Adaptive != tc.adaptive || p.DailyLimit != tc.want {
				t.Fatalf("legacy limit was not migrated correctly: %+v %v", p, err)
			}
		})
	}
}
