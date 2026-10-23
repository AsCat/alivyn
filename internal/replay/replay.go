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
)

type Policy struct {
	QuietStart            string `json:"quiet_start"`
	QuietEnd              string `json:"quiet_end"`
	DailyLimit            int    `json:"daily_limit"`
	RecoveryCooldownHours int    `json:"recovery_cooldown_hours"`
}

type Event struct {
	At        string `json:"at"`
	Type      string `json:"type"`
	Key       string `json:"key"`
	HappensAt string `json:"happens_at"`
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
	if f.Policy.DailyLimit < 1 {
		return nil, errors.New("daily limit must be positive")
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
		case "user_event":
			when, err := time.Parse(time.RFC3339, event.HappensAt)
			if err != nil || event.Key == "" {
				return nil, fmt.Errorf("invalid user event at %s", event.At)
			}
			if old, exists := active[event.Key]; exists {
				canceled = append(canceled, old)
			}
			active[event.Key] = intent{topic: event.Key, due: when.Add(time.Hour), expires: when.Add(6 * time.Hour)}
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
			quiet, err := isQuiet(at.In(zone), f.Policy.QuietStart, f.Policy.QuietEnd)
			if err != nil {
				return nil, err
			}
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
				if at.Before(candidate.due) {
					continue
				}
				if !at.Before(candidate.expires) {
					delete(active, topic)
					decision.Reason = "expired intent is not backfilled"
					continue
				}
				if paused {
					decision.Reason = "paused"
					continue
				}
				if quiet {
					decision.Reason = "quiet hours"
					continue
				}
				if daily[day] >= f.Policy.DailyLimit {
					decision.Reason = "daily contact limit"
					continue
				}
				delete(active, topic)
				daily[day]++
				count++
				decision.DeliveryCount = count
				decision.Topic = candidate.topic
				decision.Reason = ""
				break
			}
			if gap != nil && decision.Topic == "" && !at.Before(gap.due) {
				switch {
				case !at.Before(gap.expires):
					gap = nil
				case paused:
					decision.Reason = "paused"
				case quiet:
					decision.Reason = "quiet hours"
				case daily[day] >= f.Policy.DailyLimit:
					decision.Reason = "daily contact limit"
				default:
					daily[day]++
					count++
					decision.DeliveryCount = count
					decision.Topic = gap.topic
					decision.Reason = ""
					lastGapContact = &at
					gap = nil
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
	begin, err := time.Parse("15:04", start)
	if err != nil {
		return false, err
	}
	finish, err := time.Parse("15:04", end)
	if err != nil {
		return false, err
	}
	nowMinutes := at.Hour()*60 + at.Minute()
	startMinutes := begin.Hour()*60 + begin.Minute()
	endMinutes := finish.Hour()*60 + finish.Minute()
	if startMinutes < endMinutes {
		return nowMinutes >= startMinutes && nowMinutes < endMinutes, nil
	}
	if startMinutes == endMinutes {
		return false, nil
	}
	return nowMinutes >= startMinutes || nowMinutes < endMinutes, nil
}
