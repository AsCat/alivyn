package display

import (
	"strings"
	"unicode/utf8"

	"github.com/AsCat/alivyn/internal/model"
)

// ComposeChat realizes an already authored turn for a casual text channel.
// It may change bubble boundaries and an optional final full stop, but never
// adds words or invents another conversational move. The returned text is the
// text to persist, so future turns see what was actually shown to the user.
func ComposeChat(reply string, recent []model.Message, language string) string {
	if language != "zh-CN" || strings.TrimSpace(reply) == "" {
		return reply
	}
	parts := Bubbles(reply)
	if len(parts) == 1 {
		parts = splitShortTurn(parts[0])
	}
	if len(parts) > 3 {
		return reply
	}
	// A short conversational fragment need not be printed like a sentence in
	// an essay. Change only a terminal full stop, and only after the recent
	// transcript has become uniformly punctuated.
	if recentFullStops(recent) >= 2 {
		last := len(parts) - 1
		if shortFragment(parts[last]) {
			parts[last] = strings.TrimSuffix(parts[last], "。")
		}
	}
	return strings.Join(parts, "\n\n")
}

func splitShortTurn(reply string) []string {
	if strings.ContainsAny(reply, "\n“”\"「」") || utf8.RuneCountInString(reply) > 60 {
		return []string{reply}
	}
	// Only a clear sentence boundary can become a second bubble. Commas,
	// ellipses, quotations and structured text keep their authored layout.
	for _, mark := range []string{"。", "！", "？"} {
		pos := strings.Index(reply, mark)
		if pos < 0 || pos+len(mark) >= len(reply) {
			continue
		}
		first := strings.TrimSpace(reply[:pos+len(mark)])
		second := strings.TrimSpace(reply[pos+len(mark):])
		if utf8.RuneCountInString(first) < 4 || utf8.RuneCountInString(second) < 4 || utf8.RuneCountInString(first) > 35 || utf8.RuneCountInString(second) > 35 {
			continue
		}
		if strings.ContainsAny(second, "。！？\n") && utf8.RuneCountInString(reply) > 45 {
			continue
		}
		return []string{first, second}
	}
	return []string{reply}
}

func shortFragment(text string) bool {
	if !strings.HasSuffix(text, "。") || utf8.RuneCountInString(text) > 24 || strings.ContainsAny(strings.TrimSuffix(text, "。"), "\n\r。！？!?：:") {
		return false
	}
	return true
}

func recentFullStops(recent []model.Message) int {
	count, seen := 0, 0
	for i := len(recent) - 1; i >= 0 && seen < 3; i-- {
		if recent[i].Role == "system" && strings.Contains(recent[i].Content, "Conversation time boundary:") {
			break
		}
		if recent[i].Role != "assistant" {
			continue
		}
		seen++
		if strings.HasSuffix(strings.TrimSpace(recent[i].Content), "。") {
			count++
		}
	}
	return count
}
