package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/AsCat/alivyn/internal/character"
)

func (h *History) CharacterTuning(ctx context.Context) (character.Tuning, error) {
	var tuning character.Tuning
	err := h.db.QueryRowContext(ctx, "SELECT initiative FROM character_tuning WHERE id=1").Scan(&tuning.Initiative)
	return tuning, err
}

func (h *History) SetInitiative(ctx context.Context, level int) error {
	if level < 0 || level > 100 {
		return errors.New("initiative must be between 0 and 100")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setInitiativeTx(ctx, tx, level, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func setInitiativeTx(ctx context.Context, tx *sql.Tx, level int, at time.Time) error {
	if level < 0 || level > 100 {
		return errors.New("initiative must be between 0 and 100")
	}
	var prior int
	if err := tx.QueryRowContext(ctx, "SELECT initiative FROM character_tuning WHERE id=1").Scan(&prior); err != nil {
		return err
	}
	if prior == level {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE character_tuning SET initiative=? WHERE id=1", level); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE initiative_clock SET next_check_ns=0 WHERE id=1"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE initiative_checks SET outcome='superseded',reason='initiative tuning changed' WHERE outcome='considering'"); err != nil {
		return err
	}
	return cancelPendingTopicTx(ctx, tx, "initiative", at, "initiative tuning changed")
}
