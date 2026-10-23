package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMemoryCorrectionForgetAndReopen(t *testing.T) {
	ctx := context.Background()
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 10, 26, 10, 0, 0, 0, zone)
	if err := h.AddUserMessage(ctx, "周三下午有面试", first, zone); err != nil {
		t.Fatal(err)
	}
	facts, err := h.ActiveFacts(ctx)
	if err != nil || len(facts) != 1 {
		t.Fatalf("first memory: %+v, %v", facts, err)
	}
	if facts[0].Topic != "interview" || facts[0].Provenance != "user_stated" || facts[0].Precision != "part_of_day" || facts[0].SourceText != "周三下午有面试" || !facts[0].SourceAt.Equal(first) {
		t.Fatalf("fact lost source or precision: %+v", facts[0])
	}
	oldID := facts[0].ID
	if err := h.Add(ctx, "assistant", "Wednesday at three, I will remember.", first); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "面试改到周五了", first.Add(26*time.Hour), zone); err != nil {
		t.Fatal(err)
	}
	facts, err = h.ActiveFacts(ctx)
	if err != nil || len(facts) != 1 {
		t.Fatalf("corrected memory: %+v, %v", facts, err)
	}
	if facts[0].HappensAt.In(zone).Weekday() != time.Friday || facts[0].SourceText != "面试改到周五了" {
		t.Fatalf("correction not current: %+v", facts[0])
	}
	var supersedes int64
	if err := h.db.QueryRowContext(ctx, "SELECT supersedes_id FROM memory_revisions WHERE id = ?", facts[0].ID).Scan(&supersedes); err != nil || supersedes != oldID {
		t.Fatalf("missing revision link: %d, %v", supersedes, err)
	}
	modelHistory, err := h.RecentForModel(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range modelHistory {
		if strings.Contains(entry.Content, "周三") || strings.Contains(entry.Content, "Wednesday") {
			t.Fatal("superseded source reached model prompt")
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	facts, err = h.ActiveFacts(ctx)
	if err != nil || len(facts) != 1 || facts[0].SourceText != "面试改到周五了" {
		t.Fatalf("memory did not survive reopen: %+v, %v", facts, err)
	}
	if err := h.AddUserMessage(ctx, "忘记面试", first.Add(48*time.Hour), zone); err != nil {
		t.Fatal(err)
	}
	facts, err = h.ActiveFacts(ctx)
	if err != nil || len(facts) != 0 {
		t.Fatalf("forgotten memory still active: %+v, %v", facts, err)
	}
	export, err := json.Marshal(facts)
	if err != nil || strings.Contains(string(export), "面试") {
		t.Fatalf("forgotten fact in export: %s, %v", export, err)
	}
	modelHistory, err = h.RecentForModel(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range modelHistory {
		if strings.Contains(entry.Content, "周三") || strings.Contains(entry.Content, "周五") {
			t.Fatalf("forgotten source reached model prompt: %+v", entry)
		}
	}
	fullHistory, err := h.Recent(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range fullHistory {
		if strings.Contains(entry.Content, "面试") || strings.Contains(entry.Content, "interview") || strings.Contains(entry.Content, "Wednesday") {
			t.Fatalf("forgotten fact remained in transcript: %+v", entry)
		}
	}
}

func TestModelInferenceCannotOverrideUserFact(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "Maybe a project demo next week", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	entries, err := h.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.RecordModelInference(ctx, entries[0].ID, "project_demo", at.Add(7*24*time.Hour), "day"); err != nil {
		t.Fatal(err)
	}
	facts, err := h.ActiveFacts(ctx)
	if err != nil || len(facts) != 1 || facts[0].Provenance != "model_inferred" {
		t.Fatalf("inference provenance missing: %+v, %v", facts, err)
	}
	if err := h.AddUserMessage(ctx, "周三下午有面试", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	entries, err = h.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.RecordModelInference(ctx, entries[len(entries)-1].ID, "interview", at.Add(48*time.Hour), "day"); err == nil {
		t.Fatal("model inference overwrote user-stated interview")
	}
}
