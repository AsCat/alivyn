package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/contact"
)

type ContactPart struct {
	Index int
	Text  string
	Sent  bool
}

// PrepareContactParts freezes the visible messages before the first send.
// Retries must reuse these parts, even if the model or formatter later changes.
func (h *History) PrepareContactParts(ctx context.Context, id int64, parts []string) error {
	if len(parts) < 1 || len(parts) > 3 {
		return errors.New("contact needs one to three parts")
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" || len([]rune(part)) > 180 {
			return errors.New("invalid contact part")
		}
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM contact_delivery WHERE intent_id=?", id).Scan(&state); err != nil {
		return err
	}
	if state != "pending" {
		return errors.New("contact delivery is not pending")
	}
	var existing int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM contact_delivery_parts WHERE intent_id=?", id).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	for i, part := range parts {
		if _, err := tx.ExecContext(ctx, "INSERT INTO contact_delivery_parts(intent_id,part_index,text) VALUES(?,?,?)", id, i, strings.TrimSpace(part)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (h *History) ContactParts(ctx context.Context, id int64) ([]ContactPart, error) {
	rows, err := h.db.QueryContext(ctx, "SELECT part_index,text,state FROM contact_delivery_parts WHERE intent_id=? ORDER BY part_index", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var parts []ContactPart
	for rows.Next() {
		var part ContactPart
		var state string
		if err := rows.Scan(&part.Index, &part.Text, &state); err != nil {
			return nil, err
		}
		part.Sent = state == "sent"
		parts = append(parts, part)
	}
	return parts, rows.Err()
}

func contactPartAllowedTx(ctx context.Context, tx *sql.Tx, id int64, at time.Time, zone *time.Location) (bool, error) {
	var intentState, mode string
	var paused bool
	var quietStart, quietEnd string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM contact_intents WHERE id=?", id).Scan(&intentState); err != nil {
		return false, err
	}
	if intentState != "pending" {
		return false, nil
	}
	if err := tx.QueryRowContext(ctx, "SELECT mode FROM character_control WHERE id=1").Scan(&mode); err != nil {
		return false, err
	}
	if mode != "running" {
		return false, nil
	}
	var away bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM character_absence WHERE until_ns>?)", at.UnixNano()).Scan(&away); err != nil {
		return false, err
	}
	if away {
		return false, nil
	}
	if err := tx.QueryRowContext(ctx, "SELECT paused,quiet_start,quiet_end FROM contact_settings WHERE id=1").Scan(&paused, &quietStart, &quietEnd); err != nil {
		return false, err
	}
	return !paused && !contact.Quiet(at.In(zone), quietStart, quietEnd), nil
}

func (h *History) ContactPartAllowed(ctx context.Context, id int64, at time.Time, zone *time.Location) (bool, error) {
	if zone == nil {
		return false, errors.New("contact timezone is required")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	return contactPartAllowedTx(ctx, tx, id, at, zone)
}

// Each confirmed bubble enters history immediately. An interrupted burst can
// therefore never erase a message the user actually saw.
func (h *History) MarkChannelContactPartSent(ctx context.Context, id int64, index int, at time.Time) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var body, state string
	if err := tx.QueryRowContext(ctx, "SELECT text,state FROM contact_delivery_parts WHERE intent_id=? AND part_index=?", id, index).Scan(&body, &state); err != nil {
		return err
	}
	if state == "sent" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO messages(role,content,created_at) VALUES('assistant',?,?)", body, at.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE contact_delivery_parts SET state='sent',sent_ns=? WHERE intent_id=? AND part_index=?", at.UnixNano(), id, index); err != nil {
		return err
	}
	return tx.Commit()
}

// completeContactPartialTx closes an interrupted burst after at least one
// confirmed bubble. The outbound limit counts the whole burst once.
func completeContactPartialTx(ctx context.Context, tx *sql.Tx, id int64, at time.Time, reason string) (bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT text,sent_ns FROM contact_delivery_parts WHERE intent_id=? AND state='sent' ORDER BY part_index", id)
	if err != nil {
		return false, err
	}
	var texts []string
	var sentNS int64
	for rows.Next() {
		var body string
		var ns int64
		if err := rows.Scan(&body, &ns); err != nil {
			rows.Close()
			return false, err
		}
		texts = append(texts, body)
		if ns > sentNS {
			sentNS = ns
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	if len(texts) == 0 {
		return false, nil
	}
	result, err := tx.ExecContext(ctx, "UPDATE contact_delivery SET body=?,state='sent',sent_ns=? WHERE intent_id=? AND state='pending'", strings.Join(texts, "\n\n"), sentNS, id)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed == 0 {
		return true, nil
	}
	return true, updateIntentAndLog(ctx, tx, id, "sent", reason, ContactEvent{IntentID: id, Outcome: "sent", Reason: reason, At: at})
}

func (h *History) CompleteContactPartial(ctx context.Context, id int64, at time.Time, reason string) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := completeContactPartialTx(ctx, tx, id, at, reason); err != nil {
		return err
	}
	return tx.Commit()
}

// A command or user message can cancel an intent between confirmed bubbles.
// This closes its delivery receipt without sending the remaining text.
func (h *History) ReconcilePartialContacts(ctx context.Context, at time.Time) error {
	rows, err := h.db.QueryContext(ctx, `SELECT d.intent_id FROM contact_delivery d JOIN contact_intents i ON i.id=d.intent_id
WHERE d.state='pending' AND i.state!='pending' AND EXISTS(SELECT 1 FROM contact_delivery_parts p WHERE p.intent_id=d.intent_id AND p.state='sent')`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		if err := h.CompleteContactPartial(ctx, id, at, "partial burst stopped after contact changed"); err != nil {
			return err
		}
	}
	return nil
}
