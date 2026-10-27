package main

import (
	"context"
	"hash/fnv"
	"time"
)

// The delay is measured from message arrival, so a slow model call does not
// add more waiting. A stable per-event variation avoids metronomic timing and
// keeps a retried delivery from receiving a new target.
func replyReadyAt(receivedAt time.Time, eventID, reply string) time.Time {
	h := fnv.New32a()
	_, _ = h.Write([]byte(eventID))
	length := len([]rune(reply))
	if length <= 6 {
		return receivedAt.Add(time.Duration(150+h.Sum32()%350) * time.Millisecond)
	}
	if length > 80 {
		length = 80
	}
	delay := 1100*time.Millisecond + time.Duration(length)*35*time.Millisecond + time.Duration(h.Sum32()%1600)*time.Millisecond
	return receivedAt.Add(delay)
}

func waitForReplyPace(ctx context.Context, receivedAt time.Time, eventID, reply string) error {
	remaining := time.Until(replyReadyAt(receivedAt, eventID, reply))
	if remaining <= 0 {
		return nil
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
