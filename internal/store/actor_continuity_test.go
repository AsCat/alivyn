package store

import (
	"context"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
)

func TestActorContinuityPersistsOnlyAfterAcceptedReplyAndExpires(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "你这话什么意思？", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	rows, err := h.Recent(ctx, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("recent: %+v %v", rows, err)
	}
	id := rows[0].ID
	decision := actor.Decision{
		Act: "repair", Focus: "own misread", EvidenceIDs: []string{"chat:1"}, Brevity: "short", Posture: "repairing",
		Carryover: &actor.Continuity{Posture: "repairing", Concern: "I read too much into a greeting", SourceIDs: []string{"chat:1"}, Lifetime: "hours"},
	}
	if err := h.SaveActorDecision(ctx, id, decision, at); err != nil {
		t.Fatal(err)
	}
	if state, err := h.ActorContinuityAt(ctx, id+1, at.Add(time.Hour)); err != nil || state != nil {
		t.Fatalf("unaccepted decision changed continuity: %+v %v", state, err)
	}
	if err := h.ConfirmActorDecision(ctx, id, decision, actor.Review{Pass: true}, at); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	state, err := h.ActorContinuityAt(ctx, id+1, at.Add(time.Hour))
	if err != nil || state == nil || state.Concern != decision.Carryover.Concern {
		t.Fatalf("accepted continuity did not survive restart: %+v %v", state, err)
	}
	state, err = h.ActorContinuityAt(ctx, id+1, at.Add(7*time.Hour))
	if err != nil || state != nil {
		t.Fatalf("short-lived continuity did not expire: %+v %v", state, err)
	}
	if err := h.AddUserMessage(ctx, "没事了", at.Add(2*time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	rows, err = h.Recent(ctx, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("recent: %+v %v", rows, err)
	}
	clear := actor.Decision{Act: "acknowledge", Focus: "accept repair", Brevity: "short", Posture: "steady"}
	if err := h.SaveActorDecision(ctx, rows[0].ID, clear, at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.ConfirmActorDecision(ctx, rows[0].ID, clear, actor.Review{Pass: true}, at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	state, err = h.ActorContinuityAt(ctx, rows[0].ID+1, at.Add(3*time.Hour))
	if err != nil || state != nil {
		t.Fatalf("resolved continuity remained active: %+v %v", state, err)
	}
}
