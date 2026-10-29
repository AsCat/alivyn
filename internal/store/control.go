package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

// Controls are transport-independent, explicit user input. Model output never
// enters this interpreter. Responses are snapshotted in the same transaction
// as the action, so queued commands and transport retries cannot replay it.
func isControlInput(text string) bool { return strings.HasPrefix(strings.TrimSpace(text), "/") }

func commandName(text string) string {
	switch strings.TrimSpace(text) {
	case "/帮助", "/help":
		return "help"
	case "/状态", "/status":
		return "status"
	case "/暂停", "/freeze":
		return "freeze"
	case "/唤醒", "/wake":
		return "wake"
	case "/勿扰", "/quiet":
		return "quiet"
	case "/允许主动", "/contact on":
		return "contact_on"
	case "/测试主动", "/contact test":
		return "test_contact"
	case "/诊断", "/diagnostics":
		return "diagnostics"
	case "/确认诊断", "/diagnostics confirm":
		return "confirm"
	case "/取消", "/cancel":
		return "cancel"
	case "/人物", "/persona":
		return "persona"
	case "/作息", "/routine":
		return "routine"
	case "/记忆", "/memories":
		return "memories"
	case "/心境", "/state":
		return "state"
	case "/内心", "/mind":
		return "mind"
	case "/挂念", "/threads":
		return "threads"
	case "/返回角色", "/resume":
		return "resume"
	}
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) > 0 {
		switch parts[0] {
		case "/主动性", "/initiative":
			return "initiative"
		case "/安静时段":
			return "quiet_hours"
		case "/联系上限":
			return "contact_limit"
		case "/联系自动":
			return "contact_auto"
		case "/contact":
			if len(parts) > 1 && parts[1] == "auto" {
				return "contact_auto"
			}
			if len(parts) > 1 && parts[1] == "limit" {
				return "contact_limit"
			}
			if len(parts) > 1 && parts[1] == "hours" {
				return "quiet_hours"
			}
		}
	}
	return "unknown"
}

const helpZH = `【控制台】
/状态 — 查看运行状态和联系限制
/暂停 · /唤醒 — 暂停或恢复人物对话，保留记忆
/勿扰 · /允许主动 — 关闭或允许主动联系，仍遵守安静时段
/主动性 [0–100] — 查看或调整人物主动性；25 是默认值，100 可用于高频试验
/联系自动 — 按互动节奏决定何时主动联系（默认）；每天 12 条为防误发上限
/联系上限 [1–20] — 改为固定每日上限，适合需要严格限制时
/安静时段 [HH:MM HH:MM] — 查看或调整免打扰时间；00:00 00:00 表示关闭时段限制
/测试主动 — 立即试一次人物主动联系；不进入人物记忆
/诊断 — 申请进入诊断；60 秒内 /确认诊断，或 /取消
诊断内：/人物 · /作息 · /记忆 · /心境 · /挂念 · /内心
/返回角色 — 退出诊断，恢复进入前的状态
/清除记忆 — 立即清除本机人物经历和关系，恢复初始人物状态
命令需单独发送。诊断不会进入人物记忆；/帮助 随时查看。`

const helpEN = `Control console
/status — runtime and contact limits
/freeze · /wake — pause or resume character dialogue, preserving memory
/quiet · /contact on — disable or allow proactive contact, respecting quiet hours
/initiative [0–100] — show or adjust initiative; 25 is the default
/contact auto — adapt contact cadence to reciprocity (default); 12/day emergency ceiling
/contact limit [1–20] — set a strict daily proactive-message limit
/contact hours [HH:MM HH:MM] — show or change quiet hours; 00:00 00:00 disables time restriction
/contact test — try spontaneous contact now, outside character memory
/diagnostics — request inspection; /diagnostics confirm within 60s, or /cancel
Inside diagnostics: /persona · /routine · /memories · /state · /threads · /mind
/resume — leave diagnostics and restore the previous mode
/reset — immediately erase local character experiences and relationship
Send commands separately. Diagnostic exchanges stay outside character memory. /help shows this list.`

