package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ChannelContact is a durable attempt to deliver one contact intent.
type ChannelContact struct {
	IntentID int64
	Key      string
	Topic    string
	Body     string
	FirstTry bool
}

// RetryChannelComposition is only called before the first send. It removes a
// placeholder delivery so the next attempt must compose again, never send it.
func (h *History) RetryChannelComposition(ctx context.Context, id int64) error {
	_, err := h.db.ExecContext(ctx, "DELETE FROM contact_delivery WHERE intent_id=? AND state='pending' AND attempts=1", id)
	return err
}

// NextChannelContact reevaluates policy immediately before recording an attempt.
// A recorded attempt is retried only within Feishu's one-hour UUID window.
func (h *History) NextChannelContact(ctx context.Context, at time.Time, zone *time.Location, language string) (ChannelContact, bool, error) {
	if zone == nil {
		return ChannelContact{}, false, errors.New("contact timezone is required")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id FROM contact_intents WHERE state = 'pending' AND due_ns <= ? ORDER BY due_ns, id`, at.UnixNano())
	if err != nil {
		return ChannelContact{}, false, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return ChannelContact{}, false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ChannelContact{}, false, err
	}
	for _, id := range ids {
		item, ready, err := h.prepareChannelContact(ctx, id, at, zone, language)
		if err != nil || ready {
			return item, ready, err
		}
	}
	return ChannelContact{}, false, nil
}

func (h *History) prepareChannelContact(ctx context.Context, id int64, at time.Time, zone *time.Location, language string) (ChannelContact, bool, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return ChannelContact{}, false, err
	}
	defer tx.Rollback()
	var item ContactIntent
	var due, expires int64
	err = tx.QueryRowContext(ctx, `SELECT id, topic, source_revision_id, due_ns, expires_ns, state, reason, idempotency_key FROM contact_intents WHERE id = ?`, id).
		Scan(&item.ID, &item.Topic, &item.SourceRevision, &due, &expires, &item.State, &item.Reason, &item.IdempotencyKey)
	if errors.Is(err, sql.ErrNoRows) || item.State != "pending" && err == nil {
		return ChannelContact{}, false, nil
	}
	if err != nil {
		return ChannelContact{}, false, err
	}
	item.Due, item.Expires = time.Unix(0, due), time.Unix(0, expires)
	var first, last sql.NullInt64
	var attempts int
	var body, deliveryState string
	err = tx.QueryRowContext(ctx, `SELECT body, first_attempt_ns, last_attempt_ns, attempts, state FROM contact_delivery WHERE intent_id = ?`, id).
		Scan(&body, &first, &last, &attempts, &deliveryState)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ChannelContact{}, false, err
	}
	if err == nil && deliveryState != "pending" {
		return ChannelContact{}, false, nil
	}
	if first.Valid && at.Sub(time.Unix(0, first.Int64)) >= 55*time.Minute {
		if _, err := tx.ExecContext(ctx, `UPDATE contact_delivery SET state = 'failed' WHERE intent_id = ?`, id); err != nil {
			return ChannelContact{}, false, err
		}
		event := ContactEvent{IntentID: id, Outcome: "failed", Reason: "delivery uncertain after Feishu deduplication window", At: at}
		if err := updateIntentAndLog(ctx, tx, id, "failed", event.Reason, event); err != nil {
			return ChannelContact{}, false, err
		}
		return ChannelContact{}, false, tx.Commit()
	}
	if last.Valid && at.Sub(time.Unix(0, last.Int64)) < 15*time.Second {
		return ChannelContact{}, false, nil
	}
	decision, err := evaluateIntentTxExcluding(ctx, tx, item, at, zone, id)
	if err != nil {
		return ChannelContact{}, false, err
	}
	if decision.Final {
		state := "canceled"
		if decision.Reason == "expired intent is not backfilled" {
			state = "expired"
		}
		event := ContactEvent{IntentID: id, Outcome: "canceled", Reason: decision.Reason, At: at}
		if err := updateIntentAndLog(ctx, tx, id, state, decision.Reason, event); err != nil {
			return ChannelContact{}, false, err
		}
		return ChannelContact{}, false, tx.Commit()
	}
	if !decision.Ready {
		if item.Reason != decision.Reason {
			event := ContactEvent{IntentID: id, Outcome: "skipped", Reason: decision.Reason, At: at}
			if err := updateIntentAndLog(ctx, tx, id, "pending", decision.Reason, event); err != nil {
				return ChannelContact{}, false, err
			}
			return ChannelContact{}, false, tx.Commit()
		}
		return ChannelContact{}, false, nil
	}
	if !first.Valid {
		body = contactBody(item.Topic, language)
		_, err = tx.ExecContext(ctx, `INSERT INTO contact_delivery(intent_id, body, first_attempt_ns, last_attempt_ns, attempts, state) VALUES (?, ?, ?, ?, 1, 'pending')`, id, body, at.UnixNano(), at.UnixNano())
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE contact_delivery SET last_attempt_ns = ?, attempts = attempts + 1 WHERE intent_id = ?`, at.UnixNano(), id)
	}
	if err != nil {
		return ChannelContact{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE contact_intents SET reason = 'Feishu delivery pending', updated_ns = ? WHERE id = ?`, at.UnixNano(), id); err != nil {
		return ChannelContact{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ChannelContact{}, false, err
	}
	return ChannelContact{IntentID: id, Key: item.IdempotencyKey, Topic: item.Topic, Body: body, FirstTry: !first.Valid || body == ""}, true, nil
}

// FinalizeChannelContact persists the first generated body and checks the
// contact policy again after model generation, immediately before send.
func (h *History) FinalizeChannelContact(ctx context.Context, id int64, body string, at time.Time, zone *time.Location) (bool, error) {
	if body == "" || zone == nil {
		return false, errors.New("contact body and timezone are required")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var item ContactIntent
	var due, expires int64
	err = tx.QueryRowContext(ctx, `SELECT id, topic, source_revision_id, due_ns, expires_ns, state, reason, idempotency_key
FROM contact_intents WHERE id = ?`, id).Scan(&item.ID, &item.Topic, &item.SourceRevision,
		&due, &expires, &item.State, &item.Reason, &item.IdempotencyKey)
	if errors.Is(err, sql.ErrNoRows) || item.State != "pending" && err == nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	item.Due, item.Expires = time.Unix(0, due), time.Unix(0, expires)
	var state string
	err = tx.QueryRowContext(ctx, "SELECT state FROM contact_delivery WHERE intent_id = ?", id).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) || state != "pending" && err == nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	decision, err := evaluateIntentTxExcluding(ctx, tx, item, at, zone, id)
	if err != nil {
		return false, err
	}
	if !decision.Ready {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE contact_delivery SET body = ? WHERE intent_id = ? AND state = 'pending'", body, id); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// MarkChannelContactSent records a confirmed API response in local history.
func (h *History) MarkChannelContactSent(ctx context.Context, id int64, at time.Time) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var body, state string
	err = tx.QueryRowContext(ctx, `SELECT body, state FROM contact_delivery WHERE intent_id = ?`, id).Scan(&body, &state)
	if err != nil {
		return err
	}
	if state == "sent" {
		return nil
	}
	if state != "pending" {
		return errors.New("contact delivery is not pending")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages(role, content, created_at) VALUES ('assistant', ?, ?)`, body, at.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE contact_delivery SET state = 'sent', sent_ns = ? WHERE intent_id = ?`, at.UnixNano(), id); err != nil {
		return err
	}
	event := ContactEvent{IntentID: id, Outcome: "sent", Reason: "Feishu delivery confirmed", At: at}
	if err := updateIntentAndLog(ctx, tx, id, "sent", event.Reason, event); err != nil {
		return err
	}
	return tx.Commit()
}
