package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/AsCat/alivyn/internal/world"
)

// AdvanceWorld commits each simulated day and its event in one transaction.
// The clock is local to the individual's configured timezone. Repeated calls
// within a day are idempotent, including after a process restart.
func (h *History) AdvanceWorld(ctx context.Context, now time.Time, zone *time.Location) (world.State, error) {
	if zone == nil {
		return world.State{}, errors.New("world timezone is required")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return world.State{}, err
	}
	defer tx.Rollback()
	var state world.State
	err = tx.QueryRowContext(ctx, "SELECT day, local_date, progress, last_user_turns FROM world_state WHERE id = 1").Scan(&state.Day, &state.Date, &state.Progress, &state.LastUserTurns)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	today := now.In(zone).Format("2006-01-02")
	if state.Date >= today {
		return state, nil
	}
	var turns int
	if err := tx.QueryRowContext(ctx, "SELECT meaningful_turns FROM relationship_state WHERE id = 1").Scan(&turns); err != nil {
		return state, err
	}
	var dates []string
	if state.Day == 0 {
		// Existing installations begin their world today. Old chat history does
		// not become a fictional shared past.
		state.LastUserTurns = turns
		dates = []string{today}
	} else {
		last, err := time.ParseInLocation("2006-01-02", state.Date, zone)
		if err != nil {
			return state, fmt.Errorf("invalid world date: %w", err)
		}
		end, err := time.ParseInLocation("2006-01-02", today, zone)
		if err != nil {
			return state, err
		}
		// Cap catch-up work. A long absence is recorded as a gap, not
		// narrated as dozens of witnessed daily events.
		if end.Sub(last) > 7*24*time.Hour {
			last = end.AddDate(0, 0, -7)
		}
		for day := last.AddDate(0, 0, 1); !day.After(end); day = day.AddDate(0, 0, 1) {
			dates = append(dates, day.Format("2006-01-02"))
		}
	}
	for _, date := range dates {
		var event world.Event
		state, event = world.Next(state, date, turns)
		related := 0
		if event.RelatedToUser {
			related = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO world_events(day, local_date, kind, zh, en, related_to_user)
VALUES (?, ?, ?, ?, ?, ?)`, event.Day, event.Date, event.Kind, event.Zh, event.En, related); err != nil {
			return state, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO world_state(id, day, local_date, progress, last_user_turns)
VALUES (1, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET day=excluded.day,
local_date=excluded.local_date, progress=excluded.progress, last_user_turns=excluded.last_user_turns`,
		state.Day, state.Date, state.Progress, state.LastUserTurns); err != nil {
		return state, err
	}
	return state, tx.Commit()
}

func (h *History) WorldEvents(ctx context.Context, limit int) ([]world.Event, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("world event limit must be between 1 and 100")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT day, local_date, kind, zh, en, related_to_user
FROM world_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reverse []world.Event
	for rows.Next() {
		var event world.Event
		var related int
		if err := rows.Scan(&event.Day, &event.Date, &event.Kind, &event.Zh, &event.En, &related); err != nil {
			return nil, err
		}
		event.RelatedToUser = related == 1
		reverse = append(reverse, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(reverse)-1; i < j; i, j = i+1, j-1 {
		reverse[i], reverse[j] = reverse[j], reverse[i]
	}
	return reverse, nil
}
