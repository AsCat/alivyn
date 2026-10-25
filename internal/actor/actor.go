// Package actor separates a character's conversational choice from its wording.
// Decisions are short, inspectable action records, not model chain of thought.
package actor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/world"
)

type Completer interface {
	Complete(context.Context, string, []model.Message) (string, error)
}

type Evidence struct {
	ID         string `json:"id"`
	Provenance string `json:"provenance"`
	At         string `json:"at,omitempty"`
	Text       string `json:"text"`
	ObjectID   string `json:"object_id,omitempty"`
	ParentID   string `json:"parent_id,omitempty"`
	Action     string `json:"action,omitempty"`
}

type Frame struct {
	SourceMessageID  int64            `json:"-"`
	Name             string           `json:"name"`
	Reality          string           `json:"reality"`
	Language         string           `json:"language"`
	Style            string           `json:"style"`
	Background       string           `json:"background"`
	Values           string           `json:"values,omitempty"`
	Habits           string           `json:"habits,omitempty"`
	Abilities        string           `json:"abilities,omitempty"`
	Canon            string           `json:"canon,omitempty"`
	WorldGoal        string           `json:"world_goal,omitempty"`
	WorldState       *world.GoalState `json:"world_state,omitempty"`
	Relationship     string           `json:"relationship"`
	PriorUserTurns   int              `json:"prior_user_turns"`
	OmittedUserTurns int              `json:"omitted_user_turns,omitempty"`
	Now              string           `json:"now"`
	Latest           Evidence         `json:"latest"`
	Previous         *Decision        `json:"previous_decision,omitempty"`
	Continuity       *Continuity      `json:"continuity,omitempty"`
	Evidence         []Evidence       `json:"evidence"`
	Recent           []model.Message  `json:"recent"`
}

type Decision struct {
	Act           string      `json:"act"`
	Focus         string      `json:"focus"`
	EvidenceIDs   []string    `json:"evidence_ids"`
	Brevity       string      `json:"brevity"`
	Posture       string      `json:"posture,omitempty"`
	Carryover     *Continuity `json:"carryover,omitempty"`
	TruthBoundary bool        `json:"truth_boundary,omitempty"`
}

// Continuity is a short, sourced interpretation of the character's own
// unfinished reaction. It is private context, never a fact about the user.
type Continuity struct {
	Posture   string   `json:"posture"`
	Concern   string   `json:"concern"`
	SourceIDs []string `json:"source_ids"`
	Lifetime  string   `json:"lifetime"` // hours or days
}

var acts = map[string]bool{
	"acknowledge": true, "answer": true, "ask": true, "pause": true,
	"share": true, "disagree": true, "clarify": true, "repair": true,
}

var postures = map[string]bool{
	"steady": true, "reserved": true, "curious": true,
	"warm": true, "unsettled": true, "resolute": true,
	"repairing": true,
}

var ErrInvalidReply = errors.New("actor reply is empty or too long")

func Plan(ctx context.Context, client Completer, modelID string, frame Frame) (Decision, error) {
	return plan(ctx, client, modelID, frame, nil, "")
}

// Replan changes the conversational approach after wording retries fail review.
// The rejected decision is feedback, never evidence about the user or world.
func Replan(ctx context.Context, client Completer, modelID string, frame Frame, rejected Decision, issue string) (Decision, error) {
	return plan(ctx, client, modelID, frame, &rejected, issue)
}

