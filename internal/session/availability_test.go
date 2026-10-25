package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/store"
)

func TestSleepingInboxSurvivesRestartAndReconsidersMorningBacklog(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := store.OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	zone := time.FixedZone("test", 8*3600)
	at := time.Date(2026, 10, 26, 1, 0, 0, 0, zone)
	client := &characterScript{replies: []string{`[[away:{"kind":"sleep","until":"2026-10-26T08:00:00+08:00","reason":"已经到了平时休息的时间"}]]`, "早，昨晚说的那家店在哪？"}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", Model: "mock", Timezone: "Asia/Shanghai"}, History: h, Client: client}
	if _, err := h.EnqueueFeishu(ctx, "night", "chat", "m1", "还醒着吗", at, zone); err != nil {
		t.Fatal(err)
	}
	item, ok, err := h.NextFeishuBatch(ctx, at.Add(5*time.Second))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	reply, err := s.composeCharacterReply(ctx, at.Add(5*time.Second), item.UserMessageID)
	if err != nil || reply != "" {
		t.Fatal(reply, err)
	}
	if deferred, err := h.DeferFeishuReply(ctx, item.EventID); err != nil || !deferred {
		t.Fatal(deferred, err)
	}
	if _, ok, err := h.NextFeishuBatch(ctx, at.Add(time.Hour)); err != nil || ok {
		t.Fatal("did not remain asleep", ok, err)
	}
	if _, err := h.EnqueueFeishu(ctx, "later", "chat", "m2", "找到一家卖老唱片的店", at.Add(time.Hour), zone); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnqueueFeishu(ctx, "status", "chat", "m3", "/状态", at.Add(2*time.Hour), zone); err != nil {
		t.Fatal(err)
	}
	status, ok, err := h.NextFeishuBatch(ctx, at.Add(2*time.Hour))
	if err != nil || !ok || status.EventID != "status" {
		t.Fatal(status, ok, err)
	}
	r, _, err := h.CommandReply(ctx, status.UserMessageID, "zh-CN")
	if err != nil || !strings.Contains(r, "休息") {
		t.Fatal(r, err)
	}
	if err := h.SetFeishuReply(ctx, status.EventID, r, at); err != nil {
		t.Fatal(err)
	}
	if err := h.MarkFeishuSent(ctx, status.EventID); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 {
		t.Fatal("polled the model while asleep")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = store.OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s.History = h
	morning := at.Add(7 * time.Hour)
	if _, err := h.EnqueueFeishu(ctx, "morning", "chat", "m4", "早，那家店周末开门", morning, zone); err != nil {
		t.Fatal(err)
	}
	item, ok, err = h.NextFeishuBatch(ctx, morning)
	if err != nil || !ok || item.EventID != "morning" {
		t.Fatal("backlog not coalesced", item, ok, err)
	}
	reply, err = s.composeCharacterReply(ctx, morning, item.UserMessageID)
	if err != nil || reply == "" {
		t.Fatal(reply, err)
	}
	if err := h.SetFeishuReply(ctx, item.EventID, reply, morning); err != nil {
		t.Fatal(err)
	}
	if err := h.MarkFeishuSent(ctx, item.EventID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := h.NextFeishuBatch(ctx, morning); err != nil || ok {
		t.Fatal("old deferred turn replayed", ok, err)
	}
	joined := ""
	for _, m := range client.calls[1] {
		joined += m.Content + "\n"
	}
	for _, want := range []string{"08:00:00+08:00", "找到一家卖老唱片的店", "recently returned", "No new timed absence"} {
		if !strings.Contains(joined, want) {
			t.Fatal("missing morning context", want)
		}
	}
	if strings.Contains(joined, "【控制台】") {
		t.Fatal("operator receipt leaked into character context")
	}
}

func TestWakeReleasesDeferredReplyWithoutResettingMemory(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	client := &characterScript{replies: []string{`[[away:{"kind":"alone","until":"2026-10-26T02:00:00Z","reason":"先留一段自己的时间"}]]`, "现在可以聊会儿"}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", Model: "mock"}, History: h, Client: client}
	if _, err := h.EnqueueFeishu(ctx, "old", "c", "m1", "我叫小岚", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	item, _, _ := h.NextFeishuBatch(ctx, at.Add(5*time.Second))
	if _, err := s.composeCharacterReply(ctx, at, item.UserMessageID); err != nil {
		t.Fatal(err)
	}
	if ok, err := h.DeferFeishuReply(ctx, item.EventID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := h.AddUserMessage(ctx, "/唤醒", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	item, ok, err := h.NextFeishuBatch(ctx, at.Add(time.Minute))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := s.composeCharacterReply(ctx, at.Add(time.Minute), item.UserMessageID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(client.calls[1][0].Content, "小岚") {
		t.Fatal("wake erased personal facts")
	}
}
