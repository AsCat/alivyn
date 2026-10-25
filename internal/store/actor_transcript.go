package store

import (
	"context"
	"database/sql"
	"time"
)

// ActorTranscriptThrough reconstructs the conversational order for a queued
// channel. A reply committed after later user messages still follows its
// triggering turn in the character's context.
func (h *History) ActorTranscriptThrough(ctx context.Context, limit int, throughID int64) ([]Entry, error) {
	entries, err := h.RecentForModelThrough(ctx, limit, throughID)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return entries, nil
	}
	rows, err := h.db.QueryContext(ctx, `SELECT t.source_message_id, m.id, m.role, m.content, m.created_at
FROM assistant_turns t JOIN messages m ON m.id = t.assistant_message_id
WHERE NOT EXISTS(SELECT 1 FROM local_commands c WHERE c.source_message_id=t.source_message_id) AND t.source_message_id < ? AND (t.source_message_id >= ? OR m.id >= ?)`, throughID, entries[0].ID, entries[0].ID)
	if err != nil {
		return nil, err
	}
	linked := map[int64]Entry{}
	linkedIDs := map[int64]bool{}
	for rows.Next() {
		var sourceID int64
		var assistant Entry
		var stamp string
		if err := rows.Scan(&sourceID, &assistant.ID, &assistant.Role, &assistant.Content, &stamp); err != nil {
			rows.Close()
			return nil, err
		}
		assistant.CreatedAt, err = time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			rows.Close()
			return nil, err
		}
		linked[sourceID] = assistant
		linkedIDs[assistant.ID] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(entries)+len(linked))
	for _, entry := range entries {
		if linkedIDs[entry.ID] {
			continue
		}
		result = append(result, entry)
		if entry.Role == "user" {
			if assistant, ok := linked[entry.ID]; ok && assistant.Content != "[forgotten memory]" {
				result = append(result, assistant)
			}
		}
	}
	return result, nil
}

func redactLinkedReplyTx(ctx context.Context, tx *sql.Tx, sourceID int64) error {
	// A disclosure may be an earlier member of a grouped contribution. Its
	// reply is linked to the last member, so redact/cancel that anchor too.
	const sources = `SELECT ? UNION SELECT a.user_message_id
FROM feishu_reply_groups g JOIN feishu_inbox m ON m.event_id=g.event_id
JOIN feishu_inbox a ON a.event_id=g.anchor_event_id WHERE m.user_message_id=?`
	if _, err := tx.ExecContext(ctx, `UPDATE feishu_inbox SET reply=NULL,state='sent'
WHERE state IN ('pending','ready') AND user_message_id IN (`+sources+`)`, sourceID, sourceID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE messages SET content = '[forgotten memory]'
WHERE id IN (SELECT assistant_message_id FROM assistant_turns WHERE source_message_id IN (`+sources+`))`, sourceID, sourceID)
	return err
}
