package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/store"
)

type actorScript struct {
	replies []string
	calls   int
}

func (s *actorScript) Complete(_ context.Context, _ string, _ []model.Message) (string, error) {
	reply := s.replies[s.calls]
	s.calls++
	return reply, nil
}

func TestActorDecisionPersistsAcrossRetryAndForgetRemovesIt(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "我叫小明", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &actorScript{replies: []string{
		`{"act":"acknowledge","focus":"name shared","evidence_ids":["chat:1"],"brevity":"short"}`,
		"好，小明。", `{"pass":true,"issue":""}`,
		"好，小明。", `{"pass":true,"issue":""}`,
	}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "actor_v1", WorldPack: "none"}, History: h, Client: client}
	if _, err := s.ComposeReplyAt(ctx, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ComposeReplyAt(ctx, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if client.calls != 5 {
		t.Fatalf("retry replanned instead of reusing decision: %d calls", client.calls)
	}
	if _, found, err := h.ActorDecision(ctx, 1); err != nil || !found {
		t.Fatalf("decision was not saved: found=%v err=%v", found, err)
	}
	if err := h.AddUserMessage(ctx, "忘记我的名字", at.Add(2*time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, found, err := h.ActorDecision(ctx, 1); err != nil || found {
		t.Fatalf("forget left derived decision behind: found=%v err=%v", found, err)
	}
}

func TestActorRetriesEmptyDraftWithoutReplanning(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "今天有点累", time.Now(), time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &actorScript{replies: []string{
		`{"act":"acknowledge","focus":"tired today","evidence_ids":["chat:1"],"brevity":"short"}`,
		"", "嗯，今天累了。", `{"pass":true,"issue":""}`,
	}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "actor_v1", WorldPack: "none"}, History: h, Client: client}
	reply, err := s.ComposeReply(ctx)
	if err != nil || reply != "嗯，今天累了。" || client.calls != 4 {
		t.Fatalf("empty draft did not recover: reply=%q calls=%d err=%v", reply, client.calls, err)
	}
}

func TestActorReplansWhenWordingCannotPassReview(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "今天团队把我的方案否了。", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &actorScript{replies: []string{
		`{"act":"acknowledge","focus":"proposal rejected","evidence_ids":["chat:1"],"brevity":"short"}`,
		"你一定很难过。", `{"pass":false,"issue":"Assigned a feeling the user did not state","used_world_ids":[]}`,
		"听起来你很难过。", `{"pass":false,"issue":"Still assigned a feeling","used_world_ids":[]}`,
		`{"act":"ask","focus":"team's reason","evidence_ids":["chat:1"],"brevity":"short"}`,
		"",
		"他们具体怎么说的？", `{"pass":true,"issue":"","used_world_ids":[]}`,
	}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "actor_v1", WorldPack: "none"}, History: h, Client: client}
	reply, err := s.ComposeReplyAt(ctx, at)
	if err != nil || reply != "他们具体怎么说的？" || client.calls != 9 {
		t.Fatalf("alternate approach failed: reply=%q calls=%d err=%v", reply, client.calls, err)
	}
	decision, found, err := h.ActorDecision(ctx, 1)
	if err != nil || !found || decision.Act != "ask" {
		t.Fatalf("rejected plan was still stored: %+v found=%v err=%v", decision, found, err)
	}
}

func TestActorRemovesUnsupportedClauseAfterReplan(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "睡了吗", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &actorScript{replies: []string{
		`{"act":"answer","focus":"answer whether awake","evidence_ids":["chat:1"],"brevity":"short"}`,
		"没睡，手上还有点没收完的东西。", `{"pass":false,"issue":"invented current activity"}`,
		"没睡，正忙着呢。", `{"pass":false,"issue":"still invents current activity"}`,
		`{"act":"answer","focus":"answer directly","evidence_ids":["chat:1"],"brevity":"short"}`,
		"没睡，刚把录音关了。", `{"pass":false,"issue":"invented current activity"}`,
		"还没睡。", `{"pass":true,"issue":""}`,
	}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Engine: "actor_v1", WorldPack: "none"}, History: h, Client: client}
	reply, err := s.ComposeReplyAt(ctx, at)
	if err != nil || reply != "还没睡。" || client.calls != 10 {
		t.Fatalf("final revision failed: reply=%q calls=%d err=%v", reply, client.calls, err)
	}
}

