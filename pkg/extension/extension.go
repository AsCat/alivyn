// Package extension defines the experimental capability boundary for Alivyn.
// A module may provide several capabilities. The core owns events, permissions,
// and delivery; a module only returns proposals or derived views.
package extension

import (
	"context"
	"encoding/json"
	"time"
)

const APIVersion = "alivyn.extension/v1alpha1"

type Capability string

const (
	Perceive Capability = "perceive"
	Simulate Capability = "simulate"
	Project  Capability = "project"
	Recall   Capability = "recall"
	Decide   Capability = "decide"
	Act      Capability = "act"
	Express  Capability = "express"
)

// Manifest is a declaration, not a grant of device or network access.
type Manifest struct {
	ID           string       `json:"id"`
	Version      string       `json:"version"`
	APIVersion   string       `json:"api_version"`
	Capabilities []Capability `json:"capabilities"`
	Permissions  []string     `json:"permissions,omitempty"`
}

// Event is the core-owned, immutable record from which state can be rebuilt.
// Payload is versioned by Kind. Provenance distinguishes observed, simulated,
// inferred, and external facts. Visibility limits what a module may receive.
type Event struct {
	Sequence      int64           `json:"sequence"`
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Domain        string          `json:"domain"`
	WorldID       string          `json:"world_id,omitempty"`
	SubjectID     string          `json:"subject_id"`
	At            time.Time       `json:"at"`
	RecordedAt    time.Time       `json:"recorded_at"`
	Provenance    string          `json:"provenance"`
	Visibility    string          `json:"visibility"`
	ObserverIDs   []string        `json:"observer_ids,omitempty"`
	ParentIDs     []string        `json:"parent_ids,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	Schema        string          `json:"schema"`
}

// EventReader is a scoped, read-only view supplied by the host. A visual
// console or world client resumes from Sequence without owning a second
// copy of the character's history.
type EventQuery struct {
	SubjectID string    `json:"subject_id"`
	WorldID   string    `json:"world_id,omitempty"`
	After     int64     `json:"after"`
	Until     time.Time `json:"until,omitempty"`
	Limit     int       `json:"limit"`
}

type EventReader interface {
	ReadEvents(context.Context, EventQuery) ([]Event, error)
}

type Observation struct {
	Kind      string          `json:"kind"`
	At        time.Time       `json:"at"`
	Source    string          `json:"source"`
	MediaType string          `json:"media_type,omitempty"`
	MediaRef  string          `json:"media_ref,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	ParentIDs []string        `json:"parent_ids,omitempty"`
}

// Perceptor proposes observations; the core validates and records them before
// any other module treats them as an experience.
type Perceptor interface {
	Observe(context.Context, time.Time) ([]Observation, error)
}

// Simulator proposes fictional-world transitions. The core checks canon,
// causality, elapsed time, and permissions before committing an event.
type SimulationRequest struct {
	SubjectID string          `json:"subject_id"`
	WorldID   string          `json:"world_id"`
	AsOf      time.Time       `json:"as_of"`
	World     json.RawMessage `json:"world"`
	Context   json.RawMessage `json:"context"`
}

type EventProposal struct {
	ProposalID  string          `json:"proposal_id"`
	Kind        string          `json:"kind"`
	SubjectID   string          `json:"subject_id"`
	At          time.Time       `json:"at"`
	ObserverIDs []string        `json:"observer_ids,omitempty"`
	ParentIDs   []string        `json:"parent_ids,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	Schema      string          `json:"schema"`
}

type Simulator interface {
	Advance(context.Context, SimulationRequest) ([]EventProposal, error)
}

type ProjectionRequest struct {
	SubjectID       string    `json:"subject_id"`
	BeforeMessageID int64     `json:"before_message_id,omitempty"`
	AsOf            time.Time `json:"as_of"`
}

type ProjectionResult struct {
	Namespace string          `json:"namespace"`
	Version   string          `json:"version"`
	Value     json.RawMessage `json:"value"`
	SourceIDs []string        `json:"source_ids,omitempty"`
	At        time.Time       `json:"at"`
}

// Projector derives an actor's state from recorded experience. Namespaces may
// represent affect, relationships, interests, goals, attention, or other state.
type Projector interface {
	Project(context.Context, ProjectionRequest) (ProjectionResult, error)
}

type RecallQuery struct {
	SubjectID       string    `json:"subject_id"`
	BeforeMessageID int64     `json:"before_message_id,omitempty"`
	AsOf            time.Time `json:"as_of"`
	Topic           string    `json:"topic,omitempty"`
	Limit           int       `json:"limit,omitempty"`
}

type MemoryHit struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Content    string          `json:"content"`
	SourceIDs  []string        `json:"source_ids,omitempty"`
	Fields     json.RawMessage `json:"fields,omitempty"`
	Confidence float64         `json:"confidence,omitempty"`
}

// Retriever selects memories. It cannot author a new event or make an
// unsupported inference into a fact.
type Retriever interface {
	Recall(context.Context, RecallQuery) ([]MemoryHit, error)
}

type DecisionRequest struct {
	SubjectID string          `json:"subject_id"`
	AsOf      time.Time       `json:"as_of"`
	Context   json.RawMessage `json:"context"`
}

type Decision struct {
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	SourceIDs []string        `json:"source_ids,omitempty"`
}

type Decider interface {
	Decide(context.Context, DecisionRequest) (Decision, error)
}

// ActionRequest is an intent, never proof that an external action occurred.
// The core must authorize, deduplicate, and record its result before use.
type ActionRequest struct {
	ID             string          `json:"id"`
	SubjectID      string          `json:"subject_id"`
	Operation      string          `json:"operation"`
	Target         string          `json:"target"`
	Arguments      json.RawMessage `json:"arguments"`
	SourceIDs      []string        `json:"source_ids,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type ActionReceipt struct {
	RequestID string          `json:"request_id"`
	Status    string          `json:"status"`
	At        time.Time       `json:"at"`
	Result    json.RawMessage `json:"result,omitempty"`
	Source    string          `json:"source"`
}

type Actuator interface {
	Execute(context.Context, ActionRequest) (ActionReceipt, error)
}

type ExpressionRequest struct {
	SubjectID string          `json:"subject_id"`
	Channel   string          `json:"channel"`
	Decision  Decision        `json:"decision"`
	Context   json.RawMessage `json:"context"`
}

type Expression struct {
	MediaType string          `json:"media_type"`
	Payload   json.RawMessage `json:"payload"`
}

type Expresser interface {
	Express(context.Context, ExpressionRequest) ([]Expression, error)
}
