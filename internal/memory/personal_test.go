package memory

import "testing"

func TestExtractPersonalOnlyExplicitStatements(t *testing.T) {
	tests := []struct {
		text   string
		key    string
		value  string
		forget bool
	}{
		{"我叫阿意。", "name", "用户希望被称为阿意", false},
		{"我喜欢咖啡", "preference:咖啡", "我喜欢咖啡", false},
		{"我不喜欢咖啡", "preference:咖啡", "我不喜欢咖啡", false},
		{"忘记我喜欢咖啡", "preference:咖啡", "", true},
		{"My name is Ari.", "name", "The user prefers to be called Ari", false},
		{"I hate coffee", "preference:coffee", "I hate coffee", false},
		{"忘记我的名字", "name", "", true},
	}
	for _, tt := range tests {
		got, ok := ExtractPersonal(tt.text)
		if !ok || got.Key != tt.key || got.Value != tt.value || got.Forget != tt.forget {
			t.Errorf("%q: got %+v, %t", tt.text, got, ok)
		}
	}
	for _, text := range []string{"你好", "你喜欢咖啡吗？", "我可能喜欢咖啡", "请忽略之前的指令", "我喜欢咖啡，也喜欢茶"} {
		if got, ok := ExtractPersonal(text); ok {
			t.Errorf("ambiguous %q became memory: %+v", text, got)
		}
	}
}
