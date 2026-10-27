package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/sharedclient"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/mdp/qrterminal/v3"
)

func pairSharedFeishu(ctx context.Context, relayURL string, out io.Writer) error {
	if _, err := config.Load(); err != nil {
		return err
	}
	if relayURL == "" {
		return errors.New("shared Feishu relay is not published yet; an operator can pass its HTTPS URL to 'alivyn channel feishu connect <relay-url>'")
	}
	if err := config.ValidateRelayURL(relayURL); err != nil {
		return err
	}
	var saved config.SharedFeishu
	if current, err := config.LoadSharedFeishu(); err == nil && current.RelayURL == relayURL {
		saved = current
	}
	client := sharedclient.Client{BaseURL: relayURL, Token: saved.Token}
	if saved.Token != "" {
		paired, err := client.Status(ctx)
		if err == nil && paired {
			fmt.Fprintln(out, "Shared Feishu bot is already paired. / 共享飞书机器人已配对。")
			return nil
		}
	}
	if saved.Token == "" || !time.Now().Before(saved.ExpiresAt) {
		pair, err := (sharedclient.Client{BaseURL: relayURL}).Pair(ctx)
		if err != nil {
			return err
		}
		saved = config.SharedFeishu{RelayURL: relayURL, Token: pair.Token, Code: pair.Code, ExpiresAt: pair.ExpiresAt, BotURL: pair.BotURL}
		if err := config.SaveSharedFeishu(saved); err != nil {
			return err
		}
		client.Token = pair.Token
	}
	fmt.Fprintln(out, "Scan the shared Feishu bot, then send this pairing code in its private chat. / 扫码进入共享机器人私聊，然后发送下面的配对码：")
	qrterminal.GenerateHalfBlock(saved.BotURL, qrterminal.M, out)
	fmt.Fprintf(out, "%s\nCode / 配对码：%s\nExpires / 截止：%s\n", saved.BotURL, saved.Code, saved.ExpiresAt.Local().Format(time.RFC3339))
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for time.Now().Before(saved.ExpiresAt) {
		paired, err := client.Status(ctx)
		if err == nil && paired {
			saved.Code = ""
			saved.ExpiresAt = time.Time{}
			if err := config.SaveSharedFeishu(saved); err != nil {
				return err
			}
			fmt.Fprintln(out, "Paired. / 已配对。")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return errors.New("pairing code expired; retry pairing. If you sent the code, the relay operator should check the Feishu receive-message event, persistent-connection mode, and app publication")
}

type sharedSender struct{ client sharedclient.Client }

func (s sharedSender) Send(ctx context.Context, input *types.SendInput) (*types.SendResult, error) {
	if err := s.client.Send(ctx, input.Text); err != nil {
		return nil, err
	}
	return &types.SendResult{}, nil
}

func runSharedFeishu(out io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	modelClient, err := configuredClient(c)
	if err != nil {
		return err
	}
	connection, err := config.LoadSharedFeishu()
	if err != nil {
		return err
	}
	client := sharedclient.Client{BaseURL: connection.RelayURL, Token: connection.Token}
	paired, err := client.Status(ctx)
	if err != nil {
		return err
	}
	if !paired {
		return errors.New("shared Feishu bot is not paired; run 'alivyn channel feishu connect' first")
	}
	dir, err := config.Directory()
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "runtime.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another Alivyn runtime is already running")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	zone, err := c.Location()
	if err != nil {
		return err
	}
	s := session.Session{Config: c, History: h, Client: modelClient}
	defer s.StartCharacterReflection(ctx)()
	wakeReplies := make(chan struct{}, 1)
	go workFeishu(ctx, h, s, sharedSender{client: client}, out, wakeReplies)
	go workChannelContact(ctx, h, s, zone, c.Language, func() bool { return true }, client.SendProactive, out)
	fmt.Fprintln(out, "Shared Feishu bot connected. / 共享飞书机器人已连接。")
	for ctx.Err() == nil {
		messages, err := client.Poll(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(out, "Relay temporarily unavailable: %v\n", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, message := range messages {
			added, err := h.EnqueueFeishu(ctx, message.EventID, message.ChatID, message.MessageID, message.Text, time.Now(), zone)
			if err != nil {
				return err
			}
			if added {
				notifyFeishuReply(wakeReplies)
			}
			if err := client.Ack(ctx, []string{message.EventID}); err != nil {
				return err
			}
		}
	}
	return nil
}
