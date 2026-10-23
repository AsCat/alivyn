package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
)

type completeFunc func(context.Context, string, []model.Message) (string, error)

func (f completeFunc) Complete(ctx context.Context, name string, messages []model.Message) (string, error) {
	return f(ctx, name, messages)
}

func TestConversationSurvivesReopeningTUI(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s := session.Session{
		Config:  config.Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "custom", BaseURL: "http://127.0.0.1:8080", Model: "mock"},
		History: h,
		Client: completeFunc(func(_ context.Context, _ string, messages []model.Message) (string, error) {
			if len(messages) != 2 || messages[1].Content != "Remember this moment" {
				t.Errorf("unexpected model context: %+v", messages)
			}
			return "I will remember our conversation.", nil
		}),
	}
	m := newConversation(ctx, s, nil)
	m.input.SetValue("Remember this moment")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(conversation)
	if cmd == nil || !m.pending {
		t.Fatal("submitting a message did not start an asynchronous reply")
	}
	before, err := h.Recent(ctx, 10)
	if err != nil || len(before) != 1 || before[0].Role != "user" {
		t.Fatalf("user message was not saved before model call: %+v, %v", before, err)
	}
	updated, _ = m.Update(cmd())
	m = updated.(conversation)
	if m.pending || m.hasError {
		t.Fatalf("reply did not complete: pending=%v status=%s", m.pending, m.status)
	}
	entries, err := h.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	reopened := newConversation(ctx, s, entries)
	view := reopened.View().Content
	if !strings.Contains(view, "Remember this moment") || !strings.Contains(view, "I will remember our conversation.") {
		t.Fatal("reopened TUI did not show the saved conversation")
	}
}

func TestModelFailureKeepsUserMessage(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s := session.Session{
		Config:  config.Config{Name: "Ari", Language: "en", Model: "mock"},
		History: h,
		Client: completeFunc(func(context.Context, string, []model.Message) (string, error) {
			return "", errors.New("provider unavailable")
		}),
	}
	m := newConversation(ctx, s, nil)
	m.input.SetValue("Please stay")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(conversation)
	updated, _ = m.Update(cmd())
	m = updated.(conversation)
	if !m.hasError || !strings.Contains(m.View().Content, "provider unavailable") {
		t.Fatal("model error was not visible in the TUI")
	}
	entries, err := h.Recent(ctx, 10)
	if err != nil || len(entries) != 1 || entries[0].Content != "Please stay" {
		t.Fatalf("failed model call lost user text: %+v, %v", entries, err)
	}
}
