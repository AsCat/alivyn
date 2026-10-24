package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/sharedrelay"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	im "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

type relayFeishuSender struct {
	channel types.Channel
	client  *lark.Client
}

func (s relayFeishuSender) Send(ctx context.Context, chatID, text string) error {
	_, err := s.channel.Send(ctx, &types.SendInput{ChatID: chatID, Text: text})
	return err
}

func (s relayFeishuSender) SendIdempotent(ctx context.Context, chatID, message, key string) error {
	content, err := json.Marshal(struct {
		Text string `json:"text"`
	}{message})
	if err != nil {
		return err
	}
	req := im.NewCreateMessageReqBuilder().ReceiveIdType("chat_id").Body(
		im.NewCreateMessageReqBodyBuilder().ReceiveId(chatID).MsgType("text").Content(string(content)).Uuid(key).Build(),
	).Build()
	resp, err := s.client.Im.Message.Create(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("Feishu send failed: code %d", resp.Code)
	}
	return nil
}

func runRelay(args []string, out io.Writer) error {
	if len(args) != 1 || args[0] != "serve" {
		return errors.New("usage: alivyn relay serve")
	}
	appID := os.Getenv("ALIVYN_RELAY_FEISHU_APP_ID")
	appSecret := os.Getenv("ALIVYN_RELAY_FEISHU_APP_SECRET")
	botURL := os.Getenv("ALIVYN_RELAY_FEISHU_BOT_URL")
	if appID == "" && appSecret == "" {
		if local, err := config.LoadFeishu(); err == nil {
			appID, appSecret = local.AppID, local.AppSecret
		}
	}
	if appID != "" && botURL == "" {
		botURL = "https://applink.feishu.cn/client/bot/open?appId=" + url.QueryEscape(appID)
	}
	if appID == "" || appSecret == "" || botURL == "" {
		return errors.New("relay operator must connect a local Feishu app or set ALIVYN_RELAY_FEISHU_APP_ID and ALIVYN_RELAY_FEISHU_APP_SECRET")
	}
	listen := os.Getenv("ALIVYN_RELAY_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:8787"
	}
	dir := os.Getenv("ALIVYN_RELAY_DATA")
	if dir == "" {
		var err error
		dir, err = config.Directory()
		if err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	apiClient := lark.NewClient(appID, appSecret)
	ch := channel.NewChannel(apiClient, larkws.NewClient(appID, appSecret))
	relay, err := sharedrelay.Open(ctx, dir, botURL, relayFeishuSender{channel: ch, client: apiClient})
	if err != nil {
		return err
	}
	defer relay.Close()
	ch.OnReady(func() { fmt.Fprintln(out, "Shared Feishu bot WebSocket connected.") })
	ch.OnMessage(func(eventCtx context.Context, msg *types.NormalizedMessage) error {
		if msg == nil || msg.ChatType != "p2p" || msg.RawContentType != "text" || msg.UserID == "" {
			return nil
		}
		ack, err := relay.Incoming(eventCtx, msg.UserID, sharedrelay.Message{
			EventID: msg.EventID, ChatID: msg.ChatID, MessageID: msg.MessageID, Text: msg.Content,
		}, time.Now())
		if err != nil {
			return err
		}
		if ack != "" {
			go func(chatID, text string) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_ = (relayFeishuSender{channel: ch}).Send(ctx, chatID, text)
			}(msg.ChatID, ack)
		}
		return nil
	})
	server := &http.Server{Addr: listen, Handler: relay.Handler(), ReadHeaderTimeout: 5 * time.Second}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.ListenAndServe()
	}()
	channelDone := make(chan error, 1)
	go func() {
		channelDone <- ch.Start(ctx)
	}()
	fmt.Fprintf(out, "Relay API listening on %s; publish it through an HTTPS reverse proxy.\n", listen)
	var result error
	select {
	case result = <-serverDone:
		if errors.Is(result, http.ErrServerClosed) {
			result = nil
		}
	case result = <-channelDone:
		if errors.Is(result, context.Canceled) {
			result = nil
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	_ = ch.Stop(shutdownCtx)
	return result
}
