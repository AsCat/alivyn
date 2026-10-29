package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/contact"
	"modernc.org/sqlite"
)

type ContactIntent struct {
	ID             int64
	Topic          string
	SourceRevision sql.NullInt64
	Due            time.Time
	Expires        time.Time
	State          string
	Reason         string
	IdempotencyKey string
}

type ContactEvent struct {
	IntentID int64
	Outcome  string
	Reason   string
	At       time.Time
}

type OutboxMessage struct {
	IntentID       int64
	MessageID      int64
	IdempotencyKey string
	Topic          string
	Body           string
	SentAt         time.Time
}

func createMemoryIntentTx(ctx context.Context, tx *sql.Tx, topic string, revisionID int64, happensAt, createdAt time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO contact_intents
  (topic, source_revision_id, due_ns, expires_ns, idempotency_key, created_ns, updated_ns)
  VALUES (?, ?, ?, ?, ?, ?, ?)`, topic, revisionID, happensAt.Add(time.Hour).UnixNano(),
		happensAt.Add(6*time.Hour).UnixNano(), fmt.Sprintf("memory:%d", revisionID),
		createdAt.UnixNano(), createdAt.UnixNano())
	return err
}

func cancelPendingTopicTx(ctx context.Context, tx *sql.Tx, topic string, at time.Time, reason string) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM contact_intents WHERE topic = ? AND state = 'pending'", topic)
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
		if _, err := tx.ExecContext(ctx, "UPDATE contact_intents SET state = 'canceled', reason = ?, updated_ns = ? WHERE id = ?", reason, at.UnixNano(), id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO contact_events(intent_id, outcome, reason, at_ns) VALUES (?, 'canceled', ?, ?)", id, reason, at.UnixNano()); err != nil {
			return err
		}
	}
	return nil
}

func (h *History) ContactPolicy(ctx context.Context) (contact.Policy, error) {
	var p contact.Policy
	var paused int
	err := h.db.QueryRowContext(ctx, "SELECT paused, quiet_start, quiet_end, daily_limit FROM contact_settings WHERE id = 1").
		Scan(&paused, &p.QuietStart, &p.QuietEnd, &p.DailyLimit)
	p.Paused = paused != 0
	return p, err
}

func (h *History) SetContactPolicy(ctx context.Context, p contact.Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	paused := 0
	if p.Paused {
		paused = 1
	}
	_, err := h.db.ExecContext(ctx, `UPDATE contact_settings
SET paused = ?, quiet_start = ?, quiet_end = ?, daily_limit = ? WHERE id = 1`,
		paused, p.QuietStart, p.QuietEnd, p.DailyLimit)
	return err
}

func (h *History) SetPaused(ctx context.Context, paused bool) error {
	value := 0
	if paused {
		value = 1
	}
	_, err := h.db.ExecContext(ctx, "UPDATE contact_settings SET paused = ? WHERE id = 1", value)
	return err
}

func (h *History) ContactIntents(ctx context.Context, limit int) ([]ContactIntent, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("intent limit must be between 1 and 1000")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, topic, source_revision_id, due_ns, expires_ns, state, reason, idempotency_key
FROM contact_intents ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var intents []ContactIntent
	for rows.Next() {
		var item ContactIntent
		var due, expires int64
		if err := rows.Scan(&item.ID, &item.Topic, &item.SourceRevision, &due, &expires,
			&item.State, &item.Reason, &item.IdempotencyKey); err != nil {
			return nil, err
		}
		item.Due, item.Expires = time.Unix(0, due), time.Unix(0, expires)
		intents = append(intents, item)
	}
	return intents, rows.Err()
}

func (h *History) ContactEvents(ctx context.Context, limit int) ([]ContactEvent, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("event limit must be between 1 and 1000")
	}
	rows, err := h.db.QueryContext(ctx, "SELECT intent_id, outcome, reason, at_ns FROM contact_events ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []ContactEvent
	for rows.Next() {
		var event ContactEvent
		var ns int64
		if err := rows.Scan(&event.IntentID, &event.Outcome, &event.Reason, &ns); err != nil {
			return nil, err
		}
		event.At = time.Unix(0, ns)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (h *History) Outbox(ctx context.Context, limit int) ([]OutboxMessage, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("outbox limit must be between 1 and 1000")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT intent_id, message_id, idempotency_key, topic, body, sent_ns
FROM contact_outbox ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []OutboxMessage
	for rows.Next() {
		var message OutboxMessage
		var ns int64
		if err := rows.Scan(&message.IntentID, &message.MessageID, &message.IdempotencyKey, &message.Topic, &message.Body, &ns); err != nil {
			return nil, err
		}
		message.SentAt = time.Unix(0, ns)
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

// ResumeRuntime records a startup and creates at most one time-gap intent per
// 72 hours after a gap of at least six hours. It does not send a message.
func (h *History) ResumeRuntime(ctx context.Context, at time.Time) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lastActive, lastGap sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT last_active_ns, last_gap_created_ns FROM runtime_state WHERE id = 1").Scan(&lastActive, &lastGap); err != nil {
		return err
	}
	recentUser := false
	var userStamp string
	err = tx.QueryRowContext(ctx, "SELECT created_at FROM messages WHERE role = 'user' ORDER BY id DESC LIMIT 1").Scan(&userStamp)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		userAt, parseErr := time.Parse(time.RFC3339Nano, userStamp)
		if parseErr != nil {
			return parseErr
		}
		recentUser = !userAt.After(at) && at.Sub(userAt) < 10*time.Minute
	}
	if lastActive.Valid && at.Sub(time.Unix(0, lastActive.Int64)) >= 6*time.Hour &&
		(!lastGap.Valid || at.Sub(time.Unix(0, lastGap.Int64)) >= 72*time.Hour) && !recentUser {
		var pending int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM contact_intents WHERE topic = 'time_gap' AND state = 'pending'").Scan(&pending); err != nil {
			return err
		}
		if pending == 0 {
			_, err := tx.ExecContext(ctx, `INSERT INTO contact_intents
  (topic, due_ns, expires_ns, idempotency_key, created_ns, updated_ns)
  VALUES ('time_gap', ?, ?, ?, ?, ?)`, at.UnixNano(), at.Add(6*time.Hour).UnixNano(),
				fmt.Sprintf("gap:%d", lastActive.Int64), at.UnixNano(), at.UnixNano())
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE runtime_state SET last_gap_created_ns = ? WHERE id = 1", at.UnixNano()); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE runtime_state SET last_active_ns = ? WHERE id = 1", at.UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *History) Heartbeat(ctx context.Context, at time.Time) error {
	_, err := h.db.ExecContext(ctx, "UPDATE runtime_state SET last_active_ns = ? WHERE id = 1", at.UnixNano())
	return err
}

// ProcessDue rereads every due intent and policy inside a transaction. Its
// built-in test transport writes to the local outbox in the same transaction.
func (h *History) ProcessDue(ctx context.Context, at time.Time, zone *time.Location, language string) ([]ContactEvent, error) {
	return h.processDue(ctx, at, zone, language, outboxSink{})
}

type txSink interface {
	Deliver(context.Context, *sql.Tx, ContactIntent, string, time.Time) error
}

type outboxSink struct{}

func (outboxSink) Deliver(ctx context.Context, tx *sql.Tx, intent ContactIntent, body string, at time.Time) error {
	result, err := tx.ExecContext(ctx, "INSERT INTO messages(role, content, created_at) VALUES ('assistant', ?, ?)", body, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	messageID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO contact_outbox(intent_id, message_id, idempotency_key, topic, body, sent_ns)
VALUES (?, ?, ?, ?, ?, ?)`, intent.ID, messageID, intent.IdempotencyKey, intent.Topic, body, at.UnixNano())
	return err
}

