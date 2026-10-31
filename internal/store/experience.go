package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/pkg/extension"
)

// The ledger stores source references and metadata, not duplicated private
// content. This lets existing forgetting and reset operations remain effective.
// Triggers append within the same transaction as their source records.
func (h *History) ensureExperienceEvents(ctx context.Context) error {
	_, err := h.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS experience_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL UNIQUE,
  source_table TEXT NOT NULL,
  source_id INTEGER NOT NULL,
  happened_at TEXT NOT NULL,
  recorded_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  domain TEXT NOT NULL,
  world_id TEXT NOT NULL DEFAULT '',
  provenance TEXT NOT NULL,
  visibility TEXT NOT NULL,
  parent_id TEXT NOT NULL DEFAULT '',
  UNIQUE(source_table, source_id)
);
CREATE INDEX IF NOT EXISTS experience_events_source ON experience_events(source_table, source_id);
CREATE TABLE IF NOT EXISTS simulated_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  world_id TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  occurred_ns INTEGER NOT NULL,
  kind TEXT NOT NULL,
  parent_event_id INTEGER REFERENCES simulated_events(id),
  observer_ids_json TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  schema TEXT NOT NULL,
  idempotency_key TEXT NOT NULL UNIQUE
);
CREATE INDEX IF NOT EXISTS simulated_events_world_time ON simulated_events(world_id, occurred_ns);
CREATE TRIGGER IF NOT EXISTS experience_message_insert AFTER INSERT ON messages BEGIN
  INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,provenance,visibility)
  VALUES('chat:'||NEW.id,'messages',NEW.id,NEW.created_at,'conversation','observed_chat','private');
END;
CREATE TRIGGER IF NOT EXISTS experience_goal_world_insert AFTER INSERT ON goal_world_events BEGIN
  INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,world_id,provenance,visibility,parent_id)
  VALUES('worldv2:'||NEW.id,'goal_world_events',NEW.id,NEW.local_date,'simulated_world','tidehaven',NEW.provenance,'character',
    CASE WHEN NEW.parent_event_id IS NULL THEN '' ELSE 'worldv2:'||NEW.parent_event_id END);
END;
CREATE TRIGGER IF NOT EXISTS experience_legacy_world_insert AFTER INSERT ON world_events BEGIN
  INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,world_id,provenance,visibility)
  VALUES('worldv1:'||NEW.id,'world_events',NEW.id,NEW.local_date,'simulated_world','tidehaven','authored_v1','character');
END;
CREATE TRIGGER IF NOT EXISTS experience_reading_insert AFTER INSERT ON life_readings BEGIN
  INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,provenance,visibility)
  VALUES('reading:'||NEW.id,'life_readings',NEW.id,NEW.local_date,'observed_real','external_excerpt','character');
END;
CREATE TRIGGER IF NOT EXISTS experience_simulated_insert AFTER INSERT ON simulated_events BEGIN
  INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,world_id,provenance,visibility,parent_id)
  VALUES('worldsim:'||NEW.id,'simulated_events',NEW.id,datetime(NEW.occurred_ns/1000000000,'unixepoch'),
    'simulated_world',NEW.world_id,'simulator','character',
    CASE WHEN NEW.parent_event_id IS NULL THEN '' ELSE 'worldsim:'||NEW.parent_event_id END);
END;
CREATE TRIGGER IF NOT EXISTS experience_contact_insert AFTER INSERT ON contact_events BEGIN
  INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,provenance,visibility)
  VALUES('contact:'||NEW.id,'contact_events',NEW.id,datetime(NEW.at_ns/1000000000,'unixepoch'),
    'interaction_policy','runtime_decision','owner');
END;
CREATE TRIGGER IF NOT EXISTS experience_inner_wake_done AFTER UPDATE OF outcome ON inner_wakes
WHEN NEW.outcome='done' BEGIN
  INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,provenance,visibility)
  VALUES('innerwake:'||NEW.id,'inner_wakes',NEW.id,datetime(NEW.at_ns/1000000000,'unixepoch'),
    'inference','character_state','owner');
