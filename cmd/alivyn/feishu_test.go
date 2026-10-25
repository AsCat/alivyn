package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type burstCompleter struct {
	calls    int
	messages []model.Message
	reply    string
}

type heldCompleter struct {
	started chan struct{}
	release chan struct{}
	calls   int
}

func (f *heldCompleter) Complete(ctx context.Context, _ string, _ []model.Message) (string, error) {
	f.calls++
	close(f.started)
	select {
	case <-f.release:
		return "old context reply", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestResetDuringGenerationCannotSendOrResurrectOldReply(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now().Add(-time.Minute)
	if _, err := h.EnqueueFeishu(ctx, "old", "chat", "m1", "我叫林晓", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &heldCompleter{started: make(chan struct{}), release: make(chan struct{})}
	s := session.Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", Model: "mock", WorldPack: "none"}, History: h, Client: client}
	sender := &flakySender{}
	done := make(chan error, 1)
	go func() { done <- processFeishuOnce(ctx, h, s, sender) }()
	select {
	case <-client.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := h.EnqueueFeishu(ctx, "reset", "chat", "m2", "/清除记忆", at.Add(time.Second), time.UTC); err != nil {
		t.Fatal(err)
	}
	close(client.release)
	if err := <-done; !errors.Is(err, store.ErrTurnSuperseded) {
		t.Fatal("old generation was not discarded", err)
	}
	if sender.calls != 0 {
		t.Fatal("old reply sent")
	}
	if err := processFeishuOnce(ctx, h, s, sender); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 || client.calls != 1 {
		t.Fatal("reset acknowledgement made a model call or was not sent")
	}
	if entries, err := h.RecentForModel(ctx, 20); err != nil || len(entries) != 0 {
		t.Fatalf("cleared exchange restored: %+v %v", entries, err)
	}
}

func (f *burstCompleter) Complete(_ context.Context, _ string, messages []model.Message) (string, error) {
	f.calls++
	f.messages = messages
	return f.reply, nil
}

func TestCharacterBurstHasOneReplyOrDeliberateSilence(t *testing.T) {
	for _, reply := range []string{"有一点", "[[wait]]"} {
		t.Run(reply, func(t *testing.T) {
			ctx := context.Background()
			h, err := store.OpenHistory(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			at := time.Now().Add(-time.Minute)
			for i, text := range []string{"今天把画翻出来了", "也没那么差", "就是颜色淡了一点"} {
				id := fmt.Sprint(i)
				if _, err := h.EnqueueFeishu(ctx, id, "chat", id, text, at.Add(time.Duration(i)*3*time.Second), time.UTC); err != nil {
					t.Fatal(err)
				}
			}
			client := &burstCompleter{reply: reply}
			s := session.Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "character_v2", WorldPack: "none"}, History: h, Client: client}
			sender := &flakySender{}
			for range 3 {
				if err := processFeishuOnce(ctx, h, s, sender); err != nil {
					t.Fatal(err)
				}
			}
			wantSends := 1
			if reply == "[[wait]]" {
				wantSends = 0
			}
			if client.calls != 1 || sender.calls != wantSends {
				t.Fatalf("burst split or silence sent: calls=%d sends=%d", client.calls, sender.calls)
			}
			var users int
			for _, m := range client.messages {
				if m.Role == "user" {
					users++
				}
			}
			if users != 3 {
				t.Fatalf("missing input: %+v", client.messages)
			}
			entries, err := h.RecentForModel(ctx, 10)
			if err != nil || len(entries) != 3+wantSends {
				t.Fatalf("wrong transcript length: %d %v", len(entries), err)
			}
		})
	}
}

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

func TestFeishuCharacterDialogueSurvivesSendFailureWithoutRegeneration(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.EnqueueFeishu(ctx, "character-event", "chat", "message", "……", time.Now().Add(-time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &fakeCompleter{}
	s := session.Session{Config: config.Config{Name: "Ari", Language: "en", Model: "mock", Engine: "character_v2", WorldPack: "none"}, History: h, Client: client}
	sender := &flakySender{fail: true}
	if err := processFeishuOnce(ctx, h, s, sender); err == nil {
		t.Fatal("send failure hidden")
	}
	if _, _, ready, err := h.NextCharacterReflection(ctx, time.Now().Add(time.Minute)); err != nil || ready {
		t.Fatalf("unsent reply reflected: %t %v", ready, err)
	}
	sender.fail = false
	if err := processFeishuOnce(ctx, h, s, sender); err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || sender.calls != 2 {
		t.Fatalf("regenerated reply: model=%d sends=%d", client.calls, sender.calls)
	}
	if _, ok, err := h.NextFeishu(ctx); err != nil || ok {
		t.Fatal("inbox not drained")
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
