package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type FeishuInboxItem struct {
	EventID       string
	ChatID        string
	MessageID     string
	UserMessageID int64
	Reply         string
	State         string
	ReceivedAt    time.Time
}

// EnqueueFeishu stores a Feishu event and its conversation entry in one
// transaction. A retried event never creates a second conversation entry.
func (h *History) EnqueueFeishu(ctx context.Context, eventID, chatID, messageID, text string, at time.Time, zone *time.Location) (bool, error) {
	if eventID == "" || chatID == "" || messageID == "" || strings.TrimSpace(text) == "" || len(text) > 65536 {
		return false, errors.New("invalid Feishu message")
	}
	if isControlInput(text) {
		h.deliveryMu.Lock()
		defer h.deliveryMu.Unlock()
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var existing int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM feishu_inbox WHERE event_id = ? UNION SELECT 1 FROM inbox_receipts WHERE event_id = ?", eventID, eventID).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	userMessageID, err := addUserMessageTx(ctx, tx, text, at, zone)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO feishu_inbox(event_id, chat_id, message_id, user_message_id, state, received_ns)
VALUES (?, ?, ?, ?, 'pending', ?)`, eventID, chatID, messageID, userMessageID, at.UnixNano())
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (h *History) NextFeishu(ctx context.Context) (FeishuInboxItem, bool, error) {
	var item FeishuInboxItem
	var receivedNS int64
	err := h.db.QueryRowContext(ctx, `SELECT event_id, chat_id, message_id, user_message_id, COALESCE(reply,''), state, received_ns
FROM feishu_inbox WHERE state IN ('pending','ready') ORDER BY received_ns, rowid LIMIT 1`).
		Scan(&item.EventID, &item.ChatID, &item.MessageID, &item.UserMessageID, &item.Reply, &item.State, &receivedNS)
	if errors.Is(err, sql.ErrNoRows) {
		return FeishuInboxItem{}, false, nil
	}
	if err == nil {
		item.ReceivedAt = time.Unix(0, receivedNS)
	}
	return item, err == nil, err
}

// HasLaterFeishuInput lets an invalid draft yield to a newer contribution in
// the same private chat. The older user's text remains in conversation history.
func (h *History) HasLaterFeishuInput(ctx context.Context, eventID string) (bool, error) {
	var newer bool
	err := h.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM feishu_inbox current JOIN feishu_inbox next
		ON next.chat_id=current.chat_id
		AND (next.received_ns>current.received_ns OR (next.received_ns=current.received_ns AND next.rowid>current.rowid))
		WHERE current.event_id=? AND next.state IN ('pending','ready'))`, eventID).Scan(&newer)
	return newer, err
}

// SetFeishuReply commits generated text to the local conversation and inbox
// together. A send retry reuses the same text instead of asking the model again.
func (h *History) SetFeishuReply(ctx context.Context, eventID, reply string, at time.Time) error {
	return h.SetFeishuReplyParts(ctx, eventID, reply, []string{reply}, at)
}

// SetFeishuReplyParts prepares one logical turn and its delivery bubbles in
// one transaction. Existing callers continue to prepare a single bubble.
func (h *History) SetFeishuReplyParts(ctx context.Context, eventID, reply string, parts []string, at time.Time) error {
	_, err := h.prepareFeishuReplyParts(ctx, eventID, reply, parts, at, false)
	return err
}

// PrepareFeishuReplyPartsIfCurrent is the speculative dialogue commit. A
// newer private message can replace an unsent character draft atomically;
// false means the later message should be considered with both inputs.
func (h *History) PrepareFeishuReplyPartsIfCurrent(ctx context.Context, eventID, reply string, parts []string, at time.Time) (bool, error) {
	return h.prepareFeishuReplyParts(ctx, eventID, reply, parts, at, true)
}