func controlMessageTx(ctx context.Context, tx *sql.Tx, text string, at time.Time) (int64, bool, error) {
	var mode, returnMode string
	var deadline int64
	if err := tx.QueryRowContext(ctx, "SELECT mode,return_mode,confirm_until_ns FROM character_control WHERE id=1").Scan(&mode, &returnMode, &deadline); err != nil {
		return 0, false, err
	}
	if !isControlInput(text) && mode == "running" {
		return 0, false, nil
	}
	name := commandName(text)
	zh, en := "", ""
	testEligible := false
	exec := func(query string, args ...any) error { _, err := tx.ExecContext(ctx, query, args...); return err }
	var err error
	if !isControlInput(text) {
		name = "blocked"
		zh, en = "【控制台】人物对话已暂停。这条消息不会进入人物记忆；可用 /唤醒 恢复，或 /返回角色 退出诊断。", "Character dialogue is paused. This message will not enter character memory. Use /wake to resume or /resume to leave diagnostics."
		if mode == "diagnostic" {
			zh, en = "【控制台】当前在诊断模式，人物聊天已暂停。发送 /返回角色 继续聊天；这条消息不会进入人物记忆。", "Diagnostics is active; character chat is paused. Send /resume to return to chat. This message will not enter character memory."
		}
	} else {
		switch name {
		case "help":
			zh, en = helpZH, helpEN
		case "status":
			var paused bool
			var start, end, limitMode string
			var limit int
			err = tx.QueryRowContext(ctx, "SELECT paused,quiet_start,quiet_end,daily_limit,limit_mode FROM contact_settings WHERE id=1").Scan(&paused, &start, &end, &limit, &limitMode)
			var initiative int
			if err == nil {
				err = tx.QueryRowContext(ctx, "SELECT initiative FROM character_tuning WHERE id=1").Scan(&initiative)
			}
			labels := map[string]string{"running": "人物运行中", "paused": "人物已暂停", "diagnostic": "诊断中"}
			contactZH := "允许（由人物决定是否联系）"
			if paused {
				contactZH = "已关闭"
			}
			frequencyZH := fmt.Sprintf("固定每日上限 %d", limit)
			frequencyEN := fmt.Sprintf("fixed daily limit %d", limit)
			if limitMode == "adaptive" {
				frequencyZH = "自适应节奏（防误发上限 12/日）"
				frequencyEN = "adaptive cadence (emergency ceiling 12/day)"
			}
			zh = fmt.Sprintf("【控制台】%s\n主动联系：%s；主动性：%d/100\n安静时段：%s–%s（配置时区）；联系频率：%s\n暂停和诊断期间不会主动联系。", labels[mode], contactZH, initiative, start, end, frequencyZH)
			en = fmt.Sprintf("Mode: %s\nProactive contact disabled: %t; initiative: %d/100\nQuiet hours: %s–%s (configured timezone); frequency: %s\nPaused and diagnostic modes block proactive contact.", mode, paused, initiative, start, end, frequencyEN)
			if err == nil {
				var kind string
				var until int64
				e := tx.QueryRowContext(ctx, "SELECT kind,until_ns FROM character_absence WHERE until_ns>?", at.UnixNano()).Scan(&kind, &until)
				if e == nil {
					labels := map[string]string{"sleep": "休息", "busy": "忙碌", "alone": "独处"}
					stamp := time.Unix(0, until).In(at.Location()).Format(time.RFC3339)
					zh += "\n人物暂离：" + labels[kind] + "；预计返回：" + stamp + "。期间普通消息会等待，可用 /唤醒 提前恢复。"
					en += "\nAway: " + kind + "; expected return: " + stamp + ". Ordinary messages wait; /wake returns early."
				} else if !errors.Is(e, sql.ErrNoRows) {
					err = e
				}
			}
		case "freeze":
			err = suspendCharacterTx(ctx, tx, at)
			if err == nil {
				err = exec("UPDATE character_control SET mode='paused',return_mode='paused',confirm_until_ns=0 WHERE id=1")
			}
			zh, en = "【控制台】已暂停人物对话和主动联系，记忆保留。输入 /唤醒 恢复。", "Character dialogue and proactive contact paused; memory preserved. Use /wake to resume."
		case "wake":
			err = exec("UPDATE character_control SET mode='running',return_mode='running',confirm_until_ns=0 WHERE id=1")
			if err == nil {
				err = exec("UPDATE character_absence SET until_ns=MIN(until_ns,?)", at.UnixNano())
			}
			if err == nil {
				err = exec(`INSERT INTO reply_deferrals(source_message_id,until_ns) SELECT source_message_id,? FROM character_turns WHERE json_extract(turn_json,'$.absence') IS NOT NULL ON CONFLICT(source_message_id) DO NOTHING`, at.UnixNano())
			}
			if err == nil {
				err = exec("UPDATE reply_deferrals SET until_ns=MIN(until_ns,?)", at.UnixNano())
			}
			zh, en = "【控制台】已恢复聊天，记忆保留。勿扰只影响她主动联系，不影响回复你的消息。", "Chat resumed; memory preserved. Do-not-disturb limits proactive contact, not replies to your messages."
		case "quiet", "contact_on":
			err = exec("UPDATE contact_settings SET paused=? WHERE id=1", name == "quiet")
			if name == "quiet" {
				zh, en = "【控制台】已关闭主动联系，你仍可以正常找她聊天。", "Proactive contact disabled. You can still start a conversation."
			} else {
				zh, en = "【控制台】已允许主动联系，仍遵守安静时段、联系频率和人物当前状态；不会强制立即发消息。", "Proactive contact allowed, subject to quiet hours, contact cadence and character mode. This does not force an immediate message."
			}
		case "test_contact":
			if mode != "running" {
				zh, en = "【控制台】人物目前已暂停；先用 /唤醒 或 /返回角色 恢复。", "Character dialogue is paused; use /wake or /resume first."
				break
			}
			var paused bool
			var level int
			err = tx.QueryRowContext(ctx, "SELECT paused FROM contact_settings WHERE id=1").Scan(&paused)
			if err == nil {
				err = tx.QueryRowContext(ctx, "SELECT initiative FROM character_tuning WHERE id=1").Scan(&level)
			}
			if err != nil {
				break
			}
			if paused || level == 0 {
				zh, en = "【控制台】主动联系目前已关闭；先用 /允许主动 并把 /主动性 设为大于 0。", "Proactive contact is disabled; use /contact on and set /initiative above 0 first."
				break
			}
			testEligible = true
		case "initiative":
			parts := strings.Fields(strings.TrimSpace(text))
			var level int
			err = tx.QueryRowContext(ctx, "SELECT initiative FROM character_tuning WHERE id=1").Scan(&level)
			if err != nil {
				break
			}
			if len(parts) == 2 {
				value, parseErr := strconv.Atoi(parts[1])
				if parseErr != nil || value < 0 || value > 100 {
					zh, en = "【控制台】用法：/主动性 0–100。", "Usage: /initiative 0–100."
					break
				}
				err = setInitiativeTx(ctx, tx, value, at)
				level = value
			} else if len(parts) > 2 {
				zh, en = "【控制台】用法：/主动性 0–100。", "Usage: /initiative 0–100."
				break
			}
			var limit int
			var limitMode string
			if err == nil {
				err = tx.QueryRowContext(ctx, "SELECT daily_limit,limit_mode FROM contact_settings WHERE id=1").Scan(&limit, &limitMode)
			}
			frequencyZH := fmt.Sprintf("固定每日上限 %d", limit)
			frequencyEN := fmt.Sprintf("fixed daily limit %d", limit)
			if limitMode == "adaptive" {
				frequencyZH = "自适应节奏"
				frequencyEN = "adaptive cadence"
			}
			zh = fmt.Sprintf("【控制台】主动性 %d/100。0 关闭自主发起；调高后会更常考虑联系，未收到回复时也可能再次开口。联系频率：%s；勿扰时段照常生效。", level, frequencyZH)
			en = fmt.Sprintf("Initiative %d/100. Higher levels consider contact more often and may follow up after no reply. Frequency: %s; quiet hours still apply.", level, frequencyEN)
		case "contact_auto":
			if _, err = tx.ExecContext(ctx, "UPDATE contact_settings SET limit_mode='adaptive',daily_limit=12 WHERE id=1"); err == nil {
				zh, en = "【控制台】已启用自适应联系节奏：根据互动和未回复情况决定何时再联系；每天 12 条仅作为防误发上限。勿扰和暂停照常生效。", "Adaptive contact cadence enabled. Reciprocity and unanswered messages guide timing; 12/day is only an emergency ceiling. Quiet hours and pause still apply."
			}
		case "contact_limit":
			parts := strings.Fields(strings.TrimSpace(text))
			var limit int
			var limitMode string
			err = tx.QueryRowContext(ctx, "SELECT daily_limit,limit_mode FROM contact_settings WHERE id=1").Scan(&limit, &limitMode)
			if err != nil {
				break
			}
			valueText := ""
			if len(parts) == 2 && parts[0] == "/联系上限" {
				valueText = parts[1]
			} else if len(parts) == 3 && parts[0] == "/contact" && parts[1] == "limit" {
				valueText = parts[2]
			} else if len(parts) != 1 && !(len(parts) == 2 && parts[0] == "/contact" && parts[1] == "limit") {
				zh, en = "【控制台】用法：/联系上限 1–20。", "Usage: /contact limit 1–20."
				break
			}
			if valueText != "" {
				value, parseErr := strconv.Atoi(valueText)
				if parseErr != nil || value < 1 || value > 20 {
					zh, en = "【控制台】用法：/联系上限 1–20。", "Usage: /contact limit 1–20."
					break
				}
				err = exec("UPDATE contact_settings SET daily_limit=?,limit_mode='fixed' WHERE id=1", value)
				limit = value
				limitMode = "fixed"
			}
			if limitMode == "adaptive" {
				zh, en = "【控制台】当前使用自适应联系节奏；每天 12 条仅作为防误发上限。", "Adaptive contact cadence is active; 12/day is only an emergency ceiling."
			} else {
				zh = fmt.Sprintf("【控制台】固定每日主动联系上限：%d。输入 /联系自动 可恢复自适应节奏。勿扰时段继续生效。", limit)
				en = fmt.Sprintf("Fixed daily proactive-message limit: %d. Use /contact auto for adaptive cadence. Quiet hours remain active.", limit)
			}
		case "quiet_hours":
			parts := strings.Fields(strings.TrimSpace(text))
			var start, end string
			err = tx.QueryRowContext(ctx, "SELECT quiet_start,quiet_end FROM contact_settings WHERE id=1").Scan(&start, &end)
			if err != nil {
				break
			}
			if len(parts) == 3 && parts[0] == "/安静时段" || len(parts) == 4 && parts[0] == "/contact" && parts[1] == "hours" {
				newStart, newEnd := parts[len(parts)-2], parts[len(parts)-1]
				if _, parseErr := time.Parse("15:04", newStart); parseErr != nil {
					zh, en = "【控制台】用法：/安静时段 HH:MM HH:MM。", "Usage: /contact hours HH:MM HH:MM."
					break
				}
				if _, parseErr := time.Parse("15:04", newEnd); parseErr != nil {
					zh, en = "【控制台】用法：/安静时段 HH:MM HH:MM。", "Usage: /contact hours HH:MM HH:MM."
					break
				}
				err = exec("UPDATE contact_settings SET quiet_start=?,quiet_end=? WHERE id=1", newStart, newEnd)
				start, end = newStart, newEnd
			} else if len(parts) != 1 && !(len(parts) == 2 && parts[0] == "/contact" && parts[1] == "hours") {
				zh, en = "【控制台】用法：/安静时段 HH:MM HH:MM。", "Usage: /contact hours HH:MM HH:MM."
				break
			}
			statusZH, statusEN := "", ""
			if start == end {
				statusZH, statusEN = "（全天不限制）", " (no time restriction)"
			}
			zh = fmt.Sprintf("【控制台】安静时段：%s–%s%s。每日联系上限和人物自己的选择继续生效。", start, end, statusZH)
			en = fmt.Sprintf("Quiet hours: %s–%s%s. The daily limit and character choice still apply.", start, end, statusEN)
		case "diagnostics":
			if mode == "diagnostic" {
				zh, en = "【诊断】已在诊断中。/人物 /记忆 /心境 /挂念 /内心；/返回角色 退出。", "Already in diagnostics. /persona /memories /state /threads /mind; /resume to leave."
				break
			}
			err = exec("UPDATE character_control SET confirm_until_ns=? WHERE id=1", at.Add(time.Minute).UnixNano())
			zh, en = "【诊断入口】进入后将暂停人物对话与主动联系，查看已保存的内部记录。请在 60 秒内单独发送 /确认诊断，或 /取消。", "Entering diagnostics pauses character dialogue and proactive contact and exposes saved internal records. Send /diagnostics confirm within 60 seconds, or /cancel."
		case "confirm":
			if mode == "diagnostic" {
				zh, en = "【诊断】已在诊断中。", "Already in diagnostics."
				break
			}
			if deadline == 0 || at.UnixNano() >= deadline || at.UnixNano() < deadline-int64(time.Minute) {
				zh, en = "【控制台】没有有效的诊断申请。请先发送 /诊断。", "No active diagnostic request. Send /diagnostics first."
				break
			}
			err = suspendCharacterTx(ctx, tx, at)
			if err == nil {
				err = exec("UPDATE character_control SET mode='diagnostic',return_mode=?,confirm_until_ns=0 WHERE id=1", mode)
			}
			zh, en = "【诊断已开启】人物对话与主动联系已暂停。\n/人物 /记忆 /心境 /挂念 /内心\n这些是已保存的设定与状态，不是实时思维链。/返回角色 退出。", "Diagnostics active; character dialogue and proactive contact paused.\n/persona /memories /state /threads /mind\nThese are saved settings and state, not a live chain of thought. /resume to leave."
		case "cancel":
			err = exec("UPDATE character_control SET confirm_until_ns=0 WHERE id=1")
			zh, en = "【控制台】诊断申请已取消。若已进入诊断，用 /返回角色 退出。", "Diagnostic request canceled. If already in diagnostics, use /resume to leave."
		case "resume":
			if mode == "diagnostic" {
				err = exec("UPDATE character_control SET mode=return_mode,confirm_until_ns=0 WHERE id=1")
				zh, en = "【控制台】已退出诊断，返回人物。", "Diagnostics closed; character resumed."
				if returnMode == "paused" {
					zh, en = "【控制台】已退出诊断。人物恢复为进入前的暂停状态；/唤醒 可恢复对话。", "Diagnostics closed. Character remains paused as before inspection; /wake resumes dialogue."
				}
			} else {
				err = exec("UPDATE character_control SET confirm_until_ns=0 WHERE id=1")
				zh, en = "【控制台】当前不在诊断中。若人物已暂停，请用 /唤醒。", "Not in diagnostics. Use /wake if the character is paused."
			}
		case "persona", "routine", "memories", "state", "threads", "mind":
			if mode != "diagnostic" {
				name = "locked"
				zh, en = "【控制台】内部记录需先进入诊断：/诊断，然后 /确认诊断。", "Internal records require diagnostics: /diagnostics, then /diagnostics confirm."
			} else {
				zh, en, err = diagnosticSnapshotTx(ctx, tx, name)
			}
		default:
			zh, en = "【控制台】未识别此命令，请用 /帮助 查看。命令需单独发送。", "Unknown command. Use /help. Send commands separately."
		}
	}
	if err != nil {
		return 0, true, err
	}
	r, err := tx.ExecContext(ctx, "INSERT INTO messages(role,content,created_at) VALUES('user',?,?)", text, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, true, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, true, err
	}
	if testEligible {
		result, requestErr := requestInitiativeTestTx(ctx, tx, at, id)
		if requestErr != nil {
			return 0, true, requestErr
		}
		switch result {
		case "advanced":
			zh, en = "【控制台】已把排队中的主动联系提前到现在。后台会尽快生成并发送；人物仍可能选择不发，勿扰和每日上限仍生效。", "A queued spontaneous contact was moved to now. The character may still choose silence; quiet hours and the daily limit still apply."
		case "processing":
			zh, en = "【控制台】已有一条主动联系正在生成或发送，请稍等。", "A spontaneous contact is already being composed or delivered."
		default:
			zh, en = "【控制台】已请求人物现在考虑主动联系。通常几十秒内会有结果；人物仍可选择不发，勿扰和每日上限仍生效。", "Asked the character to consider spontaneous contact now. It may choose silence; quiet hours and the daily limit still apply."
		}
	}
	if err = exec("INSERT INTO local_commands(source_message_id,command) VALUES(?,?)", id, name); err == nil {
		err = exec("INSERT INTO command_results(source_message_id,zh,en) VALUES(?,?,?)", id, zh, en)
	}
	return id, true, err
}

