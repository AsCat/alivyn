package world

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/pkg/extension"
)

type sceneModelStub struct {
	reply string
	calls int
}

func (s *sceneModelStub) CompleteJSON(_ context.Context, _ string, messages []model.Message) (string, error) {
	s.calls++
	if len(messages) != 2 {
		panic("world simulator received unexpected model context")
	}
	return s.reply, nil
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
