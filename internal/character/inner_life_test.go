package character

import (
	"testing"

	"github.com/AsCat/alivyn/internal/actor"
)

func TestInnerStateRequiresSourcedFocusAndMotives(t *testing.T) {
	refs := []actor.Evidence{{ID: "chat:7", Provenance: "observed_chat"}, {ID: "self:character", Provenance: "authored_character"}}
	valid := `{"focus":"connect","reason":"想再聊一会儿","evidence_ids":["chat:7"],"impulses":[{"kind":"reconnect","want":"想问候","subject":"之前聊的旧歌","evidence_ids":["chat:7"]}],"revisit_in_minutes":120}`
	state, err := ParseInnerState(valid, refs)
	if err != nil || state.Focus != "connect" || len(state.Impulses) != 1 {
		t.Fatalf("valid inner state rejected: %+v %v", state, err)
	}
	for _, raw := range []string{
		`{"focus":"connect","reason":"想聊","evidence_ids":["chat:7"],"impulses":[],"revisit_in_minutes":120}`,
		`{"focus":"own_life","reason":"想整理录音","evidence_ids":["world:missing"],"impulses":[],"revisit_in_minutes":120}`,
		`{"focus":"rest","reason":"想安静","evidence_ids":[],"impulses":[],"revisit_in_minutes":5}`,
	} {
		if _, err := ParseInnerState(raw, refs); err == nil {
			t.Fatalf("invalid inner state accepted: %s", raw)
		}
	}
}
