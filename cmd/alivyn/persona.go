package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/persona"
)

func runPersona(args []string, out io.Writer) error {
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
		fmt.Fprintf(out, "Persona pack set to %s. Restart a running channel to apply it. Identity and memories are retained.\n", args[2])
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
	return errors.New("usage: alivyn persona show|set natural|romantic|companion|pack list|pack show|pack set natural|vivid|background show|background set <text>|routine show|routine set <text>")
}

func displayPersonaBackground(c config.Config) string {
	if c.EffectiveEngine() == "character_v2" && strings.TrimSpace(c.Background) == "" {
		return character.DefaultPersona(c.Language)
	}
	return c.EffectiveBackground()
}
