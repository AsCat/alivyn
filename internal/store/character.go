package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/character"
)

func (h *History) CharacterTurn(ctx context.Context, sourceID int64) (character.Turn, bool, error) {
	var raw string
	err := h.db.QueryRowContext(ctx, "SELECT turn_json FROM character_turns WHERE source_message_id = ?", sourceID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return character.Turn{}, false, nil
	}
	if err != nil {
		return character.Turn{}, false, err
	}
	var t character.Turn
	err = json.Unmarshal([]byte(raw), &t)
	return t, err == nil, err
}

// CharacterSelfFactsBefore returns the latest delivered version of each stable
// fictional biography fact. Every row remains tied to its original turn.
func (h *History) CharacterSelfFactsBefore(ctx context.Context, sourceID int64) ([]character.SelfFact, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT f.fact_key,f.value FROM character_self_facts f
JOIN assistant_turns a ON a.source_message_id=f.source_message_id
JOIN messages m ON m.id=a.assistant_message_id
WHERE f.source_message_id<? AND m.content!='[forgotten memory]'
AND f.id=(SELECT MAX(n.id) FROM character_self_facts n WHERE n.fact_key=f.fact_key AND n.source_message_id<?)
ORDER BY f.id DESC LIMIT 24`, sourceID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var facts []character.SelfFact
	for rows.Next() {
		var fact character.SelfFact
		if err := rows.Scan(&fact.Key, &fact.Value); err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, rows.Err()
}

// RecentClosedExchange is a conversational boundary, not a delivery pause.
// A later completed action replaces it; old boundaries expire naturally.
func (h *History) RecentClosedExchange(ctx context.Context, sourceID int64, now time.Time) (time.Time, error) {
	var raw, stamp string
	err := h.db.QueryRowContext(ctx, `SELECT turn_json,created_at FROM character_turns
WHERE source_message_id<? AND committed=1 AND json_extract(turn_json,'$.mode')!='wait'
ORDER BY source_message_id DESC LIMIT 1`, sourceID).Scan(&raw, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	var t character.Turn
	if err := json.Unmarshal([]byte(raw), &t); err != nil || t.Mode != "close" {
		return time.Time{}, err
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return time.Time{}, err
	}
	if age := now.Sub(at); age < 0 || age > 4*time.Hour {
		return time.Time{}, nil
	}
	return at, nil
}

func (h *History) OpenThreadSources(ctx context.Context, threads []character.OpenThread) ([]actor.Evidence, error) {
	var sources []actor.Evidence
	seen := map[string]bool{}
	for _, th := range threads {
		for _, ref := range th.SourceIDs {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			id, err := strconv.ParseInt(strings.TrimPrefix(ref, "chat:"), 10, 64)
			if err != nil {
				continue
			}
			var role, text, at string
			err = h.db.QueryRowContext(ctx, "SELECT role,content,created_at FROM messages WHERE id=? AND content!='[forgotten memory]'", id).Scan(&role, &text, &at)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if runes := []rune(text); len(runes) > 2000 {
				text = string(runes[:2000]) + "…"
			}
			sources = append(sources, actor.Evidence{ID: ref, Provenance: "observed_chat", At: at, Text: role + ": " + text})
		}
	}
	return sources, nil
}

func (h *History) CharacterReplySource(ctx context.Context, sourceID int64) (actor.Evidence, bool, error) {
	var id int64
	var content, at string
	err := h.db.QueryRowContext(ctx, `SELECT m.id,m.content,m.created_at FROM assistant_turns a
JOIN messages m ON m.id=a.assistant_message_id WHERE a.source_message_id=?`, sourceID).Scan(&id, &content, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return actor.Evidence{}, false, nil
	}
	if err != nil {
		return actor.Evidence{}, false, err
	}
	return actor.Evidence{ID: "chat:" + strconv.FormatInt(id, 10), Provenance: "observed_chat", At: at, Text: "assistant: " + content}, true, nil
}

func (h *History) SaveCharacterTurn(ctx context.Context, sourceID int64, t character.Turn, at time.Time) error {
	if t.Absence != nil {
		if err := t.Absence.Validate(at.Truncate(time.Second)); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	r, err := h.db.ExecContext(ctx, `INSERT INTO character_turns(source_message_id,turn_json,created_at)
SELECT ?,?,? WHERE EXISTS(SELECT 1 FROM messages WHERE id=? AND role='user')
AND EXISTS(SELECT 1 FROM character_control WHERE mode='running' AND blocked_through_id<?)
AND (? OR NOT EXISTS(SELECT 1 FROM local_commands WHERE command='wake' AND source_message_id>?))
ON CONFLICT(source_message_id) DO NOTHING`, sourceID, string(raw), at.UTC().Format(time.RFC3339Nano), sourceID, sourceID, t.Absence == nil, sourceID)
	if err != nil {
		return err
	}
	if count, err := r.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		var allowed bool
		if err := h.db.QueryRowContext(ctx, "SELECT mode='running' AND blocked_through_id<? AND (? OR NOT EXISTS(SELECT 1 FROM local_commands WHERE command='wake' AND source_message_id>?)) FROM character_control WHERE id=1", sourceID, t.Absence == nil, sourceID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return ErrTurnSuperseded
		}
		if _, found, err := h.CharacterTurn(ctx, sourceID); err != nil {
			return err
		} else if !found {
			return ErrTurnSuperseded
		}
	}
	return nil
}

// CharacterMomentBefore excludes drafts and later queued turns. Feelings and
// scenes fade with elapsed time; a short attention note lasts at most 3 days.
func (h *History) CharacterMomentBefore(ctx context.Context, sourceID int64, now time.Time) (character.Moment, time.Time, error) {
	t, at, err := h.CharacterStateBefore(ctx, sourceID, now)
	return t.Moment, at, err
}

func (h *History) CharacterStateBefore(ctx context.Context, sourceID int64, now time.Time) (character.Turn, time.Time, error) {
	var raw, stamp string
	err := h.db.QueryRowContext(ctx, `SELECT turn_json,created_at FROM character_turns
WHERE source_message_id < ? AND committed = 1 AND json_extract(turn_json,'$.state_updated') = 1
AND COALESCE(json_extract(turn_json,'$.state_version'),0) >= 2
ORDER BY source_message_id DESC LIMIT 1`, sourceID).Scan(&raw, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return character.Turn{}, time.Time{}, nil
	}
	if err != nil {
		return character.Turn{}, time.Time{}, err
	}
	var t character.Turn
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return character.Turn{}, time.Time{}, err
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return character.Turn{}, time.Time{}, err
	}
	age := now.Sub(at)
	if age < 0 || age > 72*time.Hour {
		return character.Turn{}, time.Time{}, nil
	}
	if age > 8*time.Hour {
		t.Moment.Feeling = ""
		t.Moment.Cause = ""
	}
	if age > 2*time.Hour {
		t.Moment.Intention = ""
		t.Moment.Energy = ""
		t.Moment.Engagement = ""
	}
	if age > 6*time.Hour {
		t.Moment.Scene = ""
	}
	return t, at, nil
}

// commitCharacterTurnTx runs with the transcript commit, so failed sends reuse
// one prepared reply and a draft never changes a later turn's emotional state.
func commitCharacterTurnTx(ctx context.Context, tx *sql.Tx, sourceID int64, reply string) error {
	var raw, stamp string
	var committed bool
	err := tx.QueryRowContext(ctx, "SELECT turn_json,created_at,committed FROM character_turns WHERE source_message_id = ?", sourceID).Scan(&raw, &stamp, &committed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // Other dialogue engines share the same channel.
	}
	if err != nil {
		return err
	}
	var t character.Turn
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return err
	}
	if t.Reply != reply {
		return errors.New("character reply differs from its saved turn")
	}
	if committed {
		return nil
	}
	if err := saveAbsenceTx(ctx, tx, sourceID, t); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE character_turns SET committed = 1 WHERE source_message_id = ?", sourceID); err != nil {
		return err
	}
	// Keep the existing world-engagement ledger compatible. This is a receipt
	// of what was said, not a preselected conversational action.
	if len(t.UsedWorldIDs) == 0 {
		return nil
	}
	return recordCharacterWorldTx(ctx, tx, sourceID, t, stamp)
}

func recordCharacterWorldTx(ctx context.Context, tx *sql.Tx, sourceID int64, t character.Turn, stamp string) error {
	if len(t.UsedWorldIDs) == 0 {
		return nil
	}
	d := actor.Decision{Act: "share", Focus: "shared a world event", EvidenceIDs: t.UsedWorldIDs, Brevity: "normal", Posture: "steady"}
	dRaw, _ := json.Marshal(d)
	if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO actor_decisions(source_message_id,decision_json,decided_at) VALUES(?,?,?)", sourceID, string(dRaw), stamp); err != nil {
		return err
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return err
	}
	for _, ref := range t.UsedWorldIDs {
		id, err := strconv.ParseInt(strings.TrimPrefix(ref, "worldv2:"), 10, 64)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO goal_world_engagements(source_message_id,object_id,event_id,at_ns)
SELECT ?,object_id,id,? FROM goal_world_events WHERE id = ?`, sourceID, at.UnixNano(), id); err != nil {
			return err
		}
	}
	return nil
}