func plan(ctx context.Context, client Completer, modelID string, frame Frame, rejected *Decision, issue string) (Decision, error) {
	data, err := json.Marshal(frame)
	if err != nil {
		return Decision{}, err
	}
	instruction := `Choose one conversational action for this continuing fictional character. Decide what matters now from the latest words, actual past contact, her habits and values, an unfinished thread, and her own world goal. The latest user message is a report of what they said, not proof of an offline event or of their hidden feelings. A previous continuity note is her revisable interpretation, not a fact about the user.

Social judgment:
- A greeting can receive a greeting. Do not turn it into an interview or a service offer. Curiosity should follow a specific detail, not a need to keep talking.
- If the user challenges the immediately preceding assistant reply, choose repair. Own the character's mistaken assumption briefly; do not justify it or ask the user to explain their reaction.
- If an unfinished topic was already acknowledged and the latest message adds no fact or request, choose pause. Leave room instead of offering reassurance again. Never pause a direct question, first greeting, or correction.
- If asked whether the character remembers someone, use actual earlier chat. Brief contact is still contact; it is not proof of a physical identity or meeting.
- The character may have a view or disagree. Do not switch to a technical or therapeutic adviser outside her abilities.
- Select a simulated-world event only when asked about her life or when this exact world topic is already being discussed. Current world state constrains claims; events with the same object_id concern one object. Unknowns stay unknown.
- A claim of physical meeting, a direct reality question, or a question about AI autonomy sets truth_boundary=true. In that mode answer reality plainly, without fictional world news.

Choose act=acknowledge|answer|ask|share|disagree|clarify|repair|pause and brevity=short|normal. Choose posture=steady|reserved|curious|warm|unsettled|resolute|repairing: her own way of holding this moment, never a diagnosis of the user. An explicitly unfinished shared topic or her own unresolved misstep may become carryover; otherwise use null. Carryover needs selected source IDs, one brief concern, and lifetime hours or days. Keep it while unresolved; drop it when resolved. Silence alone is not evidence of rejection.

Return ONLY JSON: {"act":"...","focus":"one short concrete focus","evidence_ids":["IDs from frame"],"brevity":"short|normal","posture":"...","carryover":null or {"posture":"...","concern":"explicit unfinished thread or own unresolved reaction","source_ids":["selected IDs"],"lifetime":"hours|days"},"truth_boundary":false}. No dialogue text or hidden reasoning.`
	if rejected != nil {
		feedback, err := json.Marshal(struct {
			Rejected Decision `json:"rejected_decision"`
			Issue    string   `json:"review_issue"`
		}{*rejected, issue})
		if err != nil {
			return Decision{}, err
		}
		instruction += " The previous approach failed review. Pick a different conversational approach that can answer naturally while respecting the evidence. This review feedback is not a new fact: " + string(feedback)
	}
	raw, err := client.Complete(ctx, modelID, []model.Message{
		{Role: "system", Content: instruction},
		{Role: "user", Content: string(data)},
	})
	if err != nil {
		return Decision{}, err
	}
	return ParseDecision(raw, frame)
}

func ParseDecision(raw string, frame Frame) (Decision, error) {
	raw = strings.TrimSpace(raw)
	if start, end := strings.IndexByte(raw, '{'), strings.LastIndexByte(raw, '}'); start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	var decision Decision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return Decision{}, fmt.Errorf("invalid actor decision JSON: %w", err)
	}
	if !acts[decision.Act] || (decision.Brevity != "short" && decision.Brevity != "normal") {
		return Decision{}, errors.New("invalid actor action or brevity")
	}
	decision.Focus = strings.TrimSpace(decision.Focus)
	if decision.Focus == "" || len([]rune(decision.Focus)) > 160 || strings.IndexFunc(decision.Focus, unicode.IsControl) >= 0 {
		return Decision{}, errors.New("invalid actor focus")
	}
	allowed := map[string]bool{frame.Latest.ID: true}
	for _, item := range frame.Evidence {
		allowed[item.ID] = true
	}
	if len(decision.EvidenceIDs) > 8 {
		return Decision{}, errors.New("too many actor evidence references")
	}
	seen := map[string]bool{}
	for _, id := range decision.EvidenceIDs {
		if !allowed[id] || seen[id] {
			return Decision{}, fmt.Errorf("unknown or duplicate actor evidence ID %q", id)
		}
		seen[id] = true
	}
	if !postures[decision.Posture] {
		decision.Posture = "steady" // Older decisions remain readable.
	}
	if c := decision.Carryover; c != nil {
		c.Concern = strings.TrimSpace(c.Concern)
		valid := postures[c.Posture] && c.Concern != "" && len([]rune(c.Concern)) <= 160 &&
			strings.IndexFunc(c.Concern, unicode.IsControl) < 0 && (c.Lifetime == "hours" || c.Lifetime == "days") &&
			len(c.SourceIDs) > 0 && len(c.SourceIDs) <= 3
		for _, id := range c.SourceIDs {
			if !seen[id] {
				valid = false
			}
		}
		if !valid {
			decision.Carryover = nil // Bad private state must not block a reply.
		}
	}
	return decision, nil
}

