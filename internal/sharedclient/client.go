package sharedclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/sharedrelay"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func (c Client) endpoint(path string) (string, error) {
	if err := config.ValidateRelayURL(c.BaseURL); err != nil {
		return "", err
	}
	return strings.TrimRight(c.BaseURL, "/") + path, nil
}

func (c Client) do(ctx context.Context, method, path string, input, output any) error {
	endpoint, err := c.endpoint(path)
	if err != nil {
		return err
	}
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	response, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("relay returned HTTP %d", response.StatusCode)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output)
}

func (c Client) Pair(ctx context.Context) (sharedrelay.Pairing, error) {
	var result sharedrelay.Pairing
	err := c.do(ctx, http.MethodPost, "/v1/pair", struct{}{}, &result)
	return result, err
}

func (c Client) Status(ctx context.Context) (bool, error) {
	var result struct {
		Paired bool `json:"paired"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/status", nil, &result)
	return result.Paired, err
}

func (c Client) Poll(ctx context.Context) ([]sharedrelay.Message, error) {
	var result struct {
		Messages []sharedrelay.Message `json:"messages"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/messages", nil, &result)
	return result.Messages, err
}

func (c Client) Ack(ctx context.Context, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/v1/ack", struct {
		EventIDs []string `json:"event_ids"`
	}{eventIDs}, nil)
}

func (c Client) Send(ctx context.Context, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("empty message")
	}
	return c.do(ctx, http.MethodPost, "/v1/send", struct {
		Text string `json:"text"`
	}{text}, nil)
}
