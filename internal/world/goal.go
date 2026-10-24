package world

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

// GoalState is the durable projection of one bounded fictional project. The
// project can branch on actual conversation, but never turns private user words
// into an event in the character's world.
type GoalState struct {
	Day                   int    `json:"day"`
	Date                  string `json:"date"`
	Batch                 int    `json:"batch"`
	LedgerChecked         bool   `json:"ledger_checked"`
	IdentityVerified      bool   `json:"identity_verified"`
	AudioRecovered        bool   `json:"audio_recovered"`
	PublicationDiscussed  bool   `json:"publication_discussed"`
	Reflected             bool   `json:"reflected"`
	PermissionChecked     bool   `json:"permission_checked"`
	PublicationAuthorized bool   `json:"publication_authorized"`
	PrivateCopyReady      bool   `json:"private_copy_ready"`
	Closed                bool   `json:"closed"`
	LastWorldEngagements  int    `json:"last_world_engagements"`
	LastEventID           int64  `json:"last_event_id"`
}

type GoalEvent struct {
	ID            int64  `json:"id"`
	Day           int    `json:"day"`
	Date          string `json:"date"`
	Action        string `json:"action"`
	ObjectID      string `json:"object_id"`
	ParentID      int64  `json:"parent_id,omitempty"`
	Provenance    string `json:"provenance"`
	Zh            string `json:"zh"`
	En            string `json:"en"`
	RelatedToUser bool   `json:"related_to_user"`
	Catchup       bool   `json:"catchup"`
}

const GoalZh = "修复并核对同一批邻里口述录音；身份或授权未核实前只做内部留存，无法确认的地方明确留白。"
const GoalEn = "Restore and check one batch of neighborhood oral histories; keep it private while identity or permission is unresolved, and mark unknowns clearly."

// SelectGoalAction chooses a valid action from the current goal state. It does
// not depend on a seven-day script or the last line of dialogue.
func SelectGoalAction(s GoalState, engagements int) string {
	if s.Batch == 0 || s.Closed {
		return "intake"
	}
	if !s.LedgerChecked && !s.AudioRecovered {
		if engagements > s.LastWorldEngagements {
			return "inspect"
		}
		return "repair"
	}
	if !s.LedgerChecked {
		return "inspect"
	}
	if !s.AudioRecovered {
		return "repair"
	}
	if !s.PublicationDiscussed {
		return "discuss"
	}
	if engagements > s.LastWorldEngagements && !s.Reflected {
		return "reflect"
	}
	if !s.PermissionChecked {
		return "search_permission"
	}
	if !s.PrivateCopyReady {
		return "private_preview"
	}
	return "catalog"
}