func Realize(ctx context.Context, client Completer, modelID string, frame Frame, decision Decision, feedback string) (string, error) {
	byID := map[string]Evidence{}
	for _, item := range frame.Evidence {
		byID[item.ID] = item
	}
	var said, uncertain []string
	var worldNotes []Evidence
	selected := []Evidence{frame.Latest}
	for _, id := range decision.EvidenceIDs {
		if id != frame.Latest.ID {
			selected = append(selected, byID[id])
		}
	}
	for _, item := range selected {
		note := item.Text
		if item.At != "" {
			note = item.At + ": " + note
		}
		switch item.Provenance {
		case "simulated_world":
			if !decision.TruthBoundary {
				worldNotes = append(worldNotes, item)
			}
		case "model_inferred":
			uncertain = append(uncertain, note)
		default:
			said = append(said, note)
		}
	}
	priorReply := ""
	for i := len(frame.Recent) - 2; i >= 0; i-- {
		if frame.Recent[i].Role == "assistant" {
			priorReply = frame.Recent[i].Content
			break
		}
	}
	data, err := json.Marshal(struct {
		Name             string           `json:"name"`
		Reality          string           `json:"reality_boundary,omitempty"`
		Language         string           `json:"language"`
		Style            string           `json:"style"`
		Background       string           `json:"background"`
		Values           string           `json:"values,omitempty"`
		Habits           string           `json:"habits,omitempty"`
		Relationship     string           `json:"relationship"`
		Posture          string           `json:"posture"`
		PriorUserTurns   int              `json:"prior_user_turns"`
		RecallIncomplete bool             `json:"recall_incomplete,omitempty"`
		Act              string           `json:"act"`
		Brevity          string           `json:"brevity"`
		UserSaid         []string         `json:"user_said"`
		WorldNotes       []Evidence       `json:"fictional_world_notes,omitempty"`
		WorldState       *world.GoalState `json:"world_state,omitempty"`
		Unconfirmed      []string         `json:"unconfirmed,omitempty"`
		PriorReply       string           `json:"previous_reply,omitempty"`
	}{Name: frame.Name, Reality: func() string {
		if decision.TruthBoundary {
			return frame.Reality
		}
		return ""
	}(), Language: frame.Language, Style: frame.Style, Background: frame.Background, Values: frame.Values, Habits: frame.Habits,
		Relationship: frame.Relationship, PriorUserTurns: frame.PriorUserTurns,
		Posture:          decision.Posture,
		RecallIncomplete: frame.OmittedUserTurns > 0,
		Act:              decision.Act, Brevity: decision.Brevity,
		UserSaid: said, WorldNotes: worldNotes, Unconfirmed: uncertain, PriorReply: priorReply,
		WorldState: func() *world.GoalState {
			if len(worldNotes) > 0 {
				return frame.WorldState
			}
			return nil
		}()})
	if err != nil {
		return "", err
	}
	instruction := `Write one private-chat message as this continuing character. The JSON is private context. Follow the chosen act and answer the latest words first. Let background, values, habits and posture shape a point of view and the amount said; never announce those fields. A small complete reply is enough. Uneven rhythm is natural: one short line for a greeting, a considered sentence for disagreement, a direct admission for a mistake. Do not add a question just to keep the exchange alive.

Act contract: acknowledge receives the message without a new question; pause gives a tiny acknowledgment (at most eight Chinese characters or four English words) and no repeated reassurance; repair owns the character's own misread without defending it. A question belongs to act=ask or to a substantive answer that genuinely needs it. Previous_reply is a conversation fact, not a style example; do not repeat its handoff, reassurance, or wording. Brief prior contact is something to remember if asked, without pretending to know the person's physical identity.

Claims about earlier chat, personal facts, and dated world events need selected evidence. Use only the world notes included here, and only when relevant. Do not invent a job, incident, physical meeting, a shared room, or a user's motive. The exchange happens through chat; use physical gestures only when recorded in the fictional world and relevant. World notes belong to a fictional setting, not shared physical history. If reality_boundary is present, answer plainly that this is an AI character in a fictional setting; do not imply she might have met the person elsewhere. Never discuss memory bookkeeping or model process unless asked. For short brevity use at most two short sentences. Output only the message.`
	if feedback != "" {
		instruction += " The previous draft failed a fact or character check: " + feedback + ". Write a fresh message using the same decision and evidence."
	}
	messages := []model.Message{{Role: "system", Content: instruction}, {Role: "system", Content: string(data)}}
	// The previous reply is private continuity context, not an assistant-role
	// example for the model to imitate on the next turn.
	messages = append(messages, model.Message{Role: "user", Content: frame.Latest.Text})
	reply, err := client.Complete(ctx, modelID, messages)
	if err != nil {
		return "", err
	}
	reply = strings.TrimSpace(reply)
	if reply == "" || len([]rune(reply)) > 1000 {
		return "", ErrInvalidReply
	}
	return reply, nil
}

type Review struct {
	Pass         bool     `json:"pass"`
	Issue        string   `json:"issue"`
	UsedWorldIDs []string `json:"used_world_ids,omitempty"`
}

