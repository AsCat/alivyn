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
