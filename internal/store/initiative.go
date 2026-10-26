package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/contact"
	"github.com/AsCat/alivyn/internal/relationship"
)

type InitiativeCheck struct {
	ID        int64
	SourceID  int64
	ThroughID int64
	Reason    string
	Ready     bool
}

const (
	initiativeConsiderationGap = 2 * time.Hour
	initiativeRoutineBudget    = 4
	initiativeFreshBudget      = 6
)

// Repeated consideration of an unchanged conversation cannot consume the
// entire day and prevent a later, newly shared conversation from being heard.
// The extra two checks still have a strict daily API limit.
func initiativeBudgetExhaustedTx(ctx context.Context, tx *sql.Tx, count int, sourceID int64) (bool, error) {
	if count < initiativeRoutineBudget {
		return false, nil
	}
	if count >= initiativeFreshBudget {
		return true, nil
	}
	var lastSource int64
	err := tx.QueryRowContext(ctx, "SELECT source_message_id FROM initiative_checks ORDER BY id DESC LIMIT 1").Scan(&lastSource)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil // An inspection/reset may remove checks, not the budget.
	}
	return lastSource == sourceID, err
}

// initiativeWindowTx is shared by proposal creation and the final send gate.
// No model call can override these checks.
func initiativeWindowTx(ctx context.Context, tx *sql.Tx, now time.Time, zone *time.Location, excludeIntent int64) (InitiativeCheck, error) {
	var check InitiativeCheck
	var running bool
	if err := tx.QueryRowContext(ctx, "SELECT mode='running' FROM character_control WHERE id=1").Scan(&running); err != nil {
		return check, err
	}
	if !running {
		check.Reason = "character suspended"
		return check, nil
	}
	var away bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM character_absence WHERE until_ns>?)", now.UnixNano()).Scan(&away); err != nil {
		return check, err
	}
	if away {
		check.Reason = "character away"
		return check, nil
	}
	if zone == nil {
		return check, errors.New("initiative timezone is required")
	}
	var paused bool
	var start, end string
	if err := tx.QueryRowContext(ctx, "SELECT paused,quiet_start,quiet_end FROM contact_settings WHERE id=1").Scan(&paused, &start, &end); err != nil {
		return check, err
	}
	if paused {
		check.Reason = "paused"
		return check, nil
	}
	if contact.Quiet(now.In(zone), start, end) {
		check.Reason = "quiet hours"
		return check, nil
	}
	r, err := relationshipFrom(ctx, tx)
	if err != nil {
		return check, err
	}
	if r.Reserved {
		check.Reason = "user requested distance"
		return check, nil
	}
	if r.Stage == relationship.Stranger {
		check.Reason = "not enough shared conversation"
		return check, nil
	}
	var userStamp, latestStamp, role string
	err = tx.QueryRowContext(ctx, "SELECT id,created_at FROM messages WHERE role='user' ORDER BY id DESC LIMIT 1").Scan(&check.SourceID, &userStamp)
	if errors.Is(err, sql.ErrNoRows) {
		check.Reason = "no conversation"
		return check, nil
	}
	if err != nil {
		return check, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT id,role,created_at FROM messages ORDER BY id DESC LIMIT 1").Scan(&check.ThroughID, &role, &latestStamp); err != nil {
		return check, err
	}
	userAt, err := time.Parse(time.RFC3339Nano, userStamp)
	if err != nil {
		return check, err
	}
	lastAt, err := time.Parse(time.RFC3339Nano, latestStamp)
	if err != nil {
		return check, err
	}
	if now.Before(userAt) || now.Sub(userAt) > 72*time.Hour {
		check.Reason = "conversation too old"
		return check, nil
	}
	var pending int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM feishu_inbox WHERE state IN ('pending','ready')").Scan(&pending); err != nil {
		return check, err
	}
	var completedQuiet bool
	if role == "user" {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM character_turns
WHERE source_message_id=? AND committed=1 AND json_extract(turn_json,'$.mode')='wait')`, check.ThroughID).Scan(&completedQuiet); err != nil {
			return check, err
		}
	}
	if pending > 0 || (role != "assistant" && !completedQuiet) {
		check.Reason = "private reply pending"
		return check, nil
	}
	gap := 30 * time.Minute
	if r.Stage == relationship.Acquaintance {
		gap = 45 * time.Minute
	}
	gap += time.Duration(check.SourceID%11) * time.Minute
	if now.Sub(lastAt) < gap {
		check.Reason = "conversation still recent"
		return check, nil
	}
	var unanswered int
	if err := tx.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM contact_delivery WHERE state='sent' AND sent_ns>=?) +
 (SELECT COUNT(*) FROM contact_outbox WHERE sent_ns>=?)`, userAt.UnixNano(), userAt.UnixNano()).Scan(&unanswered); err != nil {
		return check, err
	}
	if unanswered > 0 {
		check.Reason = "waiting for user to return"
		return check, nil
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM contact_intents WHERE state='pending' AND expires_ns>? AND id!=?", now.UnixNano(), excludeIntent).Scan(&pending); err != nil {
		return check, err
	}
	if pending > 0 {
		check.Reason = "another contact pending"
		return check, nil
	}
	policy, err := evaluateIntentTxExcluding(ctx, tx, ContactIntent{ID: excludeIntent, Topic: "initiative_budget", Due: now, Expires: now.Add(time.Minute)}, now, zone, excludeIntent)
	if err != nil {
		return check, err
	}
	if !policy.Ready {
		check.Reason = policy.Reason
		return check, nil
	}
	check.Ready = true
	check.Reason = "ready to consider"
	return check, nil
}

