package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/AsCat/alivyn/pkg/extension"
)

func runInspect(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: alivyn inspect timeline|person|graph|demo")
	}
	if args[0] == "demo" {
		return inspectDemo(ctx, out)
	}
	if args[0] != "timeline" && args[0] != "person" && args[0] != "graph" {
		return errors.New("usage: alivyn inspect timeline|person|graph|demo")
	}
	c, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	if args[0] == "person" {
		return printPerson(ctx, h, c, out)
	}
	events, err := recentExperience(ctx, h, 40)
	if err != nil {
		return err
	}
	if args[0] == "graph" {
		printExperienceGraph(out, events, c.Language)
	} else {
		printExperienceTimeline(out, events, c.Language)
	}
	return nil
}

func recentExperience(ctx context.Context, h *store.History, count int) ([]extension.Event, error) {
	seq, err := h.EventSequence(ctx)
	if err != nil {
		return nil, err
	}
	start := seq - 200
	if start < 0 {
		start = 0
	}
	events, err := h.ReadEvents(ctx, extension.EventQuery{After: start, Limit: 200})
	if err != nil {
		return nil, err
	}
	if len(events) > count {
		events = events[len(events)-count:]
	}
	return events, nil
}

func eventSummary(e extension.Event, language string) string {
	var payload map[string]any
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return "[invalid payload]"
	}
	field := func(key string) string {
		value, _ := payload[key].(string)
		return value
	}
	switch e.Kind {
	case "chat.message":
		return field("role") + ": " + field("text")
	case "life.reading":
		return field("title") + " · " + field("thought")
	case "contact.decision":
		return field("topic") + " · " + field("outcome") + " · " + field("reason")
	default:
		if language == "zh-CN" {
			return field("zh")
		}
		return field("en")
	}
}

