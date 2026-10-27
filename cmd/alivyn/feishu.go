package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/display"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/sharedclient"
	"github.com/AsCat/alivyn/internal/store"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/mdp/qrterminal/v3"
)

func runChannel(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 2 || len(args) > 3 || args[0] != "feishu" {
		return errors.New("usage: alivyn channel feishu connect [relay-url]|pair [relay-url]|run|status|add")
	}
	switch args[1] {
	case "connect", "pair":
		ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer cancel()
		relayURL := os.Getenv("ALIVYN_RELAY_URL")
		if len(args) == 3 {
			relayURL = args[2]
		}
		if err := pairSharedFeishu(ctx, relayURL, out); err != nil {
			return err
		}
		if args[1] == "connect" {
			return runSharedFeishu(out)
		}
		return nil
	case "add":
		if len(args) != 2 {
			return errors.New("usage: alivyn channel feishu add")
		}
		return addFeishu(ctx, out)
	case "status":
		if len(args) != 2 {
			return errors.New("usage: alivyn channel feishu status")
		}
		if exists, err := sharedFeishuExists(); err != nil {
			return err
		} else if exists {
			c, err := config.LoadSharedFeishu()
			if err != nil {
				return err
			}
			paired, err := (sharedclient.Client{BaseURL: c.RelayURL, Token: c.Token}).Status(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Shared Feishu bot: paired=%t relay=%s\n", paired, c.RelayURL)
			return nil
		}
		c, err := config.LoadFeishu()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Feishu configured: app=%s paired_chat=%t (messages require 'alivyn channel feishu run')\n", c.AppID, c.ChatID != "")
		return nil
	case "run":
		if len(args) != 2 {
			return errors.New("usage: alivyn channel feishu run")
		}
		if exists, err := sharedFeishuExists(); err != nil {
			return err
		} else if exists {
			return runSharedFeishu(out)
		}
		return runSelfFeishu(out)
	default:
		return errors.New("usage: alivyn channel feishu connect [relay-url]|pair [relay-url]|run|status|add")
	}
}

func sharedFeishuExists() (bool, error) {
	path, err := config.SharedFeishuPath()
	if err != nil {
		return false, err
	}
	_, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func addFeishu(ctx context.Context, out io.Writer) error {
	if _, err := config.Load(); err != nil {
		return err
	}
	path, err := config.FeishuPath()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("Feishu is already configured; existing app credentials were left untouched")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	minimal := false
	result, err := registration.RegisterApp(ctx, &registration.Options{
		Source:     "alivyn",
		CreateOnly: true,
		AppPreset: &registration.AppPreset{
			Name: "Alivyn · 意续",
			Desc: "A personal AI companion with local memory / 具有本地记忆的 AI 伙伴",
		},
		Addons: &registration.AppAddons{
			Preset: &minimal,
			Scopes: registration.AppAddonsScopes{Tenant: []string{
				"im:message.p2p_msg:readonly", "im:message:send_as_bot",
			}},
			Events: registration.AppAddonsEvents{Items: registration.AppAddonsEventItems{
				Tenant: []string{"im.message.receive_v1"},
			}},
		},
		OnQRCode: func(info *registration.QRCodeInfo) {
			fmt.Fprintln(out, "Scan in Feishu to create and authorize your own Alivyn bot / 用飞书扫码创建并授权自己的意续机器人：")
			qrterminal.GenerateHalfBlock(info.URL, qrterminal.M, out)
			fmt.Fprintf(out, "%s\nExpires in %d seconds / %d 秒后过期。\n", info.URL, info.ExpireIn, info.ExpireIn)
		},
	})
	if err != nil {
		return fmt.Errorf("Feishu QR registration failed: %w", err)
	}
	if result == nil || result.UserInfo == nil || result.UserInfo.OpenID == "" {
		return errors.New("Feishu registration did not return the scanning user's open_id; connection not saved")
	}
	if err := config.SaveFeishu(config.FeishuConnection{
		AppID: result.ClientID, AppSecret: result.ClientSecret, OwnerOpenID: result.UserInfo.OpenID,
	}); err != nil {
		return err
	}
	fmt.Fprintln(out, "Feishu credentials saved locally (0600). Run 'alivyn channel feishu run', then send your bot a private text message. / 凭证已私密保存；启动命令后，给机器人发一条私聊文字。")
	return nil
}

func acceptFeishuMessage(owner string, msg *types.NormalizedMessage) bool {
	return msg != nil && msg.ChatType == "p2p" && msg.UserID == owner && owner != "" &&
		msg.RawContentType == "text" && msg.EventID != "" && msg.ChatID != "" && msg.MessageID != "" &&
		strings.TrimSpace(msg.Content) != "" && len(msg.Content) <= 65536
}

func runSelfFeishu(out io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	client, err := configuredClient(c)
	if err != nil {
		return err
	}
	connection, err := config.LoadFeishu()
	if err != nil {
		return err
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
	apiClient := lark.NewClient(connection.AppID, connection.AppSecret)
	ch := channel.NewChannel(apiClient, newFeishuWSClient(connection.AppID, connection.AppSecret))
	ch.OnReady(func() {
		fmt.Fprintln(out, "Feishu connected. Send a private text message to your bot. / 飞书已连接，请给机器人发私聊文字。")
	})
	var connectionMu sync.Mutex
	ch.OnMessage(func(eventCtx context.Context, msg *types.NormalizedMessage) error {
		if !acceptFeishuMessage(connection.OwnerOpenID, msg) {
			return nil
		}
		connectionMu.Lock()
		defer connectionMu.Unlock()
		if connection.ChatID != "" && connection.ChatID != msg.ChatID {
			return nil
		}
		_, err := h.EnqueueFeishu(eventCtx, msg.EventID, msg.ChatID, msg.MessageID, msg.Content, time.Now(), zone)
		if err != nil {
			return err
		}
		if connection.ChatID == "" {
			updated := connection
			updated.ChatID = msg.ChatID
			if err := config.SaveFeishu(updated); err != nil {
				return err
			}
			connection = updated
			fmt.Fprintln(out, "Feishu private chat paired. / 飞书私聊已配对。")
		}
		return nil
	})
	s := session.Session{Config: c, History: h, Client: client}
	defer s.StartCharacterReflection(ctx)()
	go workFeishu(ctx, h, s, ch, out)
	go workChannelContact(ctx, h, s, zone, c.Language, func() bool {
		connectionMu.Lock()
		defer connectionMu.Unlock()
		return connection.ChatID != ""
	}, func(ctx context.Context, body, key string) error {
		connectionMu.Lock()
		chatID := connection.ChatID
		connectionMu.Unlock()
		if chatID == "" {
			return errors.New("Feishu private chat has not been paired")
		}
		sum := sha256.Sum256([]byte(connection.AppID + ":" + key))
		return (relayFeishuSender{client: apiClient}).SendIdempotent(ctx, chatID, body, hex.EncodeToString(sum[:16]))
	}, out)
	err = ch.Start(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func newFeishuWSClient(appID, appSecret string) *larkws.Client {
	// Channel.OnMessage registers on the dispatcher's event handler. Without
	// this dispatcher, the socket connects but never acknowledges messages.
	return larkws.NewClient(appID, appSecret,
		larkws.WithEventHandler(dispatcher.NewEventDispatcher("", "")))
}

func workFeishu(ctx context.Context, h *store.History, s session.Session, ch feishuSender, out io.Writer) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	nextAttempt := time.Time{}
	for {
		if !time.Now().Before(nextAttempt) {
			if err := processFeishuOnce(ctx, h, s, ch); err != nil && !errors.Is(err, store.ErrTurnSuperseded) && ctx.Err() == nil {
				fmt.Fprintf(out, "Feishu delivery pending; will retry: %v\n", err)
				nextAttempt = time.Now().Add(15 * time.Second)
			} else {
				nextAttempt = time.Time{}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type feishuSender interface {
	Send(context.Context, *types.SendInput) (*types.SendResult, error)
}

func processFeishuOnce(ctx context.Context, h *store.History, s session.Session, sender feishuSender) error {
	var item store.FeishuInboxItem
	var ok bool
	var err error
	if s.Config.EffectiveEngine() == "character_v2" {
		pause := 4 * time.Second
		item, ok, err = h.NextFeishuBatchWithPause(ctx, time.Now(), pause)
	} else {
		item, ok, err = h.NextFeishu(ctx)
	}
	if err != nil || !ok {
		return err
	}
	if item.State == "pending" {
		reply, err := s.ComposeReplyFor(ctx, item.UserMessageID)
		if err != nil {
			if errors.Is(err, character.ErrDialogueConflict) {
				newer, lookupErr := h.HasLaterFeishuInput(ctx, item.EventID)
				if lookupErr != nil {
					return lookupErr
				}
				if newer {
					// No line passed review. Let the later user contribution
					// receive an answer that can use both messages as context.
					return h.SkipFeishuReply(ctx, item.EventID)
				}
			}
			return err
		}
		if strings.TrimSpace(reply) == "" {
			if deferred, err := h.DeferFeishuReply(ctx, item.EventID); err != nil || deferred {
				return err
			}
			return h.SkipFeishuReply(ctx, item.EventID)
		}
		parts := []string{reply}
		if s.Config.EffectiveEngine() == "character_v2" && (s.Config.EffectivePersonaPack() == "vivid" || s.Config.EffectivePersonaPack() == "dafeiyu-astrbot") {
			turn, found, err := h.CharacterTurn(ctx, item.UserMessageID)
			if err != nil {
				return err
			}
			if found && turn.Mode == "say" && turn.Absence == nil {
				parts = display.Bubbles(reply)
			}
		}
		if err := h.SetFeishuReplyParts(ctx, item.EventID, reply, parts, time.Now()); err != nil {
			return err
		}
		item.Reply = reply
	}
	if err := waitForReplyPace(ctx, item.ReceivedAt, item.EventID, item.Reply); err != nil {
		return err
	}
	return h.DeliverFeishuParts(ctx, item.EventID, func(index int, body string) error {
		if index > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(650+min(len([]rune(body))*28, 1100)) * time.Millisecond):
			}
		}
		input := &types.SendInput{ChatID: item.ChatID, Text: display.Clean(body)}
		if index == 0 {
			input.ReplyMessageID = item.MessageID
		}
		_, err := sender.Send(ctx, input)
		return err
	})
}
