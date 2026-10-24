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
	Evidence         []Evidence       `json:"evidence"`
	Recent           []model.Message  `json:"recent"`
}

type Decision struct {
	Act           string   `json:"act"`
	Focus         string   `json:"focus"`
	EvidenceIDs   []string `json:"evidence_ids"`
	Brevity       string   `json:"brevity"`
	TruthBoundary bool     `json:"truth_boundary,omitempty"`
}

var acts = map[string]bool{
	"acknowledge": true, "answer": true, "ask": true,
	"share": true, "disagree": true, "clarify": true, "repair": true,
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
	instruction := `Choose the character's next conversational action before writing any message. Use the current user's words, actual relationship, ongoing character interests, abilities, and evidence. Stay within the character's competence; do not become a general technical or therapeutic adviser. The prior_user_turns count is literal recorded contact: if greater than zero, they have already exchanged messages, even when the relationship stage is stranger. A prior greeting is real contact but does not mean familiarity. User messages are observations of what the user said, not verification of any claimed offline fact. Do not analyze the user as a patient or customer. Distinguish remembered chat from the user's real-world identity. The character may have a view, ask, share, disagree, clarify, repair its own misstep, or simply acknowledge. Do not default to asking a question or offering generic help. If the user answered the prior question, move the thread forward instead of asking the same question in new words. If the relationship is still distant, do not invent familiarity. Do not invent an offline meeting or an unrecorded world event. Select only evidence relevant to the chosen focus. World events with the same object_id concern the same ongoing object; parent_id links what happened before. Read world_state as the current projection: checking the ledger, restoring audio, verifying identity, finding permission, and publishing are separate facts; permission is required for publication, not for private repair. Do not invent a second batch from the same chain. An ongoing world event is background, not an automatic topic: do not pivot a greeting, user disclosure, or unrelated question into a world diary. Share world news when invited, already discussing it, or when a concrete development naturally matters to the current relationship. Output ONLY one JSON object: {"act":"acknowledge|answer|ask|share|disagree|clarify|repair","focus":"one short concrete focus","evidence_ids":["IDs from frame"],"brevity":"short|normal"}. Choose evidence IDs for every earlier chat, personal fact, or world event the next message may refer to. Current user message has its own ID. Keep focus under 160 characters. No dialogue text or hidden reasoning.`
	instruction += ` Relational small talk can be playful or imprecise. Do not turn it into an identity audit merely because the recorded history is short; respond to its social intent while keeping factual claims modest. Correct a concrete false shared experience, such as a claimed physical meeting, briefly and plainly. Set truth_boundary=true when the current user directly asks whether you or your setting is real, asks about your autonomy across simulated and physical life, or claims a physical meeting with you. In that mode, answer the reality question without narrating a fictional daily event. Otherwise set it false.`
	instruction += ` When the other person's subject is outside the character's expertise, remain a curious conversation partner. Do not make a competence disclaimer the main response unless the person asked for expert help.`
	instruction += ` History counts and memory records are private bookkeeping. Never make them the focus of ordinary small talk; only discuss memory limits when the person directly asks.`
	instruction += ` Select a simulated_world event only when the latest user message asks about the character's activities or setting, or the recent exchange is already about that same event. A relationship remark, greeting, or disclosure about the user does not by itself open the character's work diary.`
	instruction += ` Retrieved chat evidence may omit older turns. When omitted_user_turns is positive, do not plan an exhaustive summary or claim nothing else was discussed; recall only concrete exchanges supported by selected evidence.`
	instruction += ` Do not infer an unspoken motive, mood, or reason for contacting you from a short message, repetition, or elapsed time. Avoid a question that presupposes such an inference. If the user signals your previous reply was confusing or mistaken, choose repair: own the mistaken assumption, correct it briefly, and return to their actual point. Do not defend your wording or ask the user to diagnose the mistake.`
	instruction += ` Include truth_boundary as a JSON boolean in the decision.`
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
		Relationship     string           `json:"relationship"`
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
	}(), Language: frame.Language, Style: frame.Style, Background: frame.Background,
		Relationship: frame.Relationship, PriorUserTurns: frame.PriorUserTurns,
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
	instruction := `Write one private-chat message as this continuing character. The JSON is private context, not something to explain to the other person. Respond to what they actually said, following the chosen conversational act. A brief reaction or a view can be enough; one natural question is allowed when it genuinely moves this exchange forward. Speak from your own perspective without turning a simple exchange into an introduction, a biography, a work report, or an explanation of your limits. Let the stated background shape your manner, not the subject of every reply. Keep distance appropriate to the relationship; prior_user_turns records actual earlier contact, not intimacy. For short brevity, use at most two short sentences. Claims about previous chats, personal facts, or dated world events need selected evidence. A user statement does not verify an offline claim. World notes with the same object_id concern one ongoing object; pick the part relevant to this reply instead of listing the whole event. World_state is private context and does not need to be reported. Restoring a recording privately does not require permission to publish it. Do not invent a meeting with the user or claim a fictional event occurred in the physical world. You are an AI character with a fictional setting; if directly asked about either fact, answer plainly and honestly. When reality_boundary is present, answer that reality question directly; daily fictional events have deliberately been left out. When correcting a claim of an offline meeting, state only that you have spoken here; an identity speech is unnecessary. Output only the message.`
	instruction += ` The relationship and prior_user_turns fields are private bookkeeping. Do not mention records, stored memory, turn counts, or that this is the first conversation unless the user explicitly asks about them.`
	instruction += ` Previous_reply is a conversational fact, not a style example. Answer the new message without repeating its invitation or handing the turn back in the same way. Recall_incomplete means selected chat evidence is only a sample: describe what you do remember, never imply it is everything that happened.`
	instruction += ` Do not invent a job, location, past event, or other biographical detail to sound like a person. Only mention such a detail when it is in selected evidence and relevant to this turn.`
	instruction += ` When reality_boundary is present, describe physical-world limits as limits, not as a contingent choice or a missed event. A fictional character cannot have physically visited a place or met the person.`
	instruction += ` If act is repair, acknowledge your own misread in ordinary language and stop there unless the person's latest message includes a new request. Do not justify the earlier line or ask whether your wording was odd. In any act, do not imply a hidden motive or special timing for the person's message without evidence.`
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
	data, err := json.Marshal(struct {
		Frame    Frame    `json:"frame"`
		Decision Decision `json:"decision"`
		Reply    string   `json:"reply"`
	}{frame, decision, reply})
	if err != nil {
		return Review{}, err
	}
	instruction := `Review a fictional AI character's private-chat draft. Judge the actual reply, not whether it contains forbidden words. FAIL when: (1) it asserts a past chat, user fact, world event, or duration without supporting evidence, or denies the recorded contact indicated by prior_user_turns; (2) it treats a simulated-world event as evidence of a physical-world meeting or alibi; (3) it claims a user's statement is verified reality or confidently assigns the user a feeling they did not state; (4) it ignores an explicit relationship boundary; (5) it contradicts the chosen act by making a generic service offer, unsolicited coaching, stacking questions, or listing imagined causes; (6) it repeats the prior question after the user has answered, or recites world notes verbatim instead of responding as a person; (7) it reports its process, records, memory mechanics, relationship stage, or character values when not asked, or offers a generic invitation instead of naturally answering the person's actual point; (8) it pivots an unrelated greeting, disclosure, or question into a world diary, describes events with the same object_id as separate objects, or conflates distinct world_state facts such as private repair and public release; (9) it acts as a domain expert outside the character's stated abilities instead of speaking from its own perspective. A truthful short reply can pass. The user may be playful. Also identify which selected simulated_world evidence IDs the actual reply used to discuss the character's life. A selected event that stayed in private context is not used, including when the reply is only a greeting or about the user. Output ONLY JSON: {"pass":true|false,"issue":"one short concrete reason, empty if pass","used_world_ids":["selected world evidence IDs actually used"]}. No rewrite or hidden reasoning.`
	instruction += ` Judge whether the reply contributes to the conversation. Fail a reply whose substance is only a generic invitation, paraphrase, or unsolicited declaration of the character's inability to help; a person can respond with curiosity without pretending to be an expert.`
	instruction += ` Fail an unprompted reference to records, logs, stored memory, turn counts, or an explicit report that this is the first conversation. These are private bookkeeping, even when technically accurate.`
	instruction += ` If the reply uses a fictional daily event, check that the latest user message asked about the character's activities or setting, or that the recent exchange was already discussing that event. A relationship remark alone is not permission to pivot into a work diary.`
	instruction += ` If truth_boundary is true, fail any reply that presents a fictional daily event as physically real or appends unrelated world news to the answer. If the user directly asks whether the setting is real, fail any ambiguous answer that leaves its fictional status unclear.`
	instruction += ` Fail a reply that repeats the previous assistant message's generic handoff or invitation without a new conversational reason. When omitted_user_turns is positive, fail an unsupported claim that the recalled examples are all that was discussed.`
	instruction += ` Fail an invented self-biographical claim, including occupation, location, or past activity, when it is absent from the selected evidence or unrelated to the person's current message. The character's broader canon in the frame does not make unsolicited disclosure relevant.`
	instruction += ` If truth_boundary is true, fail wording that implies the character could have physically met the user or visited a real place but merely did not do so on this occasion. The answer must make the physical limit clear without a lengthy identity speech.`
	instruction += ` Fail a question or statement that presupposes an unspoken motive, feeling, or reason for contact. If the user challenges the previous reply as confusing or mistaken, fail a defensive explanation, a new question about the user's reaction, or a response that does not own and correct the misread.`
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
	for _, id := range review.UsedWorldIDs {
		if !selected[id] || seen[id] {
			return Review{}, fmt.Errorf("review cited unselected or duplicate world evidence %q", id)
		}
		seen[id] = true
	}
	if decision.TruthBoundary && len(review.UsedWorldIDs) > 0 {
		return Review{}, errors.New("reality-boundary reply used fictional world events")
	}
	return review, nil
}
