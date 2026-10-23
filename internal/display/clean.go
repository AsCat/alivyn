// Package display prepares untrusted text for terminal output.
package display

import (
	"strings"
	"unicode"
)

// Clean removes terminal control characters while preserving line breaks.
func Clean(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}
