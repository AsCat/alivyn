package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AsCat/alivyn/internal/world"
)

// AdvanceGoalWorld commits goal state and causally linked events atomically.
// The actor path uses a separate ledger so old authored world history remains
// intact and can be restored by switching engines.
func (h *History) AdvanceGoalWorld(ctx context.Context, now time.Time, zone *time.Location) (world.GoalState, error) {
	if zone == nil {
		return world.GoalState{}, errors.New("world timezone is required")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return world.GoalState{}, err
	}
	defer tx.Rollback()
	var state world.GoalState
	var raw string
	seeded := false
	err = tx.QueryRowContext(ctx, "SELECT state_json FROM goal_world_state WHERE id = 1").Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	if err == nil {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return state, fmt.Errorf("invalid goal world state: %w", err)
		}
	} else {
		state, err = seedGoalFromLegacyTx(ctx, tx)
		if err != nil {
			return state, err
		}
		seeded = state.Day > 0
		if seeded {
			encoded, err := json.Marshal(state)
			if err != nil {
				return state, err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO goal_world_state(id, state_json) VALUES (1, ?)", string(encoded)); err != nil {
				return state, err
			}
		}
	}
	today := now.In(zone).Format("2006-01-02")
	if state.Date >= today {
		if seeded {
			return state, tx.Commit()
		}
		return state, nil
	}
	var dates []string
	if state.Day == 0 {
		dates = []string{today}
	} else {
		last, err := time.ParseInLocation("2006-01-02", state.Date, zone)
		if err != nil {
			return state, fmt.Errorf("invalid goal world date: %w", err)
		}
		end, err := time.ParseInLocation("2006-01-02", today, zone)
		if err != nil {
			return state, err
		}
		if end.Sub(last) > 7*24*time.Hour {
			last = end.AddDate(0, 0, -7)
		}
		for day := last.AddDate(0, 0, 1); !day.After(end); day = day.AddDate(0, 0, 1) {
			dates = append(dates, day.Format("2006-01-02"))
		}
	}
	for _, date := range dates {
		dayStart, err := time.ParseInLocation("2006-01-02", date, zone)
		if err != nil {
			return state, err
		}
		objectID := fmt.Sprintf("recording-batch:%d", state.Batch)
		var engagements int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM goal_world_engagements
WHERE object_id = ? AND at_ns < ?`, objectID, dayStart.AddDate(0, 0, 1).UnixNano()).Scan(&engagements); err != nil {
			return state, err
		}
		action := world.SelectGoalAction(state, engagements)
		next, event, err := world.ApplyGoalAction(state, action, date, engagements)
		if err != nil {
			return state, err
		}
		if err := world.ValidateGoalTransition(state, next, event, engagements); err != nil {
			return state, err
		}
		event.Catchup = date != today
		var parent any
		if event.ParentID > 0 {
			parent = event.ParentID
		}
		related, catchup := 0, 0
		if event.RelatedToUser {
			related = 1
		}
		if event.Catchup {
			catchup = 1
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO goal_world_events
(day, local_date, action, object_id, parent_event_id, provenance, zh, en, related_to_user, catchup)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.Day, event.Date, event.Action,
			event.ObjectID, parent, event.Provenance, event.Zh, event.En, related, catchup)
		if err != nil {
			return state, err
		}
		next.LastEventID, err = result.LastInsertId()
		if err != nil {
			return state, err
		}
		state = next
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return state, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO goal_world_state(id, state_json) VALUES (1, ?)
ON CONFLICT(id) DO UPDATE SET state_json = excluded.state_json`, string(encoded)); err != nil {
		return state, err
	}
	return state, tx.Commit()
}

