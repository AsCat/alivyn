package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestWorldDemoShowsConversationChangingNextDay(t *testing.T) {
	var out bytes.Buffer
	if err := worldDemo(context.Background(), &out, "zh-CN"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "第 7 天") || !strings.Contains(text, "和用户聊过后") || !strings.Contains(text, "临时数据") {
		t.Fatalf("incomplete seven-day demo: %s", text)
	}
}
