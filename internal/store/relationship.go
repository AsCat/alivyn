package store

import (
	"context"
	"database/sql"
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
	var exists int
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM relationship_state WHERE id = 1").Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
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
	return tx.Commit()
}

func advanceRelationshipTx(ctx context.Context, tx *sql.Tx, text string, disclosure bool, at time.Time, zone *time.Location) error {
	s, err := relationshipFrom(ctx, tx)
	if err != nil {
		return err
	}
	s, _ = relationship.Advance(s, text, disclosure, at, zone)
	return saveRelationshipTx(ctx, tx, s)
}
