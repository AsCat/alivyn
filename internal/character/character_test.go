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

func TestImportedGuestPromptReplacesAlivynBasePrompt(t *testing.T) {
	c := Context{Persona: "你是「大肥鱼」，住在 QQ 里的蓝色小鲸鱼娘。", DirectPersona: true,
		Frame: actor.Frame{Name: "大肥鱼", Language: "zh-CN", Recent: []model.Message{{Role: "user", Content: "嗨"}}}}
	messages, err := Messages(c)
	if err != nil {
		t.Fatal(err)
	}
	system := messages[0].Content
	if !strings.HasPrefix(system, c.Persona) || !strings.Contains(system, "actual transport is Feishu") {
		t.Fatal("imported persona or transport boundary missing")
	}
	if strings.Contains(system, "你在私聊里扮演一个持续生活的具体人物") {
		t.Fatal("Alivyn's default prompt was mixed into the guest persona")
	}
	for _, message := range messages[1:] {
		if strings.Contains(message.Content, "这一轮默认只说一个很短的意思") {
			t.Fatal("Alivyn's short-reply meter suppressed the guest persona")
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

func TestEpisodeRequiresObservedConversationEvidence(t *testing.T) {
	frame := actor.Frame{Evidence: []actor.Evidence{{ID: "chat:7", Provenance: "observed_chat"}, {ID: "worldv2:1", Provenance: "simulated_world"}}}
	raw := `{"mode":"say","reply":"嗯。","episode":{"event":"对方解释了刚才的玩笑","effect":"我不再绷着","interpretation":"也许只是想逗我","source_ids":["chat:7"]}}`
	if _, err := Parse(raw, frame); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"chat:999", "worldv2:1"} {
		if _, err := Parse(strings.Replace(raw, "chat:7", ref, 1), frame); err == nil {
			t.Fatal("unsupported episode source accepted", ref)
		}
	}
}
