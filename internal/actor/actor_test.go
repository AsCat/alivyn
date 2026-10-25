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

type captureReviewCompleter struct{ messages []model.Message }

func (c *captureReviewCompleter) Complete(_ context.Context, _ string, messages []model.Message) (string, error) {
	c.messages = messages
	return `{"pass":true,"issue":"","used_world_ids":[]}`, nil
}

func TestRealizationOnlyReceivesRelevantWorldContext(t *testing.T) {
	frame := Frame{
		Name: "意续", PublicIdentity: "意续，在旧港档案室做录音修复。", Language: "zh-CN", Reality: "AI character; world is fictional", Canon: "internal world biography", Abilities: "internal competence notes",
		WorldGoal: "internal project goal", Latest: Evidence{ID: "chat:1", Provenance: "observed_chat", Text: "你好"},
		Evidence: []Evidence{{ID: "character:identity", Provenance: "character_identity", Text: "意续，在旧港档案室做录音修复。"}, {ID: "worldv2:1", Provenance: "simulated_world", Text: "修复了一盒录音", ObjectID: "batch:1"}},
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
	if !strings.Contains(private, `"focus":"greeting"`) || strings.Contains(private, `"public_identity"`) {
		t.Fatalf("chosen conversational focus was lost before realization: %s", private)
	}
	if _, err := Realize(context.Background(), client, "mock", frame,
		Decision{Act: "answer", Focus: "identity", Brevity: "short", EvidenceIDs: []string{"character:identity"}}, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(client.messages[1].Content, `"public_identity":"意续，在旧港档案室做录音修复。"`) {
		t.Fatal("selected stable identity was hidden from realization")
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
	if !strings.Contains(private, "AI character; world is fictional") || strings.Contains(private, "修复了一盒录音") || strings.Contains(private, `"public_identity"`) {
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

func TestRealizationReceivesEmotionalMoveAndOrderedDialogue(t *testing.T) {
	frame := Frame{
		Name: "意续", Language: "zh-CN", Relationship: "acquaintance",
		Latest:     Evidence{ID: "chat:5", Provenance: "observed_chat", Text: "……"},
		Continuity: &Continuity{Posture: "unsettled", Concern: "She wants to finish saying why the recording matters", SourceIDs: []string{"chat:3"}, Lifetime: "hours"},
		Recent: []model.Message{
			{Role: "user", Content: "那盒录音还在吗？"},
			{Role: "assistant", Content: "在。我还没想好要不要交出去。"},
			{Role: "user", Content: "……"},
		},
	}
	decision := Decision{Act: "pause", Focus: "leave room after the unresolved recording question", Want: "finish explaining the recording, but give the other person space", Feeling: "a little unsure", Brevity: "short", Posture: "unsettled"}
	client := &captureCompleter{}
	if _, err := Realize(context.Background(), client, "mock", frame, decision, ""); err != nil {
		t.Fatal(err)
	}
	private := client.messages[1].Content
	for _, want := range []string{`"focus":"leave room`, `"want":"finish explaining`, `"feeling":"a little unsure"`, `"unfinished_reaction":`, `"recent_dialogue":`, "那盒录音还在吗？", "我还没想好要不要交出去。"} {
		if !strings.Contains(private, want) {
			t.Fatalf("realization lost %q: %s", want, private)
		}
	}
	if len(client.messages) != 3 || client.messages[2].Role != "user" {
		t.Fatalf("dialogue was replayed as assistant-role style examples: %+v", client.messages)
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

func TestDecisionAllowsConversationalRepair(t *testing.T) {
	frame := Frame{Latest: Evidence{ID: "chat:3"}, Evidence: []Evidence{{ID: "chat:1"}}}
	raw := `{"act":"repair","focus":"own a mistaken assumption","evidence_ids":["chat:1","chat:3"],"brevity":"short"}`
	decision, err := ParseDecision(raw, frame)
	if err != nil || decision.Act != "repair" {
		t.Fatalf("repair could not be planned: %+v %v", decision, err)
	}
}

func TestRealityBoundaryRequiresExplicitRealityQuestion(t *testing.T) {
	for _, tc := range []struct {
		user string
		want bool
	}{
		{"你是谁啊", false},
		{"Who are you?", false},
		{"Do you really remember me?", false},
		{"你意识到我刚说什么了吗？", false},
		{"你是AI还是真人？", true},
		{"你是AI吗？", true},
		{"Is your world real?", true},
	} {
		frame := Frame{Latest: Evidence{ID: "chat:1", Text: tc.user}}
		decision, err := ParseDecision(`{"act":"answer","focus":"identity","evidence_ids":["chat:1"],"brevity":"short","truth_boundary":true}`, frame)
		if err != nil || decision.TruthBoundary != tc.want {
			t.Fatalf("%q: boundary=%t want=%t err=%v", tc.user, decision.TruthBoundary, tc.want, err)
		}
	}
}

func TestAmbiguousPunctuationCanBeLeftUnanswered(t *testing.T) {
	for _, input := range []string{"...", "。。。", "……", "—"} {
		if !ShouldHold(input) {
			t.Fatalf("%q should be held", input)
		}
	}
	for _, input := range []string{"你在吗？", "?", "!", "嗯...", "🙂"} {
		if ShouldHold(input) {
			t.Fatalf("%q has meaning that should reach the actor", input)
		}
	}
}

func TestDecisionKeepsOnlySourcedContinuity(t *testing.T) {
	frame := Frame{Latest: Evidence{ID: "chat:3"}, Evidence: []Evidence{{ID: "chat:1"}}}
	valid := `{"act":"repair","focus":"own misread","evidence_ids":["chat:1","chat:3"],"brevity":"short","posture":"repairing","carryover":{"posture":"repairing","concern":"I guessed a motive without evidence","source_ids":["chat:1"],"lifetime":"hours"}}`
	decision, err := ParseDecision(valid, frame)
	if err != nil || decision.Carryover == nil || decision.Carryover.Posture != "repairing" {
		t.Fatalf("sourced continuity was lost: %+v %v", decision, err)
	}
	invalid := strings.Replace(valid, `"source_ids":["chat:1"]`, `"source_ids":["chat:999"]`, 1)
	decision, err = ParseDecision(invalid, frame)
	if err != nil || decision.Carryover != nil {
		t.Fatalf("unsourced continuity survived: %+v %v", decision, err)
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
	if err != nil || review.Pass {
		t.Fatalf("acknowledgment grew an unplanned question: %+v %v", review, err)
	}
	decision.Act = "ask"
	review, err = Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":""}`}, "mock", frame, decision, "嗨，怎么称呼？")
	if err != nil || !review.Pass {
		t.Fatalf("planned natural question was rejected: %+v %v", review, err)
	}
}

func TestReviewOnlyAcceptsSelectedWorldUse(t *testing.T) {
	frame := Frame{Latest: Evidence{ID: "chat:1", Text: "你好"}, Evidence: []Evidence{{ID: "worldv2:1", Provenance: "simulated_world", Text: "一盒录音"}}}
	decision := Decision{Act: "acknowledge", Focus: "greet", EvidenceIDs: []string{"chat:1"}, Brevity: "short"}
	review, err := Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":"","used_world_ids":["worldv2:1"]}`}, "mock", frame, decision, "你好。")
	if err != nil || review.Pass {
		t.Fatalf("unselected world citation was not a recoverable review failure: %+v err=%v", review, err)
	}
	decision.EvidenceIDs = append(decision.EvidenceIDs, "worldv2:1")
	review, err = Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":"","used_world_ids":[]}`}, "mock", frame, decision, "你好。")
	if err != nil || !review.Pass || len(review.UsedWorldIDs) != 0 {
		t.Fatalf("greeting incorrectly used private world context: %+v err=%v", review, err)
	}
	decision.TruthBoundary = true
	review, err = Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":"","used_world_ids":["worldv2:1"]}`}, "mock", frame, decision, "汐湾是虚构的。")
	if err != nil || review.Pass {
		t.Fatalf("reality-boundary world use was not a review failure: %+v err=%v", review, err)
	}
}

func TestReviewIgnoresSelectedChatIDsMistakenForWorldUse(t *testing.T) {
	frame := Frame{Latest: Evidence{ID: "chat:5", Provenance: "observed_chat", Text: "今天在商场认识你"}}
	decision := Decision{Act: "clarify", Focus: "physical meeting boundary", EvidenceIDs: []string{"chat:5"}, Brevity: "short", TruthBoundary: true}
	review, err := Check(context.Background(), fakeCompleter{response: `{"pass":true,"issue":"","used_world_ids":["chat:5"]}`}, "mock", frame, decision, "我们只在这里聊过，没在线下见过。")
	if err != nil || !review.Pass || len(review.UsedWorldIDs) != 0 {
		t.Fatalf("chat evidence became a world event: %+v %v", review, err)
	}
}

func TestReviewReceivesOnlySelectedEvidence(t *testing.T) {
	frame := Frame{
		Latest:    Evidence{ID: "chat:9", Provenance: "observed_chat", Text: "你好"},
		WorldGoal: "private world goal", Canon: "private world canon",
		Evidence: []Evidence{
			{ID: "chat:7", Provenance: "observed_chat", Text: "在吗"},
			{ID: "worldv2:1", Provenance: "simulated_world", Text: "unselected recording event"},
		},
	}
	client := &captureReviewCompleter{}
	decision := Decision{Act: "acknowledge", Focus: "greet", EvidenceIDs: []string{"chat:9", "chat:7"}, Brevity: "short"}
	if _, err := Check(context.Background(), client, "mock", frame, decision, "嗨。"); err != nil {
		t.Fatal(err)
	}
	private := client.messages[1].Content
	if strings.Contains(private, "unselected recording event") || strings.Contains(private, "private world goal") || strings.Contains(private, "private world canon") {
		t.Fatalf("review saw context that could not ground this reply: %s", private)
	}
	if !strings.Contains(private, "在吗") {
		t.Fatalf("review lost selected chat evidence: %s", private)
	}
}
