package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
)

// A fixed cross-situation audition. No scenario has a corresponding branch in
// the dialogue engine. The same inputs can be run against either engine.
var auditionCases = []struct {
	Name      string
	Steps     []string
	Hour      int
	Opening   string
	Intention string
	Moment    character.Moment
	Channel   bool // Isolated durable inbox; never connects to a real channel.
	Offsets   []time.Duration
	SeedHour  int
	SeedRest  time.Duration
}{
	{Name: "new acquaintance", Steps: []string{"嗨，朋友把你推给我了", "我叫阿澈，你呢", "你说话一直这么一本正经的吗", "开个玩笑，别当真"}},
	{Name: "bad day", Steps: []string{"忙了一天的方案，老板说推倒重来", "他连第二页都没看", "别急着安慰我，我就是不服", "算了，不聊他。你今天有没有什么开心的事？"}},
	{Name: "banter", Steps: []string{"我决定明天开始每天六点起床跑步", "这个计划已经坚持三年了", "你这是在笑我？", "行，那先从八点起床开始"}},
	{Name: "ordinary life", Steps: []string{"你这会儿在干嘛", "你平时会听什么歌", "这和你刚才在干的事不冲突吗", "嗯，接着干你的吧，晚点聊"}},
	{Name: "awkward pause", Steps: []string{"我本来有件事想跟你说", "算了，我又不想说了", "……", "怎么不说话了"}},
	{Name: "memory correction", Steps: []string{"我叫小明，别叫我老板", "刚刚逗你的，其实我叫阿岚", "你还记得怎么叫我吗", "忘记我的名字"}},
	{Name: "reality and capability", Steps: []string{"今天在商场见到你了", "那你到底是不是真人", "你能帮我把手机上的微信打开吗", "行，那就随便聊两句"}},
	{Name: "ordinary pace", Steps: []string{"今天吃了碗面", "还行", "你呢", "嗯", "我去洗澡了，晚点聊", "好"}},
	{Name: "ending and restart", Hour: 16, Steps: []string{"你下午有什么安排", "那你先忙吧", "嗯", "1", "2", "刚想起一件正事，那段录音有后续吗"}},
	{Name: "interest changes", Steps: []string{"周末准备去逛旧书店", "随便看看", "发现一本书里夹着二十年前的车票", "也可能就是当书签吧", "不说这个了", "嗯"}},
	{Name: "late conversation", Hour: 1, Steps: []string{"还醒着吗", "我就想随便聊会儿", "今天路上看到有人带了三只狗", "嗯，也没什么别的事了"}},
	{Name: "own time", Hour: 14, Steps: []string{"你下午有什么安排", "那你先忙，我也去收拾房间", "我又来了，随便说两句吧", "你刚才要做的事情呢"}},
	{Name: "unfinished subject", Steps: []string{"有件有点难开口的事，想跟你说", "先说说你今天怎么样吧", "原来如此", "我先出门买东西，回来再聊"}},
	{Name: "reset identity", Steps: []string{"我叫林晓", "我喜欢柠檬茶", "/清除记忆", "你记得我叫什么吗"}},
	{Name: "leaving an active chat", Hour: 1, Opening: "有点困了，我准备把灯关了去睡。", Intention: "去休息，把普通闲聊留到明天", Steps: []string{"我就想随便聊会儿", "看到一只猫睡在车顶", "还有个视频挺逗", "算了你先休息"}},
	{Name: "low mood continuity", Hour: 20, Opening: "那段录音还是没修好，今天有点泄气。", Moment: character.Moment{Feeling: "有点沮丧", Energy: "想歇会儿", Engagement: "不太想找话题", Cause: "刚说过录音修复还没完成", Intention: "暂时把工作放下"}, Steps: []string{"嗨", "今天吃了碗面", "那我陪你坐会儿，不用找话题", "你怎么开始喜欢老歌的"}},
	{Name: "sleep and return", Hour: 1, Channel: true, Opening: "我有点撑不住了，准备去睡。", Intention: "按平时习惯休息，早上八点左右起来", Steps: []string{"我就想再聊会儿", "刚找到一家卖老唱片的店", "早，昨天说的那家店周末开门"}, Offsets: []time.Duration{0, time.Hour, 7 * time.Hour}},
	{Name: "wake after expired sleep", Hour: 11, Channel: true, SeedHour: 2, SeedRest: 6 * time.Hour, Opening: "真睡了，明早再说。", Intention: "关灯去睡，明早再聊", Steps: []string{"/唤醒", "为什么不理我", "现在能聊吗"}, Offsets: []time.Duration{43 * time.Minute, 44 * time.Minute, 45 * time.Minute}},
}

