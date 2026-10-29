package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

func controlTestMessage(t *testing.T, h *History, text string, at time.Time) string {
	t.Helper()
	ctx := context.Background()
	if err := h.AddUserMessage(ctx, text, at, time.UTC); err != nil {
		t.Fatal(err)
	}
	r, found, err := h.CommandReply(ctx, 0, "zh-CN")
	if err != nil || !found {
		t.Fatalf("%q: %v %v", text, found, err)
	}
	return r
}

func TestInitiativeAndContactLimitCommandsPersistAndValidate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if got := controlTestMessage(t, h, "/主动性 100", at); !strings.Contains(got, "100/100") {
		t.Fatal(got)
	}
	if got := controlTestMessage(t, h, "/联系上限 5", at); !strings.Contains(got, "上限：5") {
		t.Fatal(got)
	}
	if got := controlTestMessage(t, h, "/安静时段 00:00 00:00", at); !strings.Contains(got, "全天不限制") {
		t.Fatal(got)
	}
	if got := controlTestMessage(t, h, "/主动性 101", at); !strings.Contains(got, "用法") {
		t.Fatal(got)
	}
	if got := controlTestMessage(t, h, "/联系上限 21", at); !strings.Contains(got, "用法") {
		t.Fatal(got)
	}
	if got := controlTestMessage(t, h, "/安静时段 27:00 00:00", at); !strings.Contains(got, "用法") {
		t.Fatal(got)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	tuning, err := h.CharacterTuning(ctx)
	if err != nil || tuning.Initiative != 100 {
		t.Fatal(tuning, err)
	}
	policy, err := h.ContactPolicy(ctx)
	if err != nil || policy.DailyLimit != 5 || policy.QuietStart != "00:00" || policy.QuietEnd != "00:00" {
		t.Fatal(policy, err)
	}
	if got := controlTestMessage(t, h, "/状态", at); !strings.Contains(got, "主动性：100/100") || !strings.Contains(got, "固定每日上限 5") {
		t.Fatal(got)
	}
	if got := controlTestMessage(t, h, "/联系自动", at); !strings.Contains(got, "自适应联系节奏") {
		t.Fatal(got)
	}
	if policy, err := h.ContactPolicy(ctx); err != nil || !policy.Adaptive || policy.DailyLimit != 12 {
		t.Fatal(policy, err)
	}
	if got := controlTestMessage(t, h, "/状态", at); !strings.Contains(got, "自适应节奏") {
		t.Fatal(got)
	}
	if got := controlTestMessage(t, h, "/initiative 0", at); !strings.Contains(got, "0/100") {
		t.Fatal(got)
	}
}

func TestDiagnosticEntryExpiryIsolationAndRestore(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if r := controlTestMessage(t, h, "/心境", at); !strings.Contains(r, "需先进入") {
		t.Fatal(r)
	}
	controlTestMessage(t, h, "/诊断", at)
	if r := controlTestMessage(t, h, "/确认诊断", at.Add(time.Minute)); !strings.Contains(r, "没有有效") {
		t.Fatal(r)
	}
	controlTestMessage(t, h, "/暂停", at)
	controlTestMessage(t, h, "/诊断", at)
	controlTestMessage(t, h, "/确认诊断", at.Add(time.Second))
	if r := controlTestMessage(t, h, "/心境", at); !strings.Contains(r, "暂无") {
		t.Fatal(r)
	}
	controlTestMessage(t, h, "我叫诊断秘密", at)
	if r := controlTestMessage(t, h, "/返回角色", at); !strings.Contains(r, "暂停状态") {
		t.Fatal(r)
	}
	controlTestMessage(t, h, "/勿扰", at)
	controlTestMessage(t, h, "/唤醒", at)
	p, err := h.ContactPolicy(ctx)
	if err != nil || !p.Paused {
		t.Fatal(p, err)
	}
	if entries, err := h.RecentForModel(ctx, 100); err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	var n int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM personal_memory").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	controlTestMessage(t, h, "/清除记忆", at)
	if r := controlTestMessage(t, h, "/状态", at); !strings.Contains(r, "人物运行中") {
		t.Fatal(r)
	}
}

