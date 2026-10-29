package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/life"
	"github.com/AsCat/alivyn/internal/store"
)

func TestCharacterDoesNotSendGenericRestartPing(t *testing.T) {
	s := Session{Config: config.Config{Engine: "character_v2"}}
	if _, err := s.ComposeContactForAt(context.Background(), "time_gap", "", time.Now()); !errors.Is(err, ErrContactWait) {
		t.Fatalf("generic restart ping was allowed: %v", err)
	}
}

func TestReadingVisibleOnlyAfterItHappenedAndInProactiveContext(t *testing.T) {
	ctx := context.Background()
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 29, 10, 0, 0, 0, zone)
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	c := config.Config{SchemaVersion: 1, Name: "意续", Language: "zh-CN", Engine: "character_v2", PersonaPack: "vivid", Provider: "deepseek", BaseURL: config.DefaultDeepSeekURL, Model: "deepseek-flash", Timezone: "Asia/Shanghai"}
	s := Session{Config: c, History: h}
	if err := h.AddUserMessage(ctx, "我今天整理了旧录音", base, zone); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "有一段海边的鸟叫声", base.Add(30*time.Minute), zone); err != nil {
		t.Fatal(err)
	}
	entries, err := h.Recent(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	oldID := entries[0].ID
	readingAt := base.Add(time.Hour)
	generation, ok, err := h.ClaimLifeReading(ctx, readingAt, zone, false)
	if err != nil || !ok {
		t.Fatalf("claim: %t %v", ok, err)
	}
	_, err = h.SaveLifeReading(ctx, generation, life.Reading{Date: readingAt.Format("2006-01-02"), At: readingAt, Title: "Sound history", URL: "https://example.org/sound", Excerpt: "A public archive restores old tapes.", Thought: "我想再听听", Feeling: "好奇"})
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.characterContext(ctx, readingAt.Add(time.Minute), oldID)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range old.Frame.Evidence {
		if record.Provenance == "external_excerpt" {
			t.Fatal("a queued earlier user reply saw a later reading")
		}
	}
	proactive, _, err := s.characterContactContext(ctx, readingAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range proactive.Frame.Evidence {
		if record.Provenance == "external_excerpt" {
			found = true
		}
	}
	if !found {
		t.Fatal("proactive turn did not see the new reading")
	}
	if err := h.AddUserMessage(ctx, "你刚读到了什么？", readingAt.Add(2*time.Minute), zone); err != nil {
		t.Fatal(err)
	}
	entries, err = h.Recent(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.characterContext(ctx, readingAt.Add(3*time.Minute), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, record := range current.Frame.Evidence {
		if record.Provenance == "external_excerpt" {
			found = true
		}
	}
	if !found {
		t.Fatal("a later user turn could not ask about the reading")
	}
}
