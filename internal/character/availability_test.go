package character

import (
	"strings"
	"testing"

	"github.com/AsCat/alivyn/internal/actor"
)

func TestAbsenceProtocolBoundsAndChannelCapability(t *testing.T) {
	c := Context{CanLeave: true, Frame: actor.Frame{Now: "2026-10-26T01:00:00+08:00"}}
	valid := `[[away:{"kind":"sleep","until":"2026-10-26T08:00:00+08:00","reason":"按自己的作息休息"}]]`
	a, body, err := parseAbsence(valid+"\n明天说吧，我先睡了", c)
	if err != nil || a == nil || a.Kind != "sleep" || body != "明天说吧，我先睡了" {
		t.Fatal(a, body, err)
	}
	for _, raw := range []string{
		strings.Replace(valid, "08:00:00", "00:59:00", 1),
		strings.Replace(valid, "2026-10-26T08", "2026-10-28T08", 1),
		strings.Replace(valid, "sleep", "punish", 1),
		valid + "\n[[wait]]",
		"晚安\n" + valid,
	} {
		if _, _, err := parseAbsence(raw, c); err == nil {
			t.Fatal("accepted invalid directive", raw)
		}
	}
	c.CanLeave = false
	if _, _, err := parseAbsence(valid, c); err == nil {
		t.Fatal("terminal claimed a deferred delivery")
	}
	c.CanLeave = true
	c.Occasion = "a proactive hello"
	if _, _, err := parseAbsence(valid, c); err == nil {
		t.Fatal("unsolicited turn scheduled an absence")
	}
}

func TestDeliveryEnvelopeKeepsOnlySpokenTextAndValidatedAction(t *testing.T) {
	c := Context{CanLeave: true, Frame: actor.Frame{Now: "2026-10-26T01:00:00+08:00"}}
	for _, raw := range []string{
		`{"mode":"away","reply":"明天聊。","absence":{"kind":"sleep","until":"2026-10-26T08:00:00+08:00","reason":"准备休息"}}`,
		`[[away:{"kind":"sleep","until":"2026-10-26T08:00:00+08:00","reason":"准备休息"}]]明天聊。`,
	} {
		a, body, err := parseAbsence(raw, c)
		if err != nil || a == nil || body != "明天聊。" {
			t.Fatal(a, body, err)
		}
	}
	if a, body, err := parseAbsence(`{"mode":"wait","reply":"","absence":null}`, c); err != nil || a != nil || body != "[[wait]]" {
		t.Fatal(a, body, err)
	}
	for _, raw := range []string{`{"mode":"away","reply":"晚安","absence":null}`, `{"mode":"wait","reply":"强行发话"}`, `{"mode":"reset","reply":""}`} {
		if _, _, err := parseAbsence(raw, c); err == nil {
			t.Fatal(raw)
		}
	}
}
