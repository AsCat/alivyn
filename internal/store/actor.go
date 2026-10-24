package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
)

// ActorDecision returns the short, inspectable action chosen for a user turn.
// A retry can reuse this choice without replaying the planner.
func (h *History) ActorDecision(ctx context.Context, userMessageID int64) (actor.Decision, bool, error) {
	var raw string
	err := h.db.QueryRowContext(ctx, "SELECT decision_json FROM actor_decisions WHERE source_message_id = ?", userMessageID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return actor.Decision{}, false, nil
	}
	if err != nil {
		return actor.Decision{}, false, err
	}
	var decision actor.Decision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return actor.Decision{}, false, err
	}
	return decision, true, nil
}

func (h *History) SaveActorDecision(ctx context.Context, userMessageID int64, decision actor.Decision, at time.Time) error {
	if userMessageID < 1 {
		return errors.New("actor decision requires a user message")
	}
	raw, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	_, err = h.db.ExecContext(ctx, `INSERT INTO actor_decisions(source_message_id, decision_json, decided_at)
VALUES (?, ?, ?) ON CONFLICT(source_message_id) DO UPDATE SET decision_json=excluded.decision_json,
decided_at=excluded.decided_at`, userMessageID, string(raw), at.UTC().Format(time.RFC3339Nano))
	return err
}
