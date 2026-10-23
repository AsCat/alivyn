package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/AsCat/alivyn/internal/config"
)

func TestContactCommandsAndOneShotRuntime(t *testing.T) {
	t.Setenv("ALIVYN_HOME", t.TempDir())
	c := config.Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "custom", BaseURL: "http://localhost:1234/v1", Model: "mock", Timezone: "Asia/Shanghai"}
	if err := config.SaveNew(c); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var out bytes.Buffer
	if err := runContact(ctx, []string{"policy", "23:00", "07:00", "1"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := runContact(ctx, []string{"pause"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runContact(ctx, []string{"status"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "paused=true") || !strings.Contains(out.String(), "quiet=23:00-07:00") || !strings.Contains(out.String(), "timezone=Asia/Shanghai") {
		t.Fatalf("status did not reflect saved policy: %s", out.String())
	}
	if err := runServe(true, &out); err != nil {
		t.Fatal(err)
	}
	if err := runServe(true, &out); err != nil {
		t.Fatalf("one-shot runtime lock was not released: %v", err)
	}
	if err := runContact(ctx, []string{"resume"}, &out); err != nil {
		t.Fatal(err)
	}
}
