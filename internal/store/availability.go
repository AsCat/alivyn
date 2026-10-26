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

// AvailabilityContext reports actual channel capability and the last absence.
// Terminal chat does not advertise unattended deferred delivery.
func (h *History) AvailabilityContext(ctx context.Context, sourceID int64, now time.Time) (canLeave bool, status, received string, err error) {
	err = h.db.QueryRowContext(ctx, `SELECT created_at,EXISTS(SELECT 1 FROM feishu_inbox WHERE user_message_id=messages.id)
AND NOT EXISTS(SELECT 1 FROM reply_deferrals WHERE source_message_id=messages.id)
FROM messages WHERE id=?`, sourceID).Scan(&received, &canLeave)
	if err != nil {
		return
	}
	// Wake is an explicit availability transition, even when a previous sleep
	// deadline has already passed (or no timed absence was ever recorded).
	var wakeStamp string
	err = h.db.QueryRowContext(ctx, `SELECT m.created_at FROM local_commands c JOIN messages m ON m.id=c.source_message_id WHERE c.command='wake' ORDER BY m.id DESC LIMIT 1`).Scan(&wakeStamp)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err == nil {
		wakeAt, parseErr := time.Parse(time.RFC3339Nano, wakeStamp)
		if parseErr != nil {
			err = parseErr
			return
		}
		if age := now.Sub(wakeAt); age >= 0 && age < time.Hour {
			canLeave = false
			status = fmt.Sprintf("CURRENT AVAILABILITY: awake and available. Resumed at %s; now %s. Earlier sleep, busy or solitude intentions have ended. Reconsider the latest message now; do not continue an old goodbye or claim to still be asleep. This is current runtime state, not a new shared experience or a topic to explain to the user.", wakeAt.Format(time.RFC3339), now.Format(time.RFC3339))
			err = nil
			return
		}
	}
	var kind, reason string
	var until int64
	err = h.db.QueryRowContext(ctx, "SELECT kind,reason,until_ns FROM character_absence WHERE id=1").Scan(&kind, &reason, &until)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
		return
	}
	if err != nil {
		return
	}
	end := time.Unix(0, until)
	status = fmt.Sprintf("Last absence: %s, reason: %s, return time: %s", kind, reason, end.Format(time.RFC3339))
	if now.Before(end) {
		canLeave = false
		status += "; still away"
	} else {
		status += "; CURRENT AVAILABILITY: returned and available. That absence has ended; earlier bedtime messages describe the past, not the present. Reconsider new messages at the current local time."
		if now.Sub(end) < time.Hour {
			canLeave = false
			status += "; recently returned, reconsider waiting messages before another absence"
		}
	}
	return
}

func saveAbsenceTx(ctx context.Context, tx *sql.Tx, sourceID int64, t character.Turn) error {
	if t.Absence == nil {
		return nil
	}
	var superseded bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM local_commands WHERE command='wake' AND source_message_id>?)", sourceID).Scan(&superseded); err != nil {
		return err
	}
	if superseded {
		return ErrTurnSuperseded
	}
	until, err := time.Parse(time.RFC3339, t.Absence.Until)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO reply_deferrals(source_message_id,until_ns) VALUES(?,?) ON CONFLICT DO NOTHING", sourceID, until.UnixNano()); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO character_absence(id,source_message_id,kind,reason,until_ns) VALUES(1,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET source_message_id=excluded.source_message_id,kind=excluded.kind,reason=excluded.reason,until_ns=excluded.until_ns
WHERE excluded.source_message_id>=character_absence.source_message_id`, sourceID, t.Absence.Kind, t.Absence.Reason, until.UnixNano())
	return err
}

// DeferFeishuReply keeps original input pending with no fabricated reply.
// The inbox selector retries at the return time, including across restarts.
func (h *History) DeferFeishuReply(ctx context.Context, eventID string) (bool, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id int64
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT f.user_message_id,c.turn_json FROM feishu_inbox f JOIN character_turns c ON c.source_message_id=f.user_message_id
WHERE f.event_id=? AND f.state='pending' AND json_extract(c.turn_json,'$.mode')='defer'
AND EXISTS(SELECT 1 FROM character_control WHERE mode='running' AND blocked_through_id<f.user_message_id)`, eventID).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var t character.Turn
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return false, err
	}
	if t.Absence == nil {
		return false, errors.New("defer needs a timed absence")
	}
	until, err := time.Parse(time.RFC3339, t.Absence.Until)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO reply_deferrals(source_message_id,until_ns) VALUES(?,?) ON CONFLICT DO NOTHING", id, until.UnixNano()); err != nil {
		return false, err
	}
	if err := saveAbsenceTx(ctx, tx, id, t); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func releaseDeferredTx(ctx context.Context, tx *sql.Tx, now time.Time) error {
	// A closing line prepared before an outage may be obsolete by the time
	// transport recovers. Remove its undelivered transcript receipt, then
	// reconsider the input instead of sending an old bedtime announcement.
	for _, q := range []string{
		`UPDATE messages SET content='[forgotten memory]' WHERE id IN (SELECT a.assistant_message_id FROM assistant_turns a JOIN feishu_inbox f ON f.user_message_id=a.source_message_id JOIN reply_deferrals d ON d.source_message_id=f.user_message_id JOIN character_turns c ON c.source_message_id=f.user_message_id WHERE f.state='ready' AND d.until_ns<=? AND json_extract(c.turn_json,'$.absence') IS NOT NULL)`,
		`DELETE FROM assistant_turns WHERE source_message_id IN (SELECT f.user_message_id FROM feishu_inbox f JOIN reply_deferrals d ON d.source_message_id=f.user_message_id JOIN character_turns c ON c.source_message_id=f.user_message_id WHERE f.state='ready' AND d.until_ns<=? AND json_extract(c.turn_json,'$.absence') IS NOT NULL)`,
		`UPDATE feishu_inbox SET state='pending',reply=NULL WHERE state='ready' AND user_message_id IN (SELECT d.source_message_id FROM reply_deferrals d JOIN character_turns c ON c.source_message_id=d.source_message_id WHERE until_ns<=? AND json_extract(c.turn_json,'$.absence') IS NOT NULL)`,
	} {
		if _, err := tx.ExecContext(ctx, q, now.UnixNano()); err != nil {
			return err
		}
	}
	// Release the undelivered draft and group, keeping its deferral receipt.
	if _, err := tx.ExecContext(ctx, `DELETE FROM feishu_reply_groups WHERE anchor_event_id IN
(SELECT f.event_id FROM feishu_inbox f JOIN reply_deferrals d ON d.source_message_id=f.user_message_id WHERE f.state='pending' AND d.until_ns<=?)`, now.UnixNano()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM character_turns WHERE json_extract(turn_json,'$.absence') IS NOT NULL
AND source_message_id IN (SELECT d.source_message_id FROM reply_deferrals d JOIN feishu_inbox f ON f.user_message_id=d.source_message_id WHERE d.until_ns<=? AND f.state='pending')`, now.UnixNano())
	return err
}
