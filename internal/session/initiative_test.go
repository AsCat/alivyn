package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/store"
)

func TestInitiativeFromIdleToMessageAndQuietChoice(t *testing.T) {
	for _, choice := range []string{"contact", "wait", "invalid"} {
		t.Run(choice, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
			h, err := store.OpenHistory(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			for i, text := range []string{"最近想画画", "打算画一张旧书店"} {
				at := now.Add(-3*time.Hour + time.Duration(i)*time.Hour)
				if err := h.AddUserMessage(ctx, text, at, time.UTC); err != nil {
					t.Fatal(err)
				}
				if err := h.Add(ctx, "assistant", "那块招牌挺有意思的。", at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			response := `{"action":"contact","kind":"follow_up","reason":"想到旧书店的一个画面细节","evidence_ids":["chat:3"],"after_minutes":5}`
			if choice == "wait" {
				response = `{"action":"wait","reason":"没有新的值得联系的事情"}`
			}
			if choice == "invalid" {
				response = " "
			}
			client := &characterScript{replies: []string{response, "[[wait]]"}}
			s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "character_v2", WorldPack: "none", Timezone: "UTC"}, History: h, Client: client}
			outcome, err := s.ConsiderInitiative(ctx, now)
			if choice == "invalid" {
				if err == nil {
					t.Fatal("invalid proposal accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(client.calls) != 1 {
				t.Fatalf("unexpected calls: %d", len(client.calls))
			}
			if _, err := s.ConsiderInitiative(ctx, now.Add(time.Minute)); err != nil || len(client.calls) != 1 {
				t.Fatal("cooldown made another model call", err)
			}
			items, err := h.ContactIntents(ctx, 10)
			if err != nil {
				t.Fatal(err)
			}
			if choice != "contact" {
				if len(items) != 0 {
					t.Fatal("wait/error created a contact")
				}
				return
			}
			if outcome != "queued" || len(items) != 1 {
				t.Fatalf("not queued: %s %+v", outcome, items)
			}
			if _, err := s.ComposeContactFor(ctx, "initiative", items[0].IdempotencyKey); !errors.Is(err, ErrContactWait) {
				t.Fatalf("no-message choice lost: %v", err)
			}
		})
	}
}
