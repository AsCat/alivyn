package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/store"
)

type proactiveReviewScript struct {
	dialogue []string
	reviews  []string
}

func (s *proactiveReviewScript) Complete(_ context.Context, _ string, _ []model.Message) (string, error) {
	r := s.dialogue[0]
	s.dialogue = s.dialogue[1:]
	return r, nil
}

func (s *proactiveReviewScript) CompleteJSON(_ context.Context, _ string, _ []model.Message) (string, error) {
	r := s.reviews[0]
	s.reviews = s.reviews[1:]
	return r, nil
}

func TestProactiveContactReviewsContradictoryLifeClaimBeforeDelivery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "今晚还在工作", now.Add(-time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.Add(ctx, "assistant", "这么晚啊。", now.Add(-time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	client := &proactiveReviewScript{
		dialogue: []string{
			`{"mode":"say","reply":"我已经睡了，可我还没睡。","absence":null}`,
			`{"mode":"say","reply":"我又想到你刚才说的工作了。","absence":null}`,
		},
		reviews: []string{
			`{"valid":false,"issue":"same message makes contradictory sleep claims"}`,
			`{"valid":true,"issue":""}`,
		},
	}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "character_v2", WorldPack: "none", Timezone: "UTC"}, History: h, Client: client}
	reply, err := s.composeCharacterContactAt(ctx, "想起对方说的工作", now)
	if err != nil || reply != "我又想到你刚才说的工作了。" || len(client.dialogue) != 0 || len(client.reviews) != 0 {
		t.Fatalf("unreviewed proactive reply: %q %v %+v", reply, err, client)
	}
}

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
