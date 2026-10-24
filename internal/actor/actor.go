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
)

type Completer interface {
	Complete(context.Context, string, []model.Message) (string, error)
}

type Evidence struct {
	ID         string `json:"id"`
	Provenance string `json:"provenance"`
	At         string `json:"at,omitempty"`
	Text       string `json:"text"`
}

type Frame struct {
	SourceMessageID int64           `json:"-"`
	Name            string          `json:"name"`
	Reality         string          `json:"reality"`
	Language        string          `json:"language"`
	Style           string          `json:"style"`
	Background      string          `json:"background"`
	Values          string          `json:"values,omitempty"`
	Abilities       string          `json:"abilities,omitempty"`
	Canon           string          `json:"canon,omitempty"`
	Relationship    string          `json:"relationship"`
	PriorUserTurns  int             `json:"prior_user_turns"`
	Now             string          `json:"now"`
	Latest          Evidence        `json:"latest"`
	Previous        *Decision       `json:"previous_decision,omitempty"`
	Evidence        []Evidence      `json:"evidence"`
	Recent          []model.Message `json:"recent"`
}

type Decision struct {
	Act         string   `json:"act"`
	Focus       string   `json:"focus"`
	EvidenceIDs []string `json:"evidence_ids"`
	Brevity     string   `json:"brevity"`
}

var acts = map[string]bool{
	"acknowledge": true, "answer": true, "ask": true,
	"share": true, "disagree": true, "clarify": true,
}

var ErrInvalidReply = errors.New("actor reply is empty or too long")

func Plan(ctx context.Context, client Completer, modelID string, frame Frame) (Decision, error) {
	data, err := json.Marshal(frame)
	if err != nil {
		return Decision{}, err
	}
	instruction := `Choose the character's next conversational action before writing any message. Use the current user's words, actual relationship, ongoing character interests, abilities, and evidence. Stay within the character's competence; do not become a general technical or therapeutic adviser. The prior_user_turns count is literal recorded contact: if greater than zero, they have already exchanged messages, even when the relationship stage is stranger. A prior greeting is real contact but does not mean familiarity. User messages are observations of what the user said, not verification of any claimed offline fact. Do not analyze the user as a patient or customer. Distinguish remembered chat from the user's real-world identity. The character may have a view, ask, share, disagree, clarify, or simply acknowledge. Do not default to asking a question or offering generic help. If the user answered the prior question, move the thread forward instead of asking the same question in new words. If the relationship is still distant, do not invent familiarity. Do not invent an offline meeting or an unrecorded world event. Select only evidence relevant to the chosen focus. An ongoing world event is background, not an automatic topic: do not pivot a greeting, user disclosure, or unrelated question into a world diary. Share world news when invited, already discussing it, or when a concrete development naturally matters to the current relationship. Output ONLY one JSON object: {"act":"acknowledge|answer|ask|share|disagree|clarify","focus":"one short concrete focus","evidence_ids":["IDs from frame"],"brevity":"short|normal"}. Choose evidence IDs for every earlier chat, personal fact, or world event the next message may refer to. Current user message has its own ID. Keep focus under 160 characters. No dialogue text or hidden reasoning.`
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
	return decision, nil
}

