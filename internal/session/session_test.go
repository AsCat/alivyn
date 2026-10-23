package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/store"
)

type capturingClient struct{ messages []model.Message }

func (c *capturingClient) Complete(_ context.Context, _ string, messages []model.Message) (string, error) {
	c.messages = append([]model.Message(nil), messages...)
	return "好的", nil
}

func TestReplyUsesCurrentMemoryAndRespectsForget(t *testing.T) {
	ctx := context.Background()
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 10, 0, 0, 0, zone)
	for _, text := range []string{"周三下午有面试", "面试改到周五了"} {
		if err := h.AddUserMessage(ctx, text, at, zone); err != nil {
			t.Fatal(err)
		}
	}
	client := &capturingClient{}
	s := Session{Config: config.Config{Name: "Ari", Language: "zh-CN", Model: "mock"}, History: h, Client: client}
	if _, err := s.GenerateReply(ctx); err != nil {
		t.Fatal(err)
	}
	joined := flatten(client.messages)
	if !strings.Contains(joined, "2026-10-30") || strings.Contains(joined, "周三下午有面试") || !strings.Contains(joined, "source message") {
		t.Fatalf("model prompt did not use corrected source-linked memory: %s", joined)
	}
	if err := h.AddUserMessage(ctx, "面试取消了", at, zone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateReply(ctx); err != nil {
		t.Fatal(err)
	}
	joined = flatten(client.messages)
	if !strings.Contains(joined, "面试取消了") || strings.Contains(joined, "2026-10-30") {
		t.Fatalf("cancellation was not reflected in reply context: %s", joined)
	}
	if err := h.AddUserMessage(ctx, "忘记面试", at, zone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateReply(ctx); err != nil {
		t.Fatal(err)
	}
	joined = flatten(client.messages)
	if strings.Contains(joined, "2026-10-30") || strings.Contains(joined, "周五") || strings.Contains(joined, "周三") {
		t.Fatalf("forgotten date reached model: %s", joined)
	}
	if err := h.AddUserMessage(ctx, "我想再聊聊面试", at, zone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateReply(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flatten(client.messages), "我想再聊聊面试") {
		t.Fatal("new conversation about a forgotten topic was hidden")
	}
}

func flatten(messages []model.Message) string {
	var contents []string
	for _, message := range messages {
		contents = append(contents, message.Content)
	}
	return strings.Join(contents, "\n")
}
