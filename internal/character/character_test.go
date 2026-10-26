package character

import (
	"strings"
	"testing"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/model"
)

func TestMessagesPreserveVoiceAndActualDialogue(t *testing.T) {
	c := Context{Persona: "custom character", Frame: actor.Frame{Name: "意续", Language: "zh-CN", Canon: "fictional city", Recent: []model.Message{
		{Role: "user", Content: "那件事后来呢"}, {Role: "assistant", Content: "还没弄完"}, {Role: "user", Content: "……"},
	}}, Moment: Moment{Feeling: "有点期待", Scene: "在听同一首旧歌"}}
	messages, err := Messages(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 6 || messages[1].Role != "user" || messages[2].Role != "assistant" || messages[3].Content != "……" || messages[4].Role != "system" || messages[5].Role != "system" {
		t.Fatalf("actual role order was lost: %+v", messages)
	}
	for _, value := range []string{"custom character", "fictional city", "有点期待", "在听同一首旧歌"} {
		if !strings.Contains(messages[0].Content, value) {
			t.Fatalf("missing character context %q", value)
		}
	}
}

func TestTurnAllowsAuthoredSceneButRejectsInventedWorldReferences(t *testing.T) {
	f := actor.Frame{Canon: "fictional city", Evidence: []actor.Evidence{{ID: "worldv2:3", Provenance: "simulated_world"}}}
	raw := `{"mode":"say","reply":"在等水开。","moment":{"feeling":"放松","attention":"晚饭","scene":"烧水煮面"},"used_world_ids":[]}`
	turn, err := Parse(raw, f)
	if err != nil || turn.Moment.Scene != "烧水煮面" {
		t.Fatalf("authored fictional scene rejected: %+v %v", turn, err)
	}
	bad := strings.Replace(raw, `"used_world_ids":[]`, `"used_world_ids":["worldv2:999"]`, 1)
	if _, err := Parse(bad, f); err == nil {
		t.Fatal("invented world event accepted")
	}
	if _, err := Parse(strings.Replace(raw, `"mode":"say"`, `"mode":"wait"`, 1), f); err == nil {
		t.Fatal("wait with spoken reply accepted")
	}
	if _, err := Parse(`plain text`, f); err == nil {
		t.Fatal("unstructured reply accepted")
	}
}

func TestOpenThreadRequiresRealChatSource(t *testing.T) {
	frame := actor.Frame{Evidence: []actor.Evidence{{ID: "chat:7", Provenance: "observed_chat"}, {ID: "worldv2:1", Provenance: "simulated_world"}}}
	raw := `{"mode":"say","reply":"明天再聊。","moment":{"intention":"想休息"},"open_threads":[{"subject":"用户提到有件事还没说","owner":"user","source_ids":["chat:7"]}]}`
	turn, err := Parse(raw, frame)
	if err != nil || len(turn.OpenThreads) != 1 || turn.Moment.Intention != "想休息" {
		t.Fatal(turn, err)
	}
	for _, ref := range []string{"chat:999", "worldv2:1"} {
		if _, err := Parse(strings.Replace(raw, "chat:7", ref, 1), frame); err == nil {
			t.Fatal("unsupported thread source accepted", ref)
		}
	}
}
