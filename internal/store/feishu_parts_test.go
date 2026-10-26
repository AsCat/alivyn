package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestFeishuPartialBubbleDeliveryResumesAtUnsentPart(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	if _, err := h.EnqueueFeishu(ctx, "e1", "chat", "message", "你想说什么", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFeishuReplyParts(ctx, "e1", "先听我说。\n\n其实我有点紧张。", []string{"先听我说。", "其实我有点紧张。"}, at); err != nil {
		t.Fatal(err)
	}
	var delivered []string
	failed := errors.New("temporary send failure")
	err = h.DeliverFeishuParts(ctx, "e1", func(i int, text string) error {
		if i == 1 {
			return failed
		}
		delivered = append(delivered, text)
		return nil
	})
	if !errors.Is(err, failed) || !reflect.DeepEqual(delivered, []string{"先听我说。"}) {
		t.Fatalf("unexpected partial send: %+v %v", delivered, err)
	}
	if err := h.DeliverFeishuParts(ctx, "e1", func(_ int, text string) error {
		delivered = append(delivered, text)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(delivered, []string{"先听我说。", "其实我有点紧张。"}) {
		t.Fatalf("sent part repeated on retry: %+v", delivered)
	}
	if err := h.DeliverFeishuParts(ctx, "e1", func(int, string) error { t.Fatal("completed reply sent again"); return nil }); err != nil {
		t.Fatal(err)
	}
}