func (h *History) prepareFeishuReplyParts(ctx context.Context, eventID, reply string, parts []string, at time.Time, supersedeDraft bool) (bool, error) {
	if strings.TrimSpace(reply) == "" {
		return false, errors.New("empty Feishu reply")
	}
	if len(parts) == 0 || len(parts) > 3 {
		return false, errors.New("Feishu reply needs one to three parts")
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" || len([]rune(part)) > 1000 {
			return false, errors.New("invalid Feishu reply part")
		}
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if supersedeDraft {
		superseded, err := supersedeFeishuDraftTx(ctx, tx, eventID)
		if err != nil {
			return false, err
		}
		if superseded {
			return false, tx.Commit()
		}
	}
	result, err := tx.ExecContext(ctx, "UPDATE feishu_inbox SET reply = ?, state = 'ready' WHERE event_id = ? AND state = 'pending'", reply, eventID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows != 1 {
		return false, ErrTurnSuperseded
	}
	var sentParts int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM feishu_reply_parts WHERE event_id=? AND state='sent'", eventID).Scan(&sentParts); err != nil {
		return false, err
	}
	if sentParts > 0 {
		return false, errors.New("cannot replace a partially delivered Feishu reply")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM feishu_reply_parts WHERE event_id=?", eventID); err != nil {
		return false, err
	}
	for i, part := range parts {
		if _, err := tx.ExecContext(ctx, "INSERT INTO feishu_reply_parts(event_id,part_index,text) VALUES(?,?,?)", eventID, i, strings.TrimSpace(part)); err != nil {
			return false, err
		}
	}
	var sourceID int64
	if err := tx.QueryRowContext(ctx, "SELECT user_message_id FROM feishu_inbox WHERE event_id = ?", eventID).Scan(&sourceID); err != nil {
		return false, err
	}
	if err := commitCharacterTurnTx(ctx, tx, sourceID, reply); err != nil {
		return false, err
	}
	message, err := tx.ExecContext(ctx, "INSERT INTO messages(role, content, created_at) VALUES ('assistant', ?, ?)", reply, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	assistantID, err := message.LastInsertId()
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO assistant_turns(source_message_id, assistant_message_id)
SELECT user_message_id, ? FROM feishu_inbox WHERE event_id = ?`, assistantID, eventID)
	if err != nil {
		return false, err
	}
	// The test command is outside character history. Its console acknowledgement
	// must not invalidate the initiative that the command just advanced.
	if _, err := tx.ExecContext(ctx, `UPDATE initiative_checks SET through_message_id=?
WHERE through_message_id=? AND intent_id IN (SELECT intent_id FROM initiative_test_intents)`, assistantID, sourceID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// SupersedeFeishuDraftIfNewer also covers wait and timed departures, which
// have no reply parts to prepare. It is called before the empty-reply path.
func (h *History) SupersedeFeishuDraftIfNewer(ctx context.Context, eventID string) (bool, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	superseded, err := supersedeFeishuDraftTx(ctx, tx, eventID)
	if err != nil {
		return false, err
	}
	return superseded, tx.Commit()
}

func supersedeFeishuDraftTx(ctx context.Context, tx *sql.Tx, eventID string) (bool, error) {
	var sourceID int64
	var newer, draft bool
	err := tx.QueryRowContext(ctx, `SELECT current.user_message_id,
		EXISTS(SELECT 1 FROM feishu_inbox next WHERE next.chat_id=current.chat_id
		AND (next.received_ns>current.received_ns OR (next.received_ns=current.received_ns AND next.rowid>current.rowid))
		AND next.state IN ('pending','ready')),
		EXISTS(SELECT 1 FROM character_turns c WHERE c.source_message_id=current.user_message_id AND c.committed=0)
		FROM feishu_inbox current WHERE current.event_id=?`, eventID).Scan(&sourceID, &newer, &draft)
	if err != nil || !newer || !draft {
		return false, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE feishu_inbox SET reply=NULL,state='sent' WHERE event_id=? AND state='pending'", eventID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows != 1 {
		return false, ErrTurnSuperseded
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM character_turns WHERE source_message_id=? AND committed=0", sourceID); err != nil {
		return false, err
	}
	return true, nil
}

// SkipFeishuReply consumes a turn without a visible assistant message. This
// covers chosen silence and an invalid old draft superseded by a newer user
// contribution. The inbox event remains as a retry tombstone.
func (h *History) SkipFeishuReply(ctx context.Context, eventID string) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE feishu_inbox SET reply = NULL, state = 'sent' WHERE event_id = ? AND state = 'pending'", eventID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrTurnSuperseded
	}
	var sourceID int64
	if err := tx.QueryRowContext(ctx, "SELECT user_message_id FROM feishu_inbox WHERE event_id = ?", eventID).Scan(&sourceID); err != nil {
		return err
	}
	if err := commitCharacterTurnTx(ctx, tx, sourceID, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *History) MarkFeishuSent(ctx context.Context, eventID string) error {
	_, err := h.db.ExecContext(ctx, "UPDATE feishu_inbox SET reply = NULL, state = 'sent' WHERE event_id = ? AND state = 'ready'", eventID)
	return err
}

// DeliverFeishuParts resumes at the first unsent bubble after a partial send.
// The transport call and its receipt are fenced against a concurrent reset.
func (h *History) DeliverFeishuParts(ctx context.Context, eventID string, send func(int, string) error) error {
	h.deliveryMu.RLock()
	defer h.deliveryMu.RUnlock()
	var reply string
	err := h.db.QueryRowContext(ctx, "SELECT COALESCE(reply,'') FROM feishu_inbox WHERE event_id=? AND state='ready'", eventID).Scan(&reply)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := h.db.QueryContext(ctx, "SELECT part_index,text,state FROM feishu_reply_parts WHERE event_id=? ORDER BY part_index", eventID)
	if err != nil {
		return err
	}
	type part struct {
		index int
		text  string
		state string
	}
	var parts []part
	for rows.Next() {
		var p part
		if err := rows.Scan(&p.index, &p.text, &p.state); err != nil {
			rows.Close()
			return err
		}
		parts = append(parts, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(parts) == 0 { // A ready row from an older local database.
		if err := send(0, reply); err != nil {
			return err
		}
		return h.MarkFeishuSent(ctx, eventID)
	}
	for _, p := range parts {
		if p.state == "sent" {
			continue
		}
		if err := send(p.index, p.text); err != nil {
			return err
		}
		if _, err := h.db.ExecContext(ctx, "UPDATE feishu_reply_parts SET state='sent' WHERE event_id=? AND part_index=? AND state='pending'", eventID, p.index); err != nil {
			return err
		}
	}
	return h.MarkFeishuSent(ctx, eventID)
}
