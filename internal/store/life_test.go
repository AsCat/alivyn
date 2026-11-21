package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/life"
)

func TestLifeReadingKindMigratesExistingHistory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "alivyn.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE life_readings (id INTEGER PRIMARY KEY AUTOINCREMENT, local_date TEXT NOT NULL, occurred_ns INTEGER NOT NULL, title TEXT NOT NULL, source_url TEXT NOT NULL, excerpt TEXT NOT NULL, thought TEXT NOT NULL, feeling TEXT NOT NULL, question TEXT NOT NULL, UNIQUE(local_date), UNIQUE(source_url));
	INSERT INTO life_readings(local_date,occurred_ns,title,source_url,excerpt,thought,feeling,question) VALUES('2026-11-20',1,'old','https://example.org/old','old excerpt','old thought','','')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	r, err := h.LifeReading(ctx, 1)
	if err != nil || r.Kind != life.FeedExcerpt {
		t.Fatalf("old record did not migrate: %+v %v", r, err)
	}
}

func TestLifeReadingQueuesSourcedContactAndResetFencesOldWork(t *testing.T) {
	ctx := context.Background()
	at, zone := contactTestClock(t)
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "我在整理旧录音", at.Add(-time.Hour), zone); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "这些声音还挺有意思", at.Add(-30*time.Minute), zone); err != nil {
		t.Fatal(err)
	}
	generation, ok, err := h.ClaimLifeReading(ctx, at, zone, false)
	if err != nil || !ok {
		t.Fatalf("life claim failed: %d %t %v", generation, ok, err)
	}
	r := life.Reading{Date: at.Format("2006-01-02"), At: at, Title: "Sound history", URL: "https://example.org/sound", Excerpt: "A public archive restores old recordings.", Thought: "我想再听听", Feeling: "好奇"}
	r, err = h.SaveLifeReading(ctx, generation, r)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == 0 {
		t.Fatal("reading not persisted")
	}
	intents, err := h.ContactIntents(ctx, 10)
	if err != nil || len(intents) != 1 || intents[0].Topic != "life_reading" {
		t.Fatalf("source-backed contact not queued: %+v %v", intents, err)
	}
	if err := h.AddUserMessage(ctx, "你今天看了什么？", at.Add(time.Minute), zone); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.NextChannelContact(ctx, at.Add(4*time.Minute), zone, "zh-CN"); err != nil || ready {
		t.Fatalf("reading contact interrupted a resumed conversation: %t %v", ready, err)
	}
	intents, err = h.ContactIntents(ctx, 10)
	if err != nil || intents[0].State != "canceled" {
		t.Fatalf("stale reading contact was not canceled: %+v %v", intents, err)
	}
	if _, ok, err := h.ClaimLifeReading(ctx, at.Add(time.Hour), zone, true); err != nil || ok {
		t.Fatalf("second daily reading claimed: %t %v", ok, err)
	}
	if err := h.AddUserMessage(ctx, "/reset", at.Add(2*time.Hour), zone); err != nil {
		t.Fatal(err)
	}
	readings, err := h.LifeReadingsBefore(ctx, at.Add(3*time.Hour), 3)
	if err != nil || len(readings) != 0 {
		t.Fatalf("reset retained life reading: %+v %v", readings, err)
	}
	if _, err := h.SaveLifeReading(ctx, generation, r); !errors.Is(err, ErrTurnSuperseded) {
		t.Fatalf("in-flight pre-reset activity was accepted: %v", err)
	}
}
