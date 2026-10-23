// Package memory turns explicit user statements into conservative, source-linked facts.
// It never stores a model's private reasoning or guesses about user intent.
package memory

import (
	"strings"
	"time"
)

type Action string

const (
	Upsert Action = "upsert"
	Cancel Action = "cancel"
	Forget Action = "forget"
)

type Change struct {
	Topic      string
	Action     Action
	HappensAt  time.Time
	Precision  string
	Provenance string
}

// Extract recognizes a small set of explicit interview statements. Unknown or
// ambiguous phrasing produces no fact; a later model extractor can submit
// candidates separately and mark them as model_inferred.
func Extract(text string, at time.Time, zone *time.Location) (Change, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if !strings.Contains(lower, "面试") && !strings.Contains(lower, "interview") {
		return Change{}, false
	}
	base := Change{Topic: "interview", Provenance: "user_stated"}
	for _, marker := range []string{"忘记面试", "忘记我的面试", "忘掉面试", "忘掉我的面试", "忘了面试", "面试别记了", "forget my interview", "forget the interview"} {
		if strings.Contains(lower, marker) {
			base.Action = Forget
			return base, true
		}
	}
	for _, marker := range []string{"面试取消", "取消面试", "取消了面试", "面试不去了", "别再问面试", "不要再问面试", "interview is cancelled", "interview is canceled", "cancel the interview", "don't ask about the interview"} {
		if strings.Contains(lower, marker) {
			base.Action = Cancel
			return base, true
		}
	}
	for _, marker := range []string{"上周", "昨天", "前天", "面试结束", "面试没通过", "面试没去", "last week", "yesterday", "interview finished", "interview is over"} {
		if strings.Contains(lower, marker) {
			return Change{}, false
		}
	}
	weekdays := []struct {
		markers []string
		day     time.Weekday
	}{
		{[]string{"周一", "星期一", "monday"}, time.Monday},
		{[]string{"周二", "星期二", "tuesday"}, time.Tuesday},
		{[]string{"周三", "星期三", "wednesday"}, time.Wednesday},
		{[]string{"周四", "星期四", "thursday"}, time.Thursday},
		{[]string{"周五", "星期五", "friday"}, time.Friday},
		{[]string{"周六", "星期六", "saturday"}, time.Saturday},
		{[]string{"周日", "周天", "星期日", "星期天", "sunday"}, time.Sunday},
	}
	var found time.Weekday
	count := 0
	lastPosition := -1
	for _, weekday := range weekdays {
		for _, marker := range weekday.markers {
			if position := strings.LastIndex(lower, marker); position >= 0 {
				count++
				if position > lastPosition {
					lastPosition = position
					found = weekday.day
				}
				break
			}
		}
	}
	correction := strings.Contains(lower, "改到") || strings.Contains(lower, "改成") || strings.Contains(lower, "moved to") || strings.Contains(lower, "rescheduled to")
	if count == 0 || (count > 1 && !correction) || zone == nil {
		return Change{}, false
	}
	local := at.In(zone)
	days := (int(found) - int(local.Weekday()) + 7) % 7
	if strings.Contains(lower, "下周") || strings.Contains(lower, "next week") {
		localFromMonday := (int(local.Weekday()) + 6) % 7
		foundFromMonday := (int(found) + 6) % 7
		days = 7 + foundFromMonday - localFromMonday
	}
	date := local.AddDate(0, 0, days)
	hour := 15
	precision := "day"
	if strings.Contains(lower, "上午") || strings.Contains(lower, "morning") {
		hour, precision = 10, "part_of_day"
	} else if strings.Contains(lower, "下午") || strings.Contains(lower, "afternoon") {
		precision = "part_of_day"
	} else if strings.Contains(lower, "晚上") || strings.Contains(lower, "evening") {
		hour, precision = 19, "part_of_day"
	}
	when := time.Date(date.Year(), date.Month(), date.Day(), hour, 0, 0, 0, zone)
	if !when.After(local) {
		date = date.AddDate(0, 0, 7)
		when = time.Date(date.Year(), date.Month(), date.Day(), hour, 0, 0, 0, zone)
	}
	base.Action, base.HappensAt, base.Precision = Upsert, when, precision
	return base, true
}
