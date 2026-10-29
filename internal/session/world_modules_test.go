package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/AsCat/alivyn/pkg/extension"
)

type sceneSimulator struct{}

func (sceneSimulator) Advance(_ context.Context, q extension.SimulationRequest) ([]extension.EventProposal, error) {
	scene, _ := json.Marshal(store.Scene{Place: "archive", Zh: "她在档案室听见一段旧录音。", En: "She heard an old recording in the archive."})
	return []extension.EventProposal{{ProposalID: "archive-one", Kind: "world.scene", SubjectID: q.SubjectID, At: q.AsOf, ObserverIDs: []string{"default"}, Payload: scene, Schema: "alivyn.world.scene/v1"}}, nil
}

func TestSelectedSimulatorEntersOnlyWitnessedConversationEvidence(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	r := extension.NewRegistry()
	if err := r.Register(extension.Registration{Manifest: extension.Manifest{ID: "test.world", Version: "1", APIVersion: extension.APIVersion, Capabilities: []extension.Capability{extension.Simulate}}, Simulator: sceneSimulator{}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Use("world.tidehaven", extension.Simulate, "test.world"); err != nil {
		t.Fatal(err)
	}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", WorldPack: "tidehaven"}, History: h, Extensions: r}
	at := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	ids, err := s.AdvanceWorldModule(ctx, "tidehaven", at)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	unseenPayload, _ := json.Marshal(store.Scene{Place: "reading_room", Zh: "隔壁屋里有人发现了一张纸条。", En: "Someone found a note in the next room."})
	unseenID, err := h.CommitWorldProposal(ctx, "tidehaven", "test:unseen", extension.EventProposal{Kind: "world.scene", SubjectID: "linchen", At: at.Add(-time.Minute), ObserverIDs: []string{"linchen"}, Payload: unseenPayload, Schema: "alivyn.world.scene/v1"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "今天有发生什么吗？", at.Add(time.Minute), time.UTC); err != nil {
		t.Fatal(err)
	}
	futureID, err := h.CommitWorldProposal(ctx, "tidehaven", "test:future", extension.EventProposal{Kind: "world.scene", SubjectID: "default", At: at.Add(3 * time.Minute), ObserverIDs: []string{"default"}, Payload: unseenPayload, Schema: "alivyn.world.scene/v1"}, at.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := h.Recent(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.characterContext(ctx, at.Add(2*time.Minute), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, evidence := range c.Frame.Evidence {
		found = found || evidence.ID == ids[0] && strings.Contains(evidence.Text, "旧录音")
		if evidence.ID == unseenID || evidence.ID == futureID {
			t.Fatalf("unseen or future scene entered earlier dialogue: %+v", evidence)
		}
	}
	if !found {
		t.Fatalf("witnessed scene did not reach character context: %+v", c.Frame.Evidence)
	}
}

func TestWorldSceneContactUsesItsExactWitnessedSource(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	at := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	for _, line := range []string{"最近你在修什么？", "那批录音的日期查过吗？"} {
		if err := h.AddUserMessage(ctx, line, at.Add(-time.Hour), time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	payload, _ := json.Marshal(store.Scene{Place: "archive", Zh: "她发现一份日期记录有误。", En: "She found an incorrect date record."})
	id, err := h.CommitWorldProposal(ctx, "tidehaven", "test:sourced-contact", extension.EventProposal{Kind: "world.scene", SubjectID: "default", At: at, ObserverIDs: []string{"default"}, Payload: payload, Schema: "alivyn.world.scene/v1"}, at)
	if err != nil {
		t.Fatal(err)
	}
	client := &characterScript{replies: []string{`{"mode":"say","reply":"那份日期记录果然有点问题。","absence":null}`}}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", WorldPack: "tidehaven", Model: "mock"}, History: h, Client: client}
	reply, err := s.ComposeContactForAt(ctx, "world_scene", id, at.Add(15*time.Minute))
	if err != nil || !strings.Contains(reply, "日期记录") || len(client.calls) != 1 {
		t.Fatal(reply, err, len(client.calls))
	}
	if !strings.Contains(client.calls[0][0].Content, "她发现一份日期记录有误") {
		t.Fatal("exact scene was not included in contact context")
	}
}
