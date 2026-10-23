package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/store"
)

type Completer interface {
	Complete(context.Context, string, []model.Message) (string, error)
}

type Session struct {
	Config  config.Config
	History *store.History
	Client  Completer
}

// AddUser persists the user's words before an outbound model request begins.
func (s Session) AddUser(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("message cannot be empty")
	}
	return s.History.Add(ctx, "user", text, time.Now())
}

// GenerateReply uses the latest persisted history and saves the model's reply.
// Failed model calls leave the user's message in history for a later retry.
func (s Session) GenerateReply(ctx context.Context) (string, error) {
	recent, err := s.History.Recent(ctx, 30)
	if err != nil {
		return "", err
	}
	messages := []model.Message{SystemMessage(s.Config)}
	for _, entry := range recent {
		messages = append(messages, model.Message{Role: entry.Role, Content: entry.Content})
	}
	reply, err := s.Client.Complete(ctx, s.Config.Model, messages)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(reply) == "" {
		return "", errors.New("model returned an empty reply")
	}
	if err := s.History.Add(ctx, "assistant", reply, time.Now()); err != nil {
		return "", fmt.Errorf("save model reply: %w", err)
	}
	return reply, nil
}

func SystemMessage(c config.Config) model.Message {
	language := "English"
	if c.Language == "zh-CN" {
		language = "Simplified Chinese"
	}
	return model.Message{Role: "system", Content: fmt.Sprintf("You are %s, an AI companion. Speak in %s. Keep one continuous relationship with the user. Be warm and thoughtful, but never claim to be human or conscious. Do not invent memories or events. Use the conversation history as the only source for past shared experiences. Do not reveal hidden reasoning.", c.Name, language)}
}
