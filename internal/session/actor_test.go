package session

import (
	"context"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/store"
)

type actorScript struct {
	replies []string
	calls   int
}

func (s *actorScript) Complete(_ context.Context, _ string, _ []model.Message) (string, error) {
	reply := s.replies[s.calls]
	s.calls++
	return reply, nil
}

func TestActorDecisionPersistsAcrossRetryAndForgetRemovesIt(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "我叫小明", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &actorScript{replies: []string{
		`{"act":"acknowledge","focus":"name shared","evidence_ids":["chat:1"],"brevity":"short"}`,
		"好，小明。", `{"pass":true,"issue":""}`,
		"好，小明。", `{"pass":true,"issue":""}`,
	}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "actor_v1", WorldPack: "none"}, History: h, Client: client}
	if _, err := s.ComposeReplyAt(ctx, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ComposeReplyAt(ctx, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if client.calls != 5 {
		t.Fatalf("retry replanned instead of reusing decision: %d calls", client.calls)
	}
	if _, found, err := h.ActorDecision(ctx, 1); err != nil || !found {
		t.Fatalf("decision was not saved: found=%v err=%v", found, err)
	}
	if err := h.AddUserMessage(ctx, "忘记我的名字", at.Add(2*time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, found, err := h.ActorDecision(ctx, 1); err != nil || found {
		t.Fatalf("forget left derived decision behind: found=%v err=%v", found, err)
	}
}

func TestActorRetriesEmptyDraftWithoutReplanning(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "今天有点累", time.Now(), time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &actorScript{replies: []string{
		`{"act":"acknowledge","focus":"tired today","evidence_ids":["chat:1"],"brevity":"short"}`,
		"", "嗯，今天累了。", `{"pass":true,"issue":""}`,
	}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "actor_v1", WorldPack: "none"}, History: h, Client: client}
	reply, err := s.ComposeReply(ctx)
	if err != nil || reply != "嗯，今天累了。" || client.calls != 4 {
		t.Fatalf("empty draft did not recover: reply=%q calls=%d err=%v", reply, client.calls, err)
	}
}
