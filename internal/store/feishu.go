package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type FeishuInboxItem struct {
	EventID    string
	ChatID     string
	MessageID  string
	Reply      string
	State      string
	ReceivedAt time.Time
}

// EnqueueFeishu stores a Feishu event and its conversation entry in one
// transaction. A retried event never creates a second conversation entry.
func (h *History) EnqueueFeishu(ctx context.Context, eventID, chatID, messageID, text string, at time.Time, zone *time.Location) (bool, error) {
	if eventID == "" || chatID == "" || messageID == "" || strings.TrimSpace(text) == "" || len(text) > 65536 {
		return false, errors.New("invalid Feishu message")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var existing int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM feishu_inbox WHERE event_id = ?", eventID).Scan(&existing)
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
	err := h.db.QueryRowContext(ctx, `SELECT event_id, chat_id, message_id, COALESCE(reply,''), state, received_ns
FROM feishu_inbox WHERE state IN ('pending','ready') ORDER BY received_ns, rowid LIMIT 1`).
		Scan(&item.EventID, &item.ChatID, &item.MessageID, &item.Reply, &item.State, &receivedNS)
	if errors.Is(err, sql.ErrNoRows) {
		return FeishuInboxItem{}, false, nil
	}
	if err == nil {
		item.ReceivedAt = time.Unix(0, receivedNS)
	}
	return item, err == nil, err
}

// SetFeishuReply commits generated text to the local conversation and inbox
// together. A send retry reuses the same text instead of asking the model again.
func (h *History) SetFeishuReply(ctx context.Context, eventID, reply string, at time.Time) error {
	if strings.TrimSpace(reply) == "" {
		return errors.New("empty Feishu reply")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE feishu_inbox SET reply = ?, state = 'ready' WHERE event_id = ? AND state = 'pending'", reply, eventID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("Feishu event is not pending")
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO messages(role, content, created_at) VALUES ('assistant', ?, ?)", reply, at.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *History) MarkFeishuSent(ctx context.Context, eventID string) error {
	_, err := h.db.ExecContext(ctx, "UPDATE feishu_inbox SET reply = NULL, state = 'sent' WHERE event_id = ? AND state = 'ready'", eventID)
	return err
}
