package contact

import (
	"testing"
	"time"
)

func TestEvaluateContactBoundaries(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 28, 16, 0, 0, 0, zone)
	base := Intent{Due: at, Expires: at.Add(5 * time.Hour), Current: true}
	p := Policy{QuietStart: "22:00", QuietEnd: "08:00", DailyLimit: 1}
	for _, tc := range []struct {
		name string
		at   time.Time
		p    Policy
		i    Intent
		sent int
		want string
	}{
		{"ready", at, p, base, 0, ""},
		{"not due", at.Add(-time.Minute), p, base, 0, "not due"},
		{"paused", at, Policy{QuietStart: "22:00", QuietEnd: "08:00", DailyLimit: 1, Paused: true}, base, 0, "paused"},
		{"quiet", at, Policy{QuietStart: "15:00", QuietEnd: "17:00", DailyLimit: 1}, base, 0, "quiet hours"},
		{"limit", at, p, base, 1, "daily contact limit"},
		{"expired", at.Add(5 * time.Hour), p, base, 0, "expired intent is not backfilled"},
		{"stale", at, p, Intent{Due: base.Due, Expires: base.Expires}, 0, "superseded intent"},
		{"duplicate", at, p, Intent{Due: base.Due, Expires: base.Expires, Current: true, Tried: true}, 0, "duplicate attempt suppressed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.at, zone, tc.p, tc.i, tc.sent)
			if got.Reason != tc.want || (tc.want == "" && !got.Ready) {
				t.Fatalf("unexpected decision: %+v", got)
			}
		})
	}
}
