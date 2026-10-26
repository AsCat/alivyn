package display

import "strings"

// Bubbles treats a single blank line as a conversational beat. A reply that
// cannot be split cleanly stays one message, so formatting never loses text.
func Bubbles(reply string) []string {
	normalized := strings.ReplaceAll(reply, "\r\n", "\n")
	if strings.Contains(normalized, "\n\n\n") {
		return []string{reply}
	}
	parts := strings.Split(normalized, "\n\n")
	if len(parts) < 2 || len(parts) > 3 {
		return []string{reply}
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if parts[i] == "" {
			return []string{reply}
		}
	}
	return parts
}