// NextCharacterReflection coalesces state maintenance after a quiet interval.
// The first/fourth exchange or ten elapsed minutes can qualify after 20s.
// Any remaining completed exchange is reflected after 90s of quiet, including
// a deliberate no-message turn, so a closing intention cannot stay unrecorded.
func (h *History) NextCharacterReflection(ctx context.Context, now time.Time) (int64, character.Turn, bool, error) {
	var id int64
	var raw, stamp string
	err := h.db.QueryRowContext(ctx, `SELECT c.source_message_id,c.turn_json,c.created_at FROM character_turns c
WHERE c.committed=1 AND EXISTS(SELECT 1 FROM character_control WHERE mode='running' AND blocked_through_id<c.source_message_id)
AND NOT EXISTS (SELECT 1 FROM feishu_inbox f WHERE f.state!='sent')
ORDER BY c.source_message_id DESC LIMIT 1`).Scan(&id, &raw, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, character.Turn{}, false, nil
	}
	if err != nil {
		return 0, character.Turn{}, false, err
	}
	var t character.Turn
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return 0, t, false, err
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return 0, t, false, err
	}
	if t.StateUpdated || now.Sub(at) < 20*time.Second {
		return 0, t, false, nil
	}
	var previous int64
	var previousStamp string
	err = h.db.QueryRowContext(ctx, `SELECT source_message_id,created_at FROM character_turns WHERE committed=1 AND json_extract(turn_json,'$.state_updated')=1 AND COALESCE(json_extract(turn_json,'$.state_version'),0)>=2 ORDER BY source_message_id DESC LIMIT 1`).Scan(&previous, &previousStamp)
	if errors.Is(err, sql.ErrNoRows) {
		return id, t, true, nil
	}
	if err != nil {
		return 0, t, false, err
	}
	prior, err := time.Parse(time.RFC3339Nano, previousStamp)
	if err != nil {
		return 0, t, false, err
	}
	var count int
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM character_turns WHERE committed=1 AND source_message_id>? AND source_message_id<=?", previous, id).Scan(&count); err != nil {
		return 0, t, false, err
	}
	return id, t, count >= 4 || at.Sub(prior) >= 10*time.Minute || now.Sub(at) >= 90*time.Second, nil
}

