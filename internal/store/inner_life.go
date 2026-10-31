package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

type InnerWakeClaim struct {
	ID        int64
	SourceID  int64
	ThroughID int64
	Ready     bool
	Reason    string
}

// ClaimInnerWake is independent of the outbound-contact policy. Quiet hours
// and contact pause silence delivery, not the character's private attention.
// The claim and model-call budget survive process restarts.
func (h *History) ClaimInnerWake(ctx context.Context, now time.Time, zone *time.Location) (InnerWakeClaim, error) {
	var claim InnerWakeClaim
	if zone == nil {
		return claim, errors.New("inner wake timezone is required")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return claim, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE inner_wake_clock SET checks=checks WHERE id=1"); err != nil {
		return claim, err
	}
	var mode string
	if err := tx.QueryRowContext(ctx, "SELECT mode FROM character_control WHERE id=1").Scan(&mode); err != nil {
		return claim, err
	}
	if mode != "running" {
		claim.Reason = "character suspended"
		return claim, nil
	}
	var away bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM character_absence WHERE until_ns>?)", now.UnixNano()).Scan(&away); err != nil {
		return claim, err
	}
	if away {
		claim.Reason = "character away"
		return claim, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT m.id FROM messages m WHERE m.role='user'
AND NOT EXISTS(SELECT 1 FROM local_commands c WHERE c.source_message_id=m.id)
ORDER BY m.id DESC LIMIT 1`).Scan(&claim.SourceID)
	if errors.Is(err, sql.ErrNoRows) {
		claim.Reason = "no conversation"
		return claim, nil
	}
	if err != nil {
		return claim, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM messages").Scan(&claim.ThroughID); err != nil {
		return claim, err
	}
	var next int64
	var day string
	var checks int
	var previousSource int64
	if err := tx.QueryRowContext(ctx, "SELECT next_check_ns,budget_day,checks,last_source_id FROM inner_wake_clock WHERE id=1").Scan(&next, &day, &checks, &previousSource); err != nil {
		return claim, err
	}
	today := now.In(zone).Format("2006-01-02")
	if day != today {
		checks = 0
	}
	// New conversation can refresh a settled state twice beyond the routine
	// budget; it does not unlock an unbounded burst of model calls.
	limit := 8
	if previousSource != claim.SourceID {
		limit += 2
	}
	if checks >= limit {
		claim.Reason = "inner wake daily budget"
		return claim, nil
	}
	if now.UnixNano() < next && previousSource == claim.SourceID {
		claim.Reason = "inner wake cooldown"
		return claim, nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE inner_wake_clock SET next_check_ns=?,budget_day=?,checks=?,last_source_id=? WHERE id=1", now.Add(2*time.Hour).UnixNano(), today, checks+1, claim.SourceID); err != nil {
		return claim, err
	}
	r, err := tx.ExecContext(ctx, "INSERT INTO inner_wakes(source_message_id,through_message_id,at_ns) VALUES(?,?,?)", claim.SourceID, claim.ThroughID, now.UnixNano())
	if err != nil {
		return claim, err
	}
	claim.ID, err = r.LastInsertId()
	if err != nil {
		return claim, err
	}
	claim.Ready = true
	claim.Reason = "ready to wake"
	return claim, tx.Commit()
}

func (h *History) FinishInnerWake(ctx context.Context, claim InnerWakeClaim, state character.InnerState, now time.Time) (bool, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var outcome string
	if err := tx.QueryRowContext(ctx, "SELECT outcome FROM inner_wakes WHERE id=?", claim.ID).Scan(&outcome); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil // Reset or forgetting removed the in-flight wake.
		}
		return false, err
	}
	if outcome != "considering" {
		return false, nil
	}
	var latest int64
	var mode string
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM messages").Scan(&latest); err != nil {
		return false, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT mode FROM character_control WHERE id=1").Scan(&mode); err != nil {
		return false, err
	}
	if latest != claim.ThroughID || mode != "running" {
		if _, err := tx.ExecContext(ctx, "UPDATE inner_wakes SET outcome='superseded' WHERE id=?", claim.ID); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	data, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE inner_wakes SET outcome='done',state_json=? WHERE id=?", string(data), claim.ID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE inner_wake_clock SET next_check_ns=? WHERE id=1", now.Add(time.Duration(state.RevisitIn)*time.Minute).UnixNano()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (h *History) FailInnerWake(ctx context.Context, id int64, now time.Time) error {
	result, err := h.db.ExecContext(ctx, "UPDATE inner_wakes SET outcome='error' WHERE id=? AND outcome='considering'", id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return err
	}
	_, err = h.db.ExecContext(ctx, "UPDATE inner_wake_clock SET next_check_ns=? WHERE id=1", now.Add(30*time.Minute).UnixNano())
	return err
}

// LatestInnerState returns the last completed wake; old saved attention is
// history for the next wake, not proof that she still feels the same way.
func (h *History) LatestInnerState(ctx context.Context) (character.InnerState, time.Time, bool, error) {
	var raw string
	var stamp int64
	err := h.db.QueryRowContext(ctx, "SELECT state_json,at_ns FROM inner_wakes WHERE outcome='done' ORDER BY id DESC LIMIT 1").Scan(&raw, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return character.InnerState{}, time.Time{}, false, nil
	}
	if err != nil {
		return character.InnerState{}, time.Time{}, false, err
	}
	var state character.InnerState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return character.InnerState{}, time.Time{}, false, err
	}
	return state, time.Unix(0, stamp), true, nil
}
