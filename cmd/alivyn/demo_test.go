package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDemoShowsContactLifecycle(t *testing.T) {
	var out bytes.Buffer
	if err := runDemo(context.Background(), &out, "zh-CN"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"旧提醒取消", "周三：保持安静", "意续主动说", "开机后意续说", "没有重复打招呼", "本地测试投递箱"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("demo output missing %q: %s", want, out.String())
		}
	}
}

func TestEnglishDemoShowsContactLifecycle(t *testing.T) {
	var out bytes.Buffer
	if err := runDemo(context.Background(), &out, "en"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"old follow-up canceled", "Wednesday: stayed quiet", "Alivyn reaches out", "After waking", "no repeated greeting", "local test outbox"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("English demo output missing %q: %s", want, out.String())
		}
	}
}
