package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/config"
)

func runPersona(args []string, out io.Writer) error {
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
		fmt.Fprintf(out, "%s: %s\nBackground: %s\n", c.Name, c.EffectiveStyle(), displayPersonaBackground(c))
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
	return errors.New("usage: alivyn persona show|set natural|romantic|companion|background show|background set <text>|routine show|routine set <text>")
}

func displayPersonaBackground(c config.Config) string {
	if c.EffectiveEngine() == "character_v2" && strings.TrimSpace(c.Background) == "" {
		return character.DefaultPersona(c.Language)
	}
	return c.EffectiveBackground()
}
