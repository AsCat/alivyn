package store

import (
	"context"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/relationship"
)

func TestRelationshipSurvivesReopenAndBackfillsOldMessages(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "你好", day, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "我叫小明", day, time.UTC); err != nil {
		t.Fatal(err)
	}
	h.Close()
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := h.Relationship(ctx)
	if err != nil || s.Stage != relationship.Acquaintance || s.Disclosures != 1 {
		t.Fatalf("relationship lost across restart: %+v %v", s, err)
	}
	// Simulate an older database created before relationship_state existed.
	if _, err := h.db.ExecContext(ctx, "DELETE FROM relationship_state"); err != nil {
		t.Fatal(err)
	}
	h.Close()
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s, err = h.Relationship(ctx)
	if err != nil || s.Stage != relationship.Acquaintance || s.Disclosures != 1 {
		t.Fatalf("old conversation was not backfilled: %+v %v", s, err)
	}
}
