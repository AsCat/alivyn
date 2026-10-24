package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

// ConfirmActorDecision records world attention only after a reply passes review.
// A planned decision whose reply fails cannot influence the character's world.
func (h *History) ConfirmActorDecision(ctx context.Context, userMessageID int64, decision actor.Decision, review actor.Review, at time.Time) error {
	if !review.Pass {
		return errors.New("cannot confirm a rejected actor reply")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var saved string
	if err := tx.QueryRowContext(ctx, "SELECT decision_json FROM actor_decisions WHERE source_message_id = ?", userMessageID).Scan(&saved); err != nil {
		return err
	}
	raw, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	if saved != string(raw) {
		return errors.New("actor decision changed before reply confirmation")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM goal_world_engagements WHERE source_message_id = ?", userMessageID); err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, id := range decision.EvidenceIDs {
		selected[id] = true
	}
	seen := map[string]bool{}
	for _, evidenceID := range review.UsedWorldIDs {
		if !selected[evidenceID] || !strings.HasPrefix(evidenceID, "worldv2:") {
			return fmt.Errorf("review used unselected world evidence %q", evidenceID)
		}
		eventID, err := strconv.ParseInt(strings.TrimPrefix(evidenceID, "worldv2:"), 10, 64)
		if err != nil || eventID < 1 {
			return fmt.Errorf("invalid world evidence reference %q", evidenceID)
		}
		var objectID string
		if err := tx.QueryRowContext(ctx, "SELECT object_id FROM goal_world_events WHERE id = ?", eventID).Scan(&objectID); err != nil {
			return err
		}
		if seen[objectID] {
			continue
		}
		seen[objectID] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO goal_world_engagements
(source_message_id, object_id, event_id, at_ns) VALUES (?, ?, ?, ?)`,
			userMessageID, objectID, eventID, at.UnixNano()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