func (h *History) processDue(ctx context.Context, at time.Time, zone *time.Location, language string, sink txSink) ([]ContactEvent, error) {
	if zone == nil {
		return nil, errors.New("contact timezone is required")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id FROM contact_intents
WHERE state = 'pending' AND topic NOT IN ('initiative','life_reading') AND due_ns <= ? AND NOT EXISTS
  (SELECT 1 FROM contact_delivery WHERE contact_delivery.intent_id = contact_intents.id)
ORDER BY due_ns, id`, at.UnixNano())
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	var events []ContactEvent
	for _, id := range ids {
		var event ContactEvent
		var changed bool
		for attempt := 0; attempt < 3; attempt++ {
			event, changed, err = h.processOne(ctx, id, at, zone, language, sink)
			if err == nil || !sqliteBusy(err) || attempt == 2 {
				break
			}
			select {
			case <-ctx.Done():
				return events, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 25 * time.Millisecond):
			}
		}
		if err != nil {
			return events, err
		}
		if changed {
			events = append(events, event)
		}
	}
	return events, nil
}

func sqliteBusy(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && (sqliteErr.Code()&0xff == 5 || sqliteErr.Code()&0xff == 6)
}

func (h *History) processOne(ctx context.Context, id int64, at time.Time, zone *time.Location, language string, sink txSink) (ContactEvent, bool, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return ContactEvent{}, false, err
	}
	defer tx.Rollback()
	var item ContactIntent
	var dueNS, expiresNS int64
	err = tx.QueryRowContext(ctx, `SELECT id, topic, source_revision_id, due_ns, expires_ns, state, reason, idempotency_key
FROM contact_intents WHERE id = ?`, id).Scan(&item.ID, &item.Topic, &item.SourceRevision,
		&dueNS, &expiresNS, &item.State, &item.Reason, &item.IdempotencyKey)
	if errors.Is(err, sql.ErrNoRows) {
		return ContactEvent{}, false, nil
	}
	if err != nil {
		return ContactEvent{}, false, err
	}
	if item.State != "pending" {
		return ContactEvent{}, false, nil
	}
	item.Due, item.Expires = time.Unix(0, dueNS), time.Unix(0, expiresNS)
	result, err := evaluateIntentTx(ctx, tx, item, at, zone)
	if err != nil {
		return ContactEvent{}, false, err
	}
	if result.Reason == "not due" {
		return ContactEvent{}, false, nil
	}
	if result.Final {
		state := "canceled"
		if result.Reason == "expired intent is not backfilled" {
			state = "expired"
		}
		event := ContactEvent{IntentID: id, Outcome: "canceled", Reason: result.Reason, At: at}
		if err := updateIntentAndLog(ctx, tx, id, state, result.Reason, event); err != nil {
			return ContactEvent{}, false, err
		}
		return event, true, tx.Commit()
	}
	if !result.Ready {
		if item.Reason == result.Reason {
			return ContactEvent{}, false, nil
		}
		event := ContactEvent{IntentID: id, Outcome: "skipped", Reason: result.Reason, At: at}
		if err := updateIntentAndLog(ctx, tx, id, "pending", result.Reason, event); err != nil {
			return ContactEvent{}, false, err
		}
		return event, true, tx.Commit()
	}
	// Acquire a write lock, then reread source and policy immediately before
	// delivery. The outbox write and final state are committed atomically.
	reason := "event follow-up"
	if item.Topic == "time_gap" {
		reason = "time gap after resume"
	}
	update, err := tx.ExecContext(ctx, `UPDATE contact_intents SET state = 'sent', reason = ?, updated_ns = ?
WHERE id = ? AND state = 'pending'`, reason, at.UnixNano(), id)
	if err != nil {
		return ContactEvent{}, false, err
	}
	changed, err := update.RowsAffected()
	if err != nil {
		return ContactEvent{}, false, err
	}
	if changed != 1 {
		return ContactEvent{}, false, nil
	}
	fresh, err := evaluateIntentTx(ctx, tx, item, at, zone)
	if err != nil {
		return ContactEvent{}, false, err
	}
	if !fresh.Ready {
		state, outcome := "pending", "skipped"
		if fresh.Final {
			state, outcome = "canceled", "canceled"
			if fresh.Reason == "expired intent is not backfilled" {
				state = "expired"
			}
		}
		event := ContactEvent{IntentID: id, Outcome: outcome, Reason: fresh.Reason, At: at}
		if err := updateIntentAndLog(ctx, tx, id, state, fresh.Reason, event); err != nil {
			return ContactEvent{}, false, err
		}
		return event, true, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT delivery"); err != nil {
		return ContactEvent{}, false, err
	}
	body := contactBody(item.Topic, language)
	if err := sink.Deliver(ctx, tx, item, body, at); err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO delivery"); rollbackErr != nil {
			return ContactEvent{}, false, rollbackErr
		}
		if _, releaseErr := tx.ExecContext(ctx, "RELEASE delivery"); releaseErr != nil {
			return ContactEvent{}, false, releaseErr
		}
		event := ContactEvent{IntentID: id, Outcome: "failed", Reason: "test transport failed", At: at}
		if err := updateIntentAndLog(ctx, tx, id, "failed", event.Reason, event); err != nil {
			return ContactEvent{}, false, err
		}
		return event, true, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, "RELEASE delivery"); err != nil {
		return ContactEvent{}, false, err
	}
	event := ContactEvent{IntentID: id, Outcome: "sent", Reason: reason, At: at}
	if _, err := tx.ExecContext(ctx, "INSERT INTO contact_events(intent_id, outcome, reason, at_ns) VALUES (?, ?, ?, ?)", id, event.Outcome, event.Reason, at.UnixNano()); err != nil {
		return ContactEvent{}, false, err
	}
	return event, true, tx.Commit()
}

func evaluateIntentTx(ctx context.Context, tx *sql.Tx, item ContactIntent, at time.Time, zone *time.Location) (contact.Decision, error) {
	return evaluateIntentTxExcluding(ctx, tx, item, at, zone, 0)
}

func evaluateIntentTxExcluding(ctx context.Context, tx *sql.Tx, item ContactIntent, at time.Time, zone *time.Location, excludedDeliveryID int64) (contact.Decision, error) {
	var running bool
	if err := tx.QueryRowContext(ctx, "SELECT mode='running' FROM character_control WHERE id=1").Scan(&running); err != nil {
		return contact.Decision{}, err
	}
	if !running {
		return contact.Decision{Reason: "character suspended"}, nil
	}
	var away bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM character_absence WHERE until_ns>?)", at.UnixNano()).Scan(&away); err != nil {
		return contact.Decision{}, err
	}
	if away {
		return contact.Decision{Reason: "character away"}, nil
	}
	current := true
	if item.Topic == "initiative" && at.Before(item.Expires) {
		decision, err := initiativeCurrentTx(ctx, tx, item, at, zone)
		if err != nil || !decision.Ready {
			return decision, err
		}
	}
	if item.Topic == "world_update" || item.Topic == "world_scene" || item.Topic == "life_reading" {
		// An unsolicited update must never overtake a direct reply that is still
		// queued for the same person.
		var pendingReplies int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM feishu_inbox WHERE state IN ('pending','ready')").Scan(&pendingReplies); err != nil {
			return contact.Decision{}, err
		}
		if pendingReplies > 0 {
			return contact.Decision{Reason: "private reply pending"}, nil
		}
	}
	if item.Topic == "world_scene" {
		id, err := strconv.ParseInt(strings.TrimPrefix(item.IdempotencyKey, "worldsim:"), 10, 64)
		if !strings.HasPrefix(item.IdempotencyKey, "worldsim:") || err != nil || id < 1 {
			return contact.Decision{Final: true, Reason: "world scene source unavailable"}, nil
		}
		var occurred int64
		err = tx.QueryRowContext(ctx, `SELECT occurred_ns FROM simulated_events WHERE id=? AND world_id='tidehaven'
AND EXISTS(SELECT 1 FROM json_each(observer_ids_json) WHERE value='default')`, id).Scan(&occurred)
		if errors.Is(err, sql.ErrNoRows) {
			return contact.Decision{Final: true, Reason: "world scene source unavailable"}, nil
		}
		if err != nil {
			return contact.Decision{}, err
		}
		var latestUser string
		err = tx.QueryRowContext(ctx, `SELECT created_at FROM messages WHERE role='user'
AND NOT EXISTS(SELECT 1 FROM local_commands WHERE source_message_id=messages.id)
ORDER BY id DESC LIMIT 1`).Scan(&latestUser)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return contact.Decision{}, err
		}
		if err == nil {
			lastAt, err := time.Parse(time.RFC3339Nano, latestUser)
			if err != nil {
				return contact.Decision{}, err
			}
			if lastAt.UnixNano() > occurred {
				return contact.Decision{Final: true, Reason: "conversation resumed after world scene"}, nil
			}
		}
	}
	if item.Topic == "life_reading" {
		id, err := strconv.ParseInt(strings.TrimPrefix(item.IdempotencyKey, "reading:"), 10, 64)
		if !strings.HasPrefix(item.IdempotencyKey, "reading:") || err != nil {
			return contact.Decision{Final: true, Reason: "reading source unavailable"}, nil
		}
		var occurred int64
		if err := tx.QueryRowContext(ctx, "SELECT occurred_ns FROM life_readings WHERE id=?", id).Scan(&occurred); errors.Is(err, sql.ErrNoRows) {
			return contact.Decision{Final: true, Reason: "reading source unavailable"}, nil
		} else if err != nil {
			return contact.Decision{}, err
		}
		var latestUser string
		err = tx.QueryRowContext(ctx, `SELECT created_at FROM messages WHERE role='user'
AND NOT EXISTS(SELECT 1 FROM local_commands WHERE source_message_id=messages.id)
ORDER BY id DESC LIMIT 1`).Scan(&latestUser)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return contact.Decision{}, err
		}
		if err == nil {
			lastAt, err := time.Parse(time.RFC3339Nano, latestUser)
			if err != nil {
				return contact.Decision{}, err
			}
			if lastAt.UnixNano() > occurred {
				return contact.Decision{Final: true, Reason: "conversation resumed after reading"}, nil
			}
		}
	}
	if item.SourceRevision.Valid {
		var latest int64
		var action, provenance string
		err := tx.QueryRowContext(ctx, `SELECT id, action, provenance FROM memory_revisions
WHERE topic = ? ORDER BY id DESC LIMIT 1`, item.Topic).Scan(&latest, &action, &provenance)
		if err != nil {
			return contact.Decision{}, err
		}
		current = latest == item.SourceRevision.Int64 && action == "upsert" && provenance == "user_stated"
	}
	var paused int
	var policy contact.Policy
	if err := tx.QueryRowContext(ctx, "SELECT paused, quiet_start, quiet_end, daily_limit FROM contact_settings WHERE id = 1").
		Scan(&paused, &policy.QuietStart, &policy.QuietEnd, &policy.DailyLimit); err != nil {
		return contact.Decision{}, err
	}
	policy.Paused = paused != 0
	if err := policy.Validate(); err != nil {
		return contact.Decision{}, err
	}
	local := at.In(zone)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	end := start.AddDate(0, 0, 1)
	var sentToday int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM contact_outbox WHERE sent_ns >= ? AND sent_ns < ?", start.UnixNano(), end.UnixNano()).Scan(&sentToday); err != nil {
		return contact.Decision{}, err
	}
	var channelToday int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM contact_delivery d
JOIN contact_intents i ON i.id = d.intent_id
WHERE ((d.state = 'pending' AND i.state = 'pending') OR (d.state = 'sent' AND d.sent_ns >= ? AND d.sent_ns < ?))
AND d.intent_id != ?`, start.UnixNano(), end.UnixNano(), excludedDeliveryID).Scan(&channelToday); err != nil {
		return contact.Decision{}, err
	}
	sentToday += channelToday
	return contact.Evaluate(at, zone, policy, contact.Intent{Due: item.Due, Expires: item.Expires, Current: current}, sentToday), nil
}

func updateIntentAndLog(ctx context.Context, tx *sql.Tx, id int64, state, reason string, event ContactEvent) error {
	if _, err := tx.ExecContext(ctx, "UPDATE contact_intents SET state = ?, reason = ?, updated_ns = ? WHERE id = ?", state, reason, event.At.UnixNano(), id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO contact_events(intent_id, outcome, reason, at_ns) VALUES (?, ?, ?, ?)", id, event.Outcome, event.Reason, event.At.UnixNano())
	return err
}

func contactBody(topic, language string) string {
	if topic == "initiative" || topic == "life_reading" {
		return ""
	} // A personal initiative always needs composed text.
	if language == "zh-CN" {
		if topic == "time_gap" {
			return "好像过了一段时间。现在想聊聊吗？"
		}
		if topic == "world_update" {
			return "之前聊过的那盒录音，有了点新进展。"
		}
		return "面试怎么样？如果你想聊，我在。"
	}
	if topic == "time_gap" {
		return "Some time seems to have passed. Would you like to talk?"
	}
	if topic == "world_update" {
		return "There's an update on that recording we talked about."
	}
	return "How did your interview go? I'm here if you want to talk."
}
