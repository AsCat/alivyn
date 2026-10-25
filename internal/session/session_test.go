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

type capturingClient struct{ messages []model.Message }

func (c *capturingClient) Complete(_ context.Context, _ string, messages []model.Message) (string, error) {
	c.messages = append([]model.Message(nil), messages...)
	return "好的", nil
}

type scriptedClient struct {
	replies []string
	calls   [][]model.Message
}

func TestWorldContextUsesRecordedEventsAndCanBeDisabled(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock"}, History: h}
	messages, _, err := s.contextMessages(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flatten(messages), "汐湾") || !strings.Contains(flatten(messages), "没有标签的录音") {
		t.Fatalf("fictional world not grounded in recorded event: %s", flatten(messages))
	}
	s.Config.WorldPack = "none"
	messages, _, err = s.contextMessages(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(flatten(messages), "汐湾") || strings.Contains(flatten(messages), "没有标签的录音") {
		t.Fatal("disabled world reached the model")
	}
}

func (c *scriptedClient) Complete(_ context.Context, _ string, messages []model.Message) (string, error) {
	c.calls = append(c.calls, append([]model.Message(nil), messages...))
	reply := c.replies[0]
	c.replies = c.replies[1:]
	return reply, nil
}

func TestWorldContactUsesExactSourcedEvent(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	start := time.Now().AddDate(0, 0, 1)
	if _, err := h.AdvanceGoalWorld(ctx, start, time.Local); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"那盒录音怎么处理？", "没授权前别公开。"} {
		if err := h.AddUserMessage(ctx, text, start.Add(time.Hour), time.Local); err != nil {
			t.Fatal(err)
		}
	}
	decision := actor.Decision{Act: "answer", Focus: "recording", EvidenceIDs: []string{"worldv2:1"}, Brevity: "short"}
	if err := h.SaveActorDecision(ctx, 2, decision, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.ConfirmActorDecision(ctx, 2, decision, actor.Review{Pass: true, UsedWorldIDs: []string{"worldv2:1"}}, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.AdvanceGoalWorld(ctx, start.AddDate(0, 0, 1), time.Local); err != nil {
		t.Fatal(err)
	}
	client := &capturingClient{}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", WorldPack: "none"}, History: h, Client: client}
	if _, err := s.ComposeContactFor(ctx, "world_update", "world:2"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flatten(client.messages), "采访年份") {
		t.Fatalf("contact did not include its exact source event: %s", flatten(client.messages))
	}
	if _, err := s.ComposeContactFor(ctx, "world_update", "world:1"); err == nil {
		t.Fatal("unrelated world event was accepted as a contact source")
	}
	if _, err := s.ComposeContactFor(ctx, "world_update", "world:bogus"); err == nil {
		t.Fatal("invalid world event key was accepted")
	}
}

func TestReplyUsesCurrentMemoryAndRespectsForget(t *testing.T) {
	ctx := context.Background()
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 10, 0, 0, 0, zone)
	for _, text := range []string{"周三下午有面试", "面试改到周五了"} {
		if err := h.AddUserMessage(ctx, text, at, zone); err != nil {
			t.Fatal(err)
		}
	}
	client := &capturingClient{}
	s := Session{Config: config.Config{Name: "Ari", Language: "zh-CN", Model: "mock"}, History: h, Client: client}
	if _, err := s.GenerateReply(ctx); err != nil {
		t.Fatal(err)
	}
	joined := flatten(client.messages)
	if !strings.Contains(joined, "2026-10-30") || strings.Contains(joined, "周三下午有面试") || !strings.Contains(joined, "source message") {
		t.Fatalf("model prompt did not use corrected source-linked memory: %s", joined)
	}
	if err := h.AddUserMessage(ctx, "面试取消了", at, zone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateReply(ctx); err != nil {
		t.Fatal(err)
	}
	joined = flatten(client.messages)
	if !strings.Contains(joined, "面试取消了") || strings.Contains(joined, "2026-10-30") {
		t.Fatalf("cancellation was not reflected in reply context: %s", joined)
	}
	if err := h.AddUserMessage(ctx, "忘记面试", at, zone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateReply(ctx); err != nil {
		t.Fatal(err)
	}
	joined = flatten(client.messages)
	if strings.Contains(joined, "2026-10-30") || strings.Contains(joined, "周五") || strings.Contains(joined, "周三") {
		t.Fatalf("forgotten date reached model: %s", joined)
	}
	if err := h.AddUserMessage(ctx, "我想再聊聊面试", at, zone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateReply(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flatten(client.messages), "我想再聊聊面试") {
		t.Fatal("new conversation about a forgotten topic was hidden")
	}
}

func TestReplyRecallsPersonalFactBeyondRecentTranscript(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "我不喜欢咖啡", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 31; i++ {
		if err := h.Add(ctx, "assistant", "普通聊天", at); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.AddUserMessage(ctx, "你记得我喜欢咖啡吗？", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	client := &capturingClient{}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock"}, History: h, Client: client}
	if _, err := s.ComposeReply(ctx); err != nil {
		t.Fatal(err)
	}
	joined := flatten(client.messages)
	if !strings.Contains(joined, "我不喜欢咖啡") || !strings.Contains(joined, "user message #") {
		t.Fatalf("personal fact was not retrieved: %s", joined)
	}
	if err := h.AddUserMessage(ctx, "忘记我不喜欢咖啡", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ComposeReply(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(flatten(client.messages), "我不喜欢咖啡") {
		t.Fatal("forgotten preference reached model")
	}
}

func TestProactiveCompositionUsesTheSameIdentityAndReason(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	client := &capturingClient{}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock"}, History: h, Client: client}
	if _, err := s.ComposeContact(ctx, "time_gap"); err != nil {
		t.Fatal(err)
	}
	joined := flatten(client.messages)
	if !strings.Contains(joined, "意续") || !strings.Contains(joined, "resumed after a long gap") {
		t.Fatalf("proactive message lacked identity or reason: %s", joined)
	}
	if _, err := s.ComposeContact(ctx, "unknown"); err == nil {
		t.Fatal("unknown contact reason accepted")
	}
}

func TestBareGreetingAvoidsCannedModelReplyAndNoticesRepetition(t *testing.T) {
	if !bareGreeting("你好！") || !bareGreeting("Hello.") || bareGreeting("你好，我有事想说") {
		t.Fatal("greeting classification changed")
	}
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	firstAt := time.Now().Add(-26 * time.Hour)
	if err := h.AddUserMessage(ctx, "你好", firstAt, time.Local); err != nil {
		t.Fatal(err)
	}
	client := &capturingClient{}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock"}, History: h, Client: client}
	first, err := s.ComposeReply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first != "你好，你是？" || len(client.messages) != 0 {
		t.Fatalf("bare greeting was sent through a generic model prompt: %q %+v", first, client.messages)
	}
	if err := h.Add(ctx, "assistant", first, firstAt); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "你好", firstAt.Add(time.Hour), time.Local); err != nil {
		t.Fatal(err)
	}
	second, err := s.ComposeReply(ctx)
	if err != nil || second != "你好。" || len(client.messages) != 0 {
		t.Fatalf("repeated greeting lost relationship context: %q %+v %v", second, client.messages, err)
	}
	if err := h.Add(ctx, "assistant", second, firstAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "你好", time.Now(), time.Local); err != nil {
		t.Fatal(err)
	}
	third, err := s.ComposeReply(ctx)
	if err != nil || third == "你好，你是？" {
		t.Fatalf("greeting forgot an earlier introduction after a day: %q %v", third, err)
	}
}

func TestGenericEmpathyGetsAConcreteRewrite(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "今天方案被否了。", time.Now(), time.Local); err != nil {
		t.Fatal(err)
	}
	client := &scriptedClient{replies: []string{"心里肯定不太好受。有时候是方向不合适，你觉得呢？", "谁否的？他给理由了吗？"}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock"}, History: h, Client: client}
	reply, err := s.ComposeReply(ctx)
	if err != nil || reply != "谁否的？他给理由了吗？" || len(client.calls) != 2 {
		t.Fatalf("generic draft was not rewritten: %q %d %v", reply, len(client.calls), err)
	}
	if !strings.Contains(flatten(client.calls[1]), "模板感") {
		t.Fatal("second model call had no concrete voice critique")
	}
}

func TestCustomBackgroundShapesFirstContactInsteadOfFixedGreeting(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.AddUserMessage(ctx, "你好", time.Now(), time.Local); err != nil {
		t.Fatal(err)
	}
	client := &scriptedClient{replies: []string{"你好，认识一下？"}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Background: "开朗但不会自来熟，碰到陌生人会先好奇对方是谁。"}, History: h, Client: client}
	reply, err := s.ComposeReply(ctx)
	if err != nil || reply != "你好，认识一下？" || len(client.calls) != 1 {
		t.Fatalf("custom background was bypassed: %q %v", reply, err)
	}
	joined := flatten(client.calls[0])
	if !strings.Contains(joined, "开朗但不会自来熟") || !strings.Contains(joined, "关系阶段：陌生") {
		t.Fatalf("first-contact prompt omitted character or stage: %s", joined)
	}
}

func TestRecognitionProbeUsesPreviousReplyAndRewritesTechnicalVoice(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	if err := h.AddUserMessage(ctx, "是我啊", at, time.Local); err != nil {
		t.Fatal(err)
	}
	if err := h.Add(ctx, "assistant", "给我点提示？", at); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "你不记得我了吗", at, time.Local); err != nil {
		t.Fatal(err)
	}
	client := &scriptedClient{replies: []string{"抱歉，我这边没有留下线索。再给我点提示吧。", "你还没说你是谁呢，我怎么猜。"}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock", Background: "慢热，先观察对方"}, History: h, Client: client}
	reply, err := s.ComposeReply(ctx)
	if err != nil || reply != "你还没说你是谁呢，我怎么猜。" || len(client.calls) != 2 {
		t.Fatalf("technical recognition reply was not rewritten: %q %v", reply, err)
	}
	if !strings.Contains(flatten(client.calls[0]), "别再重复要提示") {
		t.Fatal("model was not told it had already asked for a clue")
	}
}

func TestFirstContactSocialBeatsUseActualContext(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	client := &capturingClient{}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Model: "mock"}, History: h, Client: client}
	for _, step := range []struct{ user, reply string }{
		{"你好", "你好，你是？"},
		{"是我啊", "你这是让我猜谜？"},
		{"你不记得我了吗", "你连名字都没说，我怎么认你。"},
		{"我今天在商场认识你的", "那不是我。我们只在这儿聊过。"},
	} {
		if err := h.AddUserMessage(ctx, step.user, time.Now(), time.Local); err != nil {
			t.Fatal(err)
		}
		got, err := s.GenerateReply(ctx)
		if err != nil || got != step.reply {
			t.Fatalf("%q: got %q, want %q (%v)", step.user, got, step.reply, err)
		}
	}
	if len(client.messages) != 0 {
		t.Fatal("first-contact social beats unexpectedly used the model")
	}
	state, err := h.Relationship(ctx)
	if err != nil || state.Stage != "stranger" {
		t.Fatalf("recognition probes created false familiarity: %+v %v", state, err)
	}
}

func flatten(messages []model.Message) string {
	var contents []string
	for _, message := range messages {
		contents = append(contents, message.Content)
	}
	return strings.Join(contents, "\n")
}
