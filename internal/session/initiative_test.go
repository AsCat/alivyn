package session

import (
	"context"
	"errors"
	"strings"
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

func TestProactiveContextIncludesCharacterOwnEstablishedMotives(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "我还在工作", now.Add(-time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.Add(ctx, "assistant", "你忙你的，我听着。", now.Add(-time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "character_v2", PersonaPack: "dafeiyu-astrbot", WorldPack: "none", Timezone: "UTC"}, History: h}
	_, refs, err := s.characterContactContext(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref.ID == "self:character" {
			if ref.Provenance != "authored_character" || len([]rune(ref.Text)) > 1101 {
				t.Fatalf("invalid character motive: %+v", ref)
			}
			return
		}
	}
	t.Fatal("character's own motives were unavailable to initiative")
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
			inner := `{"focus":"rest","reason":"此刻想安静一会儿","evidence_ids":[],"impulses":[],"revisit_in_minutes":120}`
			if choice != "wait" {
				inner = `{"focus":"connect","reason":"还想聊旧书店","evidence_ids":["chat:3"],"impulses":[{"kind":"follow_up","want":"想再听他聊画画","subject":"旧书店的画面","evidence_ids":["chat:3"]}],"revisit_in_minutes":120}`
			}
			client := &characterScript{replies: []string{inner, response, "[[wait]]"}}
			s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "character_v2", WorldPack: "none", Timezone: "UTC"}, History: h, Client: client}
			outcome, err := s.ConsiderInitiative(ctx, now)
			if choice == "invalid" {
				if err == nil {
					t.Fatal("invalid proposal accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			expectedCalls := 2
			if choice == "wait" {
				expectedCalls = 1 // A private rest choice needs no contact model call.
			}
			if choice == "invalid" {
				expectedCalls = 3 // One bounded plain-completion retry.
			}
			if len(client.calls) != expectedCalls {
				t.Fatalf("unexpected calls: %d", len(client.calls))
			}
			if choice == "contact" {
				if got := client.calls[1][1].Content; !strings.Contains(got, "想再听他聊画画") {
					t.Fatalf("current impulse missing from action choice: %s", got)
				}
				beats, err := h.RecentOutreach(ctx, now, 1)
				if err != nil || len(beats) != 1 || len(beats[0].Impulses) != 1 {
					t.Fatalf("impulse history missing: %+v %v", beats, err)
				}
			}
			if _, err := s.ConsiderInitiative(ctx, now.Add(time.Minute)); err != nil || len(client.calls) != expectedCalls {
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

func TestInnerWakeContinuesDuringContactQuietHours(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "那首旧歌挺好听", now.Add(-time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &characterScript{replies: []string{`{"focus":"rest","reason":"今晚想休息","evidence_ids":[],"impulses":[],"revisit_in_minutes":120}`}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "character_v2", WorldPack: "none", Timezone: "UTC"}, History: h, Client: client}
	outcome, err := s.ConsiderInitiative(ctx, now)
	if err != nil || outcome != "quiet hours" || len(client.calls) != 1 {
		t.Fatalf("quiet hours stopped inner life or allowed contact: %q %d %v", outcome, len(client.calls), err)
	}
	inner, _, exists, err := h.LatestInnerState(ctx)
	if err != nil || !exists || inner.Focus != "rest" {
		t.Fatalf("private wake was not saved: %+v %t %v", inner, exists, err)
	}
}

func TestInnerWakeFailureFallsBackToContactDecision(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "旧书店那幅画还没画完", now.Add(-3*time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.Add(ctx, "assistant", "那块招牌我还记得。", now.Add(-3*time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "我刚又翻到那个速写本", now.Add(-2*time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.Add(ctx, "assistant", "你后来还添了颜色吗", now.Add(-2*time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	client := &characterScript{replies: []string{
		`{"focus":"invalid","reason":"bad model output","revisit_in_minutes":120}`,
		`{"impulses":[]}`,
		`{"action":"wait","reason":"今天没有新的话题"}`,
	}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "character_v2", WorldPack: "none", Timezone: "UTC"}, History: h, Client: client}
	outcome, err := s.ConsiderInitiative(ctx, now)
	if err != nil || outcome != "wait" || len(client.calls) != 3 {
		t.Fatalf("inner wake failure disabled contact path: %q %d %v", outcome, len(client.calls), err)
	}
}
