package character

import (
	"testing"
	"time"
)

func TestMaxInitiativeIsObservableWithinMinutes(t *testing.T) {
	p := ProfileForInitiative(100)
	if p.FirstContactGap != 2*time.Minute || p.FollowupGap != 4*time.Minute || p.ConsiderationGap != 4*time.Minute || p.MaxUnanswered != 4 || p.RoutineDailyChecks != 48 {
		t.Fatalf("unexpected maximum initiative: %+v", p)
	}
	defaultProfile := ProfileForInitiative(25)
	if defaultProfile.FirstContactGap != 30*time.Minute || defaultProfile.FollowupGap != 2*time.Hour {
		t.Fatalf("default cadence changed: %+v", defaultProfile)
	}
}
