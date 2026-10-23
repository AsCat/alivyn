// Package replay evaluates contact decisions against a recorded event timeline.
// It deliberately does not call a model or deliver messages.
package replay

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/AsCat/alivyn/internal/contact"
	"github.com/AsCat/alivyn/internal/memory"
)

type Policy struct {
	QuietStart            string `json:"quiet_start"`
	QuietEnd              string `json:"quiet_end"`
	DailyLimit            int    `json:"daily_limit"`
	RecoveryCooldownHours int    `json:"recovery_cooldown_hours"`
}

type Event struct {
	At         string `json:"at"`
	Type       string `json:"type"`
	SourceText string `json:"source_text"`
}

type Expected struct {
	At            string `json:"at"`
	DeliveryCount int    `json:"delivery_count"`
	Topic         string `json:"topic"`
	Reason        string `json:"reason"`
}

type Fixture struct {
	SchemaVersion int        `json:"schema_version"`
	Name          string     `json:"name"`
	Timezone      string     `json:"timezone"`
	Policy        Policy     `json:"policy"`
	Events        []Event    `json:"events"`
	Expect        []Expected `json:"expect"`
}

type Decision struct {
	At            string
	DeliveryCount int
	Topic         string
	Reason        string
}

type intent struct {
	topic   string
	due     time.Time
	expires time.Time
}

func Load(path string) (Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, err
	}
	var f Fixture
	if err := json.Unmarshal(data, &f); err != nil {
		return Fixture{}, err
	}
	if f.SchemaVersion != 1 || f.Name == "" || len(f.Events) == 0 {
		return Fixture{}, errors.New("invalid scenario fixture")
	}
	return f, nil
}

func Run(f Fixture) ([]Decision, error) {
	zone, err := time.LoadLocation(f.Timezone)
	if err != nil {
		return nil, err
	}
	basePolicy := contact.Policy{QuietStart: f.Policy.QuietStart, QuietEnd: f.Policy.QuietEnd, DailyLimit: f.Policy.DailyLimit}
	if err := basePolicy.Validate(); err != nil {
		return nil, err
	}
	active := map[string]intent{}
	var canceled []intent
	var gap *intent
	var stoppedAt *time.Time
	var lastGapContact *time.Time
	suppressedGap := false
	paused := false
	daily := map[string]int{}
	count := 0
	var decisions []Decision
	var previous time.Time
	for _, event := range f.Events {
		at, err := time.Parse(time.RFC3339, event.At)
		if err != nil || (!previous.IsZero() && !at.After(previous)) {
			return nil, fmt.Errorf("invalid or unordered event time %q", event.At)
		}
		previous = at
		switch event.Type {
		case "user_message":
			if event.SourceText == "" {
				return nil, fmt.Errorf("missing user message at %s", event.At)
			}
			change, ok := memory.Extract(event.SourceText, at, zone)
			if !ok {
				continue
			}
			if old, exists := active[change.Topic]; exists {
				canceled = append(canceled, old)
			}
			if change.Action == memory.Upsert {
				active[change.Topic] = intent{topic: change.Topic, due: change.HappensAt.Add(time.Hour), expires: change.HappensAt.Add(6 * time.Hour)}
			} else {
				delete(active, change.Topic)
			}
		case "pause":
			paused = true
		case "resume":
			paused = false
		case "runtime_stopped":
			stoppedAt = &at
		case "runtime_resumed":
			if stoppedAt != nil && at.Sub(*stoppedAt) >= 6*time.Hour {
				cooldown := time.Duration(f.Policy.RecoveryCooldownHours) * time.Hour
				if lastGapContact == nil || at.Sub(*lastGapContact) >= cooldown {
					candidate := intent{topic: "time_gap", due: at, expires: at.Add(6 * time.Hour)}
					gap = &candidate
				} else {
					suppressedGap = true
				}
			} else if lastGapContact != nil && at.Sub(*lastGapContact) < time.Duration(f.Policy.RecoveryCooldownHours)*time.Hour {
				suppressedGap = true
			}
			stoppedAt = nil
		case "clock_tick":
			decision := Decision{At: event.At, DeliveryCount: count}
			day := at.In(zone).Format("2006-01-02")
			policy := basePolicy
			policy.Paused = paused
			topics := make([]string, 0, len(active))
			for topic := range active {
				topics = append(topics, topic)
			}
			sort.Slice(topics, func(left, right int) bool {
				a, b := active[topics[left]], active[topics[right]]
				if a.due.Equal(b.due) {
					return topics[left] < topics[right]
				}
				return a.due.Before(b.due)
			})
			for _, topic := range topics {
				candidate := active[topic]
				result := contact.Evaluate(at, zone, policy, contact.Intent{Due: candidate.due, Expires: candidate.expires, Current: true}, daily[day])
				if result.Reason == "not due" {
					continue
				}
				if result.Final {
					delete(active, topic)
					decision.Reason = result.Reason
					continue
				}
				if !result.Ready {
					decision.Reason = result.Reason
					continue
				}
				delete(active, topic)
				daily[day]++
				count++
				decision.DeliveryCount = count
				decision.Topic = candidate.topic
				decision.Reason = "event follow-up"
				break
			}
			if gap != nil && decision.Topic == "" && !at.Before(gap.due) {
				result := contact.Evaluate(at, zone, policy, contact.Intent{Due: gap.due, Expires: gap.expires, Current: true}, daily[day])
				switch {
				case result.Final:
					gap = nil
				case result.Ready:
					daily[day]++
					count++
					decision.DeliveryCount = count
					decision.Topic = gap.topic
					decision.Reason = "time gap after resume"
					lastGapContact = &at
					gap = nil
				default:
					decision.Reason = result.Reason
				}
			}
			canceledDue := false
			remaining := canceled[:0]
			for _, old := range canceled {
				if !at.Before(old.due) {
					canceledDue = true
				} else {
					remaining = append(remaining, old)
				}
			}
			canceled = remaining
			if decision.Topic == "" && decision.Reason == "" && canceledDue {
				decision.Reason = "superseded intent"
			}
			if decision.Topic == "" && decision.Reason == "" && suppressedGap {
				decision.Reason = "cooldown and duplicate suppression"
			}
			suppressedGap = false
			decisions = append(decisions, decision)
		default:
			return nil, fmt.Errorf("unknown event type %q", event.Type)
		}
	}
	return decisions, nil
}

func Verify(f Fixture) ([]Decision, error) {
	decisions, err := Run(f)
	if err != nil {
		return nil, err
	}
	if len(decisions) != len(f.Expect) {
		return decisions, fmt.Errorf("%s: expected %d clock decisions, got %d", f.Name, len(f.Expect), len(decisions))
	}
	byTime := map[string]Decision{}
	for _, decision := range decisions {
		byTime[decision.At] = decision
	}
	for _, expected := range f.Expect {
		got, ok := byTime[expected.At]
		if !ok || got.DeliveryCount != expected.DeliveryCount || got.Topic != expected.Topic || got.Reason != expected.Reason {
			return decisions, fmt.Errorf("%s at %s: got %+v, expected %+v", f.Name, expected.At, got, expected)
		}
	}
	return decisions, nil
}

func isQuiet(at time.Time, start, end string) (bool, error) {
	if err := (contact.Policy{QuietStart: start, QuietEnd: end, DailyLimit: 1}).Validate(); err != nil {
		return false, err
	}
	return contact.Quiet(at, start, end), nil
}
