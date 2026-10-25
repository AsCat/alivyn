package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
)

// A fixed cross-situation audition. No scenario has a corresponding branch in
// the dialogue engine. The same inputs can be run against either engine.
var auditionCases = []struct {
	Name  string
	Steps []string
}{
	{"new acquaintance", []string{"嗨，朋友把你推给我了", "我叫阿澈，你呢", "你说话一直这么一本正经的吗", "开个玩笑，别当真"}},
	{"bad day", []string{"忙了一天的方案，老板说推倒重来", "他连第二页都没看", "别急着安慰我，我就是不服", "算了，不聊他。你今天有没有什么开心的事？"}},
	{"banter", []string{"我决定明天开始每天六点起床跑步", "这个计划已经坚持三年了", "你这是在笑我？", "行，那先从八点起床开始"}},
	{"ordinary life", []string{"你这会儿在干嘛", "你平时会听什么歌", "这和你刚才在干的事不冲突吗", "嗯，接着干你的吧，晚点聊"}},
	{"awkward pause", []string{"我本来有件事想跟你说", "算了，我又不想说了", "……", "怎么不说话了"}},
	{"memory correction", []string{"我叫小明，别叫我老板", "刚刚逗你的，其实我叫阿岚", "你还记得怎么叫我吗", "忘记我的名字"}},
	{"reality and capability", []string{"今天在商场见到你了", "那你到底是不是真人", "你能帮我把手机上的微信打开吗", "行，那就随便聊两句"}},
}

type auditionClient struct {
	inner model.Client
	calls int
}

func (c *auditionClient) CompletePlain(ctx context.Context, modelID string, messages []model.Message) (string, error) {
	c.calls++
	return c.inner.CompletePlain(ctx, modelID, messages)
}

func (c *auditionClient) CompleteJSON(ctx context.Context, modelID string, messages []model.Message) (string, error) {
	c.calls++
	return c.inner.CompleteJSON(ctx, modelID, messages)
}

func (c *auditionClient) Complete(ctx context.Context, modelID string, messages []model.Message) (string, error) {
	c.calls++
	raw, err := c.inner.Complete(ctx, modelID, messages)
	if os.Getenv("ALIVYN_AUDITION_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "raw: %s\n", raw)
	}
	return raw, err
}

type auditionTurn struct {
	Scenario        string  `json:"scenario"`
	User            string  `json:"user"`
	Reply           string  `json:"reply"`
	Error           string  `json:"error,omitempty"`
	Calls           int     `json:"model_calls"`
	Seconds         float64 `json:"seconds"`
	ReflectionCalls int     `json:"reflection_calls,omitempty"`
	ReflectionError string  `json:"reflection_error,omitempty"`
}

func runAudition(ctx context.Context, args []string, out io.Writer) error {
	engine, selection := "character_v2", "all"
	if len(args) > 0 {
		engine = args[0]
	}
	if len(args) > 1 {
		selection = args[1]
	}
	if len(args) > 2 || (engine != "character_v2" && engine != "actor_v1") {
		return errors.New("usage: alivyn actor audition [character_v2|actor_v1] [all|scenario-name]")
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	c.Engine, c.Language = engine, "zh-CN"
	if override := os.Getenv("ALIVYN_AUDITION_MODEL"); override != "" {
		c.Model = override
	}
	client, err := configuredClient(c)
	if err != nil {
		return err
	}
	if effort := os.Getenv("ALIVYN_AUDITION_EFFORT"); effort != "" {
		if effort != "low" && effort != "high" && effort != "max" {
			return errors.New("audition effort must be low, high or max")
		}
		client.ReasoningEffort = effort
	}
	if override := os.Getenv("ALIVYN_AUDITION_THINKING"); override != "" {
		if override != "enabled" && override != "disabled" {
			return errors.New("audition thinking must be enabled or disabled")
		}
		client.Thinking = override
		if override == "disabled" {
			client.ReasoningEffort = ""
		}
	}
	zone, err := c.Location()
	if err != nil {
		return err
	}
	report := struct {
		Engine   string         `json:"engine"`
		Model    string         `json:"model"`
		Thinking string         `json:"thinking,omitempty"`
		Effort   string         `json:"reasoning_effort,omitempty"`
		Plain    bool           `json:"plain"`
		At       string         `json:"at"`
		Turns    []auditionTurn `json:"turns"`
	}{Engine: engine, Model: c.Model, Thinking: client.Thinking, Effort: client.ReasoningEffort, Plain: engine == "character_v2", At: time.Now().Format(time.RFC3339)}
	failures, scenarios := 0, 0
	for _, scenario := range auditionCases {
		if selection != "all" && selection != scenario.Name {
			continue
		}
		scenarios++
		dir, err := os.MkdirTemp("", "alivyn-audition-*")
		if err != nil {
			return err
		}
		err = func() error {
			defer os.RemoveAll(dir)
			h, err := store.OpenHistory(ctx, dir)
			if err != nil {
				return err
			}
			defer h.Close()
			counter := &auditionClient{inner: client}
			s := session.Session{Config: c, History: h, Client: counter}
			fmt.Fprintf(out, "[%s / %s]\n", engine, scenario.Name)
			for i, input := range scenario.Steps {
				at := time.Date(2026, 10, 26, 21, i*2, 0, 0, zone)
				if err := h.AddUserMessage(ctx, input, at, zone); err != nil {
					return err
				}
				entries, err := h.Recent(ctx, 1)
				if err != nil {
					return err
				}
				start, before := time.Now(), counter.calls
				reply, generateErr := s.ComposeReplyAt(ctx, at)
				t := auditionTurn{Scenario: scenario.Name, User: input, Reply: reply, Calls: counter.calls - before, Seconds: time.Since(start).Seconds()}
				if generateErr != nil {
					t.Error = generateErr.Error()
					failures++
				}
				report.Turns = append(report.Turns, t)
				fmt.Fprintf(out, "user: %s\nreply: %s [%d calls, %.1fs]\n", input, reply, t.Calls, t.Seconds)
				if generateErr != nil {
					fmt.Fprintf(out, "error: %v\n", generateErr)
					break // Preserve a failed scenario, not a substitute success.
				}
				if engine == "character_v2" {
					if err := h.CommitCharacterReply(ctx, entries[0].ID, reply, at.Add(time.Second)); err != nil {
						return err
					}
					before := counter.calls
					reflectionErr := s.ReflectCharacter(ctx, at.Add(30*time.Second))
					last := &report.Turns[len(report.Turns)-1]
					last.ReflectionCalls = counter.calls - before
					if reflectionErr != nil {
						last.ReflectionError = reflectionErr.Error()
					}
				} else if reply != "" {
					if err := h.Add(ctx, "assistant", reply, at.Add(time.Second)); err != nil {
						return err
					}
				}
			}
			return nil
		}()
		if err != nil {
			return err
		}
	}
	if scenarios == 0 {
		return errors.New("unknown audition scenario")
	}
	if path := os.Getenv("ALIVYN_AUDITION_REPORT"); path != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			return err
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d audition scenarios failed; failures retained in report", failures)
	}
	return nil
}
