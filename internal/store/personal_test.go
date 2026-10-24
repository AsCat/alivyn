package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPersonalMemoryCorrectionForgetAndReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	for _, text := range []string{"我喜欢咖啡", "我不喜欢咖啡"} {
		if err := h.AddUserMessage(ctx, text, at, time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	facts, err := h.ActivePersonal(ctx)
	if err != nil || len(facts) != 1 || facts[0].Value != "我不喜欢咖啡" {
		t.Fatalf("correction lost: %+v, %v", facts, err)
	}
	recent, err := h.RecentForModel(ctx, 30)
	if err != nil || len(recent) != 1 || recent[0].Content != "我不喜欢咖啡" {
		t.Fatalf("stale preference reached model: %+v, %v", recent, err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "忘记我喜欢咖啡", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	facts, err = h.ActivePersonal(ctx)
	if err != nil || len(facts) != 0 {
		t.Fatalf("forgotten preference still active: %+v, %v", facts, err)
	}
	recent, err = h.RecentForModel(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range recent {
		if strings.Contains(entry.Content, "咖啡") {
			t.Fatalf("forgotten preference reached model: %+v", entry)
		}
	}
	full, err := h.Recent(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range full {
		if strings.Contains(entry.Content, "咖啡") {
			t.Fatalf("forgotten source remained in transcript: %+v", entry)
		}
	}
	var retained int
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM personal_memory WHERE memory_key LIKE '%咖啡%' OR value LIKE '%咖啡%'").Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("forgotten subject remained in personal memory: %d %v", retained, err)
	}
}
