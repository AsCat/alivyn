package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
)

// runInitiativeDemo uses a virtual clock and an isolated history. Only synthetic
// conversations go to the configured provider; no transport sends any message.
func runInitiativeDemo(ctx context.Context, out io.Writer) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	c.Engine = "character_v2"
	c.Language = "zh-CN"
	c.Timezone = "UTC"
	client, err := configuredClient(c)
	if err != nil {
		return err
	}
	cases := []struct{ Name, User, Assistant string }{
		{"share a detail", "我最近想画旧书店，喜欢旧招牌掉漆的那种感觉。你想到什么可以再跟我说。", "我也喜欢那种留着旧痕迹的地方。"},
		{"respect concentration", "接下来我要集中写东西，到下午三点之前先别找我。", "好，你先写。"},
		{"closed subject", "刚才那个事解决了，不想再聊它。", "好，那就翻篇了。"},
	}
	for _, tc := range cases {
		err := func() error {
			dir, err := os.MkdirTemp("", "alivyn-initiative-demo-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
			h, err := store.OpenHistory(ctx, dir)
			if err != nil {
				return err
			}
			defer h.Close()
			now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
			if err := h.AddUserMessage(ctx, "我叫阿岚", now.Add(-3*time.Hour), time.UTC); err != nil {
				return err
			}
			if err := h.Add(ctx, "assistant", "我叫意续。", now.Add(-3*time.Hour+time.Second)); err != nil {
				return err
			}
			if err := h.AddUserMessage(ctx, tc.User, now.Add(-2*time.Hour), time.UTC); err != nil {
				return err
			}
			if err := h.Add(ctx, "assistant", tc.Assistant, now.Add(-2*time.Hour+time.Second)); err != nil {
				return err
			}
			counter := &auditionClient{inner: client}
			s := session.Session{Config: c, History: h, Client: counter}
			result, err := s.ConsiderInitiative(ctx, now)
			if err != nil {
				return err
			}
			report := map[string]any{"scenario": tc.Name, "outcome": result, "calls": counter.calls}
			if inner, _, exists, err := h.LatestInnerState(ctx); err != nil {
				return err
			} else if exists {
				report["inner_state"] = inner
			}
			if decisions, err := h.RecentOutreach(ctx, now, 1); err != nil {
				return err
			} else if len(decisions) > 0 {
				report["contact_choice"] = decisions[0]
			}
			items, err := h.ContactIntents(ctx, 10)
			if err != nil {
				return err
			}
			if len(items) > 0 {
				p, err := h.InitiativeProposal(ctx, items[0].IdempotencyKey)
				if err != nil {
					return err
				}
				report["proposal"] = p
				// Generate at the same virtual time; no message is delivered.
				text, err := s.ComposeContactForAt(ctx, items[0].Topic, items[0].IdempotencyKey, items[0].Due)
				if err != nil {
					report["generation_error"] = err.Error()
				} else {
					report["message"] = text
				}
				report["calls"] = counter.calls
			}
			data, err := json.Marshal(report)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, string(data))
			return nil
		}()
		if err != nil {
			return err
		}
	}
	return nil
}
