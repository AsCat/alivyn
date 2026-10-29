package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/relationship"
	"github.com/AsCat/alivyn/internal/world"
	"github.com/AsCat/alivyn/pkg/extension"
)

// Scene is the first accepted world proposal schema. A future world package
// may register more validators without granting a simulator database access.
type Scene struct {
	Place string `json:"place"`
	Zh    string `json:"zh"`
	En    string `json:"en"`
}

func validateScene(worldID string, p extension.EventProposal, now time.Time) (Scene, error) {
	if p.Schema != "alivyn.world.scene/v1" || p.Kind != "world.scene" || p.SubjectID == "" || p.At.IsZero() || p.At.After(now) || len(p.Payload) > 4096 {
		return Scene{}, errors.New("invalid world scene proposal")
	}
	var scene Scene
	if err := json.Unmarshal(p.Payload, &scene); err != nil {
		return Scene{}, err
	}
	if strings.TrimSpace(scene.Place) == "" || len(scene.Place) > 80 || strings.TrimSpace(scene.Zh) == "" || strings.TrimSpace(scene.En) == "" || len([]rune(scene.Zh)) > 300 || len([]rune(scene.En)) > 300 {
		return Scene{}, errors.New("world scene needs a place and short bilingual description")
	}
	if worldID == "tidehaven" && !world.IsTidehavenPlace(scene.Place) {
		return Scene{}, errors.New("world scene place is outside Tidehaven")
	}
	if len(p.ObserverIDs) > 8 || len(p.ParentIDs) > 1 {
		return Scene{}, errors.New("too many world scene observers or parents")
	}
	seen := map[string]bool{}
	for _, observer := range p.ObserverIDs {
		if observer == "" || len(observer) > 80 || seen[observer] {
			return Scene{}, errors.New("invalid world scene observer")
		}
		seen[observer] = true
	}
	return scene, nil
}

