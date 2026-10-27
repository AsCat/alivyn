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
	ID          int64
	SourceID    int64
	ThroughID   int64
	Unanswered  int
	Level       int
	TestRequest bool
	Reason      string
	Ready       bool
}

// Repeated consideration of an unchanged conversation cannot consume the
// entire day and prevent a later, newly shared conversation from being heard.
// The extra two checks still have a strict daily API limit.
func initiativeBudgetExhaustedTx(ctx context.Context, tx *sql.Tx, count int, sourceID int64, profile character.InitiativeProfile) (bool, error) {
	if count < profile.RoutineDailyChecks {
		return false, nil
	}
	if count >= profile.FreshDailyChecks {
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
func initiativeWindowTx(ctx context.Context, tx *sql.Tx, now time.Time, zone *time.Location, excludeIntent int64, allowGap bool) (InitiativeCheck, error) {
	var check InitiativeCheck
	check.TestRequest = allowGap
	if !check.TestRequest {
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM initiative_test_request WHERE expires_ns>?)", now.UnixNano()).Scan(&check.TestRequest); err != nil {
			return check, err
		}
	}
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
	if err := tx.QueryRowContext(ctx, "SELECT initiative FROM character_tuning WHERE id=1").Scan(&check.Level); err != nil {
		return check, err
	}
	profile := character.ProfileForInitiative(check.Level)
	if !profile.Enabled {
		check.Reason = "initiative disabled"
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
	err = tx.QueryRowContext(ctx, `SELECT m.id,m.created_at FROM messages m WHERE m.role='user'
AND NOT EXISTS(SELECT 1 FROM local_commands c WHERE c.source_message_id=m.id)
ORDER BY m.id DESC LIMIT 1`).Scan(&check.SourceID, &userStamp)
	if errors.Is(err, sql.ErrNoRows) {
		check.Reason = "no conversation"
		return check, nil
	}
	if err != nil {
		return check, err
	}
	var lastVisibleID int64
	if err := tx.QueryRowContext(ctx, `SELECT m.id,m.role,m.created_at FROM messages m
WHERE NOT EXISTS(SELECT 1 FROM local_commands c WHERE c.source_message_id=m.id)
AND NOT EXISTS(SELECT 1 FROM assistant_turns a JOIN local_commands c ON c.source_message_id=a.source_message_id WHERE a.assistant_message_id=m.id)
ORDER BY m.id DESC LIMIT 1`).Scan(&lastVisibleID, &role, &latestStamp); err != nil {
		return check, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM messages").Scan(&check.ThroughID); err != nil {
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
	gap := profile.FirstContactGap
	if r.Stage == relationship.Acquaintance {
		gap += 15 * time.Minute
	}
	var unanswered int
	if err := tx.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM contact_delivery WHERE state='sent' AND sent_ns>=?) +
 (SELECT COUNT(*) FROM contact_outbox WHERE sent_ns>=?)`, userAt.UnixNano(), userAt.UnixNano()).Scan(&unanswered); err != nil {
		return check, err
	}
	check.Unanswered = unanswered
	if unanswered >= profile.MaxUnanswered {
		check.Reason = "waiting for user to return"
		return check, nil
	}
	if unanswered > 0 {
		gap = profile.FollowupGap
	}
	variation := 11
	if check.Level > 75 {
		variation = 11 - (check.Level-75)*7/25
	}
	gap += time.Duration((check.SourceID+int64(unanswered)*7)%int64(variation)) * time.Minute
	if !check.TestRequest && now.Sub(lastAt) < gap {
		check.Reason = "conversation still recent"
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
	c, err := initiativeWindowTx(ctx, tx, now, zone, 0, false)
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
	profile := character.ProfileForInitiative(c.Level)
	exhausted, err := initiativeBudgetExhaustedTx(ctx, tx, count, c.SourceID, profile)
	if err != nil {
		return c, err
	}
	if exhausted {
		c.Ready = false
		c.Reason = "daily consideration budget"
		return c, nil
	}
	if !c.TestRequest && now.UnixNano() < next {
		fresh, err := initiativeHasNewConversationTx(ctx, tx, c.SourceID)
		if err != nil {
			return c, err
		}
		if !fresh {
			c.Ready = false
			c.Reason = "consideration cooldown"
			return c, nil
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE initiative_clock SET next_check_ns=?,budget_day=?,checks=? WHERE id=1", now.Add(profile.ConsiderationGap).UnixNano(), today, count+1); err != nil {
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
	if c.TestRequest {
		if _, err := tx.ExecContext(ctx, "DELETE FROM initiative_test_request WHERE id=1"); err != nil {
			return c, err
		}
	}
	return c, tx.Commit()
}

func (h *History) InitiativeStatus(ctx context.Context, now time.Time, zone *time.Location) (InitiativeCheck, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return InitiativeCheck{}, err
	}
	defer tx.Rollback()
	c, err := initiativeWindowTx(ctx, tx, now, zone, 0, false)
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
		exhausted, err = initiativeBudgetExhaustedTx(ctx, tx, checks, c.SourceID, character.ProfileForInitiative(c.Level))
		if err != nil {
			return c, err
		}
	}
	if exhausted {
		c.Ready = false
		c.Reason = "daily consideration budget"
	} else if !c.TestRequest && now.UnixNano() < next {
		fresh, err := initiativeHasNewConversationTx(ctx, tx, c.SourceID)
		if err != nil {
			return c, err
		}
		if !fresh {
			c.Ready = false
			c.Reason = "consideration cooldown"
		}
	}
	return c, nil
}

func (h *History) FinishInitiative(ctx context.Context, c InitiativeCheck, p character.Initiative, now time.Time, zone *time.Location) (bool, error) {
	p.Unanswered = c.Unanswered // The model cannot choose or spoof the outreach round.
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
		fresh, err := initiativeWindowTx(ctx, tx, now, zone, 0, c.TestRequest)
		if err != nil {
			return false, err
		}
		if !fresh.Ready || fresh.SourceID != c.SourceID || fresh.Unanswered != c.Unanswered || fresh.Level != c.Level {
			outcome = "superseded"
		} else {
			outcome = "queued"
		}
	}
	var duplicate int
	if outcome == "queued" {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM initiative_checks c JOIN contact_intents i ON i.id=c.intent_id
WHERE c.topic_key=? AND c.at_ns>? AND i.state IN ('pending','sent')`, initiativeTopicKey(p, c.Unanswered), now.Add(-72*time.Hour).UnixNano()).Scan(&duplicate); err != nil {
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
		delay := p.AfterMinutes
		if c.Level >= 90 && delay > 3 {
			delay = 3
		}
		due := now.Add(time.Duration(delay) * time.Minute)
		if c.TestRequest {
			due = now
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO contact_intents(topic,due_ns,expires_ns,idempotency_key,created_ns,updated_ns)
VALUES('initiative',?,?,?,?,?)`, due.UnixNano(), now.Add(3*time.Hour).UnixNano(), fmt.Sprintf("initiative:%d", c.ID), now.UnixNano(), now.UnixNano())
		if err != nil {
			return false, err
		}
		intentID, err = r.LastInsertId()
		if err != nil {
			return false, err
		}
		if c.TestRequest {
			if _, err := tx.ExecContext(ctx, "INSERT INTO initiative_test_intents(intent_id) VALUES(?)", intentID); err != nil {
				return false, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE initiative_checks SET outcome=?,reason=?,proposal_json=?,topic_key=?,intent_id=? WHERE id=?", outcome, p.Reason, string(data), initiativeTopicKey(p, c.Unanswered), intentID, c.ID); err != nil {
		return false, err
	}
	if outcome == "wait" && p.ReconsiderIn > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE initiative_clock SET next_check_ns=? WHERE id=1", now.Add(time.Duration(p.ReconsiderIn)*time.Minute).UnixNano()); err != nil {
			return false, err
		}
	}
	return outcome == "queued", tx.Commit()
}

func initiativeHasNewConversationTx(ctx context.Context, tx *sql.Tx, sourceID int64) (bool, error) {
	var previous int64
	if err := tx.QueryRowContext(ctx, "SELECT source_message_id FROM initiative_checks ORDER BY id DESC LIMIT 1").Scan(&previous); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, err
	}
	return previous != sourceID, nil
}

func initiativeTopicKey(p character.Initiative, unanswered int) string {
	return fmt.Sprintf("%s:%d", p.TopicKey(), unanswered)
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
	var allowGap bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM initiative_test_intents WHERE intent_id=?)", item.ID).Scan(&allowGap); err != nil {
		return contact.Decision{}, err
	}
	c, err := initiativeWindowTx(ctx, tx, now, zone, item.ID, allowGap)
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

// RecentOutreach returns the character's actual decisions and delivery result.
// A later consideration can continue an impulse, change course, or drop it.
func (h *History) RecentOutreach(ctx context.Context, now time.Time, limit int) ([]character.OutreachBeat, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := h.db.QueryContext(ctx, `SELECT c.at_ns,c.outcome,c.reason,c.proposal_json,COALESCE(i.state,'')
FROM initiative_checks c LEFT JOIN contact_intents i ON i.id=c.intent_id
WHERE c.at_ns>=? AND c.outcome IN ('wait','queued')
ORDER BY c.id DESC LIMIT ?`, now.Add(-72*time.Hour).UnixNano(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []character.OutreachBeat
	for rows.Next() {
		var at int64
		var outcome, reason, raw, state string
		if err := rows.Scan(&at, &outcome, &reason, &raw, &state); err != nil {
			return nil, err
		}
		beat := character.OutreachBeat{At: time.Unix(0, at).Format(time.RFC3339), Action: outcome, Reason: reason, Delivered: state == "sent"}
		var proposal character.Initiative
		if raw != "" && json.Unmarshal([]byte(raw), &proposal) == nil {
			beat.Kind = proposal.Kind
			beat.Impulses = proposal.Impulses
		}
		result = append(result, beat)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
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
	occasion := "You previously chose a personal reason to reach out: " + p.Reason + ". This is a " + p.Kind + " grounded in these records: " + strings.Join(p.EvidenceIDs, ", ") + ". Say the concrete thing naturally in one concise turn, without explaining the scheduling or requiring an answer. Do not re-answer the user's old message."
	if p.Unanswered > 0 {
		occasion += fmt.Sprintf(" You have sent %d proactive message(s) since their last reply. You may briefly notice their absence or wonder where they went if that fits your relationship, but do not repeat your last question or imply they owe a response.", p.Unanswered)
	}
	return occasion + " If the reason no longer makes sense, return exactly [[wait]]."
}
