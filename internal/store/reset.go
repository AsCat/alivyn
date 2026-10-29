package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var ErrTurnSuperseded = errors.New("turn was cleared or superseded")

func IsResetCommand(text string) bool {
	switch strings.TrimSpace(text) {
	case "/清除记忆", "/reset":
		return true
	}
	return false
}

// resetCharacterTx resets the authored life and relationship atomically, not
// provider/channel configuration or the user's contact restrictions. IDs are
// never reused: an old model result cannot attach itself to the new life.
func resetCharacterTx(ctx context.Context, tx *sql.Tx, command string, at time.Time) (int64, error) {
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO inbox_receipts(event_id) SELECT event_id FROM feishu_inbox"); err != nil {
		return 0, err
	}
	for _, table := range []string{
		"character_absence", "reply_deferrals",
		"feishu_reply_parts", "feishu_reply_groups", "feishu_inbox", "initiative_test_intents", "initiative_test_request", "initiative_checks", "contact_events", "contact_outbox", "contact_delivery_parts", "contact_delivery", "contact_intents",
		"goal_world_engagements", "actor_decisions", "actor_continuity", "character_self_facts", "character_turns", "assistant_turns", "command_results", "local_commands",
		"personal_memory", "memory_revisions", "relationship_snapshots", "relationship_state",
		"life_readings", "goal_world_events", "goal_world_state", "world_events", "world_state", "messages",
	} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE life_reading_clock SET generation=generation+1, claimed_date='', claimed_until_ns=0, completed_date='' WHERE id=1`); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE character_control SET mode='running',return_mode='running',confirm_until_ns=0,blocked_through_id=0 WHERE id=1"); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE runtime_state SET last_active_ns=?,last_gap_created_ns=NULL WHERE id=1", at.UnixNano()); err != nil {
		return 0, err
	}
	// The API budget is operational state, not a character memory. Keeping
	// it prevents repeated resets from causing unbounded background calls.
	r, err := tx.ExecContext(ctx, "INSERT INTO messages(role,content,created_at) VALUES('user',?,?)", strings.TrimSpace(command), at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO local_commands(source_message_id,command) VALUES(?,'reset')", id)
	return id, err
}

func (h *History) CommandReply(ctx context.Context, sourceID int64, language string, persona ...string) (string, bool, error) {
	if sourceID == 0 {
		if err := h.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM messages").Scan(&sourceID); err != nil {
			return "", false, err
		}
	}
	var command string
	err := h.db.QueryRowContext(ctx, "SELECT command FROM local_commands WHERE source_message_id=?", sourceID).Scan(&command)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if command != "reset" {
		var zh, en string
		err := h.db.QueryRowContext(ctx, "SELECT zh,en FROM command_results WHERE source_message_id=?", sourceID).Scan(&zh, &en)
		if err != nil {
			return "", true, err
		}
		reply := en
		if language == "zh-CN" {
			reply = zh
		}
		if command == "persona" && len(persona) > 0 {
			reply += "\n" + persona[0]
		}
		if command == "routine" && len(persona) > 1 {
			reply += "\n" + persona[1]
		}
		return reply, true, nil
	}
	if language == "zh-CN" {
		return "记忆已清除，已恢复初始人物状态。人物设定、模型配置和飞书连接已保留。", true, nil
	}
	return "Memory cleared. The character has returned to its initial state. Character settings, model configuration and the Feishu connection are preserved.", true, nil
}

// DeliverFeishu fences reset against the final transport operation. A reset
// can cancel a prepared reply; an already-started send must finish first.
func (h *History) DeliverFeishu(ctx context.Context, eventID string, send func() error) error {
	h.deliveryMu.RLock()
	defer h.deliveryMu.RUnlock()
	var exists bool
	if err := h.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM feishu_inbox WHERE event_id=? AND state='ready')", eventID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := send(); err != nil {
		return err
	}
	return h.MarkFeishuSent(ctx, eventID)
}

func (h *History) DeliverContact(ctx context.Context, id int64, at time.Time, zone *time.Location, body string, send func() error) error {
	h.deliveryMu.RLock()
	defer h.deliveryMu.RUnlock()
	ok, err := h.FinalizeChannelContact(ctx, id, body, at, zone)
	if err != nil || !ok {
		return err
	}
	if err := send(); err != nil {
		return err
	}
	return h.MarkChannelContactSent(ctx, id, time.Now())
}

// DeliverContactParts sends a short burst as one logical proactive contact.
// Every bubble has its own durable receipt and transport idempotency key.
func (h *History) DeliverContactParts(ctx context.Context, id int64, at time.Time, zone *time.Location, body string, parts []string, send func(int, int, string) error) error {
	h.deliveryMu.RLock()
	defer h.deliveryMu.RUnlock()
	started := time.Now()
	current := func() time.Time { return at.Add(time.Since(started)) }
	saved, err := h.ContactParts(ctx, id)
	if err != nil {
		return err
	}
	if len(saved) == 0 {
		ok, err := h.FinalizeChannelContact(ctx, id, body, at, zone)
		if err != nil || !ok {
			return err
		}
		if err := h.PrepareContactParts(ctx, id, parts); err != nil {
			return err
		}
		saved, err = h.ContactParts(ctx, id)
		if err != nil {
			return err
		}
	}
	for _, part := range saved {
		if part.Sent {
			continue
		}
		allowed, err := h.ContactPartAllowed(ctx, id, current(), zone)
		if err != nil {
			return err
		}
		if !allowed {
			return h.CompleteContactPartial(ctx, id, current(), "partial burst stopped by contact policy")
		}
		if err := send(part.Index, len(saved), part.Text); err != nil {
			return err
		}
		if err := h.MarkChannelContactPartSent(ctx, id, part.Index, current()); err != nil {
			return err
		}
	}
	return h.MarkChannelContactSent(ctx, id, current())
}