func TestActorWorldEvidenceKeepsCausalObject(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	first := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "你今天忙什么？", first, time.UTC); err != nil {
		t.Fatal(err)
	}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "actor_v1", WorldPack: "tidehaven"}, History: h}
	if _, err := s.actorFrame(ctx, first, 0); err != nil {
		t.Fatal(err)
	}
	decision := actor.Decision{Act: "answer", Focus: "recording box", EvidenceIDs: []string{"worldv2:1"}, Brevity: "short"}
	if err := h.SaveActorDecision(ctx, 1, decision, first.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := h.ConfirmActorDecision(ctx, 1, decision, actor.Review{Pass: true, UsedWorldIDs: []string{"worldv2:1"}}, first.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "那盒录音后来怎样？", first.AddDate(0, 0, 1), time.UTC); err != nil {
		t.Fatal(err)
	}
	frame, err := s.actorFrame(ctx, first.AddDate(0, 0, 1), 0)
	if err != nil {
		t.Fatal(err)
	}
	var worldEvidence []string
	var objects []string
	for _, item := range frame.Evidence {
		if item.Provenance == "simulated_world" {
			worldEvidence = append(worldEvidence, item.ParentID)
			objects = append(objects, item.ObjectID)
		}
	}
	if len(objects) != 2 || objects[0] != objects[1] || worldEvidence[1] != "worldv2:1" || frame.WorldState == nil || !frame.WorldState.LedgerChecked || frame.WorldState.IdentityVerified || frame.WorldState.PublicationAuthorized {
		t.Fatalf("actor received disconnected world events: %+v", frame.Evidence)
	}
}

func TestActorContactContextReadsTheSameGoalLedger(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "actor_v1", WorldPack: "tidehaven"}, History: h}
	messages, _, err := s.contextMessages(ctx, time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC))
	if err != nil || len(messages) == 0 || !strings.Contains(messages[0].Content, "recording-batch:1") {
		t.Fatalf("contact context lost the actor world: %+v err=%v", messages, err)
	}
	goalEvents, err := h.GoalWorldEvents(ctx, 10)
	if err != nil || len(goalEvents) != 1 {
		t.Fatalf("actor world was not persisted: %+v err=%v", goalEvents, err)
	}
	legacy, err := h.WorldEvents(ctx, 10)
	if err != nil || len(legacy) != 0 {
		t.Fatalf("contact context advanced a second world: %+v err=%v", legacy, err)
	}
}

func TestActorFrameKeepsEarlySubstantiveTurnAcrossRepetitiveSmallTalk(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "我周末打算去看摄影展。", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := h.AddUserMessage(ctx, "嗨", at.Add(time.Duration(i+1)*time.Minute), time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.AddUserMessage(ctx, "我之前说周末要去哪里？", at.Add(11*time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "actor_v1", WorldPack: "none"}, History: h}
	frame, err := s.actorFrame(ctx, at.Add(11*time.Minute), 0)
	if err != nil {
		t.Fatal(err)
	}
	if frame.OmittedUserTurns != 2 {
		t.Fatalf("omitted turn count = %d, want 2", frame.OmittedUserTurns)
	}
	found := false
	for _, evidence := range frame.Evidence {
		if evidence.ID == "chat:1" && evidence.Text == "我周末打算去看摄影展。" {
			found = true
		}
	}
	if !found {
		t.Fatal("early substantive turn was crowded out by greetings")
	}
}