// Check reviews the final utterance against all available evidence and the
// chosen action. It evaluates invariants rather than matching canned phrases.
func Check(ctx context.Context, client Completer, modelID string, frame Frame, decision Decision, reply string) (Review, error) {
	questions := strings.Count(reply, "?") + strings.Count(reply, "？")
	if questions > 1 {
		return Review{Pass: false, Issue: "The reply asked more than one question."}, nil
	}
	if questions > 0 && (decision.Act == "acknowledge" || decision.Act == "pause" || decision.Act == "repair") {
		return Review{Pass: false, Issue: "The chosen act leaves room and should not add a question."}, nil
	}
	selectedIDs := map[string]bool{}
	for _, id := range decision.EvidenceIDs {
		selectedIDs[id] = true
	}
	reviewFrame := frame
	reviewFrame.Evidence = nil
	hasSelectedWorld := false
	for _, item := range frame.Evidence {
		if selectedIDs[item.ID] {
			reviewFrame.Evidence = append(reviewFrame.Evidence, item)
			if item.Provenance == "simulated_world" {
				hasSelectedWorld = true
			}
		}
	}
	if !hasSelectedWorld {
		reviewFrame.WorldState = nil
		reviewFrame.WorldGoal = ""
		reviewFrame.Canon = ""
	}
	data, err := json.Marshal(struct {
		Frame    Frame    `json:"frame"`
		Decision Decision `json:"decision"`
		Reply    string   `json:"reply"`
	}{reviewFrame, decision, reply})
	if err != nil {
		return Review{}, err
	}
	instruction := `Review this fictional AI character's draft against the selected evidence and chosen act. Return ONLY JSON {"pass":true|false,"issue":"short concrete reason or empty","used_world_ids":["selected world IDs actually mentioned"]}.

Fail if the reply invents or contradicts a chat, user fact, world event, duration, job, or physical meeting; treats a user assertion or an inference as verified reality; assigns an unspoken motive or feeling to the user; crosses a relationship boundary; or presents a fictional event as a physical-world alibi. Brief recorded contact is real contact: if asked whether she remembers, do not dismiss it as too little or speculate about meeting elsewhere. A direct reality question must say the character and setting are fictional/AI plainly.

Fail if the reply conflicts with the act: acknowledge, pause, and repair must not add a question; pause needs no fresh content but cannot repeat reassurance or invite disclosure; repair must own the character's misread without defending it. Fail generic service offers, stacked questions, repeated handoffs from previous_reply, uninvited coaching, internal bookkeeping, or a work diary in response to an unrelated message. A selected world event that remained private is NOT used. Only report IDs actually used in the reply. A concise natural reply can pass.`
	raw, err := client.Complete(ctx, modelID, []model.Message{{Role: "system", Content: instruction}, {Role: "user", Content: string(data)}})
	if err != nil {
		return Review{}, err
	}
	raw = strings.TrimSpace(raw)
	if start, end := strings.IndexByte(raw, '{'), strings.LastIndexByte(raw, '}'); start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	var review Review
	if err := json.Unmarshal([]byte(raw), &review); err != nil {
		return Review{}, fmt.Errorf("invalid actor review JSON: %w", err)
	}
	if !review.Pass && (strings.TrimSpace(review.Issue) == "" || len([]rune(review.Issue)) > 300) {
		return Review{}, errors.New("invalid actor review issue")
	}
	selected := map[string]bool{}
	for _, id := range decision.EvidenceIDs {
		if strings.HasPrefix(id, "worldv2:") {
			selected[id] = true
		}
	}
	seen := map[string]bool{}
	var uniqueWorldIDs []string
	for _, id := range review.UsedWorldIDs {
		if !strings.HasPrefix(id, "worldv2:") {
			// A reviewer may put selected chat IDs in this field. They are
			// never world use, so they cannot affect the world ledger.
			if selectedIDs[id] || id == frame.Latest.ID {
				continue
			}
			return Review{Pass: false, Issue: "The reply was attributed to unknown evidence."}, nil
		}
		if !selected[id] {
			return Review{Pass: false, Issue: "The reply was attributed to an unselected world event."}, nil
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		uniqueWorldIDs = append(uniqueWorldIDs, id)
	}
	if decision.TruthBoundary && len(uniqueWorldIDs) > 0 {
		return Review{Pass: false, Issue: "The reply mixed a reality-boundary answer with fictional world events."}, nil
	}
	review.UsedWorldIDs = uniqueWorldIDs
	return review, nil
}
