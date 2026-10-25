package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/store"
)

type characterScript struct {
	replies []string
	calls   [][]model.Message
}

func TestResetCommandBypassesEveryModelAndRemovesOldContext(t *testing.T) {
	for _, engine := range []string{"legacy", "actor_v1", "character_v2"} {
		t.Run(engine, func(t *testing.T) {
			ctx := context.Background()
			h, err := store.OpenHistory(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			client := &characterScript{}
			s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: engine, WorldPack: "none", Background: "a private, thoughtful character"}, History: h, Client: client}
			if err := s.AddUser(ctx, "我叫林晓"); err != nil {
				t.Fatal(err)
			}
			if err := s.AddUser(ctx, "/清除记忆"); err != nil {
				t.Fatal(err)
			}
			reply, err := s.GenerateReply(ctx)
			if err != nil || !strings.Contains(reply, "记忆已清除") || len(client.calls) != 0 {
				t.Fatalf("command delegated to model: %s %v", reply, err)
			}
			if entries, err := h.RecentForModel(ctx, 100); err != nil || len(entries) != 0 {
				t.Fatal(entries, err)
			}
		})
	}
}

func (s *characterScript) Complete(_ context.Context, _ string, messages []model.Message) (string, error) {
	s.calls = append(s.calls, append([]model.Message(nil), messages...))
	if len(s.replies) == 0 {
		return "", errors.New("unexpected model call")
	}
	r := s.replies[0]
	s.replies = s.replies[1:]
	return r, nil
}

func TestCharacterCarriesStateAndRoleHistoryAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := store.OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 26, 21, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "……", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &characterScript{replies: []string{"怎么啦？", `{"moment":{"feeling":"疑惑","attention":"刚才那句话","scene":"听歌"},"used_world_ids":[]}`}}
	c := config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", WorldPack: "tidehaven", Model: "mock"}
	s := Session{Config: c, History: h, Client: client}
	reply, err := s.ComposeReplyAt(ctx, at)
	if err != nil || reply != "怎么啦？" || len(client.calls) != 1 {
		t.Fatalf("punctuation was intercepted or extra stages ran: %q %v", reply, err)
	}
	if m, _, err := h.CharacterMomentBefore(ctx, 2, at); err != nil || m.Feeling != "" {
		t.Fatal("draft state leaked")
	}
	if _, err := s.ComposeReplyAt(ctx, at); err != nil || len(client.calls) != 1 {
		t.Fatal("retry regenerated a saved turn")
	}
	if err := h.CommitCharacterReply(ctx, 1, reply, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReflectCharacter(ctx, at.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	h.Close()
	h, err = store.OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "你刚才在听什么", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	client.replies = []string{"一首旧歌。"}
	s.History = h
	if _, err := s.ComposeReplyAt(ctx, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	last := client.calls[2]
	if len(last) != 5 || last[2].Role != "assistant" || last[2].Content != "怎么啦？" || !strings.Contains(last[0].Content, `"scene":"听歌"`) {
		t.Fatalf("continuity missing after restart: %+v", last)
	}
}

func TestCharacterSourceBoundReplyExcludesFutureQueuedTurn(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	h.EnqueueFeishu(ctx, "one", "c", "m1", "first note", at, time.UTC)
	h.EnqueueFeishu(ctx, "two", "c", "m2", "future secret", at.Add(time.Second), time.UTC)
	client := &characterScript{replies: []string{"听到了"}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", WorldPack: "none", Model: "mock"}, History: h, Client: client}
	if _, err := s.ComposeReplyFor(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for _, m := range client.calls[0] {
		if strings.Contains(m.Content, "future secret") {
			t.Fatal("future queued message leaked into source-bound response")
		}
	}
}

func TestCharacterReflectionFailureCannotBlockOrChangeDialogue(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	if err := h.AddUserMessage(ctx, "你在听吗", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &characterScript{replies: []string{"听着呢，接着说。", "    "}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", WorldPack: "none", Model: "mock"}, History: h, Client: client}
	reply, err := s.ComposeReplyAt(ctx, at)
	if err != nil || reply != "听着呢，接着说。" || len(client.calls) != 1 {
		t.Fatalf("dialogue blocked: %q %v", reply, err)
	}
	if err := h.CommitCharacterReply(ctx, 1, reply, at); err != nil {
		t.Fatal(err)
	}
	if err := s.ReflectCharacter(ctx, at.Add(time.Minute)); err == nil {
		t.Fatal("invalid reflection accepted")
	}
	turn, found, err := h.CharacterTurn(ctx, 1)
	if err != nil || !found || turn.StateUpdated || turn.Reply != reply {
		t.Fatalf("reflection changed reply: %+v %v", turn, err)
	}
}

func TestCharacterReceivesForgetActionWithoutForgottenContent(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	if err := h.AddUserMessage(ctx, "我叫小明", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "忘记我的名字", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &characterScript{replies: []string{"好，已经忘了。"}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", WorldPack: "none", Model: "mock"}, History: h, Client: client}
	if _, err := s.ComposeReplyAt(ctx, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range client.calls[0] {
		if strings.Contains(m.Content, "小明") {
			t.Fatal("forgotten name leaked")
		}
		if strings.Contains(m.Content, "requested forgetting") {
			found = true
		}
	}
	if !found {
		t.Fatal("forget action disappeared")
	}
}
