package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestFeishuBatchSettlesAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Minute)
	for i := range 3 {
		if _, err := h.EnqueueFeishu(ctx, fmt.Sprint(i), "chat", fmt.Sprint(i), fmt.Sprintf("part %d", i), at.Add(time.Duration(i)*3*time.Second), time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := h.NextFeishuBatch(ctx, at.Add(8*time.Second)); err != nil || ok {
		t.Fatalf("answered mid-burst: %t %v", ok, err)
	}
	item, ok, err := h.NextFeishuBatch(ctx, at.Add(10*time.Second))
	if err != nil || !ok || item.EventID != "2" {
		t.Fatalf("batch: %+v %t %v", item, ok, err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.EnqueueFeishu(ctx, "later", "chat", "later", "one more", at.Add(11*time.Second), time.UTC); err != nil {
		t.Fatal(err)
	}
	item, ok, err = h.NextFeishuBatch(ctx, at.Add(20*time.Second))
	if err != nil || !ok || item.EventID != "2" {
		t.Fatalf("claimed batch changed: %+v %t %v", item, ok, err)
	}
	entries, err := h.ActorTranscriptThrough(ctx, 10, item.UserMessageID)
	if err != nil || len(entries) != 3 || entries[0].Content != "part 0" || entries[2].Content != "part 2" {
		t.Fatalf("lost or leaked batch content: %+v %v", entries, err)
	}
	if err := h.SetFeishuReply(ctx, item.EventID, "one reply", at.Add(21*time.Second)); err != nil {
		t.Fatal(err)
	}
	item, ok, err = h.NextFeishuBatch(ctx, at.Add(22*time.Second))
	if err != nil || !ok || item.Reply != "one reply" || item.State != "ready" {
		t.Fatalf("prepared reply lost: %+v %v", item, err)
	}
	if err := h.MarkFeishuSent(ctx, item.EventID); err != nil {
		t.Fatal(err)
	}
	item, ok, err = h.NextFeishuBatch(ctx, at.Add(23*time.Second))
	if err != nil || !ok || item.EventID != "later" {
		t.Fatalf("later turn lost: %+v %v", item, err)
	}
}

func TestFeishuBatchBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		chats   []string
		offsets []int
		now     int
		anchor  string
	}{
		{"separate chats", []string{"a", "b"}, []int{0, 1}, 10, "0"},
		{"separate turns", []string{"a", "a"}, []int{0, 9}, 15, "0"},
		{"bounded wait", []string{"a", "a", "a", "a", "a"}, []int{0, 3, 6, 9, 12}, 12, "4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h, err := OpenHistory(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			at := time.Now()
			for i, chat := range tc.chats {
				if _, err := h.EnqueueFeishu(ctx, fmt.Sprint(i), chat, fmt.Sprint(i), "a note", at.Add(time.Duration(tc.offsets[i])*time.Second), time.UTC); err != nil {
					t.Fatal(err)
				}
			}
			item, ok, err := h.NextFeishuBatch(ctx, at.Add(time.Duration(tc.now)*time.Second))
			if err != nil || !ok || item.EventID != tc.anchor {
				t.Fatalf("wrong grouping: %+v %t %v", item, ok, err)
			}
		})
	}
}

func TestLongerGuestPauseGroupsTwoThoughts(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now().Add(-time.Minute)
	for i, line := range []string{"你在干嘛", "有什么想对我说的吗"} {
		if _, err := h.EnqueueFeishu(ctx, fmt.Sprint(i), "chat", fmt.Sprint(i), line, at.Add(time.Duration(i)*5*time.Second), time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	if _, ready, err := h.NextFeishuBatchWithPause(ctx, at.Add(10*time.Second), 7*time.Second); err != nil || ready {
		t.Fatalf("guest answered before the conversational pause: ready=%t err=%v", ready, err)
	}
	item, ready, err := h.NextFeishuBatchWithPause(ctx, at.Add(12*time.Second), 7*time.Second)
	if err != nil || !ready || item.EventID != "1" {
		t.Fatalf("guest did not group both thoughts: %+v ready=%t err=%v", item, ready, err)
	}
	entries, err := h.ActorTranscriptThrough(ctx, 10, item.UserMessageID)
	if err != nil || len(entries) != 2 {
		t.Fatalf("both lines must reach the same reply: %+v %v", entries, err)
	}
}

func TestForgettingEarlierBatchMemberRedactsAndCancelsAnchor(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now().Add(-time.Minute)
	for i, text := range []string{"我喜欢咖啡", "你呢"} {
		if _, err := h.EnqueueFeishu(ctx, fmt.Sprint(i), "chat", fmt.Sprint(i), text, at.Add(time.Duration(i)*time.Second), time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	item, ok, err := h.NextFeishuBatch(ctx, time.Now())
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := h.SetFeishuReply(ctx, item.EventID, "我倒是喝茶更多", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "忘记我喜欢咖啡", time.Now(), time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := h.NextFeishuBatch(ctx, time.Now()); err != nil || ok {
		t.Fatalf("forgotten batch still deliverable: %t %v", ok, err)
	}
	var text string
	if err := h.db.QueryRowContext(ctx, "SELECT content FROM messages WHERE role='assistant'").Scan(&text); err != nil || text != "[forgotten memory]" {
		t.Fatalf("derived reply retained: %q %v", text, err)
	}
}
