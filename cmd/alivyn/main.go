package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	tea "charm.land/bubbletea/v2"
	"github.com/AsCat/alivyn/internal/replay"
	"github.com/AsCat/alivyn/internal/store"
)

const version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "alivyn:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return runTUI()
	}
	if args[0] != "model" && args[0] != "replay" && args[0] != "memory" && args[0] != "contact" && args[0] != "serve" && args[0] != "demo" && len(args) != 1 {
		return fmt.Errorf("unexpected arguments for %q", args[0])
	}

	switch args[0] {
	case "version":
		fmt.Printf("Alivyn %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	case "doctor":
		if err := store.Probe(context.Background()); err != nil {
			return fmt.Errorf("SQLite probe failed: %w", err)
		}
		fmt.Println("SQLite: available")
		fmt.Println("Background contact runtime: available with 'alivyn serve' (local test outbox)")
	case "init":
		return initialize(os.Stdin, os.Stdout)
	case "chat":
		return chat(context.Background(), os.Stdin, os.Stdout)
	case "tui":
		return runTUI()
	case "help":
		usage()
	case "history":
		return showHistory(context.Background(), os.Stdout)
	case "memory":
		return runMemory(context.Background(), args[1:], os.Stdout)
	case "contact":
		return runContact(context.Background(), args[1:], os.Stdout)
	case "serve":
		if len(args) > 2 || (len(args) == 2 && args[1] != "once") {
			return fmt.Errorf("usage: alivyn serve [once]")
		}
		return runServe(len(args) == 2, os.Stdout)
	case "model":
		if len(args) == 2 && args[1] == "check" {
			return checkModel(context.Background(), os.Stdout)
		}
		if len(args) == 3 && args[1] == "key" && args[2] == "import" {
			return importModelKey(os.Stdin, os.Stdout)
		}
		return fmt.Errorf("usage: alivyn model check|key import")
	case "replay":
		if len(args) != 2 {
			return fmt.Errorf("usage: alivyn replay <scenario.json>")
		}
		fixture, err := replay.Load(args[1])
		if err != nil {
			return err
		}
		decisions, err := replay.Verify(fixture)
		if err != nil {
			return err
		}
		for _, decision := range decisions {
			fmt.Printf("%s deliveries=%d topic=%s reason=%s\n", decision.At, decision.DeliveryCount, decision.Topic, decision.Reason)
		}
	case "demo":
		if len(args) == 1 {
			return runDemo(context.Background(), os.Stdout, "zh-CN")
		}
		if len(args) == 2 && (args[1] == "en" || args[1] == "zh-CN") {
			return runDemo(context.Background(), os.Stdout, args[1])
		}
		if len(args) == 2 && args[1] == "tui" {
			if _, err := tea.NewProgram(demoModel{}).Run(); err != nil {
				return fmt.Errorf("terminal demo failed: %w", err)
			}
			return nil
		}
		return fmt.Errorf("usage: alivyn demo [en|zh-CN|tui]")
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}

func usage() {
	fmt.Println("Alivyn · 意续 — persistent conversation preview")
	fmt.Println("Usage: alivyn [tui|init|chat|history|memory ...|contact status|contact pause|contact resume|contact policy <start> <end> <daily-limit>|contact intents|contact audit|contact outbox|serve [once]|model check|model key import|replay|doctor|demo [en|zh-CN|tui]|version|help]")
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
	view := tea.NewView("Alivyn · 意续\n\nTerminal rendering is working.\nPersistent conversation is available with 'alivyn chat'.\n\nPress q to quit.")
	view.AltScreen = true
	return view
}