// Existing authored events become sourced history, not rewritten fiction. The
// new goal projection resumes from the last recorded batch and local date.
func seedGoalFromLegacyTx(ctx context.Context, tx *sql.Tx) (world.GoalState, error) {
	var state world.GoalState
	var legacyDay int
	var legacyDate string
	err := tx.QueryRowContext(ctx, "SELECT day, local_date FROM world_state WHERE id = 1").Scan(&legacyDay, &legacyDate)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT day, local_date, kind, zh, en, related_to_user FROM world_events ORDER BY day")
	if err != nil {
		return state, err
	}
	type oldEvent struct {
		day, related       int
		date, kind, zh, en string
	}
	var old []oldEvent
	for rows.Next() {
		var item oldEvent
		if err := rows.Scan(&item.day, &item.date, &item.kind, &item.zh, &item.en, &item.related); err != nil {
			rows.Close()
			return state, err
		}
		old = append(old, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return state, err
	}
	if err := rows.Close(); err != nil {
		return state, err
	}
	if len(old) == 0 || old[len(old)-1].day != legacyDay {
		return state, errors.New("legacy world state has no matching final event")
	}
	lastBatch, lastID := 0, int64(0)
	for _, item := range old {
		batch := (item.day-1)/7 + 1
		var parent any
		if batch == lastBatch {
			parent = lastID
		} else {
			state.LedgerChecked = false
			state.IdentityVerified = false
			state.AudioRecovered = false
			state.PublicationDiscussed = false
			state.Reflected = false
			state.PermissionChecked = false
			state.PublicationAuthorized = false
			state.PrivateCopyReady = false
			state.Closed = false
		}
		action := legacyGoalAction(item.day, item.kind)
		result, err := tx.ExecContext(ctx, `INSERT INTO goal_world_events
(day, local_date, action, object_id, parent_event_id, provenance, zh, en, related_to_user, catchup)
VALUES (?, ?, ?, ?, ?, 'authored_v1', ?, ?, ?, 0)`, item.day, item.date, action,
			fmt.Sprintf("recording-batch:%d", batch), parent, item.zh, item.en, item.related)
		if err != nil {
			return state, err
		}
		lastID, err = result.LastInsertId()
		if err != nil {
			return state, err
		}
		lastBatch = batch
		switch action {
		case "inspect":
			state.LedgerChecked = true
		case "repair":
			state.AudioRecovered = true
		case "discuss":
			state.PublicationDiscussed = true
		case "reflect":
			state.Reflected = true
		case "search_permission":
			state.PermissionChecked = true
		case "private_preview":
			state.PrivateCopyReady = true
		case "catalog":
			state.Closed = true
		}
	}
	state.Day, state.Date, state.Batch = legacyDay, legacyDate, lastBatch
	state.LastWorldEngagements, state.LastEventID = 0, lastID
	return state, nil
}

func legacyGoalAction(day int, kind string) string {
	switch (day - 1) % 7 {
	case 0:
		return "intake"
	case 1, 4:
		return "repair"
	case 2:
		return "discuss"
	case 3:
		if kind == "reflection" {
			return "reflect"
		}
		return "inspect"
	case 5:
		return "private_preview"
	default:
		return "catalog"
	}
}

func (h *History) GoalWorldEvents(ctx context.Context, limit int) ([]world.GoalEvent, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("world event limit must be between 1 and 100")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, day, local_date, action, object_id,
COALESCE(parent_event_id, 0), provenance, zh, en, related_to_user, catchup
FROM goal_world_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reverse []world.GoalEvent
	for rows.Next() {
		var event world.GoalEvent
		var related, catchup int
		if err := rows.Scan(&event.ID, &event.Day, &event.Date, &event.Action, &event.ObjectID,
			&event.ParentID, &event.Provenance, &event.Zh, &event.En, &related, &catchup); err != nil {
			return nil, err
		}
		event.RelatedToUser, event.Catchup = related == 1, catchup == 1
		reverse = append(reverse, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(reverse)-1; left < right; left, right = left+1, right-1 {
		reverse[left], reverse[right] = reverse[right], reverse[left]
	}
	return reverse, nil
}
