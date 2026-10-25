package character

import (
	"github.com/AsCat/alivyn/internal/actor"
	"testing"
)

func TestInitiativeRequiresRealReferencesAndAllowsSilence(t *testing.T) {
	refs := []actor.Evidence{{ID: "chat:7", Provenance: "observed_chat"}}
	for _, raw := range []string{
		`{"action":"contact","kind":"follow_up","reason":"a detail","evidence_ids":["chat:999"],"after_minutes":5}`,
		`{"action":"contact","kind":"share","reason":"a detail","evidence_ids":["chat:7"],"after_minutes":0}`,
		`{"action":"contact","kind":"share","reason":"a detail","evidence_ids":[],"after_minutes":5}`,
		` `,
	} {
		if _, err := ParseInitiative(raw, refs); err == nil {
			t.Fatalf("invalid proposal accepted: %s", raw)
		}
	}
	if p, err := ParseInitiative(`{"action":"wait","reason":"the user wants to focus"}`, refs); err != nil || p.Action != "wait" || p.AfterMinutes != 0 {
		t.Fatal(p, err)
	}
}
