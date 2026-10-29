package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestInspectDemoShowsGlobalAndPersonalViews(t *testing.T) {
	var out bytes.Buffer
	if err := inspectDemo(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{
		"[人物未见]", "worldsim:1", "worldsim:2", "人物实际可见的模拟事件: 1 条",
		"e1 -->|因果| e2", "e2 -->|经历| person",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("inspection demo omitted %q: %s", expected, text)
		}
	}
	if strings.Contains(text, "e1 -->|经历| person") {
		t.Fatal("unseen event was rendered as the character's experience")
	}
}
