package character

import (
	"context"
	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/model"
	"strings"
	"testing"
)

type initiativeCapture struct{ input []model.Message }

func (c *initiativeCapture) Complete(_ context.Context, _ string, input []model.Message) (string, error) {
	c.input = input
	return `{"action":"wait","reason":"need some room"}`, nil
}

func TestInitiativeSeesConnectionAndActualOutreachHistory(t *testing.T) {
	client := &initiativeCapture{}
	ctx := Context{
		Frame:           actor.Frame{Name: "意续", Now: "2026-10-28T01:20:00+08:00"},
		Moment:          Moment{Connection: "有点好奇，但今晚没那么想一直聊天"},
		LastUserAt:      "2026-10-28T01:08:45+08:00",
		InitiativeLevel: 100,
		OutreachHistory: []OutreachBeat{{At: "2026-10-28T01:07:53+08:00", Action: "queued", Kind: "reconnect", Reason: "想到昨晚的事", Delivered: true}},
	}
	if _, err := ConsiderContact(context.Background(), client, "test", ctx, nil); err != nil {
		t.Fatal(err)
	}
	input := client.input[1].Content
	for _, expected := range []string{"recent_outreach_decisions", "想到昨晚的事", "有点好奇，但今晚没那么想一直聊天", "last_user_at"} {
		if !strings.Contains(input, expected) {
			t.Fatalf("initiative lacks %q: %s", expected, input)
		}
	}
}

func TestInitiativeRequiresRealReferencesAndAllowsSilence(t *testing.T) {
	refs := []actor.Evidence{{ID: "chat:7", Provenance: "observed_chat"}}
	for _, raw := range []string{
		`{"action":"contact","kind":"follow_up","reason":"a detail","evidence_ids":["chat:999"],"after_minutes":5}`,
		`{"action":"contact","kind":"share","reason":"a detail","evidence_ids":["chat:7"],"after_minutes":-1}`,
		`{"action":"contact","kind":"share","reason":"a detail","evidence_ids":[],"after_minutes":5}`,
		` `,
	} {
		if _, err := ParseInitiative(raw, refs); err == nil {
			t.Fatalf("invalid proposal accepted: %s", raw)
		}
	}
	if p, err := ParseInitiative(`{"action":"wait","reason":"the user wants to focus"}`, refs); err != nil || p.Action != "wait" || p.AfterMinutes != 0 {
		t.Fatal(p, err)
	}
	if p, err := ParseInitiative(`{"action":"contact","kind":"share","reason":"a small new thought","evidence_ids":["chat:7"],"after_minutes":0}`, refs); err != nil || p.AfterMinutes != 0 {
		t.Fatal(p, err)
	}
}
