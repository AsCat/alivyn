package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/memory"
)

type Fact struct {
	ID              int64     `json:"id"`
	Topic           string    `json:"topic"`
	HappensAt       time.Time `json:"happens_at"`
	Precision       string    `json:"precision"`
	Provenance      string    `json:"provenance"`
	SourceMessageID int64     `json:"source_message_id"`
	SourceText      string    `json:"source_text"`
	SourceAt        time.Time `json:"source_at"`
}

var topicName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// RecordModelInference is a separate entry point for future extractors. It
// cannot replace an active user-stated fact or create an unsourced memory.
func (h *History) RecordModelInference(ctx context.Context, sourceMessageID int64, topic string, happensAt time.Time, precision string) error {
	if sourceMessageID < 1 || !topicName.MatchString(topic) || happensAt.IsZero() {
		return errors.New("invalid inferred fact")
	}
	if precision != "exact" && precision != "part_of_day" && precision != "day" {
		return errors.New("invalid inferred fact precision")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var role, sourceAt string
	if err := tx.QueryRowContext(ctx, "SELECT role, created_at FROM messages WHERE id = ?", sourceMessageID).Scan(&role, &sourceAt); err != nil {
		return err
	}
	if role != "user" {
		return errors.New("inferred fact must cite a user message")
	}
	var previousID int64
	var previousAction, previousProvenance string
	err = tx.QueryRowContext(ctx, "SELECT id, action, provenance FROM memory_revisions WHERE topic = ? ORDER BY id DESC LIMIT 1", topic).
		Scan(&previousID, &previousAction, &previousProvenance)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && (previousProvenance == "user_stated" || previousAction == "forget") {
		return errors.New("model inference cannot replace a user decision")
	}
	var supersedes any
	if err == nil {
		supersedes = previousID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memory_revisions
  (topic, action, happens_at, precision, provenance, source_message_id, source_at, supersedes_id)
  VALUES (?, 'upsert', ?, ?, 'model_inferred', ?, ?, ?)`, topic,
		happensAt.UTC().Format(time.RFC3339Nano), precision, sourceMessageID, sourceAt, supersedes)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// AddUserMessage commits the utterance and any recognized fact together.
func (h *History) AddUserMessage(ctx context.Context, text string, at time.Time, zone *time.Location) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("message cannot be empty")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := addUserMessageTx(ctx, tx, text, at, zone); err != nil {
		return err
	}
	return tx.Commit()
}

func addUserMessageTx(ctx context.Context, tx *sql.Tx, text string, at time.Time, zone *time.Location) (int64, error) {
	change, recognized := memory.Extract(text, at, zone)
	personal, personalRecognized := memory.ExtractPersonal(text)
	result, err := tx.ExecContext(ctx, "INSERT INTO messages(role, content, created_at) VALUES('user', ?, ?)", text, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	messageID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runtime_state SET last_active_ns = MAX(COALESCE(last_active_ns, 0), ?) WHERE id = 1`, at.UnixNano()); err != nil {
		return 0, err
	}
	if err := cancelPendingTopicTx(ctx, tx, "time_gap", at, "user already active"); err != nil {
		return 0, err
	}
	if personalRecognized {
		if err := recordPersonalTx(ctx, tx, personal, messageID, at); err != nil {
			return 0, err
		}
	}
	if err := advanceRelationshipTx(ctx, tx, text, personalRecognized && !personal.Forget, at, zone); err != nil {
		return 0, err
	}
	if recognized {
		var previous sql.NullInt64
		err = tx.QueryRowContext(ctx, "SELECT id FROM memory_revisions WHERE topic = ? ORDER BY id DESC LIMIT 1", change.Topic).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		var supersedes any
		if previous.Valid {
			supersedes = previous.Int64
		}
		var happens any
		if change.Action == memory.Upsert {
			happens = change.HappensAt.UTC().Format(time.RFC3339Nano)
		}
		revisionResult, err := tx.ExecContext(ctx, `INSERT INTO memory_revisions
  (topic, action, happens_at, precision, provenance, source_message_id, source_at, supersedes_id)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, change.Topic, string(change.Action), happens, change.Precision, change.Provenance,
			messageID, at.UTC().Format(time.RFC3339Nano), supersedes)
		if err != nil {
			return 0, err
		}
		if err := cancelPendingTopicTx(ctx, tx, change.Topic, at, "superseded intent"); err != nil {
			return 0, err
		}
		if change.Action == memory.Upsert {
			revisionID, err := revisionResult.LastInsertId()
			if err != nil {
				return 0, err
			}
			if err := createMemoryIntentTx(ctx, tx, change.Topic, revisionID, change.HappensAt, at); err != nil {
				return 0, err
			}
		}
		if change.Action == memory.Forget && change.Topic == "interview" {
			// Redact each source utterance and its immediate assistant replies,
			// including replies that repeated a date without naming the topic.
			rows, err := tx.QueryContext(ctx, "SELECT source_message_id FROM memory_revisions WHERE topic = ?", change.Topic)
			if err != nil {
				return 0, err
			}
			var sources []int64
			for rows.Next() {
				var sourceID int64
				if err := rows.Scan(&sourceID); err != nil {
					rows.Close()
					return 0, err
				}
				sources = append(sources, sourceID)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return 0, err
			}
			rows.Close()
			for _, sourceID := range sources {
				if _, err := tx.ExecContext(ctx, `UPDATE messages SET content = '[forgotten memory]'
WHERE id >= ? AND id < COALESCE((SELECT MIN(id) FROM messages WHERE role = 'user' AND id > ?), 9223372036854775807)`, sourceID, sourceID); err != nil {
					return 0, err
				}
			}
			// Also remove other transcript lines that explicitly name the topic.
			if _, err := tx.ExecContext(ctx, `UPDATE messages SET content = '[forgotten memory]'
WHERE content LIKE '%面试%' OR lower(content) LIKE '%interview%'`); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE memory_revisions SET happens_at = NULL, precision = NULL WHERE topic = ?", change.Topic); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE contact_outbox SET topic = 'forgotten', body = '[forgotten memory]' WHERE topic = ?", change.Topic); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE contact_delivery SET body = '[forgotten memory]'
WHERE intent_id IN (SELECT id FROM contact_intents WHERE topic = ?)`, change.Topic); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE feishu_inbox SET reply = '[forgotten memory]' WHERE reply LIKE '%面试%' OR lower(reply) LIKE '%interview%'"); err != nil {
				return 0, err
			}
			if err := cancelPendingTopicTx(ctx, tx, change.Topic, at, "forgotten memory"); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE contact_intents
SET topic = 'forgotten', source_revision_id = NULL, due_ns = ?, expires_ns = ?, reason = 'forgotten memory', updated_ns = ?
WHERE topic = ?`, at.UnixNano(), at.UnixNano(), at.UnixNano(), change.Topic); err != nil {
				return 0, err
			}
		}
	}
	return messageID, nil
}

// ActiveFacts returns the newest non-canceled revision for each topic. This is
// the only memory view passed to the model and exported to users.
func (h *History) ActiveFacts(ctx context.Context) ([]Fact, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT r.id, r.topic, r.happens_at, r.precision, r.provenance,
       r.source_message_id, m.content, r.source_at
FROM memory_revisions r JOIN messages m ON m.id = r.source_message_id
WHERE r.action = 'upsert' AND r.id = (SELECT MAX(n.id) FROM memory_revisions n WHERE n.topic = r.topic)
ORDER BY r.topic`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var facts []Fact
	for rows.Next() {
		var fact Fact
		var happens, source string
		if err := rows.Scan(&fact.ID, &fact.Topic, &happens, &fact.Precision, &fact.Provenance,
			&fact.SourceMessageID, &fact.SourceText, &source); err != nil {
			return nil, err
		}
		fact.HappensAt, err = time.Parse(time.RFC3339Nano, happens)
		if err != nil {
			return nil, fmt.Errorf("invalid memory timestamp: %w", err)
		}
		fact.SourceAt, err = time.Parse(time.RFC3339Nano, source)
		if err != nil {
			return nil, fmt.Errorf("invalid memory source timestamp: %w", err)
		}
		facts = append(facts, fact)
	}
	return facts, rows.Err()
}

// RecentForModel omits source messages of superseded or forgotten facts.
// Superseded transcript entries remain visible to the user; forgotten entries
// are redacted in the database.
func (h *History) RecentForModel(ctx context.Context, limit int) ([]Entry, error) {
	entries, err := h.Recent(ctx, limit)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT source_message_id, topic, action,
  r.id = (SELECT MAX(n.id) FROM memory_revisions n WHERE n.topic = r.topic)
  FROM memory_revisions r`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stale := map[int64]bool{}
	forgetRequest := map[int64]bool{}
	cancelRequest := map[int64]bool{}
	for rows.Next() {
		var id int64
		var topic, action string
		var latest bool
		if err := rows.Scan(&id, &topic, &action, &latest); err != nil {
			return nil, err
		}
		if !latest || action != "upsert" {
			stale[id] = true
		}
		if latest && action == "forget" {
			forgetRequest[id] = true
		}
		if latest && action == "cancel" {
			cancelRequest[id] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	personalRows, err := h.db.QueryContext(ctx, `SELECT source_message_id, action,
  id = (SELECT MAX(n.id) FROM personal_memory n WHERE n.memory_key = p.memory_key)
  FROM personal_memory p`)
	if err != nil {
		return nil, err
	}
	defer personalRows.Close()
	for personalRows.Next() {
		var id int64
		var action string
		var latest bool
		if err := personalRows.Scan(&id, &action, &latest); err != nil {
			return nil, err
		}
		if !latest || action == "forget" {
			stale[id] = true
		}
		if latest && action == "forget" {
			forgetRequest[id] = true
		}
	}
	if err := personalRows.Err(); err != nil {
		return nil, err
	}
	filtered := make([]Entry, 0, len(entries))
	suppressReply := false
	for _, entry := range entries {
		if entry.Role == "assistant" && suppressReply {
			continue
		}
		if entry.Role == "user" {
			suppressReply = false
		}
		if cancelRequest[entry.ID] {
			filtered = append(filtered, entry)
			continue
		}
		if forgetRequest[entry.ID] {
			filtered = append(filtered, Entry{ID: entry.ID, Role: entry.Role, Content: "Please acknowledge that I asked you to forget a stored fact, without recalling its details.", CreatedAt: entry.CreatedAt})
			continue
		}
		if stale[entry.ID] {
			if entry.Role == "user" {
				suppressReply = true
			}
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}