// SaveCharacterReflection only updates its still-existing committed source.
// A forget operation during generation deletes that source; it cannot reappear.
func (h *History) SaveCharacterReflection(ctx context.Context, sourceID int64, t character.Turn) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw, stamp string
	err = tx.QueryRowContext(ctx, "SELECT turn_json,created_at FROM character_turns WHERE source_message_id=? AND committed=1 AND EXISTS(SELECT 1 FROM character_control WHERE mode='running' AND blocked_through_id<?)", sourceID, sourceID).Scan(&raw, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var old character.Turn
	if err := json.Unmarshal([]byte(raw), &old); err != nil {
		return err
	}
	if old.StateUpdated {
		return nil
	}
	if old.Reply != t.Reply || !t.StateUpdated {
		return errors.New("reflection must preserve its delivered reply")
	}
	t.Mode = old.Mode       // Reflection cannot change the executed dialogue action.
	t.Absence = old.Absence // Reflection cannot rewrite an executed availability action.
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE character_turns SET turn_json=? WHERE source_message_id=?", string(data), sourceID); err != nil {
		return err
	}
	if err := recordCharacterWorldTx(ctx, tx, sourceID, t, stamp); err != nil {
		return err
	}
	if len(t.SelfFacts) > 0 {
		var delivered string
		if err := tx.QueryRowContext(ctx, `SELECT m.content FROM assistant_turns a JOIN messages m ON m.id=a.assistant_message_id WHERE a.source_message_id=?`, sourceID).Scan(&delivered); err != nil {
			return err
		}
		if delivered != t.Reply {
			return errors.New("self facts require the exact delivered reply")
		}
		for _, fact := range t.SelfFacts {
			if _, err := tx.ExecContext(ctx, `INSERT INTO character_self_facts(fact_key,value,source_message_id,created_at) VALUES(?,?,?,?)`, fact.Key, fact.Value, sourceID, stamp); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// CommitCharacterReply is used by terminal chat and isolated evaluations.
func (h *History) CommitCharacterReply(ctx context.Context, sourceID int64, reply string, at time.Time) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE id=? AND role='user'
AND (EXISTS(SELECT 1 FROM local_commands WHERE source_message_id=messages.id)
OR EXISTS(SELECT 1 FROM character_control WHERE mode='running' AND blocked_through_id<messages.id)))`, sourceID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrTurnSuperseded
	}
	if err := commitCharacterTurnTx(ctx, tx, sourceID, reply); err != nil {
		return err
	}
	if reply != "" {
		var existing int64
		err := tx.QueryRowContext(ctx, "SELECT assistant_message_id FROM assistant_turns WHERE source_message_id = ?", sourceID).Scan(&existing)
		if err == nil {
			return tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		r, err := tx.ExecContext(ctx, "INSERT INTO messages(role,content,created_at) VALUES('assistant',?,?)", reply, at.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO assistant_turns(source_message_id,assistant_message_id) VALUES(?,?)", sourceID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
