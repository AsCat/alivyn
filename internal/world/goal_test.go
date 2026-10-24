package world

import (
	"strings"
	"testing"
	"time"
)

func TestGoalActionsPreserveOneObjectAndRequirePrerequisites(t *testing.T) {
	state := GoalState{}
	if _, _, err := ApplyGoalAction(state, "private_preview", "2026-10-24", 0); err == nil {
		t.Fatal("private preview was allowed before a batch existed")
	}
	var events []GoalEvent
	for day := 0; day < 7; day++ {
		date := time.Date(2026, 10, 24+day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		action := SelectGoalAction(state, 0)
		next, event, err := ApplyGoalAction(state, action, date, 0)
		if err != nil || ValidateGoalTransition(state, next, event, 0) != nil {
			t.Fatalf("day %d failed: state=%+v event=%+v err=%v", day+1, next, event, err)
		}
		if day > 0 && day < 7 {
			if event.ObjectID != events[0].ObjectID || event.ParentID != int64(day) {
				t.Fatalf("causal object changed within batch: %+v", event)
			}
		}
		if action == "repair" && !strings.Contains(event.Zh, "那盒没有标签的录音里") {
			t.Fatal("repair no longer identifies the same recording box")
		}
		events = append(events, event)
		state = next
		state.LastEventID = int64(day + 1)
	}
	if !state.Closed || state.Batch != 1 || state.IdentityVerified || state.PublicationAuthorized {
		t.Fatalf("first batch did not close with unresolved permission: %+v", state)
	}
	if SelectGoalAction(state, 0) != "intake" {
		t.Fatal("next project did not require a new intake")
	}
	if _, _, err := ApplyGoalAction(state, "publish", "2026-10-31", 0); err == nil {
		t.Fatal("unauthorized public release was accepted")
	}
}

func TestGoalChoiceRespondsToConversationWithoutCopyingIt(t *testing.T) {
	state, _, err := ApplyGoalAction(GoalState{}, "intake", "2026-10-24", 0)
	if err != nil {
		t.Fatal(err)
	}
	state.LastEventID = 1
	if SelectGoalAction(state, 0) != "repair" || SelectGoalAction(state, 1) != "inspect" {
		t.Fatal("goal action did not branch on new conversation")
	}
	next, event, err := ApplyGoalAction(state, "inspect", "2026-10-25", 1)
	if err != nil || !event.RelatedToUser || !next.LedgerChecked || strings.Contains(event.Zh, "用户") {
		t.Fatalf("conversation influenced world incorrectly: next=%+v event=%+v err=%v", next, event, err)
	}
}

func TestForgottenEngagementDoesNotBlockWorld(t *testing.T) {
	previous := GoalState{Day: 1, Date: "2026-10-24", Batch: 1, LastEventID: 1, LastWorldEngagements: 1}
	next, event, err := ApplyGoalAction(previous, "repair", "2026-10-25", 0)
	if err != nil || event.RelatedToUser || next.LastWorldEngagements != 0 {
		t.Fatalf("forgotten engagement blocked world progression: next=%+v event=%+v err=%v", next, event, err)
	}
}