// The watermark survives wake: an in-flight result from the old generation
// cannot become valid again when the user resumes quickly.
func suspendCharacterTx(ctx context.Context, tx *sql.Tx, at time.Time) error {
	for _, q := range []string{
		"UPDATE character_control SET blocked_through_id=(SELECT COALESCE(MAX(id),0) FROM messages) WHERE id=1",
		// Prepared replies have a transcript receipt before transport. Redact
		// only unsent character replies, retaining previously delivered history.
		`UPDATE messages SET content='[forgotten memory]' WHERE id IN (
SELECT a.assistant_message_id FROM assistant_turns a JOIN feishu_inbox f ON f.user_message_id=a.source_message_id
WHERE f.state!='sent' AND NOT EXISTS(SELECT 1 FROM local_commands c WHERE c.source_message_id=f.user_message_id))`,
		`DELETE FROM character_turns WHERE source_message_id IN (SELECT user_message_id FROM feishu_inbox WHERE state!='sent')`,
		`UPDATE feishu_inbox SET state='sent',reply=NULL WHERE state!='sent' AND NOT EXISTS(SELECT 1 FROM local_commands c WHERE c.source_message_id=feishu_inbox.user_message_id)`,
		"UPDATE initiative_checks SET outcome='superseded',reason='character suspended' WHERE outcome='considering'",
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO contact_events(intent_id,outcome,reason,at_ns) SELECT id,'canceled','character suspended',? FROM contact_intents WHERE state='pending'", at.UnixNano()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE contact_intents SET state='canceled',reason='character suspended',updated_ns=? WHERE state='pending'", at.UnixNano())
	return err
}

