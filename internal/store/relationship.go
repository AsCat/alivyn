package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AsCat/alivyn/internal/relationship"
)

// Relationship is a private, coarse record of how much has actually been
// shared. The conversation, rather than the assistant's claims, moves it.
func (h *History) Relationship(ctx context.Context) (relationship.State, error) {
	return relationshipFrom(ctx, h.db)
}

// RelationshipAt reads the stage when a particular user turn arrived. A
// current request for distance still overrides an older queued reply.
func (h *History) RelationshipAt(ctx context.Context, sourceMessageID int64) (relationship.State, error) {
	var raw string
	err := h.db.QueryRowContext(ctx, "SELECT state_json FROM relationship_snapshots WHERE source_message_id = ?", sourceMessageID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return h.Relationship(ctx) // Messages recorded before snapshots existed.
	}
	if err != nil {
		return relationship.State{}, err
	}
	var state relationship.State
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return relationship.State{}, err
	}
	current, err := h.Relationship(ctx)
	if err != nil {
		return relationship.State{}, err
	}
	state.Reserved = state.Reserved || current.Reserved
	return state, nil
}

type relationshipQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func relationshipFrom(ctx context.Context, q relationshipQuerier) (relationship.State, error) {
	var s relationship.State
	var reserved int
	err := q.QueryRowContext(ctx, `SELECT stage, meaningful_turns, shared_days, disclosures, last_shared_day, reserved
FROM relationship_state WHERE id = 1`).Scan(&s.Stage, &s.MeaningfulTurns, &s.SharedDays, &s.Disclosures, &s.LastSharedDay, &reserved)
	if errors.Is(err, sql.ErrNoRows) {
		return relationship.Initial(), nil
	}
	if err != nil {
		return s, err
	}
	s.Reserved = reserved == 1
	return s, nil
}

func saveRelationshipTx(ctx context.Context, tx *sql.Tx, s relationship.State) error {
	reserved := 0
	if s.Reserved {
		reserved = 1
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO relationship_state
(id, stage, meaningful_turns, shared_days, disclosures, last_shared_day, reserved)
VALUES (1, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET stage=excluded.stage, meaningful_turns=excluded.meaningful_turns,
shared_days=excluded.shared_days, disclosures=excluded.disclosures,
last_shared_day=excluded.last_shared_day, reserved=excluded.reserved`,
		s.Stage, s.MeaningfulTurns, s.SharedDays, s.Disclosures, s.LastSharedDay, reserved)
	return err
}

func (h *History) ensureRelationship(ctx context.Context) error {
	var version int
	err := h.db.QueryRowContext(ctx, "SELECT version FROM relationship_meta WHERE id = 1").Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if version >= 2 {
		return nil
	}
	// Backfill once for existing installations. Close rows before writing because
	// this SQLite connection is intentionally limited to one open connection.
	rows, err := h.db.QueryContext(ctx, `SELECT m.content, m.created_at,
EXISTS(SELECT 1 FROM personal_memory p WHERE p.source_message_id = m.id AND p.action = 'upsert')
FROM messages m WHERE m.role = 'user' ORDER BY m.id`)
	if err != nil {
		return err
	}
	s := relationship.Initial()
	for rows.Next() {
		var content, stamp string
		var disclosure int
		if err := rows.Scan(&content, &stamp, &disclosure); err != nil {
			rows.Close()
			return err
		}
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			rows.Close()
			return fmt.Errorf("invalid relationship source timestamp: %w", err)
		}
		s, _ = relationship.Advance(s, content, disclosure == 1, at, time.Local)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveRelationshipTx(ctx, tx, s); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO relationship_meta(id, version) VALUES(1, 2)
ON CONFLICT(id) DO UPDATE SET version=excluded.version`); err != nil {
		return err
	}
	return tx.Commit()
}

func advanceRelationshipTx(ctx context.Context, tx *sql.Tx, sourceMessageID int64, text string, disclosure bool, at time.Time, zone *time.Location) error {
	s, err := relationshipFrom(ctx, tx)
	if err != nil {
		return err
	}
	s, _ = relationship.Advance(s, text, disclosure, at, zone)
	if err := saveRelationshipTx(ctx, tx, s); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO relationship_snapshots(source_message_id, state_json) VALUES (?, ?)", sourceMessageID, string(raw))
	return err
}
