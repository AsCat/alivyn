package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/AsCat/alivyn/internal/session"
)

func runLife(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 1 || args[0] != "explore" && args[0] != "status" {
		return errors.New("usage: alivyn life explore|status")
	}
	c, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	if args[0] == "explore" {
		client, err := configuredClient(c)
		if err != nil {
			return err
		}
		s := session.Session{Config: c, History: h, Client: client}
		r, err := s.ExploreLife(ctx, time.Now(), true)
		if err != nil {
			return err
		}
		if r.ID != 0 {
			fmt.Fprintf(out, "意续读到：%s\n来源：%s\n她留下的想法：%s\n主动联系仍受勿扰和每日上限约束。\n", r.Title, r.URL, r.Thought)
			return nil
		}
	}
	readings, err := h.LifeReadingsBefore(ctx, time.Now(), 1)
	if err != nil {
		return err
	}
	if len(readings) == 0 {
		fmt.Fprintln(out, "目前还没有阅读记录。需要 character_v2 人物引擎；运行 `alivyn life explore` 可立即尝试一次。")
		return nil
	}
	r := readings[0]
	fmt.Fprintf(out, "最近阅读：%s（%s）\n来源：%s\n她留下的想法：%s\n", r.Title, r.Date, r.URL, r.Thought)
	return nil
}
