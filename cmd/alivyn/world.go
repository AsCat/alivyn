package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/AsCat/alivyn/internal/world"
)

func runWorld(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: alivyn world show|timeline|demo [en|zh-CN]")
	}
	if args[0] == "demo" {
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
		return errors.New("usage: alivyn world show|timeline|demo [en|zh-CN]")
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
