package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type fakeCompleter struct{ calls int }

func (f *fakeCompleter) Complete(_ context.Context, _ string, _ []model.Message) (string, error) {
	f.calls++
	return "hello", nil
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