// ClaimInitiative persists the cadence and daily API budget before any model
// call, so restarting or a second worker cannot repeatedly assess the same gap.
func (h *History) ClaimInitiative(ctx context.Context, now time.Time, zone *time.Location) (InitiativeCheck, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return InitiativeCheck{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE initiative_clock SET checks=checks WHERE id=1"); err != nil {
		return InitiativeCheck{}, err
	}
	c, err := initiativeWindowTx(ctx, tx, now, zone, 0)
	if err != nil || !c.Ready {
		return c, err
	}
	var next int64
	var day string
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT next_check_ns,budget_day,checks FROM initiative_clock WHERE id=1").Scan(&next, &day, &count); err != nil {
		return c, err
	}
	today := now.In(zone).Format("2006-01-02")
	if day != today {
		count = 0
	}
	exhausted, err := initiativeBudgetExhaustedTx(ctx, tx, count, c.SourceID)
	if err != nil {
		return c, err
	}
	if exhausted {
		c.Ready = false
		c.Reason = "daily consideration budget"
		return c, nil
	}
	if now.UnixNano() < next {
		c.Ready = false
		c.Reason = "consideration cooldown"
		return c, nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE initiative_clock SET next_check_ns=?,budget_day=?,checks=? WHERE id=1", now.Add(initiativeConsiderationGap).UnixNano(), today, count+1); err != nil {
		return c, err
	}
	r, err := tx.ExecContext(ctx, "INSERT INTO initiative_checks(source_message_id,through_message_id,at_ns) VALUES(?,?,?)", c.SourceID, c.ThroughID, now.UnixNano())
	if err != nil {
		return c, err
	}
	c.ID, err = r.LastInsertId()
	if err != nil {
		return c, err
	}
	return c, tx.Commit()
}

func (h *History) InitiativeStatus(ctx context.Context, now time.Time, zone *time.Location) (InitiativeCheck, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return InitiativeCheck{}, err
	}
	defer tx.Rollback()
	c, err := initiativeWindowTx(ctx, tx, now, zone, 0)
	if err != nil || !c.Ready {
		return c, err
	}
	var next int64
	var day string
	var checks int
	if err := tx.QueryRowContext(ctx, "SELECT next_check_ns,budget_day,checks FROM initiative_clock WHERE id=1").Scan(&next, &day, &checks); err != nil {
		return c, err
	}
	var exhausted bool
	if day == now.In(zone).Format("2006-01-02") {
		exhausted, err = initiativeBudgetExhaustedTx(ctx, tx, checks, c.SourceID)
		if err != nil {
			return c, err
		}
	}
	if exhausted {
		c.Ready = false
		c.Reason = "daily consideration budget"
	} else if now.UnixNano() < next {
		c.Ready = false
		c.Reason = "consideration cooldown"
	}
	return c, nil
}

