package character

import (
	"context"
	"fmt"
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

type malformedJSONModel struct{ plainCalls int }

func (m *malformedJSONModel) CompleteJSON(context.Context, string, []model.Message) (string, error) {
	return `{"impulses":[`, nil
}

func (m *malformedJSONModel) Complete(_ context.Context, _ string, input []model.Message) (string, error) {
	m.plainCalls++
	if len(input) != 3 {
		return "", fmt.Errorf("retry lost its context: %d messages", len(input))
	}
	return `{"impulses":[{"kind":"share","want":"想说点自己的事","subject":"旧录音","evidence_ids":["self:character"]}]}`, nil
}

func TestImpulseFormationRecoversMalformedProviderJSON(t *testing.T) {
	client := &malformedJSONModel{}
	refs := []actor.Evidence{{ID: "self:character", Provenance: "authored_character", Text: "Likes old recordings"}}
	impulses, err := ImagineImpulses(context.Background(), client, "mock", Context{}, refs)
	if err != nil || len(impulses) != 1 || client.plainCalls != 1 {
		t.Fatalf("malformed JSON was not recovered once: %+v %d %v", impulses, client.plainCalls, err)
	}
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

func TestImpulseFormationRequiresSourcedConcreteWants(t *testing.T) {
	refs := []actor.Evidence{{ID: "self:character", Provenance: "authored_character", Text: "Likes old songs"}, {ID: "chat:7", Provenance: "observed_chat", Text: "user: I am working"}}
	valid := `{"impulses":[{"kind":"share","want":"想再聊两句","subject":"那首旧歌里有趣的一句","evidence_ids":["self:character"]}]}`
	impulses, err := ParseImpulses(valid, refs)
	if err != nil || len(impulses) != 1 || impulses[0].Kind != "share" {
		t.Fatalf("valid self-driven impulse rejected: %+v %v", impulses, err)
	}
	for _, raw := range []string{
		`{"impulses":[{"kind":"share","want":"想聊","subject":"她刚看的电影","evidence_ids":["world:missing"]}]}`,
		`{"impulses":[{"kind":"share","want":"","subject":"旧歌","evidence_ids":["self:character"]}]}`,
		`{"impulses":[{"kind":"share","want":"想聊","subject":"旧歌","evidence_ids":[]}]}`,
	} {
		if _, err := ParseImpulses(raw, refs); err == nil {
			t.Fatalf("unsupported impulse accepted: %s", raw)
		}
	}
}

func TestDistinctSubjectsFromSameCharacterEvidenceHaveDistinctKeys(t *testing.T) {
	impulses := []Impulse{
		{Kind: "share", Subject: "听到的一句旧歌", EvidenceIDs: []string{"self:character"}},
		{Kind: "share", Subject: "想起那本乱写的本子", EvidenceIDs: []string{"self:character"}},
	}
	a := Initiative{Action: "contact", Kind: "share", EvidenceIDs: []string{"self:character"}}
	a.Subject = ChosenSubject(a, impulses)
	b := a
	b.ImpulseIndex = 2
	b.Subject = ChosenSubject(b, impulses)
	if a.Subject != impulses[0].Subject || b.Subject != impulses[1].Subject || a.TopicKey() == b.TopicKey() {
		t.Fatalf("different self-driven subjects collapsed: %q %q", a.Subject, b.Subject)
	}
}
