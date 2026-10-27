package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// HasInitiativeTestRequest lets the live worker notice a one-shot test without
// waiting for its normal one-minute consideration tick.
func (h *History) HasInitiativeTestRequest(ctx context.Context, now time.Time) (bool, error) {
	var pending bool
	err := h.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM initiative_test_request WHERE expires_ns>?)", now.UnixNano()).Scan(&pending)
	return pending, err
}

// requestInitiativeTestTx is invoked only by an explicit local control command.
// It advances an existing proposal when possible; otherwise it asks the normal
// model-driven scheduler to consider one contact immediately.
func requestInitiativeTestTx(ctx context.Context, tx *sql.Tx, at time.Time, commandID int64) (string, error) {
	var id int64
	var due int64
	err := tx.QueryRowContext(ctx, `SELECT i.id,i.due_ns FROM contact_intents i
JOIN initiative_checks c ON c.intent_id=i.id
WHERE i.topic='initiative' AND i.state='pending' ORDER BY i.id DESC LIMIT 1`).Scan(&id, &due)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err == nil {
		if due <= at.UnixNano() {
			return "processing", nil
		}
		var attempted bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM contact_delivery WHERE intent_id=?)", id).Scan(&attempted); err != nil {
			return "", err
		}
		if attempted {
			return "processing", nil
		}
		if _, err := tx.ExecContext(ctx, "UPDATE contact_intents SET due_ns=?,updated_ns=? WHERE id=?", at.UnixNano(), at.UnixNano(), id); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE initiative_checks SET through_message_id=? WHERE intent_id=?", commandID, id); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO initiative_test_intents(intent_id) VALUES(?)", id); err != nil {
			return "", err
		}
		return "advanced", nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO initiative_test_request(id,requested_ns,expires_ns) VALUES(1,?,?)
ON CONFLICT(id) DO UPDATE SET requested_ns=excluded.requested_ns,expires_ns=excluded.expires_ns`, at.UnixNano(), at.Add(5*time.Minute).UnixNano()); err != nil {
		return "", err
	}
	return "requested", nil
}
