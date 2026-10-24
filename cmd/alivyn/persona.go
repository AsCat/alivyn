package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/AsCat/alivyn/internal/config"
)

func runPersona(args []string, out io.Writer) error {
	if len(args) == 1 && args[0] == "show" {
		c, err := config.Load()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: %s\n", c.Name, c.EffectiveStyle())
		return nil
	}
	if len(args) == 2 && args[0] == "set" {
		if err := config.SaveStyle(args[1]); err != nil {
			return err
		}
		fmt.Fprintf(out, "Persona style set to %s. Restart a running channel to apply it.\n", args[1])
		return nil
	}
	return errors.New("usage: alivyn persona show|set natural|romantic|companion")
}
