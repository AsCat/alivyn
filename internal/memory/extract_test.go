package memory

import (
	"testing"
	"time"
)

func TestInterviewExtractionFromConversation(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 26, 10, 0, 0, 0, zone)
	cases := []struct {
		text      string
		action    Action
		date      string
		precision string
	}{
		{"周三下午有面试", Upsert, "2026-10-28", "part_of_day"},
		{"面试从周三改到周五了", Upsert, "2026-10-30", "day"},
		{"下周三下午有面试", Upsert, "2026-11-04", "part_of_day"},
		{"My interview moved to Friday", Upsert, "2026-10-30", "day"},
		{"面试取消了", Cancel, "", ""},
		{"请忘记我的面试", Forget, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			change, ok := Extract(tc.text, at, zone)
			if !ok || change.Topic != "interview" || change.Action != tc.action || change.Provenance != "user_stated" {
				t.Fatalf("unexpected change: %+v, ok=%v", change, ok)
			}
			if tc.date != "" && (change.HappensAt.In(zone).Format("2006-01-02") != tc.date || change.Precision != tc.precision) {
				t.Fatalf("wrong date or precision: %+v", change)
			}
		})
	}
	for _, text := range []string{"周三或周五有面试", "最近在准备面试", "周三去散步", "上周三有面试", "周五面试没通过"} {
		if _, ok := Extract(text, at, zone); ok {
			t.Fatalf("ambiguous or unrelated message was treated as a fact: %q", text)
		}
	}
}
