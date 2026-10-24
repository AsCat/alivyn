package main

import (
	"context"
	"testing"
	"time"
)

func TestReplyPacingIsBoundedAndDoesNotAddDelayAfterSlowGeneration(t *testing.T) {
	received := time.Now()
	for _, reply := range []string{"你好。", "这个问题我想多说几句。"} {
		ready := replyReadyAt(received, "event-123", reply)
		if ready.Before(received.Add(time.Second)) || ready.After(received.Add(6*time.Second)) || !ready.Equal(replyReadyAt(received, "event-123", reply)) {
			t.Fatalf("invalid pacing target for %q: %v", reply, ready)
		}
		if err := waitForReplyPace(context.Background(), received.Add(-time.Minute), "event-123", reply); err != nil {
			t.Fatal(err)
		}
	}
}
