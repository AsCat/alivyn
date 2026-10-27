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
		lower, upper := time.Second, 6*time.Second
		if len([]rune(reply)) <= 6 {
			lower, upper = 150*time.Millisecond, 500*time.Millisecond
		}
		if ready.Before(received.Add(lower)) || ready.After(received.Add(upper)) || !ready.Equal(replyReadyAt(received, "event-123", reply)) {
			t.Fatalf("invalid pacing target for %q: %v", reply, ready)
		}
		if err := waitForReplyPace(context.Background(), received.Add(-time.Minute), "event-123", reply); err != nil {
			t.Fatal(err)
		}
	}
}