func TestMindInspectionShowsSavedConnectionAndContactChoice(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if got := controlTestMessage(t, h, "/内心", at); !strings.Contains(got, "需先进入诊断") {
		t.Fatal(got)
	}
	if err := h.AddUserMessage(ctx, "今晚还在忙", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	entries, err := h.Recent(ctx, 1)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	sourceID := entries[0].ID
	turn := character.Turn{Mode: "say", Reply: "那我先陪你聊会儿。", StateUpdated: true, StateVersion: 2,
		Moment: character.Moment{Feeling: "有点好奇", Connection: "愿意再靠近一点", Intention: "想再说两句"}}
	if err := h.SaveCharacterTurn(ctx, sourceID, turn, at); err != nil {
		t.Fatal(err)
	}
	if err := h.CommitCharacterReply(ctx, sourceID, turn.Reply, at); err != nil {
		t.Fatal(err)
	}
	var throughID int64
	if err := h.db.QueryRowContext(ctx, "SELECT MAX(id) FROM messages").Scan(&throughID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO initiative_checks(source_message_id,through_message_id,at_ns,outcome,reason)
VALUES(?,?,?,'wait','想继续聊，但这一刻先留点空隙')`, sourceID, throughID, at.Add(time.Minute).UnixNano()); err != nil {
		t.Fatal(err)
	}
	controlTestMessage(t, h, "/诊断", at.Add(2*time.Minute))
	controlTestMessage(t, h, "/确认诊断", at.Add(2*time.Minute+time.Second))
	got := controlTestMessage(t, h, "/内心", at.Add(2*time.Minute+2*time.Second))
	for _, expected := range []string{"愿意再靠近一点", "想再说两句", "那时决定先不联系", "这一刻先留点空隙", "不代表此刻仍在实时思考"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("mind missing %q: %s", expected, got)
		}
	}
}

func TestPauseFencesPreparedAndInflightRepliesAcrossWakeAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	h, err := OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if _, err := h.EnqueueFeishu(ctx, "old", "chat", "m1", "听我说", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	old, _, _ := h.NextFeishu(ctx)
	turn := character.Turn{Mode: "say", Reply: "旧回复"}
	if err := h.SaveCharacterTurn(ctx, old.UserMessageID, turn, at); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFeishuReply(ctx, "old", turn.Reply, at); err != nil {
		t.Fatal(err)
	}
	controlTestMessage(t, h, "/freeze", at)
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if r := controlTestMessage(t, h, "/status", at); !strings.Contains(r, "人物已暂停") {
		t.Fatal(r)
	}
	controlTestMessage(t, h, "/wake", at)
	if err := h.SaveCharacterTurn(ctx, old.UserMessageID, turn, at); !errors.Is(err, ErrTurnSuperseded) {
		t.Fatal(err)
	}
	if err := h.CommitCharacterReply(ctx, old.UserMessageID, turn.Reply, at); !errors.Is(err, ErrTurnSuperseded) {
		t.Fatal(err)
	}
	if err := h.DeliverFeishu(ctx, "old", func() error { t.Fatal("old message delivered"); return nil }); err != nil {
		t.Fatal(err)
	}
	if entries, err := h.RecentForModel(ctx, 100); err != nil {
		t.Fatal(err)
	} else {
		for _, e := range entries {
			if e.Content == turn.Reply {
				t.Fatal("unsent text remains in context")
			}
		}
	}
	if err := h.AddUserMessage(ctx, "新消息", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	entries, _ := h.Recent(ctx, 1)
	if err := h.SaveCharacterTurn(ctx, entries[0].ID, turn, at); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedCommandsKeepTheirOwnSnapshotAndAreNotBatched(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	for i, text := range []string{"/诊断", "/确认诊断", "/心境", "/返回角色", "/心境"} {
		key := text + time.Duration(i).String()
		if _, err := h.EnqueueFeishu(ctx, key, "chat", key, text, at.Add(time.Duration(i)*time.Millisecond), time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	for i, expected := range []string{"60 秒", "诊断已开启", "暂无", "返回人物", "需先进入"} {
		item, ok, err := h.NextFeishuBatch(ctx, at.Add(time.Second))
		if err != nil || !ok {
			t.Fatal(i, ok, err)
		}
		r, found, err := h.CommandReply(ctx, item.UserMessageID, "zh-CN")
		if err != nil || !found || !strings.Contains(r, expected) {
			t.Fatal(i, r, err)
		}
		if err := h.SetFeishuReply(ctx, item.EventID, r, at); err != nil {
			t.Fatal(err)
		}
		if err := h.DeliverFeishu(ctx, item.EventID, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if entries, err := h.RecentForModel(ctx, 100); err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestForgettingClearsDiagnosticCopiesAndAbsenceReasons(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Now()
	if err := h.AddUserMessage(ctx, "我叫小岚", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	controlTestMessage(t, h, "/诊断", at)
	controlTestMessage(t, h, "/确认诊断", at)
	r := controlTestMessage(t, h, "/记忆", at)
	if !strings.Contains(r, "小岚") {
		t.Fatal(r)
	}
	entries, _ := h.Recent(ctx, 1)
	sourceID := entries[0].ID
	if err := h.CommitCharacterReply(ctx, sourceID, r, at); err != nil {
		t.Fatal(err)
	}
	controlTestMessage(t, h, "/返回角色", at)
	if _, err := h.db.Exec("INSERT INTO character_absence(id,source_message_id,kind,reason,until_ns) VALUES(1,1,'alone','小岚刚聊过的事',?)", at.Add(time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "忘记我的名字", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	r, _, err = h.CommandReply(ctx, sourceID, "zh-CN")
	if err != nil || strings.Contains(r, "小岚") {
		t.Fatal(r, err)
	}
	var n int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM character_absence").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestAwayBlocksAllProactiveGatesButWakePreservesQuietPolicy(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	seedInitiative(t, h, at)
	if _, err := h.db.Exec("INSERT INTO character_absence(id,source_message_id,kind,reason,until_ns) SELECT 1,MAX(id),'busy','自己的事',? FROM messages", at.Add(time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	check, err := h.ClaimInitiative(ctx, at, time.UTC)
	if err != nil || check.Ready || check.Reason != "character away" {
		t.Fatal(check, err)
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := evaluateIntentTx(ctx, tx, ContactIntent{Topic: "interview", Due: at, Expires: at.Add(time.Hour)}, at, time.UTC)
	tx.Rollback()
	if err != nil || d.Ready || d.Reason != "character away" {
		t.Fatal(d, err)
	}
	controlTestMessage(t, h, "/勿扰", at)
	controlTestMessage(t, h, "/唤醒", at)
	p, err := h.ContactPolicy(ctx)
	if err != nil || !p.Paused {
		t.Fatal(p, err)
	}
}

func TestExpiredUnsentClosingIsReconsideredButFreshRetryIsStable(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	if _, err := h.EnqueueFeishu(ctx, "night", "c", "m1", "再聊会儿", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	closing := character.Turn{Mode: "say", Reply: "我先睡了", Absence: &character.Absence{Kind: "sleep", Until: at.Add(7 * time.Hour).Format(time.RFC3339), Reason: "平时的休息时间"}}
	if err := h.SaveCharacterTurn(ctx, 1, closing, at); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFeishuReply(ctx, "night", closing.Reply, at); err != nil {
		t.Fatal(err)
	}
	morning := at.Add(7 * time.Hour)
	item, ok, err := h.NextFeishuBatch(ctx, morning)
	if err != nil || !ok || item.State != "pending" {
		t.Fatal(item, ok, err)
	}
	if _, found, err := h.CharacterTurn(ctx, 1); err != nil || found {
		t.Fatal("obsolete closing retained", err)
	}
	turn := character.Turn{Mode: "say", Reply: "早，昨天那件事怎么样了"}
	if err := h.SaveCharacterTurn(ctx, 1, turn, morning); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFeishuReply(ctx, "night", turn.Reply, morning); err != nil {
		t.Fatal(err)
	}
	item, ok, err = h.NextFeishuBatch(ctx, morning.Add(time.Minute))
	if err != nil || !ok || item.State != "ready" || item.Reply != turn.Reply {
		t.Fatal("fresh retry reset", item, ok, err)
	}
}

func TestWakeInvalidatesPendingSleepActionWithoutDroppingInput(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	if _, err := h.EnqueueFeishu(ctx, "old", "c", "m1", "在吗", at, time.UTC); err != nil {
		t.Fatal(err)
	}
	old := character.Turn{Mode: "say", Reply: "先睡了", Absence: &character.Absence{Kind: "sleep", Until: at.Add(7 * time.Hour).Format(time.RFC3339), Reason: "休息"}}
	if err := h.SaveCharacterTurn(ctx, 1, old, at); err != nil {
		t.Fatal(err)
	}
	controlTestMessage(t, h, "/wake", at.Add(time.Second))
	if err := h.SetFeishuReply(ctx, "old", old.Reply, at.Add(2*time.Second)); !errors.Is(err, ErrTurnSuperseded) {
		t.Fatal("wake overtaken by old sleep draft", err)
	}
	if err := h.SaveCharacterTurn(ctx, 1, old, at.Add(2*time.Second)); !errors.Is(err, ErrTurnSuperseded) {
		t.Fatal("old in-flight sleep survived wake", err)
	}
	item, ready, err := h.NextFeishuBatch(ctx, at.Add(5*time.Second))
	if err != nil || !ready || item.UserMessageID != 1 {
		t.Fatal(item, ready, err)
	}
	if _, found, err := h.CharacterTurn(ctx, 1); err != nil || found {
		t.Fatal("stale draft still cached", err)
	}
	current := character.Turn{Mode: "say", Reply: "在的"}
	if err := h.SaveCharacterTurn(ctx, 1, current, at.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFeishuReply(ctx, "old", current.Reply, at.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
}