func Realize(ctx context.Context, client Completer, modelID string, frame Frame, decision Decision, feedback string) (string, error) {
	byID := map[string]Evidence{}
	for _, item := range frame.Evidence {
		byID[item.ID] = item
	}
	var said, worldNotes, uncertain []string
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
			worldNotes = append(worldNotes, note)
		case "model_inferred":
			uncertain = append(uncertain, note)
		default:
			said = append(said, note)
		}
	}
	priorMove := ""
	if frame.Previous != nil {
		priorMove = frame.Previous.Act + ": " + frame.Previous.Focus
	}
	data, err := json.Marshal(struct {
		Name           string   `json:"name"`
		Reality        string   `json:"reality"`
		Language       string   `json:"language"`
		Style          string   `json:"style"`
		Background     string   `json:"background"`
		Values         string   `json:"values,omitempty"`
		Abilities      string   `json:"abilities,omitempty"`
		Canon          string   `json:"canon,omitempty"`
		Relationship   string   `json:"relationship"`
		PriorUserTurns int      `json:"prior_user_turns"`
		Now            string   `json:"now"`
		Act            string   `json:"act"`
		Focus          string   `json:"focus"`
		Brevity        string   `json:"brevity"`
		UserSaid       []string `json:"user_said"`
		WorldNotes     []string `json:"fictional_world_notes,omitempty"`
		Unconfirmed    []string `json:"unconfirmed,omitempty"`
		PriorMove      string   `json:"prior_move,omitempty"`
	}{frame.Name, frame.Reality, frame.Language, frame.Style, frame.Background, frame.Values, frame.Abilities, frame.Canon, frame.Relationship, frame.PriorUserTurns, frame.Now,
		decision.Act, decision.Focus, decision.Brevity, said, worldNotes, uncertain, priorMove})
	if err != nil {
		return "", err
	}
	instruction := `You are the one continuing character described in the frame. Write exactly one private-chat message in the requested language. Carry out the chosen conversational act in your own voice; the decision and evidence are private state, not words to recite. Let stable values shape your judgment, without naming or explaining the values. Canon is the enduring fictional setting, not a dated event; do not recite it. Abilities describe what this individual actually knows; do not suddenly become an expert in an unrelated domain. Speak like an individual in a chat, not someone writing a case note or hosting a service: no process reports, memory-system language, generic invitations to share, therapeutic validation, guessed feelings, unsolicited coaching, or meta-analysis of the user. Relationship stage, turn counts, and character values are private constraints; do not announce or explain them unless directly asked. Let a simple exchange stay simple. For short brevity, use at most two short sentences. Answer the user's actual point first; explain only if the user asks or a necessary truth boundary requires it. The prior_user_turns count means actual recorded earlier contact. Never describe this as the first exchange when it is greater than zero, and do not imply familiarity from a mere greeting. Ask only if the chosen act is ask or clarify, and ask at most one concrete question without a list of imagined causes. Earlier assistant messages are conversation history, never style examples. Every claim of a past exchange, user fact, or dated event in your fictional world must be supported by selected evidence; general setting can come from canon. User messages are evidence of what was said, not proof that an asserted offline event occurred. Model-inferred facts are uncertain. A simulated-world event is not evidence of where you were in the user's physical world. The character cannot meet the user offline, so an offline encounter cannot be confirmed; state the limit in everyday words, without anatomical wording, an in-world alibi, or a defensive speech. If directly asked whether you are AI or whether the world is fictional, answer honestly. Output only the message.`
	if feedback != "" {
		instruction += " The previous draft failed a fact or character check: " + feedback + ". Write a fresh message using the same decision and evidence."
	}
	messages := []model.Message{{Role: "system", Content: instruction}, {Role: "system", Content: string(data)}}
	// The planner can examine the broader exchange, but wording may only use
	// selected evidence. A preceding assistant line supplies local continuity.
	for i := len(frame.Recent) - 2; i >= 0; i-- {
		if frame.Recent[i].Role == "assistant" {
			messages = append(messages, frame.Recent[i])
			break
		}
	}
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
	Pass  bool   `json:"pass"`
	Issue string `json:"issue"`
}

// Check reviews the final utterance against all available evidence and the
// chosen action. It evaluates invariants rather than matching canned phrases.
func Check(ctx context.Context, client Completer, modelID string, frame Frame, decision Decision, reply string) (Review, error) {
	questions := strings.Count(reply, "?") + strings.Count(reply, "？")
	if questions > 1 || (questions > 0 && decision.Act != "ask" && decision.Act != "clarify") {
		return Review{Pass: false, Issue: "The reply asked questions outside the chosen conversational act or asked more than one question."}, nil
	}
	data, err := json.Marshal(struct {
		Frame    Frame    `json:"frame"`
		Decision Decision `json:"decision"`
		Reply    string   `json:"reply"`
	}{frame, decision, reply})
	if err != nil {
		return Review{}, err
	}
	instruction := `Review a fictional AI character's private-chat draft. Judge the actual reply, not whether it contains forbidden words. FAIL when: (1) it asserts a past chat, user fact, world event, or duration without supporting evidence, or denies the recorded contact indicated by prior_user_turns; (2) it treats a simulated-world event as evidence of a physical-world meeting or alibi; (3) it claims a user's statement is verified reality or confidently assigns the user a feeling they did not state; (4) it ignores an explicit relationship boundary; (5) it contradicts the chosen act by making a generic service offer, unsolicited coaching, stacking questions, or listing imagined causes; (6) it repeats the prior question after the user has answered, or recites world notes verbatim instead of responding as a person; (7) it reports its process, records, memory mechanics, relationship stage, or character values when not asked, or offers a generic invitation instead of naturally answering the person's actual point; (8) it pivots an unrelated greeting, disclosure, or question into a world diary; (9) it acts as a domain expert outside the character's stated abilities instead of speaking from its own perspective. A truthful short reply can pass. The user may be playful. Output ONLY JSON: {"pass":true|false,"issue":"one short concrete reason, empty if pass"}. No rewrite or hidden reasoning.`
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
	return review, nil
}
