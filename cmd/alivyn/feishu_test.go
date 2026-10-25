package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type actorQueueCompleter struct {
	frames []struct {
		Latest struct {
			Text string `json:"text"`
		} `json:"latest"`
		Evidence []struct {
			Text string `json:"text"`
		} `json:"evidence"`
	}
	calls int
}

func (f *actorQueueCompleter) Complete(_ context.Context, _ string, messages []model.Message) (string, error) {
	f.calls++
	switch (f.calls - 1) % 3 {
	case 0:
		var frame struct {
			Latest struct {
				Text string `json:"text"`
			} `json:"latest"`
			Evidence []struct {
				Text string `json:"text"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal([]byte(messages[1].Content), &frame); err != nil {
			return "", err
		}
		f.frames = append(f.frames, frame)
		return `{"act":"acknowledge","focus":"the user's current message","evidence_ids":[],"brevity":"short"}`, nil
	case 1:
		return "明白。", nil
	default:
		return `{"pass":true,"issue":""}`, nil
	}
}

func TestFeishuActorAnswersQueuedMessagesInOrder(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now().Add(-time.Minute)
	if _, err := h.EnqueueFeishu(ctx, "e1", "chat", "m1", "first private note", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnqueueFeishu(ctx, "e2", "chat", "m2", "second private note", at.Add(time.Second), time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &actorQueueCompleter{}
	s := session.Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "actor_v1", WorldPack: "none"}, History: h, Client: client}
	sender := &flakySender{}
	for range 2 {
		if err := processFeishuOnce(ctx, h, s, sender); err != nil {
			t.Fatal(err)
		}
	}
	if len(client.frames) != 2 || client.frames[0].Latest.Text != "first private note" || client.frames[1].Latest.Text != "second private note" {
		t.Fatalf("inbox messages were answered against the wrong turn: %+v", client.frames)
	}
	for _, evidence := range client.frames[0].Evidence {
		if evidence.Text == "second private note" {
			t.Fatal("future queued message leaked into first reply")
		}
	}
}

type fakeCompleter struct{ calls int }

func (f *fakeCompleter) Complete(_ context.Context, _ string, _ []model.Message) (string, error) {
	f.calls++
	return "hello", nil
}

type pauseCompleter struct{ calls int }

func (f *pauseCompleter) Complete(_ context.Context, _ string, _ []model.Message) (string, error) {
	f.calls++
	return `{"act":"pause","focus":"leave an ambiguous turn unanswered","evidence_ids":[],"brevity":"short"}`, nil
}

func TestFeishuActorCanLeaveAmbiguousTurnUnanswered(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.EnqueueFeishu(ctx, "e1", "chat", "m1", "。。。", time.Now(), time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &pauseCompleter{}
	s := session.Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "actor_v1", WorldPack: "none"}, History: h, Client: client}
	sender := &flakySender{}
	if err := processFeishuOnce(ctx, h, s, sender); err != nil {
		t.Fatal(err)
	}
	if err := processFeishuOnce(ctx, h, s, sender); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 0 || client.calls != 0 {
		t.Fatalf("silence caused a send or regenerated a decision: sends=%d model=%d", sender.calls, client.calls)
	}
	if _, ok, err := h.NextFeishu(ctx); err != nil || ok {
		t.Fatalf("silent event remained pending: found=%t err=%v", ok, err)
	}
	transcript, err := h.RecentForModel(ctx, 10)
	if err != nil || len(transcript) != 1 || transcript[0].Role != "user" {
		t.Fatalf("silence invented an assistant turn: %+v err=%v", transcript, err)
	}
}

type flakySender struct {
	calls int
	fail  bool
}

func (f *flakySender) Send(_ context.Context, _ *types.SendInput) (*types.SendResult, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("temporary send failure")
	}
	return &types.SendResult{}, nil
}

func TestFeishuReplyReusesGeneratedTextAfterSendFailure(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.EnqueueFeishu(ctx, "e1", "chat", "message", "How was our last conversation?", time.Now().Add(-time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	completer := &fakeCompleter{}
	s := session.Session{Config: config.Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "custom", BaseURL: "http://localhost:1234", Model: "mock"}, History: h, Client: completer}
	sender := &flakySender{fail: true}
	if err := processFeishuOnce(ctx, h, s, sender); err == nil {
		t.Fatal("send failure was hidden")
	}
	sender.fail = false
	if err := processFeishuOnce(ctx, h, s, sender); err != nil {
		t.Fatal(err)
	}
	if completer.calls != 1 || sender.calls != 2 {
		t.Fatalf("reply was regenerated: model=%d send=%d", completer.calls, sender.calls)
	}
}

func TestFeishuGateRequiresScanningOwnerPrivateText(t *testing.T) {
	good := &types.NormalizedMessage{EventID: "e", MessageID: "m", ChatID: "c", ChatType: "p2p", UserID: "owner", RawContentType: "text", Content: "hi"}
	if !acceptFeishuMessage("owner", good) {
		t.Fatal("owner private message rejected")
	}
	for _, change := range []func(*types.NormalizedMessage){
		func(m *types.NormalizedMessage) { m.UserID = "other" },
		func(m *types.NormalizedMessage) { m.ChatType = "group" },
		func(m *types.NormalizedMessage) { m.RawContentType = "image" },
	} {
		m := *good
		change(&m)
		if acceptFeishuMessage("owner", &m) {
			t.Fatalf("unsafe message accepted: %+v", m)
		}
	}
}

func TestFeishuWebSocketRegistersEventDispatcher(t *testing.T) {
	if newFeishuWSClient("app", "secret").EventHandler() == nil {
		t.Fatal("WebSocket connected without an event dispatcher; Feishu messages would time out")
	}
}
