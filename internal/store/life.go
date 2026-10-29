package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/AsCat/alivyn/internal/life"
	"github.com/AsCat/alivyn/internal/relationship"
)

// ClaimLifeReading gives one runtime at most one reading per local day. Its
// generation fences an in-flight model result against /reset.
func (h *History) ClaimLifeReading(ctx context.Context, now time.Time, zone *time.Location, force bool) (int64, bool, error) {
	if zone == nil {
		return 0, false, errors.New("life reading timezone is required")
	}
	local := now.In(zone)
	if !force && (local.Hour() < 10 || local.Hour() >= 21) {
		return 0, false, nil
	}
	day := local.Format("2006-01-02")
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	var generation, until int64
	var claimed, completed string
	if err := tx.QueryRowContext(ctx, `SELECT generation,claimed_date,claimed_until_ns,completed_date FROM life_reading_clock WHERE id=1`).Scan(&generation, &claimed, &until, &completed); err != nil {
		return 0, false, err
	}
	if completed == day || claimed == day && until > now.UnixNano() {
		return generation, false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE life_reading_clock SET claimed_date=?,claimed_until_ns=? WHERE id=1`, day, now.Add(15*time.Minute).UnixNano()); err != nil {
		return 0, false, err
	}
	return generation, true, tx.Commit()
}

func (h *History) ReleaseLifeReading(ctx context.Context, generation int64, date string) error {
	_, err := h.db.ExecContext(ctx, `UPDATE life_reading_clock SET claimed_date='',claimed_until_ns=0 WHERE id=1 AND generation=? AND claimed_date=?`, generation, date)
	return err
}

func (h *History) SaveLifeReading(ctx context.Context, generation int64, reading life.Reading) (life.Reading, error) {
	if reading.At.IsZero() || reading.Title == "" || reading.URL == "" || reading.Excerpt == "" || reading.Thought == "" || reading.Date == "" {
		return life.Reading{}, errors.New("incomplete sourced life reading")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return life.Reading{}, err
	}
	defer tx.Rollback()
	var current, until int64
	var claimed string
	if err := tx.QueryRowContext(ctx, `SELECT generation,claimed_date,claimed_until_ns FROM life_reading_clock WHERE id=1`).Scan(&current, &claimed, &until); err != nil {
		return life.Reading{}, err
	}
	if current != generation || claimed != reading.Date || until < reading.At.UnixNano() {
		return life.Reading{}, ErrTurnSuperseded
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO life_readings(local_date,occurred_ns,title,source_url,excerpt,thought,feeling,question)
VALUES(?,?,?,?,?,?,?,?)`, reading.Date, reading.At.UnixNano(), reading.Title, reading.URL, reading.Excerpt, reading.Thought, reading.Feeling, reading.Question)
	if err != nil {
		return life.Reading{}, err
	}
	reading.ID, err = result.LastInsertId()
	if err != nil {
		return life.Reading{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE life_reading_clock SET claimed_date='',claimed_until_ns=0,completed_date=? WHERE id=1`, reading.Date); err != nil {
		return life.Reading{}, err
	}
	// A concrete new experience may motivate contact. The usual quiet hours,
	// daily limit, pause, pending-reply and final send gates still apply.
	relation, err := relationshipFrom(ctx, tx)
	if err != nil {
		return life.Reading{}, err
	}
	var lastUser string
	err = tx.QueryRowContext(ctx, `SELECT created_at FROM messages WHERE role='user'
AND NOT EXISTS(SELECT 1 FROM local_commands WHERE source_message_id=messages.id)
ORDER BY id DESC LIMIT 1`).Scan(&lastUser)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return life.Reading{}, err
	}
	var recentlyTalked bool
	if err == nil {
		at, parseErr := time.Parse(time.RFC3339Nano, lastUser)
		if parseErr != nil {
			return life.Reading{}, parseErr
		}
		recentlyTalked = !reading.At.Before(at) && reading.At.Sub(at) <= 72*time.Hour
	}
	if relation.Stage != relationship.Stranger && !relation.Reserved && recentlyTalked {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO contact_intents(topic,due_ns,expires_ns,idempotency_key,created_ns,updated_ns)
VALUES('life_reading',?,?,?,?,?)`, reading.At.Add(3*time.Minute).UnixNano(), reading.At.Add(18*time.Hour).UnixNano(),
			fmt.Sprintf("reading:%d", reading.ID), reading.At.UnixNano(), reading.At.UnixNano())
		if err != nil {
			return life.Reading{}, err
		}
	}
	return reading, tx.Commit()
}

func (h *History) LifeReading(ctx context.Context, id int64) (life.Reading, error) {
	var r life.Reading
	var stamp int64
	err := h.db.QueryRowContext(ctx, `SELECT id,local_date,occurred_ns,title,source_url,excerpt,thought,feeling,question FROM life_readings WHERE id=?`, id).
		Scan(&r.ID, &r.Date, &stamp, &r.Title, &r.URL, &r.Excerpt, &r.Thought, &r.Feeling, &r.Question)
	r.At = time.Unix(0, stamp)
	return r, err
}

func (h *History) LifeReadingsBefore(ctx context.Context, before time.Time, limit int) ([]life.Reading, error) {
	if limit < 1 || limit > 50 {
		return nil, errors.New("invalid life reading limit")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id,local_date,occurred_ns,title,source_url,excerpt,thought,feeling,question
FROM life_readings WHERE occurred_ns<=? ORDER BY occurred_ns DESC,id DESC LIMIT ?`, before.UnixNano(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var readings []life.Reading
	for rows.Next() {
		var r life.Reading
		var stamp int64
		if err := rows.Scan(&r.ID, &r.Date, &stamp, &r.Title, &r.URL, &r.Excerpt, &r.Thought, &r.Feeling, &r.Question); err != nil {
			return nil, err
		}
		r.At = time.Unix(0, stamp)
		readings = append(readings, r)
	}
	return readings, rows.Err()
}
