package store

import (
	"context"
	"testing"
	"time"
)

func TestFeishuInboxDeduplicatesAndRecovers(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for n := range 2 {
		added, err := h.EnqueueFeishu(ctx, "event-1", "chat-1", "message-1", "interview on Friday", now, time.UTC)
		if err != nil || added != (n == 0) {
			t.Fatalf("enqueue %d: added=%t err=%v", n, added, err)
		}
	}
	entries, err := h.Recent(ctx, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("duplicate entered conversation: %+v, %v", entries, err)
	}
	item, ok, err := h.NextFeishu(ctx)
	if err != nil || !ok || item.State != "pending" {
		t.Fatalf("missing pending event: %+v, %t, %v", item, ok, err)
	}
	if err := h.SetFeishuReply(ctx, item.EventID, "good luck", now); err != nil {
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
	item, ok, err = h.NextFeishu(ctx)
	if err != nil || !ok || item.State != "ready" || item.Reply != "good luck" {
		t.Fatalf("generated reply was not recovered: %+v, %t, %v", item, ok, err)
	}
	if err := h.MarkFeishuSent(ctx, item.EventID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := h.NextFeishu(ctx); err != nil || ok {
		t.Fatalf("sent event returned to queue: %t, %v", ok, err)
	}
	entries, err = h.Recent(ctx, 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("conversation history was not durable: %+v, %v", entries, err)
	}
}

func TestQueuedReplyFollowsItsSourceAndForgetRedactsIt(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	if _, err := h.EnqueueFeishu(ctx, "e1", "c", "m1", "我叫小明", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnqueueFeishu(ctx, "e2", "c", "m2", "你怎么称呼我？", at.Add(time.Second), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFeishuReply(ctx, "e1", "你好，小明。", at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	transcript, err := h.ActorTranscriptThrough(ctx, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(transcript) != 3 || transcript[0].Content != "我叫小明" || transcript[1].Content != "你好，小明。" || transcript[2].Content != "你怎么称呼我？" {
		t.Fatalf("queued reply was not placed after its source: %+v", transcript)
	}
	if _, err := h.EnqueueFeishu(ctx, "e3", "c", "m3", "等下还有一件事", at.Add(3*time.Second), time.UTC); err != nil {
		t.Fatal(err)
	}
	transcript, err = h.ActorTranscriptThrough(ctx, 10, 4)
	if err != nil || len(transcript) != 4 || transcript[1].Content != "你好，小明。" || transcript[2].Content != "你怎么称呼我？" {
		t.Fatalf("later transcript lost causal message order: %+v err=%v", transcript, err)
	}
	if err := h.AddUserMessage(ctx, "忘记我的名字", at.Add(4*time.Second), time.UTC); err != nil {
		t.Fatal(err)
	}
	entries, err := h.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Content == "我叫小明" || entry.Content == "你好，小明。" {
			t.Fatalf("linked reply survived forgetting: %+v", entries)
		}
	}
	item, ok, err := h.NextFeishu(ctx)
	if err != nil || !ok || item.EventID != "e2" {
		t.Fatalf("forgotten queued reply still pending for delivery: %+v found=%v err=%v", item, ok, err)
	}
}
