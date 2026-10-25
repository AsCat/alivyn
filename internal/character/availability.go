package character

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/AsCat/alivyn/internal/model"
)

// Absence is a bounded action chosen by the character, not a claim that an AI
// physically sleeps. The durable channel enforces it and revisits queued input.
type Absence struct {
	Kind   string `json:"kind"`
	Until  string `json:"until"`
	Reason string `json:"reason"`
}

func DefaultRoutine(language string) string {
	if language == "zh-CN" {
		return "通常零点半前后休息，早上八点左右起来；白天做事时注意力有限，傍晚更容易闲聊，偶尔想一个人待着。这是个人习惯，可以因具体安排或在意的话题改变，不是到点背台词。精神会随休息、生活进展和交流消耗而变化。"
	}
	return "Usually rests around 00:30 and gets up around 08:00; limited attention during daytime work, more open to chatting in the evening, sometimes wants time alone. Personal habits may shift for concrete plans or a subject that matters; they are not lines to recite on a schedule. Energy varies with rest, life events and time spent interacting."
}

func availabilityInstruction(c Context) model.Message {
	text := `Your daily routine is a habit, not a rigid schedule. Let local time, previous intentions, actual life records, energy, topic interest and the relationship jointly guide your choice. Ordinary low interest or low spirits can mean a smaller reply, a natural ending or silence; do not fabricate grievances or be warm and eager on every turn. Do not tell the user you are assessing these variables. [[wait]] means no further message is needed and schedules no follow-up. It is also valid when an optional topic holds no interest, even without a formal goodbye. Do not use silence as punishment or a test of affection; important help, corrections and unresolved concrete questions deserve an answer or an honest boundary.
`
	if !c.CanLeave || c.Occasion != "" {
		text += "No new timed absence is available on this turn. Reply now or use [[wait]]. Do not output an away directive or promise a scheduled follow-up."
	} else {
		text += `Before wording a reply, choose whether to engage at all. Choose say to speak now, wait to leave an optional exchange quiet, or away for a timed absence. Your own expressed plans have weight: normally carry through a decision to leave instead of repeatedly extending it just because more ordinary small talk arrives. A substantial new concern or a subject you truly want to pursue can justify changing your mind. The time since the last chat matters; messages an hour apart are not a continuous two-minute exchange.
You may actually leave the conversation for sleep, another activity or time alone. If your words say you are leaving now, choose away, not say: merely saying goodbye does not put you to sleep or make you unavailable. The channel executes your absence; regular incoming messages wait until that time, then are reconsidered together. Choose a plausible return time from the current local time and your routine (at least one minute, at most sixteen hours away). For a sleep period that is already underway, leave the reply empty without a goodnight announcement. For an ongoing live exchange, a short closing message may fit better. Do not use a timed absence merely because the user writes little. Only use it when actually choosing to leave, not as decoration for every closing remark. You can remain awake for a meaningful conversation. On returning, consider when messages were sent and whether they still need an answer; no repeated apology or invented story about what happened offscreen. Never invent a real-world emergency or private user experience to justify an absence.
OUTPUT CONTRACT overrides the plain-text formatting instructions above: return one JSON object with exactly mode, reply and absence. No internal monologue, emotion report or reasoning.
Speak now: {"mode":"say","reply":"actual short chat message","absence":null}
No message needed: {"mode":"wait","reply":"","absence":null}
Leave: {"mode":"away","reply":"optional short closing message, or empty","absence":{"kind":"sleep or busy or alone (choose one)","until":"RFC3339 timestamp with timezone","reason":"brief grounded reason"}}
The user sees only reply; the other fields control delivery. Words saying you are leaving must use away and a real return time.`
	}
	return model.Message{Role: "system", Content: text}
}

func parseAbsence(raw string, c Context) (*Absence, string, error) {
	if strings.HasPrefix(raw, "{") {
		var envelope struct {
			Mode    string   `json:"mode"`
			Reply   string   `json:"reply"`
			Absence *Absence `json:"absence"`
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			return nil, "", errors.New("invalid dialogue action JSON")
		}
		if envelope.Mode == "say" && envelope.Absence == nil && strings.TrimSpace(envelope.Reply) != "" {
			return nil, envelope.Reply, nil
		}
		if envelope.Mode == "wait" && envelope.Absence == nil && envelope.Reply == "" {
			return nil, "[[wait]]", nil
		}
		if envelope.Mode != "away" || envelope.Absence == nil || !c.CanLeave || c.Occasion != "" {
			return nil, "", errors.New("invalid dialogue action")
		}
		now, err := time.Parse(time.RFC3339, c.Frame.Now)
		if err != nil {
			return nil, "", err
		}
		if err := envelope.Absence.Validate(now); err != nil {
			return nil, "", err
		}
		if strings.Contains(envelope.Reply, "[[") {
			return nil, "", errors.New("mixed dialogue action formats")
		}
		return envelope.Absence, strings.TrimSpace(envelope.Reply), nil
	}
	if strings.HasPrefix(raw, "[[say]]") {
		body := strings.TrimSpace(strings.TrimPrefix(raw, "[[say]]"))
		if strings.Contains(body, "[[") {
			return nil, "", errors.New("multiple dialogue directives")
		}
		return nil, body, nil
	}
	if !strings.HasPrefix(raw, "[[away:") {
		if strings.Contains(raw, "[[away") {
			return nil, "", errors.New("away directive must be the first line")
		}
		return nil, raw, nil
	}
	if !c.CanLeave || c.Occasion != "" {
		return nil, "", errors.New("timed absence is unavailable for this turn")
	}
	end := strings.Index(raw, "]]")
	if end < 0 {
		return nil, "", errors.New("invalid away directive")
	}
	line, body := raw[:end+2], raw[end+2:]
	var a Absence
	dec := json.NewDecoder(strings.NewReader(strings.TrimSuffix(strings.TrimPrefix(line, "[[away:"), "]]")))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return nil, "", errors.New("invalid absence JSON")
	}
	now, err := time.Parse(time.RFC3339, c.Frame.Now)
	if err != nil {
		return nil, "", err
	}
	if err := a.Validate(now); err != nil {
		return nil, "", err
	}
	body = strings.TrimSpace(body)
	if strings.Contains(body, "[[") {
		return nil, "", errors.New("multiple dialogue directives")
	}
	return &a, body, nil
}

func (a Absence) Validate(now time.Time) error {
	until, err := time.Parse(time.RFC3339, a.Until)
	if err != nil || until.Sub(now) < time.Minute || until.Sub(now) > 16*time.Hour {
		return errors.New("absence must end between one minute and sixteen hours from now")
	}
	if a.Kind != "sleep" && a.Kind != "busy" && a.Kind != "alone" {
		return errors.New("invalid absence kind")
	}
	if strings.TrimSpace(a.Reason) == "" || len([]rune(a.Reason)) > 160 || strings.IndexFunc(a.Reason, unicode.IsControl) >= 0 {
		return errors.New("absence needs a short single-line reason")
	}
	return nil
}