func (h *History) FinishInitiative(ctx context.Context, c InitiativeCheck, p character.Initiative, now time.Time, zone *time.Location) (bool, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var state string
	err = tx.QueryRowContext(ctx, "SELECT outcome FROM initiative_checks WHERE id=?", c.ID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if state != "considering" {
		return false, nil
	}
	var latest int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM messages").Scan(&latest); err != nil {
		return false, err
	}
	outcome := "wait"
	if latest != c.ThroughID {
		outcome = "superseded"
	}
	if p.Action == "contact" && outcome != "superseded" {
		fresh, err := initiativeWindowTx(ctx, tx, now, zone, 0)
		if err != nil {
			return false, err
		}
		if !fresh.Ready || fresh.SourceID != c.SourceID {
			outcome = "superseded"
		} else {
			outcome = "queued"
		}
	}
	var duplicate int
	if outcome == "queued" {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM initiative_checks c JOIN contact_intents i ON i.id=c.intent_id
WHERE c.topic_key=? AND c.at_ns>? AND i.state IN ('pending','sent')`, p.TopicKey(), now.Add(-72*time.Hour).UnixNano()).Scan(&duplicate); err != nil {
			return false, err
		}
		if duplicate > 0 {
			outcome = "duplicate"
		}
	}
	data, err := json.Marshal(p)
	if err != nil {
		return false, err
	}
	var intentID any
	if outcome == "queued" {
		r, err := tx.ExecContext(ctx, `INSERT INTO contact_intents(topic,due_ns,expires_ns,idempotency_key,created_ns,updated_ns)
VALUES('initiative',?,?,?,?,?)`, now.Add(time.Duration(p.AfterMinutes)*time.Minute).UnixNano(), now.Add(3*time.Hour).UnixNano(), fmt.Sprintf("initiative:%d", c.ID), now.UnixNano(), now.UnixNano())
		if err != nil {
			return false, err
		}
		intentID, err = r.LastInsertId()
		if err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE initiative_checks SET outcome=?,reason=?,proposal_json=?,topic_key=?,intent_id=? WHERE id=?", outcome, p.Reason, string(data), p.TopicKey(), intentID, c.ID); err != nil {
		return false, err
	}
	return outcome == "queued", tx.Commit()
}

func (h *History) FailInitiative(ctx context.Context, id int64) error {
	_, err := h.db.ExecContext(ctx, "UPDATE initiative_checks SET outcome='error',reason='consideration failed; no contact created' WHERE id=? AND outcome='considering'", id)
	return err
}

func initiativeCurrentTx(ctx context.Context, tx *sql.Tx, item ContactIntent, now time.Time, zone *time.Location) (contact.Decision, error) {
	var sourceID, throughID int64
	err := tx.QueryRowContext(ctx, "SELECT source_message_id,through_message_id FROM initiative_checks WHERE intent_id=? AND outcome='queued'", item.ID).Scan(&sourceID, &throughID)
	if errors.Is(err, sql.ErrNoRows) {
		return contact.Decision{Final: true, Reason: "initiative source removed"}, nil
	}
	if err != nil {
		return contact.Decision{}, err
	}
	var latest int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM messages").Scan(&latest); err != nil {
		return contact.Decision{}, err
	}
	if latest != throughID {
		return contact.Decision{Final: true, Reason: "conversation changed"}, nil
	}
	c, err := initiativeWindowTx(ctx, tx, now, zone, item.ID)
	if err != nil {
		return contact.Decision{}, err
	}
	if c.SourceID != 0 && c.SourceID != sourceID {
		return contact.Decision{Final: true, Reason: "conversation changed"}, nil
	}
	return contact.Decision{Ready: c.Ready, Reason: c.Reason}, nil
}

func (h *History) InitiativeProposal(ctx context.Context, key string) (character.Initiative, error) {
	var raw string
	err := h.db.QueryRowContext(ctx, `SELECT c.proposal_json FROM initiative_checks c JOIN contact_intents i ON i.id=c.intent_id
WHERE i.idempotency_key=? AND i.state='pending' AND c.outcome='queued'`, key).Scan(&raw)
	if err != nil {
		return character.Initiative{}, err
	}
	var p character.Initiative
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}

// CancelChannelContact consumes a deliberate no-message choice, without retry.
func (h *History) CancelChannelContact(ctx context.Context, id int64, now time.Time) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM contact_intents WHERE id=?", id).Scan(&state); err != nil {
		return err
	}
	if state != "pending" {
		return nil
	}
	reason := "character chose to leave space"
	if err := updateIntentAndLog(ctx, tx, id, "canceled", reason, ContactEvent{IntentID: id, Outcome: "canceled", Reason: reason, At: now}); err != nil {
		return err
	}
	return tx.Commit()
}

func clearInitiativesTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM initiative_checks")
	return err
}

func InitiativeOccasion(p character.Initiative) string {
	return "You previously chose a personal reason to reach out: " + p.Reason + ". This is a " + p.Kind + " grounded in these records: " + strings.Join(p.EvidenceIDs, ", ") + ". Say the concrete thing naturally in one short message, without explaining the scheduling or requiring an answer. Do not re-answer the user's old message. If the reason no longer makes sense, return exactly [[wait]]."
}
