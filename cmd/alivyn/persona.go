package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/persona"
)

func runPersona(args []string, out io.Writer) error {
	if len(args) >= 2 && args[0] == "tune" && args[1] == "initiative" {
		if len(args) > 3 {
			return errors.New("usage: alivyn persona tune initiative [0..100]")
		}
		_, h, err := openConfiguredHistory(context.Background())
		if err != nil {
			return err
		}
		defer h.Close()
		if len(args) == 3 {
			level, err := strconv.Atoi(args[2])
			if err != nil {
				return errors.New("initiative must be between 0 and 100")
			}
			if err := h.SetInitiative(context.Background(), level); err != nil {
				return err
			}
		}
		tuning, err := h.CharacterTuning(context.Background())
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "initiative=%d (0=off, 25=default, 100=high; contact daily limit and quiet hours still apply)\n", tuning.Initiative)
		return nil
	}
	if len(args) == 2 && args[0] == "pack" && args[1] == "list" {
		c, err := config.Load()
		if err != nil {
			return err
		}
		for _, p := range persona.All() {
			marker := " "
			if p.ID == c.EffectivePersonaPack() {
				marker = "*"
			}
			description := p.Description
			if c.Language == "zh-CN" {
				description = p.DescriptionZH
			}
			fmt.Fprintf(out, "%s %s — %s / %s\n", marker, p.ID, p.NameZH, description)
		}
		return nil
	}
	if len(args) == 2 && args[0] == "pack" && args[1] == "show" {
		c, err := config.Load()
		if err != nil {
			return err
		}
		p, _ := persona.Get(c.EffectivePersonaPack())
		description := p.Description
		if c.Language == "zh-CN" {
			description = p.DescriptionZH
		}
		fmt.Fprintf(out, "%s — %s / %s\n%s\n", p.ID, p.NameZH, p.NameEN, description)
		return nil
	}
	if len(args) == 3 && args[0] == "pack" && args[1] == "set" {
		if err := config.SavePersonaPack(args[2]); err != nil {
			return err
		}
		if selected, ok := persona.Get(args[2]); ok && selected.Standalone {
			fmt.Fprintf(out, "Guest persona set to %s. Use an isolated ALIVYN_HOME and restart the channel.\n", args[2])
		} else {
			fmt.Fprintf(out, "Persona pack set to %s. Restart a running channel to apply it. Identity and memories are retained.\n", args[2])
		}
		return nil
	}
	if len(args) == 2 && args[0] == "routine" && args[1] == "show" {
		c, err := config.Load()
		if err != nil {
			return err
		}
		routine := c.Routine
		if strings.TrimSpace(routine) == "" {
			routine = character.DefaultRoutine(c.Language)
		}
		fmt.Fprintln(out, routine)
		return nil
	}
	if len(args) >= 3 && args[0] == "routine" && args[1] == "set" {
		if err := config.SaveRoutine(strings.Join(args[2:], " ")); err != nil {
			return err
		}
		fmt.Fprintln(out, "Routine saved. Restart a running channel to apply it.")
		return nil
	}
	if len(args) == 1 && args[0] == "show" {
		c, err := config.Load()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: %s (pack: %s)\nBackground: %s\n", c.Name, c.EffectiveStyle(), c.EffectivePersonaPack(), displayPersonaBackground(c))
		return nil
	}
	if len(args) == 2 && args[0] == "background" && args[1] == "show" {
		c, err := config.Load()
		if err != nil {
			return err
		}
		fmt.Fprintln(out, displayPersonaBackground(c))
		return nil
	}
	if len(args) >= 3 && args[0] == "background" && args[1] == "set" {
		if err := config.SaveBackground(strings.Join(args[2:], " ")); err != nil {
			return err
		}
		fmt.Fprintln(out, "Persona background saved. Restart a running channel to apply it.")
		return nil
	}
	if len(args) == 2 && args[0] == "set" {
		if err := config.SaveStyle(args[1]); err != nil {
			return err
		}
		fmt.Fprintf(out, "Persona style set to %s. Restart a running channel to apply it.\n", args[1])
		return nil
	}
	return errors.New("usage: alivyn persona show|set natural|romantic|companion|pack list|pack show|pack set natural|vivid|dafeiyu-astrbot|background show|background set <text>|routine show|routine set <text>|tune initiative [0..100]")
}

func displayPersonaBackground(c config.Config) string {
	if selected, ok := persona.Get(c.EffectivePersonaPack()); ok && selected.Standalone {
		return "Imported guest persona: " + selected.NameZH + " (see internal/persona/third_party/dafeiyu-astrbot-v2.1.md)"
	}
	if c.EffectiveEngine() == "character_v2" && strings.TrimSpace(c.Background) == "" {
		return character.DefaultPersona(c.Language)
	}
	return c.EffectiveBackground()
}
