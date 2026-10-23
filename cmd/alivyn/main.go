package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"alivyn/internal/store"
	tea "charm.land/bubbletea/v2"
)

const version = "0.0.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "alivyn:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		usage()
		return nil
	}

	switch args[0] {
	case "version":
		fmt.Printf("Alivyn %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	case "doctor":
		if err := store.Probe(context.Background()); err != nil {
			return fmt.Errorf("SQLite probe failed: %w", err)
		}
		fmt.Println("SQLite: available")
		fmt.Println("Agent runtime: not implemented yet")
	case "demo":
		if _, err := tea.NewProgram(demoModel{}).Run(); err != nil {
			return fmt.Errorf("terminal demo failed: %w", err)
		}
	default:
		usage()
	}
	return nil
}

func usage() {
	fmt.Println("Alivyn · 意续 — foundation prototype")
	fmt.Println("Usage: alivyn <version|doctor|demo>")
}

type demoModel struct{}

func (demoModel) Init() tea.Cmd { return nil }

func (m demoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (demoModel) View() tea.View {
	view := tea.NewView("Alivyn · 意续\n\nFoundation prototype: terminal rendering is working.\nThe persistent AI individual is the next milestone.\n\nPress q to quit.")
	view.AltScreen = true
	return view
}
