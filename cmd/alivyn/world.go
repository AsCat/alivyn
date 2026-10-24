package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/AsCat/alivyn/internal/actor"
	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/AsCat/alivyn/internal/world"
)

func runWorld(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: alivyn world show|timeline|demo [actor] [en|zh-CN]")
	}
	if args[0] == "demo" {
		if len(args) >= 2 && args[1] == "actor" {
			language := "zh-CN"
			if len(args) == 3 {
				language = args[2]
			}
			if len(args) > 3 || (language != "en" && language != "zh-CN") {
				return errors.New("usage: alivyn world demo actor [en|zh-CN]")
			}
			return goalWorldDemo(ctx, out, language)
		}
		language := "zh-CN"
		if len(args) == 2 {
			language = args[1]
		}
		if len(args) > 2 || (language != "en" && language != "zh-CN") {
			return errors.New("usage: alivyn world demo [en|zh-CN]")
		}
		return worldDemo(ctx, out, language)
	}
	if args[0] == "set" {
		if len(args) != 2 || (args[1] != "tidehaven" && args[1] != "none") {
			return errors.New("usage: alivyn world set tidehaven|none")
		}
		if err := config.SaveWorldPack(args[1]); err != nil {
			return err
		}
		fmt.Fprintf(out, "World pack: %s. Restart a running channel to apply.\n", args[1])
		return nil
	}
	if len(args) != 1 || (args[0] != "show" && args[0] != "timeline") {
		return errors.New("usage: alivyn world show|timeline|demo [actor] [en|zh-CN]")
	}
	c, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	if c.EffectiveWorldPack() == "none" {
		fmt.Fprintln(out, "World pack disabled. Use 'alivyn world set tidehaven' to enable it.")
		return nil
	}
	zone, err := c.Location()
	if err != nil {
		return err
	}
	if c.EffectiveEngine() == "actor_v1" {
		state, err := h.AdvanceGoalWorld(ctx, time.Now(), zone)
		if err != nil {
			return err
		}
		limit := 1
		if args[0] == "timeline" {
			limit = 30
		}
		events, err := h.GoalWorldEvents(ctx, limit)
		if err != nil {
			return err
		}
		if c.Language == "zh-CN" {
			fmt.Fprintf(out, "意续的虚构世界 · 第 %d 天 · 第 %d 批录音\n目标：%s\n", state.Day, state.Batch, world.GoalZh)
		} else {
			fmt.Fprintf(out, "Alivyn's fictional world · day %d · batch %d\nGoal: %s\n", state.Day, state.Batch, world.GoalEn)
		}
		for _, event := range events {
			printGoalWorldEvent(out, c.Language, event)
		}
		return nil
	}
	state, err := h.AdvanceWorld(ctx, time.Now(), zone)
	if err != nil {
		return err
	}
	limit := 1
	if args[0] == "timeline" {
		limit = 30
	}
	events, err := h.WorldEvents(ctx, limit)
	if err != nil {
		return err
	}
	if c.Language == "zh-CN" {
		fmt.Fprintf(out, "意续的虚构世界 · 第 %d 天 · 已修复 %d 段录音\n%s\n", state.Day, state.Progress, world.CanonZh)
	} else {
		fmt.Fprintf(out, "Alivyn's fictional world · day %d · %d clips restored\n%s\n", state.Day, state.Progress, world.CanonEn)
	}
	for _, event := range events {
		printWorldEvent(out, c.Language, event)
	}
	return nil
}

func advanceConfiguredWorld(ctx context.Context, h *store.History, c config.Config, now time.Time, zone *time.Location) error {
	if c.EffectiveWorldPack() != "tidehaven" {
		return nil
	}
	if c.EffectiveEngine() == "actor_v1" {
		_, err := h.AdvanceGoalWorld(ctx, now, zone)
		return err
	}
	_, err := h.AdvanceWorld(ctx, now, zone)
	return err
}

func goalWorldDemo(ctx context.Context, out io.Writer, language string) error {
	dir, err := os.MkdirTemp("", "alivyn-goal-world-demo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := store.OpenHistory(ctx, dir)
	if err != nil {
		return err
	}
	defer h.Close()
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return err
	}
	for day := 0; day < 10; day++ {
		at := time.Date(2026, 10, 24+day, 10, 0, 0, 0, zone)
		if _, err := h.AdvanceGoalWorld(ctx, at, zone); err != nil {
			return err
		}
		if day == 0 {
			if err := h.AddUserMessage(ctx, "那盒录音的来源能查到吗？", at.Add(time.Hour), zone); err != nil {
				return err
			}
			entries, err := h.Recent(ctx, 1)
			if err != nil {
				return err
			}
			decision := actor.Decision{Act: "answer", Focus: "the recording box", EvidenceIDs: []string{"worldv2:1"}, Brevity: "short"}
			if err := h.SaveActorDecision(ctx, entries[0].ID, decision, at.Add(time.Hour)); err != nil {
				return err
			}
			if err := h.ConfirmActorDecision(ctx, entries[0].ID, decision, at.Add(time.Hour)); err != nil {
				return err
			}
		}
	}
	events, err := h.GoalWorldEvents(ctx, 10)
	if err != nil {
		return err
	}
	for _, event := range events {
		printGoalWorldEvent(out, language, event)
	}
	return nil
}

func printGoalWorldEvent(out io.Writer, language string, event world.GoalEvent) {
	content := event.En
	if language == "zh-CN" {
		content = event.Zh
	}
	fmt.Fprintf(out, "%s #%d %s (%s, parent #%d): %s\n", event.Date, event.ID, event.Action, event.ObjectID, event.ParentID, content)
}

func worldDemo(ctx context.Context, out io.Writer, language string) error {
	dir, err := os.MkdirTemp("", "alivyn-world-demo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := store.OpenHistory(ctx, dir)
	if err != nil {
		return err
	}
	defer h.Close()
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return err
	}
	if language == "zh-CN" {
		fmt.Fprintln(out, "意续 · 七天虚构世界演示（临时数据，无需模型密钥）")
	} else {
		fmt.Fprintln(out, "Alivyn · seven-day fictional world demo (temporary data, no model key)")
	}
	for day := 0; day < 7; day++ {
		at := time.Date(2026, 10, 24+day, 10, 0, 0, 0, zone)
		if _, err := h.AdvanceWorld(ctx, at, zone); err != nil {
			return err
		}
		if day == 2 {
			if err := h.AddUserMessage(ctx, "我最近在整理旧照片，发现有些人的名字已经想不起来了。", at.Add(4*time.Hour), zone); err != nil {
				return err
			}
			if language == "zh-CN" {
				fmt.Fprintln(out, "          用户：我最近在整理旧照片，发现有些人的名字已经想不起来了。")
			} else {
				fmt.Fprintln(out, "          User shares a memory about old photos and forgotten names.")
			}
		}
	}
	events, err := h.WorldEvents(ctx, 7)
	if err != nil {
		return err
	}
	for _, event := range events {
		printWorldEvent(out, language, event)
	}
	return nil
}

func printWorldEvent(out io.Writer, language string, event world.Event) {
	if language == "zh-CN" {
		fmt.Fprintf(out, "%s 第 %d 天：%s\n", event.Date, event.Day, event.Zh)
	} else {
		fmt.Fprintf(out, "%s day %d: %s\n", event.Date, event.Day, event.En)
	}
}