END;
INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,provenance,visibility)
SELECT 'chat:'||src.id,'messages',src.id,src.created_at,'conversation','observed_chat','private' FROM messages src
WHERE NOT EXISTS(SELECT 1 FROM experience_events e WHERE e.source_table='messages' AND e.source_id=src.id) ORDER BY src.id;
INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,world_id,provenance,visibility,parent_id)
SELECT 'worldv2:'||id,'goal_world_events',id,local_date,'simulated_world','tidehaven',provenance,'character',
  CASE WHEN src.parent_event_id IS NULL THEN '' ELSE 'worldv2:'||src.parent_event_id END FROM goal_world_events src
WHERE NOT EXISTS(SELECT 1 FROM experience_events e WHERE e.source_table='goal_world_events' AND e.source_id=src.id) ORDER BY src.id;
INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,world_id,provenance,visibility)
SELECT 'worldv1:'||src.id,'world_events',src.id,src.local_date,'simulated_world','tidehaven','authored_v1','character' FROM world_events src
WHERE NOT EXISTS(SELECT 1 FROM experience_events e WHERE e.source_table='world_events' AND e.source_id=src.id) ORDER BY src.id;
INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,provenance,visibility)
SELECT 'reading:'||src.id,'life_readings',src.id,src.local_date,'observed_real','external_excerpt','character' FROM life_readings src
WHERE NOT EXISTS(SELECT 1 FROM experience_events e WHERE e.source_table='life_readings' AND e.source_id=src.id) ORDER BY src.id;
INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,world_id,provenance,visibility,parent_id)
SELECT 'worldsim:'||src.id,'simulated_events',src.id,datetime(src.occurred_ns/1000000000,'unixepoch'),
  'simulated_world',src.world_id,'simulator','character',
  CASE WHEN src.parent_event_id IS NULL THEN '' ELSE 'worldsim:'||src.parent_event_id END FROM simulated_events src
WHERE NOT EXISTS(SELECT 1 FROM experience_events e WHERE e.source_table='simulated_events' AND e.source_id=src.id) ORDER BY src.id;
INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,provenance,visibility)
SELECT 'contact:'||src.id,'contact_events',src.id,datetime(src.at_ns/1000000000,'unixepoch'),
  'interaction_policy','runtime_decision','owner' FROM contact_events src
WHERE NOT EXISTS(SELECT 1 FROM experience_events e WHERE e.source_table='contact_events' AND e.source_id=src.id) ORDER BY src.id;
INSERT OR IGNORE INTO experience_events(event_id,source_table,source_id,happened_at,domain,provenance,visibility)
SELECT 'innerwake:'||src.id,'inner_wakes',src.id,datetime(src.at_ns/1000000000,'unixepoch'),
  'inference','character_state','owner' FROM inner_wakes src WHERE src.outcome='done'
AND NOT EXISTS(SELECT 1 FROM experience_events e WHERE e.source_table='inner_wakes' AND e.source_id=src.id) ORDER BY src.id;
`)
	return err
}

// EventSequence is the durable cursor for a terminal console or later GUI.
func (h *History) EventSequence(ctx context.Context) (int64, error) {
	var sequence int64
	err := h.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM experience_events`).Scan(&sequence)
	return sequence, err
}

// RecentEventCursor locates the last actual rows rather than subtracting
// from an autoincrement value, which may contain gaps after ignored inserts.
func (h *History) RecentEventCursor(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 200 {
		return 0, errors.New("invalid recent event limit")
	}
	var first sql.NullInt64
	err := h.db.QueryRowContext(ctx, `SELECT MIN(sequence) FROM (
SELECT sequence FROM experience_events ORDER BY sequence DESC LIMIT ?)`, limit).Scan(&first)
	if err != nil || !first.Valid {
		return 0, err
	}
	return first.Int64 - 1, nil
}

