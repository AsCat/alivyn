package display

import (
	"strings"
	"testing"
)

func TestCleanRemovesTerminalControls(t *testing.T) {
	got := Clean("hello\x1b[2J\rworld\nnext")
	if strings.ContainsAny(got, "\x1b\r") || !strings.Contains(got, "world\nnext") {
		t.Fatalf("unsafe display text: %q", got)
	}
}
