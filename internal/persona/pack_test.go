package persona

import (
	"strings"
	"testing"
)

func TestImportedAstrBotPersonaIsOriginalPromptBody(t *testing.T) {
	p, ok := Get("dafeiyu-astrbot")
	if !ok || !p.Standalone {
		t.Fatal("imported character must be a separate guest persona")
	}
	got := Text(p.ID, "zh-CN")
	if !strings.HasPrefix(got, "你是「大肥鱼」") || !strings.Contains(got, "【分寸与情绪】") {
		t.Fatal("upstream persona body was not embedded")
	}
	if strings.Contains(got, "以下 `---` 之后的正文") {
		t.Fatal("upstream import instructions entered the model prompt")
	}
}