// ApplyGoalAction checks prerequisites and returns the next state and event.
// It is also the validation boundary for a future model-suggested action.
func ApplyGoalAction(previous GoalState, action, date string, engagements int) (GoalState, GoalEvent, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil || (previous.Date != "" && date <= previous.Date) {
		return GoalState{}, GoalEvent{}, errors.New("world date must advance")
	}
	// Forgetting can remove a derived engagement. The world continues without
	// treating that deletion as a new encounter.
	if action != SelectGoalAction(previous, engagements) {
		return GoalState{}, GoalEvent{}, fmt.Errorf("world action %q has unmet prerequisites", action)
	}
	next := previous
	next.Day++
	next.Date = date
	related := previous.Batch > 0 && engagements > previous.LastWorldEngagements
	next.LastWorldEngagements = engagements
	event := GoalEvent{Day: next.Day, Date: date, Action: action, Provenance: "simulated_goal_v1", RelatedToUser: related}
	if action == "intake" {
		event.RelatedToUser = false
		next.Batch++
		next.LastWorldEngagements = 0
		next.LedgerChecked = false
		next.IdentityVerified = false
		next.AudioRecovered = false
		next.PublicationDiscussed = false
		next.Reflected = false
		next.PermissionChecked = false
		next.PublicationAuthorized = false
		next.PrivateCopyReady = false
		next.Closed = false
		next.LastEventID = 0
	} else {
		event.ParentID = previous.LastEventID
		if event.ParentID < 1 {
			return GoalState{}, GoalEvent{}, errors.New("world action has no causal parent")
		}
	}
	event.ObjectID = fmt.Sprintf("recording-batch:%d", next.Batch)
	labelZh := "那盒没有标签的录音"
	labelEn := "the same unlabeled box of recordings"
	if next.Batch > 1 {
		labelZh = fmt.Sprintf("第 %d 批录音", next.Batch)
		labelEn = fmt.Sprintf("recording batch %d", next.Batch)
	}
	switch action {
	case "intake":
		event.Zh = fmt.Sprintf("在旧港档案室接手%s，决定先核清来源并修复，授权不明前不公开。", labelZh)
		event.En = fmt.Sprintf("At the Old Harbor archive, took in %s. The plan is to identify and restore it, keeping it private until permission is clear.", labelEn)
	case "inspect":
		next.LedgerChecked = true
		event.Zh = fmt.Sprintf("查了%s的登记簿，找到采访年份；受访者身份和授权仍待核实。", labelZh)
		event.En = fmt.Sprintf("Checked the ledger for %s and found the interview year; the speaker's identity and permission remain unverified.", labelEn)
	case "repair":
		next.AudioRecovered = true
		event.Zh = fmt.Sprintf("修复了%s里一段被杂音盖住的口述，听清其中提到阅览室的旧地址；没有替听不清的部分补词。", labelZh)
		event.En = fmt.Sprintf("Restored a noisy oral-history clip from %s and heard the reading room's old address; left the inaudible words unresolved.", labelEn)
	case "discuss":
		next.PublicationDiscussed = true
		event.Zh = fmt.Sprintf("林澄想尽快公开%s中的片段；你坚持先确认受访者同意，暂时搁置发布。", labelZh)
		event.En = fmt.Sprintf("Lin Chen wanted to publish a clip from %s; you insisted on consent first, so publication was postponed.", labelEn)
	case "reflect":
		next.Reflected = true
		event.Zh = fmt.Sprintf("和用户聊过后，重新列出%s待核查的问题；没有把用户的话写进档案，也没有把聊天当成授权。", labelZh)
		event.En = fmt.Sprintf("After a conversation, clarified open questions about %s without copying the user's words into the archive or treating the chat as permission.", labelEn)
	case "search_permission":
		next.PermissionChecked = true
		event.Zh = fmt.Sprintf("去阅览室核对%s的授权记录，仍没找到可以公开的凭据。", labelZh)
		event.En = fmt.Sprintf("Checked the reading room's permission records for %s; found no authorization to publish.", labelEn)
	case "private_preview":
		next.PrivateCopyReady = true
		event.Zh = fmt.Sprintf("和林澄做了%s的内部试听版，只留在档案室，不对外发布。", labelZh)
		event.En = fmt.Sprintf("Made a private listening copy of %s with Lin Chen; it stays in the archive and is not published.", labelEn)
	case "catalog":
		next.Closed = true
		event.Zh = fmt.Sprintf("把%s的修复结果与尚未核实的身份、授权问题一并登记；这一批先内部留存。", labelZh)
		event.En = fmt.Sprintf("Cataloged the repairs and unresolved identity and permission questions for %s; this batch remains private.", labelEn)
	}
	return next, event, nil
}

// ValidateGoalTransition protects persisted state from an invalid proposer.
func ValidateGoalTransition(previous, next GoalState, event GoalEvent, engagements int) error {
	wantState, wantEvent, err := ApplyGoalAction(previous, event.Action, event.Date, engagements)
	if err != nil {
		return err
	}
	// Database IDs are assigned after insertion; they are not model input.
	event.ID, event.Catchup = 0, false
	if !reflect.DeepEqual(next, wantState) || !reflect.DeepEqual(event, wantEvent) {
		return errors.New("world event and state do not match validated transition")
	}
	return nil
}