// CommitWorldProposal is the only write path for a simulator's v1 scene.
// The proposal is validated, linked to an existing parent in the same world,
// and committed with its experience index in one SQLite transaction.
func (h *History) CommitWorldProposal(ctx context.Context, worldID, key string, proposal extension.EventProposal, now time.Time) (string, error) {
	if worldID == "" || len(worldID) > 80 || key == "" || len(key) > 180 {
		return "", errors.New("world ID and idempotency key are required")
	}
	if _, err := validateScene(worldID, proposal, now); err != nil {
		return "", err
	}
	observers, err := json.Marshal(proposal.ObserverIDs)
	if err != nil {
		return "", err
	}
	var parent any
	if len(proposal.ParentIDs) == 1 {
		ref := proposal.ParentIDs[0]
		if !strings.HasPrefix(ref, "worldsim:") {
			return "", errors.New("world scene parent must be a simulated event")
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(ref, "worldsim:"), 10, 64)
		if err != nil || id < 1 {
			return "", errors.New("invalid world scene parent")
		}
		parent = id
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var existingID int64
	var existingWorld, existingSubject, existingKind, existingObservers, existingPayload, existingSchema string
	var existingAt int64
	var existingParent sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id,world_id,subject_id,occurred_ns,kind,parent_event_id,observer_ids_json,payload_json,schema
FROM simulated_events WHERE idempotency_key=?`, key).Scan(&existingID, &existingWorld, &existingSubject, &existingAt, &existingKind, &existingParent, &existingObservers, &existingPayload, &existingSchema)
	if err == nil {
		wantedParent, _ := parent.(int64)
		if existingWorld != worldID || existingSubject != proposal.SubjectID || existingAt != proposal.At.UnixNano() || existingKind != proposal.Kind || existingParent.Int64 != wantedParent || existingObservers != string(observers) || existingPayload != string(proposal.Payload) || existingSchema != proposal.Schema {
			return "", errors.New("idempotency key already belongs to a different world event")
		}
		return fmt.Sprintf("worldsim:%d", existingID), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if parentID, ok := parent.(int64); ok {
		var parentWorld string
		var parentAt int64
		err := tx.QueryRowContext(ctx, "SELECT world_id,occurred_ns FROM simulated_events WHERE id=?", parentID).Scan(&parentWorld, &parentAt)
		if err != nil || parentWorld != worldID || parentAt > proposal.At.UnixNano() {
			return "", errors.New("world scene parent is missing, later, or in another world")
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO simulated_events(world_id,subject_id,occurred_ns,kind,parent_event_id,observer_ids_json,payload_json,schema,idempotency_key)
VALUES(?,?,?,?,?,?,?,?,?)`, worldID, proposal.SubjectID, proposal.At.UnixNano(), proposal.Kind, parent, string(observers), string(proposal.Payload), proposal.Schema, key)
	if err != nil {
		return "", err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	// A witnessed experience may become a contact candidate. It is still
	// subject to the ordinary relationship, quiet-hour and send policies.
	if slicesContain(proposal.ObserverIDs, "default") {
		relation, err := relationshipFrom(ctx, tx)
		if err != nil {
			return "", err
		}
		if relation.Stage != relationship.Stranger && !relation.Reserved {
			var lastUser string
			err = tx.QueryRowContext(ctx, `SELECT created_at FROM messages WHERE role='user'
AND NOT EXISTS(SELECT 1 FROM local_commands WHERE source_message_id=messages.id)
ORDER BY id DESC LIMIT 1`).Scan(&lastUser)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return "", err
			}
			if err == nil {
				lastAt, err := time.Parse(time.RFC3339Nano, lastUser)
				if err != nil {
					return "", err
				}
				if !proposal.At.Before(lastAt) && proposal.At.Sub(lastAt) <= 72*time.Hour {
					_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO contact_intents(topic,due_ns,expires_ns,idempotency_key,created_ns,updated_ns)
VALUES('world_scene',?,?,?,?,?)`, proposal.At.Add(15*time.Minute).UnixNano(), proposal.At.Add(8*time.Hour).UnixNano(),
						fmt.Sprintf("worldsim:%d", id), proposal.At.UnixNano(), proposal.At.UnixNano())
					if err != nil {
						return "", err
					}
				}
			}
		}
	}
	return fmt.Sprintf("worldsim:%d", id), tx.Commit()
}

func slicesContain(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// SimulatedScene returns only an event that the character actually witnessed.
func (h *History) SimulatedScene(ctx context.Context, id int64) (extension.Event, error) {
	var subject, worldID, kind, observers, payload, schema string
	var stamp, parent int64
	err := h.db.QueryRowContext(ctx, `SELECT subject_id,world_id,kind,observer_ids_json,payload_json,schema,occurred_ns,COALESCE(parent_event_id,0)
FROM simulated_events WHERE id=? AND EXISTS(SELECT 1 FROM json_each(observer_ids_json) WHERE value='default')`, id).
		Scan(&subject, &worldID, &kind, &observers, &payload, &schema, &stamp, &parent)
	if err != nil {
		return extension.Event{}, err
	}
	var observerIDs []string
	if err := json.Unmarshal([]byte(observers), &observerIDs); err != nil {
		return extension.Event{}, err
	}
	event := extension.Event{ID: fmt.Sprintf("worldsim:%d", id), SubjectID: subject, WorldID: worldID,
		At: time.Unix(0, stamp).UTC(), Kind: kind, Domain: "simulated_world", Provenance: "simulator",
		Visibility: "character", ObserverIDs: observerIDs, Payload: json.RawMessage(payload), Schema: schema}
	if parent > 0 {
		event.ParentIDs = []string{fmt.Sprintf("worldsim:%d", parent)}
	}
	return event, nil
}

// VisibleSimulatedScenes returns only scenes the actor actually witnessed.
// It reads from canonical events, not a model-generated memory summary.
func (h *History) VisibleSimulatedScenes(ctx context.Context, worldID, observerID string, before time.Time, limit int) ([]extension.Event, error) {
	if limit < 1 || limit > 20 || observerID == "" || worldID == "" {
		return nil, errors.New("invalid visible scene query")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT s.id,s.subject_id,s.occurred_ns,s.kind,s.observer_ids_json,s.payload_json,s.schema,COALESCE(s.parent_event_id,0),e.sequence,e.recorded_at
FROM simulated_events s JOIN experience_events e ON e.source_table='simulated_events' AND e.source_id=s.id
WHERE s.world_id=? AND s.occurred_ns<=? AND EXISTS(SELECT 1 FROM json_each(s.observer_ids_json) WHERE value=?)
ORDER BY s.occurred_ns DESC,s.id DESC LIMIT ?`, worldID, before.UnixNano(), observerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []extension.Event
	for rows.Next() {
		var id, stamp, parent, sequence int64
		var subject, kind, observers, payload, schema, recorded string
		if err := rows.Scan(&id, &subject, &stamp, &kind, &observers, &payload, &schema, &parent, &sequence, &recorded); err != nil {
			return nil, err
		}
		recordedAt, err := time.ParseInLocation("2006-01-02 15:04:05", recorded, time.UTC)
		if err != nil {
			return nil, err
		}
		var observerIDs []string
		if err := json.Unmarshal([]byte(observers), &observerIDs); err != nil {
			return nil, err
		}
		event := extension.Event{Sequence: sequence, ID: fmt.Sprintf("worldsim:%d", id), Kind: kind, Domain: "simulated_world", WorldID: worldID,
			SubjectID: subject, At: time.Unix(0, stamp).UTC(), RecordedAt: recordedAt, Provenance: "simulator", Visibility: "character", ObserverIDs: observerIDs,
			Payload: json.RawMessage(payload), Schema: schema}
		if parent > 0 {
			event.ParentIDs = []string{fmt.Sprintf("worldsim:%d", parent)}
		}
		result = append(result, event)
	}
	return result, rows.Err()
}
