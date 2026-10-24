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

type captureCompleter struct{ messages []model.Message }

func (c *captureCompleter) Complete(_ context.Context, _ string, messages []model.Message) (string, error) {
	c.messages = messages
	return "你好。", nil
}

func TestRealizationOnlyReceivesRelevantWorldContext(t *testing.T) {
	frame := Frame{
		Name: "意续", Language: "zh-CN", Reality: "AI character; world is fictional", Canon: "internal world biography", Abilities: "internal competence notes",
		WorldGoal: "internal project goal", Latest: Evidence{ID: "chat:1", Provenance: "observed_chat", Text: "你好"},
		Evidence: []Evidence{{ID: "worldv2:1", Provenance: "simulated_world", Text: "修复了一盒录音", ObjectID: "batch:1"}},
	}
	client := &captureCompleter{}
	if _, err := Realize(context.Background(), client, "mock", frame,
		Decision{Act: "acknowledge", Focus: "greeting", Brevity: "short", EvidenceIDs: []string{"chat:1"}}, ""); err != nil {
		t.Fatal(err)
	}
	private := client.messages[1].Content
	if strings.Contains(private, "internal world biography") || strings.Contains(private, "internal competence notes") || strings.Contains(private, "internal project goal") || strings.Contains(private, "修复了一盒录音") || strings.Contains(private, "world_state") {
		t.Fatalf("unrelated internal context reached greeting realization: %s", private)
	}
	if strings.Contains(private, "greeting") || strings.Contains(private, "focus") {
		t.Fatalf("planner wording reached realization: %s", private)
	}
	if _, err := Realize(context.Background(), client, "mock", frame,
		Decision{Act: "answer", Focus: "recording", Brevity: "short", EvidenceIDs: []string{"worldv2:1"}}, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(client.messages[1].Content, "修复了一盒录音") {
		t.Fatal("selected world event was hidden from realization")
	}
	if _, err := Realize(context.Background(), client, "mock", frame,
		Decision{Act: "answer", Focus: "is the world real", Brevity: "short", TruthBoundary: true, EvidenceIDs: []string{"worldv2:1"}}, ""); err != nil {
		t.Fatal(err)
	}
	private = client.messages[1].Content
	if !strings.Contains(private, "AI character; world is fictional") || strings.Contains(private, "修复了一盒录音") {
		t.Fatalf("reality answer leaked fictional daily event: %s", private)
	}
}

func TestRealizationTreatsPreviousReplyAsContinuityNotExample(t *testing.T) {
	frame := Frame{
		Name: "意续", Language: "zh-CN", PriorUserTurns: 12, OmittedUserTurns: 3,
		Latest:   Evidence{ID: "chat:13", Provenance: "observed_chat", Text: "我上次说要去哪里？"},
		Evidence: []Evidence{{ID: "chat:1", Provenance: "observed_chat", Text: "我周末要去看摄影展。"}},
		Recent: []model.Message{
			{Role: "user", Content: "在听吗"},
			{Role: "assistant", Content: "在，想说什么就说。"},
			{Role: "user", Content: "我上次说要去哪里？"},
		},
	}
	client := &captureCompleter{}
	_, err := Realize(context.Background(), client, "mock", frame,
		Decision{Act: "answer", Focus: "remembered outing", EvidenceIDs: []string{"chat:1"}, Brevity: "short"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.messages) != 3 || client.messages[2].Role != "user" {
		t.Fatalf("previous assistant turn became a style example: %+v", client.messages)
	}
	private := client.messages[1].Content
	if !strings.Contains(private, `"recall_incomplete":true`) || !strings.Contains(private, `"previous_reply":"在，想说什么就说。"`) {
		t.Fatalf("continuity or recall limit missing from private context: %s", private)
	}
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

func TestReviewAllowsOneNaturalQuestionButRejectsStacking(t *testing.T) {
	frame := Frame{Latest: Evidence{ID: "chat:1"}}
	decision := Decision{Act: "acknowledge", Focus: "greet", EvidenceIDs: []string{"chat:1"}, Brevity: "short"}
	review, err := Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":""}`}, "mock", frame, decision, "你是谁？你从哪里来？")
	if err != nil || review.Pass {
		t.Fatalf("stacked questions passed: %+v %v", review, err)
	}
	review, err = Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":""}`}, "mock", frame, decision, "嗨，怎么称呼？")
	if err != nil || !review.Pass {
		t.Fatalf("one natural question was rejected: %+v %v", review, err)
	}
}

func TestReviewOnlyAcceptsSelectedWorldUse(t *testing.T) {
	frame := Frame{Latest: Evidence{ID: "chat:1", Text: "你好"}, Evidence: []Evidence{{ID: "worldv2:1", Provenance: "simulated_world", Text: "一盒录音"}}}
	decision := Decision{Act: "acknowledge", Focus: "greet", EvidenceIDs: []string{"chat:1"}, Brevity: "short"}
	_, err := Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":"","used_world_ids":["worldv2:1"]}`}, "mock", frame, decision, "你好。")
	if err == nil {
		t.Fatal("review counted a world event the decision did not select")
	}
	decision.EvidenceIDs = append(decision.EvidenceIDs, "worldv2:1")
	review, err := Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":"","used_world_ids":[]}`}, "mock", frame, decision, "你好。")
	if err != nil || !review.Pass || len(review.UsedWorldIDs) != 0 {
		t.Fatalf("greeting incorrectly used private world context: %+v err=%v", review, err)
	}
	decision.TruthBoundary = true
	_, err = Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":"","used_world_ids":["worldv2:1"]}`}, "mock", frame, decision, "汐湾是虚构的。")
	if err == nil {
		t.Fatal("reality-boundary answer used a fictional daily event")
	}
}
