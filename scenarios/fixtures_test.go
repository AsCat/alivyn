package scenarios

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fixture struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	Timezone      string `json:"timezone"`
	Policy        struct {
		QuietStart string `json:"quiet_start"`
		QuietEnd   string `json:"quiet_end"`
		DailyLimit int    `json:"daily_limit"`
	} `json:"policy"`
	Events []struct {
		At        string `json:"at"`
		Type      string `json:"type"`
		Key       string `json:"key"`
		HappensAt string `json:"happens_at"`
	} `json:"events"`
	Expect []struct {
		At            string `json:"at"`
		DeliveryCount int    `json:"delivery_count"`
		Topic         string `json:"topic"`
		Reason        string `json:"reason"`
	} `json:"expect"`
}

// These checks keep the specification fixtures internally consistent until the
// runtime scenario runner can assert the actual contact decisions.
func TestFixturesAreReplayable(t *testing.T) {
	paths, err := filepath.Glob("*.json")
	if err != nil || len(paths) < 3 {
		t.Fatalf("expected at least three scenario fixtures: paths=%v, error=%v", paths, err)
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var f fixture
			if err := json.Unmarshal(data, &f); err != nil {
				t.Fatal(err)
			}
			if f.SchemaVersion != 1 || f.Name == "" || len(f.Events) == 0 || len(f.Expect) == 0 {
				t.Fatal("missing schema version, name, events, or expectations")
			}
			if _, err := time.LoadLocation(f.Timezone); err != nil {
				t.Fatalf("invalid timezone: %v", err)
			}
			if f.Policy.DailyLimit < 1 {
				t.Fatal("daily contact limit must be positive")
			}
			for _, clock := range []string{f.Policy.QuietStart, f.Policy.QuietEnd} {
				if _, err := time.Parse("15:04", clock); err != nil {
					t.Fatalf("invalid quiet hour %q: %v", clock, err)
				}
			}
			var previous time.Time
			ticks := map[string]bool{}
			for _, event := range f.Events {
				at := parseTime(t, event.At)
				if !previous.IsZero() && !at.After(previous) {
					t.Fatal("events must be strictly chronological")
				}
				previous = at
				switch event.Type {
				case "user_event":
					if event.Key == "" || event.HappensAt == "" {
						t.Fatal("user event needs key and event time")
					}
					parseTime(t, event.HappensAt)
				case "clock_tick":
					ticks[event.At] = true
				case "pause", "resume", "runtime_stopped", "runtime_resumed":
				default:
					t.Fatalf("unknown event type %q", event.Type)
				}
			}
			previousCount := 0
			for _, expected := range f.Expect {
				parseTime(t, expected.At)
				if !ticks[expected.At] {
					t.Fatalf("expectation at %s has no clock tick", expected.At)
				}
				if expected.DeliveryCount < previousCount || (expected.Topic == "" && expected.Reason == "") {
					t.Fatal("delivery count decreased or expectation lacks explanation")
				}
				previousCount = expected.DeliveryCount
			}
		})
	}
}

func parseTime(t *testing.T, value string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("invalid timestamp %q: %v", value, err)
	}
	return at
}
