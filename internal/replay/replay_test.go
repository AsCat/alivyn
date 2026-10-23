package replay

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPublishedScenarios(t *testing.T) {
	paths, err := filepath.Glob("../../scenarios/*.json")
	if err != nil || len(paths) < 3 {
		t.Fatalf("scenario files unavailable: %v, %v", paths, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Verify(fixture); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQuietHoursCrossMidnight(t *testing.T) {
	for _, tc := range []struct {
		at   string
		want bool
	}{
		{"2026-10-26T21:59:00+08:00", false},
		{"2026-10-26T22:00:00+08:00", true},
		{"2026-10-27T07:59:00+08:00", true},
		{"2026-10-27T08:00:00+08:00", false},
	} {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		got, err := isQuiet(at, "22:00", "08:00")
		if err != nil || got != tc.want {
			t.Fatalf("%s: quiet=%v, err=%v", tc.at, got, err)
		}
	}
}

func TestConversationCancellationSuppressesFollowUp(t *testing.T) {
	for _, userText := range []string{"面试取消了", "忘记面试"} {
		fixture := Fixture{
			Timezone: "Asia/Shanghai",
			Policy:   Policy{QuietStart: "22:00", QuietEnd: "08:00", DailyLimit: 2},
			Events: []Event{
				{At: "2026-10-26T10:00:00+08:00", Type: "user_message", SourceText: "周三下午有面试"},
				{At: "2026-10-27T12:00:00+08:00", Type: "user_message", SourceText: userText},
				{At: "2026-10-28T16:00:00+08:00", Type: "clock_tick"},
			},
		}
		decisions, err := Run(fixture)
		if err != nil || len(decisions) != 1 || decisions[0].DeliveryCount != 0 || decisions[0].Reason != "superseded intent" {
			t.Fatalf("%q produced contact: %+v, %v", userText, decisions, err)
		}
	}
}
