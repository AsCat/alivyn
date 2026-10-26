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
	turn := character.Turn{Mode: "say", Reply: "小明，记住了。", StateUpdated: true, StateVersion: 2, Moment: character.Moment{Feeling: "好奇", Attention: "小明刚才说的事", Scene: "在听歌"}, Episode: &character.Episode{Event: "对方说自己叫小明", Effect: "我记住了这个称呼", SourceIDs: []string{"chat:1"}}}
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
	if episodes, err := h.CharacterEpisodesBefore(ctx, 2); err != nil || len(episodes) != 1 || episodes[0].Event != "对方说自己叫小明" {
		t.Fatalf("committed episode missing: %+v %v", episodes, err)
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
	if episodes, err := h.CharacterEpisodesBefore(ctx, 3); err != nil || len(episodes) != 0 {
		t.Fatalf("forget left autobiographical episode: %+v %v", episodes, err)
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
	t1 := character.Turn{Mode: "say", Reply: "明天再聊，我想歇会儿。", StateUpdated: true, StateVersion: 2, Moment: character.Moment{Intention: "休息"}, OpenThreads: []character.OpenThread{{Subject: "用户有件事尚未说", Owner: "user", SourceIDs: []string{"chat:1"}}}}
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

func TestConversationalClosePersistsThroughReflectionAndExpires(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 16, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "先这样吧", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.SaveCharacterTurn(ctx, 1, character.Turn{Mode: "close", Reply: "嗯，回头聊。"}, at); err != nil {
		t.Fatal(err)
	}
	if err := h.CommitCharacterReply(ctx, 1, "嗯，回头聊。", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := h.SaveCharacterReflection(ctx, 1, character.Turn{Mode: "say", Reply: "嗯，回头聊。", StateUpdated: true, Moment: character.Moment{Feeling: "平静"}}); err != nil {
		t.Fatal(err)
	}
	if saved, found, err := h.CharacterTurn(ctx, 1); err != nil || !found || saved.Mode != "close" {
		t.Fatalf("reflection rewrote the executed boundary: %+v %t %v", saved, found, err)
	}
	if closed, err := h.RecentClosedExchange(ctx, 3, at.Add(time.Minute)); err != nil || closed.IsZero() {
		t.Fatalf("the next contribution lost the boundary: %v %v", closed, err)
	}
	if err := h.AddUserMessage(ctx, "嗯", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.SaveCharacterTurn(ctx, 3, character.Turn{Mode: "wait"}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := h.CommitCharacterReply(ctx, 3, "", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if closed, err := h.RecentClosedExchange(ctx, 4, at.Add(2*time.Minute)); err != nil || closed.IsZero() {
		t.Fatalf("quiet continuation erased the earlier closure: %v %v", closed, err)
	}
	if closed, err := h.RecentClosedExchange(ctx, 4, at.Add(5*time.Hour)); err != nil || !closed.IsZero() {
		t.Fatalf("old boundary did not expire: %v %v", closed, err)
	}
}

func TestCharacterSelfFactsRequireDeliveredSourceAndSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 26, 16, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "你是哪里人？", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	spoken := character.Turn{Mode: "say", Reply: "我在汐湾长大。"}
	if err := h.SaveCharacterTurn(ctx, 1, spoken, at); err != nil {
		t.Fatal(err)
	}
	snapshot := spoken
	snapshot.StateUpdated = true
	snapshot.SelfFacts = []character.SelfFact{{Key: "hometown", Value: "汐湾"}}
	if err := h.SaveCharacterReflection(ctx, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	if facts, err := h.CharacterSelfFactsBefore(ctx, 2); err != nil || len(facts) != 0 {
		t.Fatalf("uncommitted self facts leaked: %+v %v", facts, err)
	}
	if err := h.CommitCharacterReply(ctx, 1, spoken.Reply, at); err != nil {
		t.Fatal(err)
	}
	if err := h.SaveCharacterReflection(ctx, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if facts, err := h.CharacterSelfFactsBefore(ctx, 1); err != nil || len(facts) != 0 {
		t.Fatalf("source saw its own future fact: %+v %v", facts, err)
	}
	if facts, err := h.CharacterSelfFactsBefore(ctx, 2); err != nil || len(facts) != 1 || facts[0].Key != "hometown" || facts[0].Value != "汐湾" {
		t.Fatalf("delivered fact did not persist: %+v %v", facts, err)
	}
	if _, err := h.db.ExecContext(ctx, "DELETE FROM character_turns WHERE source_message_id=1"); err != nil {
		t.Fatal(err)
	}
	if facts, err := h.CharacterSelfFactsBefore(ctx, 2); err != nil || len(facts) != 0 {
		t.Fatalf("forgotten source left a biography fact: %+v %v", facts, err)
	}
}

func TestLegacyUnreviewedReflectionDoesNotEnterNewContext(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 16, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "你好", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	legacy := character.Turn{Mode: "say", Reply: "嗯。", StateUpdated: true, Moment: character.Moment{Cause: "未经证实的用户动机"}, OpenThreads: []character.OpenThread{{Subject: "过期的承诺", Owner: "character", SourceIDs: []string{"chat:1"}}}}
	if err := h.SaveCharacterTurn(ctx, 1, legacy, at); err != nil {
		t.Fatal(err)
	}
	if err := h.CommitCharacterReply(ctx, 1, legacy.Reply, at); err != nil {
		t.Fatal(err)
	}
	if state, _, err := h.CharacterStateBefore(ctx, 3, at.Add(time.Minute)); err != nil || state.Moment.Cause != "" || len(state.OpenThreads) != 0 {
		t.Fatalf("legacy interpretation entered new context: %+v %v", state, err)
	}
	if err := h.AddUserMessage(ctx, "新话题", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	current := character.Turn{Mode: "say", Reply: "好。", StateUpdated: true, StateVersion: 2, Moment: character.Moment{Feeling: "平静"}}
	if err := h.SaveCharacterTurn(ctx, 3, current, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := h.CommitCharacterReply(ctx, 3, current.Reply, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if state, _, err := h.CharacterStateBefore(ctx, 5, at.Add(2*time.Minute)); err != nil || state.Moment.Feeling != "平静" {
		t.Fatalf("new reviewed state not restored: %+v %v", state, err)
	}
}