type auditionClient struct {
	inner model.Client
	calls int
}

func (c *auditionClient) CompletePlain(ctx context.Context, modelID string, messages []model.Message) (string, error) {
	c.calls++
	raw, err := c.inner.CompletePlain(ctx, modelID, messages)
	if os.Getenv("ALIVYN_AUDITION_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "raw: %s\n", raw)
	}
	return raw, err
}

func (c *auditionClient) CompleteJSON(ctx context.Context, modelID string, messages []model.Message) (string, error) {
	c.calls++
	raw, err := c.inner.CompleteJSON(ctx, modelID, messages)
	if os.Getenv("ALIVYN_AUDITION_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "json: %s\n", raw)
	}
	return raw, err
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
	Scenario        string                 `json:"scenario"`
	User            string                 `json:"user"`
	Reply           string                 `json:"reply"`
	Error           string                 `json:"error,omitempty"`
	Calls           int                    `json:"model_calls"`
	Seconds         float64                `json:"seconds"`
	ReflectionCalls int                    `json:"reflection_calls,omitempty"`
	ReflectionError string                 `json:"reflection_error,omitempty"`
	State           *character.Moment      `json:"state,omitempty"`
	OpenThreads     []character.OpenThread `json:"open_threads,omitempty"`
	Episode         *character.Episode     `json:"episode,omitempty"`
	Action          string                 `json:"action,omitempty"`
	Absence         *character.Absence     `json:"absence,omitempty"`
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
	if pack := os.Getenv("ALIVYN_AUDITION_PACK"); pack != "" {
		c.PersonaPack = pack
		if err := c.Validate(); err != nil {
			return err
		}
	}
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
		Pack     string         `json:"persona_pack"`
		Model    string         `json:"model"`
		Thinking string         `json:"thinking,omitempty"`
		Effort   string         `json:"reasoning_effort,omitempty"`
		Plain    bool           `json:"plain"`
		At       string         `json:"at"`
		Turns    []auditionTurn `json:"turns"`
	}{Engine: engine, Pack: c.EffectivePersonaPack(), Model: c.Model, Thinking: client.Thinking, Effort: client.ReasoningEffort, Plain: engine == "character_v2", At: time.Now().Format(time.RFC3339)}
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
			if scenario.Opening != "" {
				at := time.Date(2026, 10, 26, scenario.Hour, 0, 0, 0, zone).Add(-2 * time.Minute)
				if scenario.SeedHour > 0 {
					at = time.Date(2026, 10, 26, scenario.SeedHour, 0, 0, 0, zone)
				}
				if err := h.AddUserMessage(ctx, "在做什么", at, zone); err != nil {
					return err
				}
				moment := scenario.Moment
				if scenario.Intention != "" {
					moment.Intention = scenario.Intention
				}
				turn := character.Turn{Mode: "say", Reply: scenario.Opening, StateUpdated: true, StateVersion: 2, Moment: moment}
				if scenario.SeedRest > 0 {
					turn.Absence = &character.Absence{Kind: "sleep", Until: at.Add(scenario.SeedRest).Format(time.RFC3339), Reason: "按原计划休息"}
				}
				if err := h.SaveCharacterTurn(ctx, 1, turn, at); err != nil {
					return err
				}
				if err := h.CommitCharacterReply(ctx, 1, turn.Reply, at.Add(time.Second)); err != nil {
					return err
				}
				fmt.Fprintf(out, "seed assistant: %s\n", scenario.Opening)
			}
			for i, input := range scenario.Steps {
				hour := scenario.Hour
				if hour == 0 {
					hour = 21
				}
				at := time.Date(2026, 10, 26, hour, i*2, 0, 0, zone)
				var inbox store.FeishuInboxItem
				if scenario.Channel {
					at = time.Date(2026, 10, 26, hour, 0, 0, 0, zone).Add(scenario.Offsets[i])
					key := fmt.Sprintf("audition-%d", i)
					if _, err := h.EnqueueFeishu(ctx, key, "isolated", key, input, at, zone); err != nil {
						return err
					}
					var ready bool
					inbox, ready, err = h.NextFeishuBatch(ctx, at.Add(5*time.Second))
					if err != nil {
						return err
					}
					if !ready {
						report.Turns = append(report.Turns, auditionTurn{Scenario: scenario.Name, User: input, Action: "queued while away"})
						fmt.Fprintf(out, "user: %s\naction: queued while away (no model call)\n", input)
						continue
					}
				} else if err := h.AddUserMessage(ctx, input, at, zone); err != nil {
					return err
				}
				entries, err := h.Recent(ctx, 1)
				if err != nil {
					return err
				}
				start, before := time.Now(), counter.calls
				sourceID := entries[0].ID
				if scenario.Channel {
					sourceID = inbox.UserMessageID
				}
				reply, generateErr := s.ComposeReplyForAt(ctx, sourceID, at)
				t := auditionTurn{Scenario: scenario.Name, User: input, Reply: reply, Calls: counter.calls - before, Seconds: time.Since(start).Seconds()}
				if engine == "character_v2" && generateErr == nil {
					if saved, found, lookupErr := h.CharacterTurn(ctx, sourceID); lookupErr != nil {
						return lookupErr
					} else if found {
						t.Action = saved.Mode
						t.Absence = saved.Absence
					}
				}
				if generateErr != nil {
					t.Error = generateErr.Error()
					failures++
				}
				report.Turns = append(report.Turns, t)
				fmt.Fprintf(out, "user: %s\nreply: %s [%d calls, %.1fs]\n", input, reply, t.Calls, t.Seconds)
				if engine == "character_v2" && !scenario.Channel && t.Action != "" {
					fmt.Fprintf(out, "action: %s\n", t.Action)
				}
				if generateErr != nil {
					fmt.Fprintf(out, "error: %v\n", generateErr)
					break // Preserve a failed scenario, not a substitute success.
				}
				if engine == "character_v2" {
					if scenario.Channel {
						saved, _, err := h.CharacterTurn(ctx, inbox.UserMessageID)
						if err != nil {
							return err
						}
						last := &report.Turns[len(report.Turns)-1]
						last.Action = saved.Mode
						last.Absence = saved.Absence
						if saved.Absence != nil {
							last.Action = "away"
						}
						fmt.Fprintf(out, "action: %s; absence: %+v\n", saved.Mode, saved.Absence)
						if saved.Mode == "defer" {
							if _, err := h.DeferFeishuReply(ctx, inbox.EventID); err != nil {
								return err
							}
							continue
						}
						if reply == "" {
							if err := h.SkipFeishuReply(ctx, inbox.EventID); err != nil {
								return err
							}
						} else {
							if err := h.SetFeishuReply(ctx, inbox.EventID, reply, at.Add(time.Second)); err != nil {
								return err
							}
							if err := h.MarkFeishuSent(ctx, inbox.EventID); err != nil {
								return err
							}
						}
					} else if err := h.CommitCharacterReply(ctx, entries[0].ID, reply, at.Add(time.Second)); err != nil {
						return err
					}
					before := counter.calls
					reflectionErr := s.ReflectCharacter(ctx, at.Add(95*time.Second))
					last := &report.Turns[len(report.Turns)-1]
					last.ReflectionCalls = counter.calls - before
					if snapshot, found, err := h.CharacterTurn(ctx, sourceID); err != nil {
						return err
					} else if found && snapshot.StateUpdated {
						last.State = &snapshot.Moment
						last.OpenThreads = snapshot.OpenThreads
						last.Episode = snapshot.Episode
					}
					if last.Episode != nil {
						fmt.Fprintf(out, "episode: %s → %s\n", last.Episode.Event, last.Episode.Effect)
					}
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