func shortText(s string, limit int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

func printExperienceTimeline(out io.Writer, events []extension.Event, language string) {
	if len(events) == 0 {
		fmt.Fprintln(out, "No visible experience events yet / 暂无可显示的经历事件")
		return
	}
	for _, event := range events {
		stamp := event.At.Local().Format("2006-01-02 15:04")
		if event.Domain == "simulated_world" && event.Kind != "world.scene" {
			stamp = event.At.Format("2006-01-02")
		}
		witness := ""
		if event.Domain == "simulated_world" && !contains(event.ObserverIDs, "default") {
			witness = " [人物未见]"
		}
		parent := ""
		if len(event.ParentIDs) > 0 {
			parent = " ← " + event.ParentIDs[0]
		}
		fmt.Fprintf(out, "%s #%d %s [%s]%s%s  %s\n", stamp, event.Sequence, event.ID, event.Domain, witness, parent, shortText(eventSummary(event, language), 110))
	}
}

func contains(items []string, item string) bool {
	for _, candidate := range items {
		if candidate == item {
			return true
		}
	}
	return false
}

func printExperienceGraph(out io.Writer, events []extension.Event, language string) {
	fmt.Fprintln(out, "```mermaid")
	fmt.Fprintln(out, "flowchart LR")
	fmt.Fprintln(out, "  person[\"意续 / Alivyn\"]")
	known := map[string]string{}
	for _, event := range events {
		id := fmt.Sprintf("e%d", event.Sequence)
		known[event.ID] = id
		label := shortText(eventSummary(event, language), 38)
		label = strings.ReplaceAll(label, "\"", "'")
		label = strings.ReplaceAll(label, "[", "(")
		label = strings.ReplaceAll(label, "]", ")")
		fmt.Fprintf(out, "  %s[\"%s · %s\"]\n", id, event.ID, label)
		if contains(event.ObserverIDs, "default") {
			fmt.Fprintf(out, "  %s -->|经历| person\n", id)
		}
	}
	for _, event := range events {
		for _, parent := range event.ParentIDs {
			if source, ok := known[parent]; ok {
				fmt.Fprintf(out, "  %s -->|因果| %s\n", source, known[event.ID])
			}
		}
	}
	fmt.Fprintln(out, "```")
}

func printPerson(ctx context.Context, h *store.History, c config.Config, out io.Writer) error {
	entries, err := h.Recent(ctx, 1)
	if err != nil {
		return err
	}
	var sourceID int64 = 1
	if len(entries) > 0 {
		sourceID = entries[0].ID + 1
	}
	state, at, err := h.CharacterStateBefore(ctx, sourceID, time.Now())
	if err != nil {
		return err
	}
	relation, err := h.Relationship(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s · %s\n关系阶段: %s\n", c.Name, c.EffectiveEngine(), relation.Stage)
	if at.IsZero() {
		fmt.Fprintln(out, "尚无已提交的人物内在状态。")
	} else {
		fmt.Fprintf(out, "最近状态更新: %s\n心情: %s\n注意: %s\n精力: %s\n意向: %s\n", at.Local().Format("2006-01-02 15:04"), valueOrDash(state.Moment.Feeling), valueOrDash(state.Moment.Attention), valueOrDash(state.Moment.Energy), valueOrDash(state.Moment.Intention))
	}
	for _, thread := range state.OpenThreads {
		fmt.Fprintf(out, "未了话题: %s (来源 %s)\n", thread.Subject, strings.Join(thread.SourceIDs, ", "))
	}
	episodes, err := h.CharacterEpisodesBefore(ctx, sourceID)
	if err != nil {
		return err
	}
	for i, episode := range episodes {
		if i >= 3 {
			break
		}
		fmt.Fprintf(out, "经历: %s → %s (来源 %s)\n", episode.Event, episode.Effect, strings.Join(episode.SourceIDs, ", "))
	}
	if c.EffectiveWorldPack() == "tidehaven" {
		visible, err := h.VisibleSimulatedScenes(ctx, "tidehaven", "default", time.Now(), 3)
		if err != nil {
			return err
		}
		for _, event := range visible {
			fmt.Fprintf(out, "最近所见世界事件: %s %s\n", event.ID, eventSummary(event, c.Language))
		}
	}
	return nil
}

func valueOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

type inspectSimulator struct{}

func (inspectSimulator) Advance(_ context.Context, req extension.SimulationRequest) ([]extension.EventProposal, error) {
	var prior []extension.Event
	if err := json.Unmarshal(req.World, &prior); err != nil {
		return nil, err
	}
	var existing []extension.Event
	for _, event := range prior {
		if event.Kind == "world.scene" {
			existing = append(existing, event)
		}
	}
	var id, subject, zh, en, place string
	var observers, parents []string
	switch len(existing) {
	case 0:
		id, subject, place = "discovery", "linchen", "archive"
		zh, en = "林澄在隔壁档案间发现一盒录音的日期有误。", "Lin Chen found a wrong date on a recording in the next room."
		observers = []string{"linchen"}
	case 1:
		id, subject, place = "reported", "default", "archive"
		zh, en = "林澄把录音日期的疑点告诉了意续，她决定先核对原始登记。", "Lin Chen told Alivyn about the date discrepancy; she decided to check the original ledger."
		observers = []string{"default", "linchen"}
		parents = []string{existing[0].ID}
	default:
		return nil, nil
	}
	payload, _ := json.Marshal(store.Scene{Place: place, Zh: zh, En: en})
	return []extension.EventProposal{{ProposalID: id, Kind: "world.scene", SubjectID: subject, At: req.AsOf, ObserverIDs: observers, ParentIDs: parents, Payload: payload, Schema: "alivyn.world.scene/v1"}}, nil
}

func inspectDemo(ctx context.Context, out io.Writer) error {
	dir, err := os.MkdirTemp("", "alivyn-inspect-demo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := store.OpenHistory(ctx, dir)
	if err != nil {
		return err
	}
	defer h.Close()
	r := extension.NewRegistry()
	if err := r.Register(extension.Registration{Manifest: extension.Manifest{ID: "alivyn.demo-world", Version: "1", APIVersion: extension.APIVersion, Capabilities: []extension.Capability{extension.Simulate}}, Simulator: inspectSimulator{}}); err != nil {
		return err
	}
	if err := r.Use("world.tidehaven", extension.Simulate, "alivyn.demo-world"); err != nil {
		return err
	}
	s := session.Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", WorldPack: "tidehaven"}, History: h, Extensions: r}
	first := time.Date(2026, 10, 29, 10, 0, 0, 0, time.Local)
	for _, at := range []time.Time{first, first.Add(time.Hour)} {
		if _, err := s.AdvanceWorldModule(ctx, "tidehaven", at); err != nil {
			return err
		}
	}
	if err := h.AddUserMessage(ctx, "你今天有什么新发现？", first.Add(2*time.Hour), time.Local); err != nil {
		return err
	}
	events, err := recentExperience(ctx, h, 20)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "隔离演示：无需模型密钥，不修改真实人物数据。")
	printExperienceTimeline(out, events, "zh-CN")
	visible, err := h.VisibleSimulatedScenes(ctx, "tidehaven", "default", first.Add(2*time.Hour), 10)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "人物实际可见的模拟事件: %d 条\n", len(visible))
	for _, event := range visible {
		fmt.Fprintf(out, "  %s %s\n", event.ID, eventSummary(event, "zh-CN"))
	}
	fmt.Fprintln(out, "事件图（复制到支持 Mermaid 的 Markdown 即可查看）：")
	printExperienceGraph(out, events, "zh-CN")
	fmt.Fprintln(out, "可以用 `alivyn inspect graph` 查看真实数据的事件图。")
	return nil
}
