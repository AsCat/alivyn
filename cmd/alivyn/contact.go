package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/contact"
	"github.com/AsCat/alivyn/internal/display"
)

func runContact(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: alivyn contact status|pause|resume|policy <start> <end> <daily-limit>|intents|audit|outbox")
	}
	c, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	zone, err := c.Location()
	if err != nil {
		return err
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return errors.New("usage: alivyn contact status")
		}
		p, err := h.ContactPolicy(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "paused=%t quiet=%s-%s daily_limit=%d timezone=%s transport=local-outbox\n",
			p.Paused, p.QuietStart, p.QuietEnd, p.DailyLimit, zone)
	case "pause", "resume":
		if len(args) != 1 {
			return errors.New("usage: alivyn contact pause|resume")
		}
		paused := args[0] == "pause"
		if err := h.SetPaused(ctx, paused); err != nil {
			return err
		}
		fmt.Fprintf(out, "paused=%t\n", paused)
	case "policy":
		if len(args) != 4 {
			return errors.New("usage: alivyn contact policy <quiet-start HH:MM> <quiet-end HH:MM> <daily-limit>")
		}
		limit, err := strconv.Atoi(args[3])
		if err != nil {
			return err
		}
		previous, err := h.ContactPolicy(ctx)
		if err != nil {
			return err
		}
		p := contact.Policy{QuietStart: args[1], QuietEnd: args[2], DailyLimit: limit, Paused: previous.Paused}
		if err := h.SetContactPolicy(ctx, p); err != nil {
			return err
		}
		fmt.Fprintf(out, "quiet=%s-%s daily_limit=%d paused=%t\n", p.QuietStart, p.QuietEnd, p.DailyLimit, p.Paused)
	case "intents":
		if len(args) != 1 {
			return errors.New("usage: alivyn contact intents")
		}
		items, err := h.ContactIntents(ctx, 100)
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Fprintf(out, "#%d %s %s due=%s expires=%s reason=%s\n", item.ID, item.Topic, item.State,
				item.Due.In(zone).Format(time.RFC3339), item.Expires.In(zone).Format(time.RFC3339), item.Reason)
		}
	case "audit":
		if len(args) != 1 {
			return errors.New("usage: alivyn contact audit")
		}
		events, err := h.ContactEvents(ctx, 100)
		if err != nil {
			return err
		}
		for _, event := range events {
			fmt.Fprintf(out, "%s intent=#%d %s reason=%s\n", event.At.In(zone).Format(time.RFC3339),
				event.IntentID, event.Outcome, event.Reason)
		}
	case "outbox":
		if len(args) != 1 {
			return errors.New("usage: alivyn contact outbox")
		}
		messages, err := h.Outbox(ctx, 100)
		if err != nil {
			return err
		}
		for _, message := range messages {
			fmt.Fprintf(out, "%s intent=#%d topic=%s key=%s body=%s\n", message.SentAt.In(zone).Format(time.RFC3339),
				message.IntentID, message.Topic, message.IdempotencyKey, display.Clean(message.Body))
		}
	default:
		return errors.New("usage: alivyn contact status|pause|resume|policy <start> <end> <daily-limit>|intents|audit|outbox")
	}
	return nil
}

func runServe(once bool, out io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	dir, err := config.Directory()
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "runtime.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("contact runtime is already running")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	zone, err := c.Location()
	if err != nil {
		return err
	}
	tick := func() error {
		now := time.Now()
		if err := h.ResumeRuntime(ctx, now); err != nil {
			return err
		}
		events, err := h.ProcessDue(ctx, now, zone, c.Language)
		if err != nil {
			return err
		}
		for _, event := range events {
			fmt.Fprintf(out, "%s intent=#%d %s reason=%s\n", event.At.In(zone).Format(time.RFC3339),
				event.IntentID, event.Outcome, event.Reason)
		}
		return nil
	}
	if err := tick(); err != nil {
		return err
	}
	if once {
		return nil
	}
	fmt.Fprintln(out, "Alivyn contact runtime is running; deliveries go to the local test outbox.")
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := tick(); err != nil {
				return err
			}
		}
	}
}
