package character

import (
	"context"
	"strings"
	"testing"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/model"
)

type reviewedScript struct {
	dialogue   []string
	reviews    []string
	secondCall []model.Message
}

func (s *reviewedScript) Complete(_ context.Context, _ string, messages []model.Message) (string, error) {
	if len(s.dialogue) == 1 {
		s.secondCall = append([]model.Message(nil), messages...)
	}
	r := s.dialogue[0]
	s.dialogue = s.dialogue[1:]
	return r, nil
}

func (s *reviewedScript) CompleteJSON(_ context.Context, _ string, _ []model.Message) (string, error) {
	r := s.reviews[0]
	s.reviews = s.reviews[1:]
	return r, nil
}

func TestIndependentReviewRevisesInconsistentDialogueAction(t *testing.T) {
	s := &reviewedScript{
		dialogue: []string{
			`{"mode":"say","reply":"我先走了，不聊了","absence":null}`,
			`{"mode":"close","reply":"先聊到这里吧","absence":null}`,
		},
		reviews: []string{
			`{"valid":false,"issue":"mode=say announces ending the exchange; use close or change the line"}`,
			`{"valid":true,"issue":""}`,
		},
	}
	c := Context{CanLeave: true, Frame: actor.Frame{Now: "2026-10-26T16:00:00+08:00", Language: "zh-CN"}}
	turn, err := GenerateChecked(context.Background(), s, "mock", c)
	if err != nil || turn.Mode != "close" || turn.Reply != "先聊到这里吧" {
		t.Fatal(turn, err)
	}
	joined := ""
	for _, m := range s.secondCall {
		joined += m.Content
	}
	if !strings.Contains(joined, "mode=say announces ending") {
		t.Fatal("the character did not receive concrete review feedback")
	}
}

func TestTimedDepartureWithClosingLineKeepsAwayMode(t *testing.T) {
	s := &reviewedScript{
		dialogue: []string{`{"mode":"away","reply":"晚安，我先睡了","absence":{"kind":"sleep","until":"2026-10-27T08:00:00+08:00","reason":"到了休息时间"}}`},
		reviews:  []string{`{"valid":true,"issue":""}`},
	}
	c := Context{CanLeave: true, Frame: actor.Frame{Now: "2026-10-27T01:00:00+08:00", Language: "zh-CN"}}
	turn, err := GenerateChecked(context.Background(), s, "mock", c)
	if err != nil || turn.Mode != "away" || turn.Absence == nil || turn.Reply != "晚安，我先睡了" {
		t.Fatalf("departure action lost: %+v %v", turn, err)
	}
}

func TestIndependentReviewRejectsTwoConflictingActions(t *testing.T) {
	s := &reviewedScript{
		dialogue: []string{
			`{"mode":"say","reply":"现在真的走了","absence":null}`,
			`{"mode":"say","reply":"我走了","absence":null}`,
		},
		reviews: []string{
			`{"valid":false,"issue":"spoken departure conflicts with say"}`,
			`{"valid":false,"issue":"spoken departure still conflicts with say"}`,
		},
	}
	_, err := GenerateChecked(context.Background(), s, "mock", Context{CanLeave: true, Frame: actor.Frame{Now: "2026-10-26T16:00:00+08:00"}})
	if err == nil {
		t.Fatal("sent a dialogue action that twice failed independent review")
	}
}

func TestReflectionReviewRevisesUnsupportedUserMotive(t *testing.T) {
	s := &reviewedScript{reviews: []string{
		`{"moment":{"feeling":"不耐烦","cause":"对方发数字试探我"},"open_threads":[],"used_world_ids":[]}`,
		`{"valid":false,"issue":"moment.cause treats a guess about why the user sent numbers as fact"}`,
		`{"moment":{"feeling":"有点不耐烦","cause":"对方接连发了数字，她只简短回应"},"open_threads":[],"used_world_ids":[]}`,
		`{"valid":true,"issue":""}`,
	}}
	c := Context{Frame: actor.Frame{Now: "2026-10-26T16:08:00+08:00", Language: "zh-CN"}}
	turn, err := Reflect(context.Background(), s, "mock", c, "先到这里吧")
	if err != nil || strings.Contains(turn.Moment.Cause, "试探") || !turn.StateUpdated {
		t.Fatal(turn, err)
	}
}