func diagnosticSnapshotTx(ctx context.Context, tx *sql.Tx, name string) (string, string, error) {
	if name == "mind" {
		return diagnosticMindTx(ctx, tx)
	}
	if name == "routine" {
		return "【作息 · 人物习惯，可随情境调整】", "Routine · character habits, adaptable to context", nil
	}
	if name == "persona" {
		return "【人物卡 · 虚构设定】", "Character card · fictional setting", nil
	}
	if name == "memories" {
		rows, err := tx.QueryContext(ctx, `SELECT memory_key || ': ' || value,source_message_id FROM personal_memory p
WHERE action='upsert' AND id=(SELECT MAX(id) FROM personal_memory WHERE memory_key=p.memory_key)
UNION ALL SELECT topic || ': ' || COALESCE(happens_at,''),source_message_id FROM memory_revisions r
WHERE action='upsert' AND id=(SELECT MAX(id) FROM memory_revisions WHERE topic=r.topic)
ORDER BY source_message_id DESC LIMIT 20`)
		if err != nil {
			return "", "", err
		}
		defer rows.Close()
		var lines []string
		for rows.Next() {
			var value string
			var id int64
			if err := rows.Scan(&value, &id); err != nil {
				return "", "", err
			}
			lines = append(lines, fmt.Sprintf("• %s [chat:%d]", value, id))
		}
		if err := rows.Err(); err != nil {
			return "", "", err
		}
		if len(lines) == 0 {
			return "【记忆】暂无已提取的有效事实。完整对话另行保存在本机。", "No active extracted facts. The full conversation is stored separately on this device.", nil
		}
		body := strings.Join(lines, "\n")
		return "【记忆 · 最近 20 条有效事实；非完整对话】\n" + body, "Memory · up to 20 active facts, not the full transcript\n" + body, nil
	}
	var raw, stamp string
	err := tx.QueryRowContext(ctx, `SELECT turn_json,created_at FROM character_turns WHERE committed=1 AND json_extract(turn_json,'$.state_updated')=1 AND COALESCE(json_extract(turn_json,'$.state_version'),0)>=2 ORDER BY source_message_id DESC LIMIT 1`).Scan(&raw, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return "【诊断】暂无已保存的人物状态；不会临时生成。", "No saved character state. Nothing is generated for this inspection.", nil
	}
	if err != nil {
		return "", "", err
	}
	var t character.Turn
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return "", "", err
	}
	if name == "threads" {
		var lines []string
		for _, th := range t.OpenThreads {
			lines = append(lines, fmt.Sprintf("• %s (%s) [%s]", th.Subject, th.Owner, strings.Join(th.SourceIDs, ", ")))
		}
		if len(lines) == 0 {
			return "【挂念】该快照没有未完的话题。记录时间：" + stamp, "No open threads in this snapshot. Recorded: " + stamp, nil
		}
		body := strings.Join(lines, "\n")
		return "【挂念 · 历史快照】" + stamp + "\n" + body, "Open threads · saved snapshot " + stamp + "\n" + body, nil
	}
	m := t.Moment
	return fmt.Sprintf("【心境 · 历史快照】%s\n感受：%s\n精力：%s\n交谈兴致：%s\n关系倾向：%s\n缘由：%s\n注意：%s\n场景：%s\n意向：%s\n这是已保存的叙事状态，可能已过期。", stamp, m.Feeling, m.Energy, m.Engagement, m.Connection, m.Cause, m.Attention, m.Scene, m.Intention), fmt.Sprintf("State · saved snapshot %s\nFeeling: %s\nEnergy: %s\nEngagement: %s\nConnection: %s\nCause: %s\nAttention: %s\nScene: %s\nIntention: %s\nStored narrative state; may be stale.", stamp, m.Feeling, m.Energy, m.Engagement, m.Connection, m.Cause, m.Attention, m.Scene, m.Intention), nil
}

// Inspections can repeat a forgotten fact. Clear these derived copies along
// with character state instead of leaving it in an old command result.
func clearDiagnosticCopiesTx(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		"DELETE FROM character_absence",
		`UPDATE messages SET content='[forgotten memory]' WHERE id IN (SELECT a.assistant_message_id FROM assistant_turns a JOIN local_commands c ON c.source_message_id=a.source_message_id WHERE c.command IN ('memories','state','threads','mind'))`,
		`UPDATE feishu_inbox SET reply=NULL,state='sent' WHERE state!='sent' AND user_message_id IN (SELECT source_message_id FROM local_commands WHERE command IN ('memories','state','threads','mind'))`,
		`UPDATE command_results SET zh='【诊断】该历史快照已随遗忘操作清除。',en='This historical snapshot was cleared by a forgetting operation.' WHERE source_message_id IN (SELECT source_message_id FROM local_commands WHERE command IN ('memories','state','threads','mind'))`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
