package actor

import (
	"context"
	"strings"
	"testing"

	"github.com/AsCat/alivyn/internal/model"
)

type fakeCompleter struct{ response string }

func (f fakeCompleter) Complete(context.Context, string, []model.Message) (string, error) {
	return f.response, nil
}

func TestDecisionRejectsInventedEvidence(t *testing.T) {
	frame := Frame{Latest: Evidence{ID: "chat:7"}, Evidence: []Evidence{{ID: "world:2"}}}
	good := `{"act":"share","focus":"recording repair","evidence_ids":["world:2"],"brevity":"short"}`
	if _, err := ParseDecision(good, frame); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(good, "world:2", "world:999", 1)
	if _, err := ParseDecision(bad, frame); err == nil {
		t.Fatal("invented world event was accepted")
	}
}

func TestReviewEnforcesChosenActWithoutPhraseBlacklist(t *testing.T) {
	frame := Frame{Latest: Evidence{ID: "chat:1"}}
	decision := Decision{Act: "acknowledge", Focus: "greet", EvidenceIDs: []string{"chat:1"}, Brevity: "short"}
	review, err := Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":""}`}, "mock", frame, decision, "你是谁？")
	if err != nil || review.Pass {
		t.Fatalf("question escaped chosen act: %+v %v", review, err)
	}
	review, err = Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":""}`}, "mock", frame, decision, "嗨。")
	if err != nil || !review.Pass {
		t.Fatalf("brief greeting was rejected: %+v %v", review, err)
	}
}
