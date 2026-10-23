// Package contact contains the deterministic policy used by the live runtime
// and scenario replay. Generated text cannot change these decisions.
package contact

import (
	"errors"
	"fmt"
	"time"
)

type Policy struct {
	QuietStart string
	QuietEnd   string
	DailyLimit int
	Paused     bool
}

type Intent struct {
	Due     time.Time
	Expires time.Time
	Current bool
	Tried   bool
}

type Decision struct {
	Ready  bool
	Final  bool
	Reason string
}

func (p Policy) Validate() error {
	if p.DailyLimit < 1 || p.DailyLimit > 20 {
		return errors.New("daily contact limit must be between 1 and 20")
	}
	if _, err := time.Parse("15:04", p.QuietStart); err != nil {
		return fmt.Errorf("invalid quiet start: %w", err)
	}
	if _, err := time.Parse("15:04", p.QuietEnd); err != nil {
		return fmt.Errorf("invalid quiet end: %w", err)
	}
	return nil
}

func Evaluate(now time.Time, zone *time.Location, p Policy, intent Intent, sentToday int) Decision {
	if !intent.Current {
		return Decision{Final: true, Reason: "superseded intent"}
	}
	if intent.Tried {
		return Decision{Final: true, Reason: "duplicate attempt suppressed"}
	}
	if !now.Before(intent.Expires) {
		return Decision{Final: true, Reason: "expired intent is not backfilled"}
	}
	if now.Before(intent.Due) {
		return Decision{Reason: "not due"}
	}
	if p.Paused {
		return Decision{Reason: "paused"}
	}
	if Quiet(now.In(zone), p.QuietStart, p.QuietEnd) {
		return Decision{Reason: "quiet hours"}
	}
	if sentToday >= p.DailyLimit {
		return Decision{Reason: "daily contact limit"}
	}
	return Decision{Ready: true}
}

func Quiet(at time.Time, start, end string) bool {
	begin, beginErr := time.Parse("15:04", start)
	finish, finishErr := time.Parse("15:04", end)
	if beginErr != nil || finishErr != nil {
		return false
	}
	nowMinutes := at.Hour()*60 + at.Minute()
	startMinutes := begin.Hour()*60 + begin.Minute()
	endMinutes := finish.Hour()*60 + finish.Minute()
	if startMinutes == endMinutes {
		return false
	}
	if startMinutes < endMinutes {
		return nowMinutes >= startMinutes && nowMinutes < endMinutes
	}
	return nowMinutes >= startMinutes || nowMinutes < endMinutes
}