// RecentWorldEvents is unaffected by a busy conversation adding many chat
// events after the most recent world scene.
func (h *History) RecentWorldEvents(ctx context.Context, worldID string, limit int) ([]extension.Event, error) {
	if worldID == "" || limit < 1 || limit > 200 {
		return nil, errors.New("invalid recent world event query")
	}
	var first sql.NullInt64
	err := h.db.QueryRowContext(ctx, `SELECT MIN(sequence) FROM (
SELECT sequence FROM experience_events WHERE world_id=? ORDER BY sequence DESC LIMIT ?)`, worldID, limit).Scan(&first)
	if err != nil {
		return nil, err
	}
	if !first.Valid {
		return nil, nil
	}
	return h.ReadEvents(ctx, extension.EventQuery{WorldID: worldID, After: first.Int64 - 1, Limit: limit})
}

// ReadEvents returns only source records still present and not forgotten.
// External plugins will receive a separately scoped host view; this store
// method is for the owner-controlled runtime and local diagnostics.
func (h *History) ReadEvents(ctx context.Context, q extension.EventQuery) ([]extension.Event, error) {
	if q.After < 0 || q.Limit < 1 || q.Limit > 200 || (q.SubjectID != "" && q.SubjectID != "default") {
		return nil, errors.New("invalid event query")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT e.sequence,e.event_id,e.source_table,e.source_id,e.happened_at,e.recorded_at,e.domain,e.world_id,e.provenance,e.visibility,e.parent_id,
  m.role,m.content,m.created_at,lc.command,
  g.action,g.object_id,g.zh,g.en,g.local_date,
  w.kind,w.zh,w.en,w.local_date,
  l.title,l.source_url,l.excerpt,l.thought,l.feeling,l.question,l.occurred_ns,
  se.kind,se.subject_id,se.observer_ids_json,se.payload_json,se.schema,se.occurred_ns,
  ce.outcome,ce.reason,ce.at_ns,ci.topic,
  iw.state_json,iw.at_ns
FROM experience_events e
LEFT JOIN messages m ON e.source_table='messages' AND m.id=e.source_id
LEFT JOIN local_commands lc ON lc.source_message_id=m.id
LEFT JOIN goal_world_events g ON e.source_table='goal_world_events' AND g.id=e.source_id
LEFT JOIN world_events w ON e.source_table='world_events' AND w.id=e.source_id
LEFT JOIN life_readings l ON e.source_table='life_readings' AND l.id=e.source_id
LEFT JOIN simulated_events se ON e.source_table='simulated_events' AND se.id=e.source_id
LEFT JOIN contact_events ce ON e.source_table='contact_events' AND ce.id=e.source_id
LEFT JOIN contact_intents ci ON ci.id=ce.intent_id
LEFT JOIN inner_wakes iw ON e.source_table='inner_wakes' AND iw.id=e.source_id AND iw.outcome='done'
WHERE e.sequence>? AND (?='' OR e.world_id=?)
ORDER BY e.sequence LIMIT ?`, q.After, q.WorldID, q.WorldID, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []extension.Event
	for rows.Next() {
		var sequence, sourceID int64
		var id, table, happened, recorded, domain, worldID, provenance, visibility, parent string
		var role, content, created, command sql.NullString
		var action, object, goalZh, goalEn, goalDate sql.NullString
		var legacyKind, legacyZh, legacyEn, legacyDate sql.NullString
		var title, url, excerpt, thought, feeling, question sql.NullString
		var occurred sql.NullInt64
		var simKind, simSubject, simObservers, simPayload, simSchema sql.NullString
		var simOccurred sql.NullInt64
		var outcome, reason, topic sql.NullString
		var contactAt sql.NullInt64
		var innerRaw sql.NullString
		var innerAt sql.NullInt64
		if err := rows.Scan(&sequence, &id, &table, &sourceID, &happened, &recorded, &domain, &worldID, &provenance, &visibility, &parent,
			&role, &content, &created, &command,
			&action, &object, &goalZh, &goalEn, &goalDate,
			&legacyKind, &legacyZh, &legacyEn, &legacyDate,
			&title, &url, &excerpt, &thought, &feeling, &question, &occurred,
			&simKind, &simSubject, &simObservers, &simPayload, &simSchema, &simOccurred,
			&outcome, &reason, &contactAt, &topic,
			&innerRaw, &innerAt); err != nil {
			return nil, err
		}
		var payload any
		var at time.Time
		var kind string
		switch table {
		case "messages":
			if !content.Valid || content.String == "[forgotten memory]" || command.Valid {
				continue
			}
			at, err = time.Parse(time.RFC3339Nano, created.String)
			kind = "chat.message"
			payload = map[string]any{"role": role.String, "text": content.String}
		case "goal_world_events":
			if !action.Valid {
				continue
			}
			at, err = time.Parse("2006-01-02", goalDate.String)
			kind = "world.action"
			payload = map[string]any{"action": action.String, "object_id": object.String, "zh": goalZh.String, "en": goalEn.String}
		case "world_events":
			if !legacyKind.Valid {
				continue
			}
			at, err = time.Parse("2006-01-02", legacyDate.String)
			kind = "world.event"
			payload = map[string]any{"action": legacyKind.String, "zh": legacyZh.String, "en": legacyEn.String}
		case "life_readings":
			if !title.Valid || !occurred.Valid {
				continue
			}
			at = time.Unix(0, occurred.Int64).UTC()
			kind = "life.reading"
			payload = map[string]any{"title": title.String, "url": url.String, "excerpt": excerpt.String, "thought": thought.String, "feeling": feeling.String, "question": question.String}
		case "simulated_events":
			if !simKind.Valid || !simOccurred.Valid || !simPayload.Valid {
				continue
			}
			at = time.Unix(0, simOccurred.Int64).UTC()
			kind = simKind.String
			payload = json.RawMessage(simPayload.String)
		case "contact_events":
			if !outcome.Valid || !contactAt.Valid {
				continue
			}
			at = time.Unix(0, contactAt.Int64).UTC()
			kind = "contact.decision"
			payload = map[string]any{"topic": topic.String, "outcome": outcome.String, "reason": reason.String}
		case "inner_wakes":
			if !innerRaw.Valid || !innerAt.Valid {
				continue
			}
			var state character.InnerState
			if err := json.Unmarshal([]byte(innerRaw.String), &state); err != nil {
				return nil, err
			}
			at = time.Unix(0, innerAt.Int64).UTC()
			kind = "person.inner_wake"
			payload = state
		default:
			return nil, fmt.Errorf("unknown experience source %q", table)
		}
		if err != nil {
			return nil, err
		}
		if !q.Until.IsZero() && at.After(q.Until) {
			continue
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		recordedAt, err := time.ParseInLocation("2006-01-02 15:04:05", recorded, time.UTC)
		if err != nil {
			return nil, err
		}
		event := extension.Event{Sequence: sequence, ID: id, Kind: kind, Domain: domain, WorldID: worldID,
			SubjectID: "default", At: at, RecordedAt: recordedAt, Provenance: provenance, Visibility: visibility,
			ObserverIDs: []string{"default"}, Payload: encoded, Schema: "alivyn." + kind + "/v1"}
		if table == "simulated_events" {
			event.SubjectID = simSubject.String
			event.Schema = simSchema.String
			if err := json.Unmarshal([]byte(simObservers.String), &event.ObserverIDs); err != nil {
				return nil, err
			}
		}
		if table == "contact_events" || table == "inner_wakes" {
			event.ObserverIDs = nil
		}
		if parent != "" {
			event.ParentIDs = []string{parent}
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
