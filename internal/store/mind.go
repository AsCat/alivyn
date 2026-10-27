package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

// diagnosticMindTx combines persisted character state with the last actual
// outreach choice. It does not call a model or represent hidden reasoning.
func diagnosticMindTx(ctx context.Context, tx *sql.Tx) (string, string, error) {
	connectionZH, connectionEN := "尚未记录", "not recorded"
	intentionZH, intentionEN := "尚未记录", "not recorded"
	feelingZH, feelingEN := "尚未记录", "not recorded"
	stateAtZH, stateAtEN := "尚无快照", "no snapshot"
	var raw, stamp string
	err := tx.QueryRowContext(ctx, `SELECT turn_json,created_at FROM character_turns WHERE committed=1
AND json_extract(turn_json,'$.state_updated')=1
AND COALESCE(json_extract(turn_json,'$.state_version'),0)>=2
ORDER BY source_message_id DESC LIMIT 1`).Scan(&raw, &stamp)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	if err == nil {
		var turn character.Turn
		if err := json.Unmarshal([]byte(raw), &turn); err != nil {
			return "", "", err
		}
		if turn.Moment.Connection != "" {
			connectionZH, connectionEN = turn.Moment.Connection, turn.Moment.Connection
		}
		if turn.Moment.Intention != "" {
			intentionZH, intentionEN = turn.Moment.Intention, turn.Moment.Intention
		}
		if turn.Moment.Feeling != "" {
			feelingZH, feelingEN = turn.Moment.Feeling, turn.Moment.Feeling
		}
		stateAtZH, stateAtEN = stamp, stamp
	}
	choiceZH, choiceEN := "尚无主动联系判断", "no spontaneous-contact decision yet"
	reasonZH, reasonEN := "", ""
	choiceAtZH, choiceAtEN := "", ""
	var at int64
	var outcome, reason, delivery string
	err = tx.QueryRowContext(ctx, `SELECT c.at_ns,c.outcome,c.reason,COALESCE(i.state,'')
FROM initiative_checks c LEFT JOIN contact_intents i ON i.id=c.intent_id
WHERE c.outcome IN ('wait','queued') ORDER BY c.id DESC LIMIT 1`).Scan(&at, &outcome, &reason, &delivery)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	if err == nil {
		switch outcome {
		case "wait":
			choiceZH, choiceEN = "那时决定先不联系", "chose not to contact at that time"
		case "queued":
			choiceZH, choiceEN = "那时想联系，尚未发出", "wanted to contact, not delivered"
			if delivery == "sent" {
				choiceZH, choiceEN = "那时想联系，已经发出", "wanted to contact and sent a message"
			}
		}
		reasonZH, reasonEN = reason, reason
		choiceAtZH, choiceAtEN = time.Unix(0, at).UTC().Format(time.RFC3339), time.Unix(0, at).UTC().Format(time.RFC3339)
	}
	zh := fmt.Sprintf("【内心 · 已保存的切片】\n感受：%s\n关系倾向：%s\n自己的意向：%s\n状态记录：%s\n最近联系决定：%s", feelingZH, connectionZH, intentionZH, stateAtZH, choiceZH)
	en := fmt.Sprintf("Mind · saved snapshot\nFeeling: %s\nConnection: %s\nOwn intention: %s\nState recorded: %s\nLast contact choice: %s", feelingEN, connectionEN, intentionEN, stateAtEN, choiceEN)
	if choiceAtZH != "" {
		zh += "\n决定时间：" + choiceAtZH + "\n当时理由：" + reasonZH
		en += "\nChoice recorded: " + choiceAtEN + "\nReason at the time: " + reasonEN
	}
	zh += "\n这是保存的状态与行动摘要；进入诊断后主动联系已暂停，不代表此刻仍在实时思考。"
	en += "\nSaved state and action summary. Diagnostics pauses outreach; this is not live thought."
	return zh, en, nil
}
