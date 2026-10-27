package store

import (
	"context"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

func TestSpeculativeFeishuDraftYieldsToNewInputBeforeSend(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	if _, err := h.EnqueueFeishu(ctx, "first", "chat", "m1", "我有话说", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	first, ready, err := h.NextFeishuReady(ctx, at)
	if err != nil || !ready || first.EventID != "first" {
		t.Fatalf("first input did not start immediately: %+v %t %v", first, ready, err)
	}
	if err := h.SaveCharacterTurn(ctx, first.UserMessageID, character.Turn{Mode: "say", Reply: "旧草稿"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnqueueFeishu(ctx, "second", "chat", "m2", "还有一句", at.Add(time.Second), time.UTC); err != nil {
		t.Fatal(err)
	}
	prepared, err := h.PrepareFeishuReplyPartsIfCurrent(ctx, "first", "旧草稿", []string{"旧草稿"}, at.Add(2*time.Second))
	if err != nil || prepared {
		t.Fatalf("stale draft was prepared: %t %v", prepared, err)
	}
	if _, found, err := h.CharacterTurn(ctx, first.UserMessageID); err != nil || found {
		t.Fatalf("stale character draft survived: %t %v", found, err)
	}
	second, ready, err := h.NextFeishuReady(ctx, at.Add(2*time.Second))
	if err != nil || !ready || second.EventID != "second" {
		t.Fatalf("new contribution did not take over: %+v %t %v", second, ready, err)
	}
	entries, err := h.ActorTranscriptThrough(ctx, 10, second.UserMessageID)
	if err != nil || len(entries) != 2 || entries[0].Content != "我有话说" || entries[1].Content != "还有一句" {
		t.Fatalf("combined user context was lost: %+v %v", entries, err)
	}
}

func TestSpeculativeWaitAlsoYieldsToNewInput(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	if _, err := h.EnqueueFeishu(ctx, "first", "chat", "m1", "嗯", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	first, ready, err := h.NextFeishuReady(ctx, at)
	if err != nil || !ready {
		t.Fatal(first, ready, err)
	}
	if err := h.SaveCharacterTurn(ctx, first.UserMessageID, character.Turn{Mode: "wait"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnqueueFeishu(ctx, "second", "chat", "m2", "其实还有事", at.Add(time.Second), time.UTC); err != nil {
		t.Fatal(err)
	}
	superseded, err := h.SupersedeFeishuDraftIfNewer(ctx, first.EventID)
	if err != nil || !superseded {
		t.Fatalf("stale wait was committed: %t %v", superseded, err)
	}
}
