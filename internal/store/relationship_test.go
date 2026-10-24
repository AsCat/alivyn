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
	if _, err := h.db.ExecContext(ctx, "DELETE FROM relationship_meta"); err != nil {
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

func TestRelationshipMigrationRemovesFalseFamiliarityFromRecognitionProbes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, line := range []string{"你好", "是我啊", "你不记得我了吗", "我今天在商场认识你的"} {
		if err := h.AddUserMessage(ctx, line, at, time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE relationship_state SET stage='acquaintance', meaningful_turns=4 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE relationship_meta SET version=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	h.Close()
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s, err := h.Relationship(ctx)
	if err != nil || s.Stage != relationship.Stranger || s.MeaningfulTurns != 0 {
		t.Fatalf("migration retained false familiarity: %+v %v", s, err)
	}
}

func TestRelationshipAtUsesTurnSnapshotAndCurrentDistance(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, line := range []string{"我正在修一段录音", "我想过一会儿再听听", "别跟我这么熟"} {
		if err := h.AddUserMessage(ctx, line, at, time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	first, err := h.RelationshipAt(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.RelationshipAt(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.Stage != relationship.Stranger || second.Stage != relationship.Acquaintance {
		t.Fatalf("queued messages shared the latest relationship stage: first=%+v second=%+v", first, second)
	}
	if !first.Reserved || !second.Reserved {
		t.Fatal("a later request for distance did not constrain pending replies")
	}
}
