// Package relationship tracks familiarity from actual shared interaction.
// Its stages are private behavior context, not a score shown to the user.
package relationship

import (
	"strings"
	"time"
	"unicode"
)

type Stage string

const (
	Stranger     Stage = "stranger"
	Acquaintance Stage = "acquaintance"
	Familiar     Stage = "familiar"
	Close        Stage = "close"
)

type State struct {
	Stage           Stage
	MeaningfulTurns int
	SharedDays      int
	Disclosures     int
	LastSharedDay   string
	Reserved        bool
}

func Initial() State { return State{Stage: Stranger} }

// Advance uses only observed conversation. Repeated greetings do not create
// familiarity; explicit requests for distance override accumulated history.
func Advance(s State, text string, disclosure bool, at time.Time, zone *time.Location) (State, string) {
	if zone == nil {
		zone = time.UTC
	}
	text = strings.TrimSpace(text)
	if asksForDistance(text) {
		s.Reserved = true
	} else if welcomesCloseness(text) {
		s.Reserved = false
	}
	if meaningful(text) {
		s.MeaningfulTurns++
		day := at.In(zone).Format("2006-01-02")
		if s.LastSharedDay != day {
			s.SharedDays++
			s.LastSharedDay = day
		}
		if disclosure {
			s.Disclosures++
		}
	}
	previous := s.Stage
	s.Stage = stageFor(s)
	if s.Stage != previous {
		if s.Reserved {
			return s, "user requested more distance"
		}
		return s, "shared conversation"
	}
	return s, ""
}

func stageFor(s State) Stage {
	stage := Stranger
	if s.MeaningfulTurns >= 2 || s.Disclosures >= 1 {
		stage = Acquaintance
	}
	if s.MeaningfulTurns >= 12 && s.SharedDays >= 3 {
		stage = Familiar
	}
	if s.MeaningfulTurns >= 40 && s.SharedDays >= 14 && s.Disclosures >= 2 {
		stage = Close
	}
	if s.Reserved && (stage == Familiar || stage == Close) {
		return Acquaintance
	}
	return stage
}

func meaningful(text string) bool {
	if asksForDistance(text) || welcomesCloseness(text) || strings.HasPrefix(text, "忘记") || strings.HasPrefix(strings.ToLower(text), "forget ") {
		return false
	}
	text = strings.ToLower(strings.TrimFunc(strings.TrimSpace(text), func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	}))
	switch text {
	case "", "你好", "嗨", "哈喽", "hi", "hello", "hey", "嗯", "好的", "好", "ok", "收到", "谢谢", "thanks":
		return false
	}
	return true
}

func asksForDistance(text string) bool {
	for _, phrase := range []string{"别跟我这么熟", "别这么亲密", "不要太亲密", "保持距离", "别叫我宝贝", "don't be so familiar", "keep your distance", "please be less familiar"} {
		if strings.Contains(strings.ToLower(text), phrase) {
			return true
		}
	}
	return false
}

func welcomesCloseness(text string) bool {
	for _, phrase := range []string{"别那么生分", "可以亲近一点", "不用那么客气", "you can be more familiar", "no need to be so formal"} {
		if strings.Contains(strings.ToLower(text), phrase) {
			return true
		}
	}
	return false
}
