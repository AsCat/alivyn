package display

import (
	"reflect"
	"testing"

	"github.com/AsCat/alivyn/internal/model"
)

func TestComposeChatVariesShortTextWithoutAddingWords(t *testing.T) {
	recent := []model.Message{
		{Role: "assistant", Content: "刚看见。"},
		{Role: "user", Content: "怎么说"},
		{Role: "assistant", Content: "这个我知道。"},
		{Role: "user", Content: "然后呢"},
	}
	got := ComposeChat("你先说。其实我也有点好奇。", recent, "zh-CN")
	if want := "你先说。\n\n其实我也有点好奇"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if parts := Bubbles(got); !reflect.DeepEqual(parts, []string{"你先说。", "其实我也有点好奇"}) {
		t.Fatalf("wrong delivery beats: %+v", parts)
	}
	// A subsequent single sentence remains one bubble; presentation follows
	// the authored thought instead of a target message count.
	recent = append(recent, model.Message{Role: "assistant", Content: got})
	if next := ComposeChat("我知道。", recent, "zh-CN"); next != "我知道" {
		t.Fatalf("unexpected repeated split: %q", next)
	}
}

func TestComposeChatPreservesQuestionsAndStructuredContent(t *testing.T) {
	recent := []model.Message{{Role: "assistant", Content: "好。"}, {Role: "assistant", Content: "嗯。"}}
	for _, in := range []string{"你想说什么？", "第一项：保存。", "他说“好。”我笑了。", "一、看这个。\n二、看那个。"} {
		if got := ComposeChat(in, recent, "zh-CN"); got != in {
			t.Fatalf("changed structured or nonperiod text: %q -> %q", in, got)
		}
	}
	if got := ComposeChat("这个我知道。", recent, "en"); got != "这个我知道。" {
		t.Fatalf("changed another language: %q", got)
	}
}
