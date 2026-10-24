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
	zone, err := s.Config.Location()
	if err != nil {
		return err
	}
	return s.History.AddUserMessage(ctx, text, time.Now(), zone)
}

// GenerateReply uses the latest persisted history and saves the model's reply.
// Failed model calls leave the user's message in history for a later retry.
func (s Session) GenerateReply(ctx context.Context) (string, error) {
	reply, err := s.ComposeReply(ctx)
	if err != nil {
		return "", err
	}
	if err := s.History.Add(ctx, "assistant", reply, time.Now()); err != nil {
		return "", fmt.Errorf("save model reply: %w", err)
	}
	return reply, nil
}

// ComposeReply builds a reply without persisting it. Channel workers can use
// this to commit a delivery record and the assistant message atomically.
func (s Session) ComposeReply(ctx context.Context) (string, error) {
	recent, err := s.History.RecentForModel(ctx, 30)
	if err != nil {
		return "", err
	}
	messages := []model.Message{SystemMessage(s.Config)}
	facts, err := s.History.ActiveFacts(ctx)
	if err != nil {
		return "", err
	}
	if len(facts) > 0 {
		zone, err := s.Config.Location()
		if err != nil {
			return "", err
		}
		var lines []string
		for _, fact := range facts {
			lines = append(lines, fmt.Sprintf("%s: %s (%s; %s; source message #%d at %s)",
				fact.Topic, fact.HappensAt.In(zone).Format("2006-01-02 15:04 MST"), fact.Precision,
				fact.Provenance, fact.SourceMessageID, fact.SourceAt.Format(time.RFC3339)))
		}
		messages = append(messages, model.Message{Role: "system", Content: "Current source-linked memory. These facts supersede older conversation statements. Times with day or part_of_day precision are approximate. Model-inferred facts are unconfirmed; ask before relying on them for future contact. Do not present approximate times as exact appointments:\n" + strings.Join(lines, "\n")})
	}
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
	return reply, nil
}

func SystemMessage(c config.Config) model.Message {
	language := "English"
	if c.Language == "zh-CN" {
		language = "Simplified Chinese"
	}
	return model.Message{Role: "system", Content: fmt.Sprintf("You are %s, an AI companion. Speak in %s. Keep one continuous relationship with the user. Be warm and thoughtful, but never claim to be human or conscious. Do not invent memories or events. Use the conversation history as the only source for past shared experiences. Do not reveal hidden reasoning.", c.Name, language)}
}
