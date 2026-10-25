package store

import (
	"context"
	"time"
)

// NextFeishuBatch waits for a small conversational pause, then durably binds
// consecutive messages to their last source. Every user message remains in the
// transcript. A claimed batch, cached generation or prepared send is immutable
// across retries and restarts. The worker must have only one consumer.
func (h *History) NextFeishuBatch(ctx context.Context, now time.Time) (FeishuInboxItem, bool, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return FeishuInboxItem{}, false, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT f.event_id,f.chat_id,f.message_id,f.user_message_id,
COALESCE(f.reply,''),f.state,f.received_ns,
EXISTS(SELECT 1 FROM feishu_reply_groups g WHERE g.event_id=f.event_id)
OR EXISTS(SELECT 1 FROM character_turns c WHERE c.source_message_id=f.user_message_id)
FROM feishu_inbox f WHERE f.state IN ('pending','ready') ORDER BY f.received_ns,f.rowid LIMIT 8`)
	if err != nil {
		return FeishuInboxItem{}, false, err
	}
	var batch []FeishuInboxItem
	locked := false
	for rows.Next() {
		var item FeishuInboxItem
		var ns int64
		var claimed bool
		if err := rows.Scan(&item.EventID, &item.ChatID, &item.MessageID, &item.UserMessageID, &item.Reply, &item.State, &ns, &claimed); err != nil {
			rows.Close()
			return FeishuInboxItem{}, false, err
		}
		item.ReceivedAt = time.Unix(0, ns)
		if len(batch) > 0 && (item.ChatID != batch[0].ChatID || item.State == "ready" || claimed || item.ReceivedAt.Sub(batch[len(batch)-1].ReceivedAt) > 4*time.Second || item.ReceivedAt.Sub(batch[0].ReceivedAt) > 12*time.Second) {
			break
		}
		batch = append(batch, item)
		if item.State == "ready" || claimed {
			locked = true
			break
		}
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return FeishuInboxItem{}, false, readErr
	}
	if len(batch) == 0 {
		return FeishuInboxItem{}, false, nil
	}
	anchor := batch[len(batch)-1]
	if locked {
		return anchor, true, nil
	}
	// Bound the wait even when someone keeps typing. No sleep or model call
	// holds the database transaction open.
	if now.Sub(anchor.ReceivedAt) < 4*time.Second && now.Sub(batch[0].ReceivedAt) < 12*time.Second && len(batch) < 8 {
		return FeishuInboxItem{}, false, nil
	}
	for _, item := range batch {
		if _, err := tx.ExecContext(ctx, "INSERT INTO feishu_reply_groups(event_id,anchor_event_id) VALUES(?,?)", item.EventID, anchor.EventID); err != nil {
			return FeishuInboxItem{}, false, err
		}
		if item.EventID != anchor.EventID {
			// Consumed by the anchor, not individually answered. The group
			// ledger distinguishes this from a separately delivered reply.
			if _, err := tx.ExecContext(ctx, "UPDATE feishu_inbox SET state='sent' WHERE event_id=? AND state='pending'", item.EventID); err != nil {
				return FeishuInboxItem{}, false, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return FeishuInboxItem{}, false, err
	}
	return anchor, true, nil
}
