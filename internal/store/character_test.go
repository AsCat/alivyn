package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

func TestCharacterStateCommitsWithReplyExpiresAndForgets(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 21, 0, 0, 0, time.UTC)
	if _, err := h.EnqueueFeishu(ctx, "one", "chat", "m1", "我叫小明", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	turn := character.Turn{Mode: "say", Reply: "小明，记住了。", StateUpdated: true, Moment: character.Moment{Feeling: "好奇", Attention: "小明刚才说的事", Scene: "在听歌"}}
	if err := h.SaveCharacterTurn(ctx, 1, turn, at); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFeishuReply(ctx, "one", "wrong reply", at); err == nil {
		t.Fatal("mismatched prepared reply committed")
	}
	if m, _, err := h.CharacterMomentBefore(ctx, 2, at); err != nil || m.Attention != "" {
		t.Fatal("failed commit changed state")
	}
	if err := h.SetFeishuReply(ctx, "one", turn.Reply, at); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		age                       time.Duration
		feeling, scene, attention bool
	}{
		{time.Hour, true, true, true}, {3 * time.Hour, true, true, true}, {7 * time.Hour, true, false, true}, {9 * time.Hour, false, false, true}, {73 * time.Hour, false, false, false},
	} {
		m, _, err := h.CharacterMomentBefore(ctx, 2, at.Add(tc.age))
		if err != nil || (m.Feeling != "") != tc.feeling || (m.Scene != "") != tc.scene || (m.Attention != "") != tc.attention {
			t.Fatalf("bad expiry at %v: %+v %v", tc.age, m, err)
		}
	}
	if err := h.AddUserMessage(ctx, "忘记我的名字", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, found, err := h.CharacterTurn(ctx, 1); err != nil || found {
		t.Fatal("forget left character state containing a name")
	}
}

func TestIntentionAndOpenThreadsSurviveRestartAndQuietTurnGetsReflected(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if err := h.AddUserMessage(ctx, "有件事还没说", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	t1 := character.Turn{Mode: "say", Reply: "明天再聊，我想歇会儿。", StateUpdated: true, Moment: character.Moment{Intention: "休息"}, OpenThreads: []character.OpenThread{{Subject: "用户有件事尚未说", Owner: "user", SourceIDs: []string{"chat:1"}}}}
	if err := h.SaveCharacterTurn(ctx, 1, t1, at); err != nil {
		t.Fatal(err)
	}
	if err := h.CommitCharacterReply(ctx, 1, t1.Reply, at); err != nil {
		t.Fatal(err)
	}
	h.Close()
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	state, _, err := h.CharacterStateBefore(ctx, 3, at.Add(time.Minute))
	if err != nil || state.Moment.Intention != "休息" || len(state.OpenThreads) != 1 {
		t.Fatal(state, err)
	}
	state, _, err = h.CharacterStateBefore(ctx, 3, at.Add(3*time.Hour))
	if err != nil || state.Moment.Intention != "" || len(state.OpenThreads) != 1 {
		t.Fatal("old intention did not expire independently", state, err)
	}
	if err := h.AddUserMessage(ctx, "不用再说那件事了", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.SaveCharacterTurn(ctx, 3, character.Turn{Mode: "wait"}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := h.CommitCharacterReply(ctx, 3, "", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	id, _, ready, err := h.NextCharacterReflection(ctx, at.Add(3*time.Minute))
	if err != nil || !ready || id != 3 {
		t.Fatalf("quiet ending never reflected: %d %t %v", id, ready, err)
	}
}

func TestCharacterWorldReceiptIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 21, 0, 0, 0, time.UTC)
	if _, err := h.AdvanceGoalWorld(ctx, at, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "今天怎么样", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	turn := character.Turn{Mode: "say", Reply: "接手了一批录音。", UsedWorldIDs: []string{"worldv2:1"}}
	if err := h.SaveCharacterTurn(ctx, 1, turn, at); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := h.CommitCharacterReply(ctx, 1, turn.Reply, at); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM goal_world_engagements WHERE source_message_id=1").Scan(&count); err != nil || count != 1 {
		t.Fatalf("world engagement missing/duplicated: %d %v", count, err)
	}
	entries, err := h.RecentForModel(ctx, 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("reply duplicated: %+v %v", entries, err)
	}
}

func TestReflectionWaitsForDeliveryAndCannotRestoreForgottenState(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	if _, err := h.EnqueueFeishu(ctx, "one", "chat", "m1", "我叫小明", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	turn := character.Turn{Mode: "say", Reply: "小明。"}
	if err := h.SaveCharacterTurn(ctx, 1, turn, at); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFeishuReply(ctx, "one", turn.Reply, at); err != nil {
		t.Fatal(err)
	}
	if _, _, ready, err := h.NextCharacterReflection(ctx, at.Add(time.Minute)); err != nil || ready {
		t.Fatalf("reflected unsent turn: %t %v", ready, err)
	}
	if err := h.MarkFeishuSent(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if _, _, ready, err := h.NextCharacterReflection(ctx, at.Add(time.Second)); err != nil || ready {
		t.Fatalf("reflected before quiet interval: %t %v", ready, err)
	}
	if id, _, ready, err := h.NextCharacterReflection(ctx, at.Add(time.Minute)); err != nil || !ready || id != 1 {
		t.Fatalf("missing reflection candidate: %d %t %v", id, ready, err)
	}
	turn.StateUpdated = true
	turn.Moment.Attention = "小明"
	if err := h.AddUserMessage(ctx, "忘记我的名字", at.Add(2*time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.SaveCharacterReflection(ctx, 1, turn); err != nil {
		t.Fatal(err)
	}
	if _, found, err := h.CharacterTurn(ctx, 1); err != nil || found {
		t.Fatalf("reflection resurrected forgotten data: %t %v", found, err)
	}
}

func TestForgetNameRedactsLaterRecallsAndCorrections(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	for i, text := range []string{"我叫小明，别叫我老板", "刚刚逗你的，其实我叫阿岚", "你还记得怎么叫我吗"} {
		if err := h.AddUserMessage(ctx, text, at.Add(time.Duration(i)*time.Minute), time.UTC); err != nil {
			t.Fatal(err)
		}
		if err := h.Add(ctx, "assistant", "阿岚，我记得你的名字。", at.Add(time.Duration(i)*time.Minute+time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.AddUserMessage(ctx, "忘记我的名字", at.Add(4*time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	entries, err := h.RecentForModel(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Content, "小明") || strings.Contains(e.Content, "阿岚") {
			t.Fatalf("name echo survived forgetting: %+v", e)
		}
	}
}
