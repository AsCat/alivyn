package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a small compatibility probe for chat-completion providers. It is
// not yet Alivyn's production provider interface.
type Client struct {
	BaseURL         string
	APIKey          string
	HTTPClient      *http.Client
	Thinking        string // Provider-specific opt-in; empty omits it for compatibility.
	Temperature     *float64
	ReasoningEffort string
	JSONOutput      bool
	MaxTokens       int
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model           string    `json:"model"`
	Messages        []Message `json:"messages"`
	Thinking        *thinking `json:"thinking,omitempty"`
	Temperature     *float64  `json:"temperature,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	ResponseFormat  *thinking `json:"response_format,omitempty"`
	MaxTokens       int       `json:"max_tokens,omitempty"`
}

type thinking struct {
	Type string `json:"type"`
}

type response struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
}

func (c Client) CompletePlain(ctx context.Context, model string, messages []Message) (string, error) {
	c.JSONOutput = false
	return c.Complete(ctx, model, messages)
}

func (c Client) CompleteJSON(ctx context.Context, model string, messages []Message) (string, error) {
	c.JSONOutput = true
	return c.Complete(ctx, model, messages)
}

func (c Client) Complete(ctx context.Context, model string, messages []Message) (string, error) {
	if model == "" || len(messages) == 0 {
		return "", errors.New("model and messages are required")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("base URL must be an absolute HTTP(S) URL")
	}
	if c.APIKey == "" {
		return "", errors.New("API key is required")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	input := request{Model: model, Messages: messages, Temperature: c.Temperature, ReasoningEffort: c.ReasoningEffort, MaxTokens: c.MaxTokens}
	if c.Thinking != "" {
		input.Thinking = &thinking{Type: c.Thinking}
	}
	if c.JSONOutput {
		input.ResponseFormat = &thinking{Type: "json_object"}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("provider returned HTTP %d", res.StatusCode)
	}
	var out response
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("invalid provider response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("provider returned no choices")
	}
	if out.Choices[0].FinishReason == "length" {
		return "", errors.New("provider output reached its token limit")
	}
	if strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("provider returned empty content")
	}
	return out.Choices[0].Message.Content, nil
}
