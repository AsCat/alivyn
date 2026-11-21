package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/AsCat/alivyn/internal/life"
	"github.com/AsCat/alivyn/internal/session"
)

func lifeActivity(kind string) string {
	if kind == life.PopularVideo {
		return "看到热门视频条目（未观看视频）"
	}
	return "读到公开摘要"
}

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
			fmt.Fprintf(out, "意续%s：%s\n来源：%s\n她留下的想法：%s\n主动联系仍受联系策略约束。\n", lifeActivity(r.Kind), r.Title, r.URL, r.Thought)
			return nil
		}
	}
	readings, err := h.LifeReadingsBefore(ctx, time.Now(), 1)
	if err != nil {
		return err
	}
	if len(readings) == 0 {
		fmt.Fprintln(out, "目前还没有公开资讯记录。需要 character_v2 人物引擎；运行 `alivyn life explore` 可立即尝试一次。")
		return nil
	}
	r := readings[0]
	fmt.Fprintf(out, "最近%s：%s（%s）\n来源：%s\n她留下的想法：%s\n", lifeActivity(r.Kind), r.Title, r.Date, r.URL, r.Thought)
	return nil
}
