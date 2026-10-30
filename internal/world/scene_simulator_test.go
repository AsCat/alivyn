package world

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/pkg/extension"
)

type sceneModelStub struct {
	reply string
	calls int
	input string
}

func (s *sceneModelStub) CompleteJSON(_ context.Context, _ string, messages []model.Message) (string, error) {
	s.calls++
	s.input = messages[1].Content
	if len(messages) != 2 {
		panic("world simulator received unexpected model context")
	}
	return s.reply, nil
}

func TestSceneSimulatorDoesNotOfferAuthoredHistoryAsCausalParent(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	at := time.Date(2026, 10, 30, 16, 0, 0, 0, zone)
	payload := json.RawMessage(`{"zh":"旧录音的索引还没整理完。","en":"The old recordings are still being indexed."}`)
	events, _ := json.Marshal([]extension.Event{{ID: "authored:1", Kind: "world.event", Domain: "simulated_world", WorldID: "tidehaven", At: at.AddDate(0, 0, -1), Payload: payload}})
	stub := &sceneModelStub{reply: `{"action":"record","place":"archive","zh":"她重新核对了那页索引。","en":"She checked that index page again.","actor_present":true,"parent_id":"authored:1"}`}
	proposals, err := (SceneSimulator{Client: stub, Model: "mock", Zone: zone}).Advance(context.Background(), extension.SimulationRequest{SubjectID: "default", WorldID: "tidehaven", AsOf: at, World: events})
	if err != nil || len(proposals) != 1 || len(proposals[0].ParentIDs) != 0 {
		t.Fatalf("valid scene lost or cited an unavailable parent: %+v %v", proposals, err)
	}
	if strings.Contains(stub.input, `"id":"authored:1"`) {
		t.Fatalf("authored event was offered as a scene parent: %s", stub.input)
	}
}

func TestSceneSimulatorUsesOneDailyModelCallAndNoChat(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	at := time.Date(2026, 10, 29, 16, 0, 0, 0, zone)
	modelStub := &sceneModelStub{reply: `{"action":"record","place":"archive","zh":"她发现一段录音的标签写错了。","en":"She found a wrong label on a recording.","actor_present":true}`}
	s := SceneSimulator{Client: modelStub, Model: "mock", Zone: zone}
	request := extension.SimulationRequest{SubjectID: "default", WorldID: "tidehaven", AsOf: at}
	proposals, err := s.Advance(context.Background(), request)
	if err != nil || len(proposals) != 1 || proposals[0].ProposalID != "2026-10-29" || modelStub.calls != 1 {
		t.Fatal(proposals, err, modelStub.calls)
	}
	worldEvents, _ := json.Marshal([]extension.Event{{Kind: "world.scene", Domain: "simulated_world", WorldID: "tidehaven", At: at, Payload: proposals[0].Payload}})
	request.World = worldEvents
	proposals, err = s.Advance(context.Background(), request)
	if err != nil || len(proposals) != 0 || modelStub.calls != 1 {
		t.Fatal(proposals, err, modelStub.calls)
	}
}
