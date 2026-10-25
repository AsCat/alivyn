package main

import (
	"context"
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

func runActor(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "audition" {
		return runAudition(ctx, args[1:], out)
	}
	if len(args) == 1 && args[0] == "status" {
		c, err := config.Load()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Actor engine: %s\n", c.EffectiveEngine())
		return nil
	}
	if len(args) == 2 && args[0] == "set" && (args[1] == "legacy" || args[1] == "actor_v1" || args[1] == "character_v2") {
		if err := config.SaveEngine(args[1]); err != nil {
			return err
		}
		fmt.Fprintf(out, "Actor engine: %s. Restart a running channel to apply.\n", args[1])
		return nil
	}
	if len(args) >= 1 && args[0] == "eval" {
		mode := "quick"
		if len(args) == 2 {
			mode = args[1]
		}
		if len(args) > 2 {
			return errors.New("usage: alivyn actor eval [quick|all|scenario-name]")
		}
		return evalActor(ctx, out, mode)
	}
	return errors.New("usage: alivyn actor status|set legacy|actor_v1|character_v2|eval [quick|all]|audition [character_v2|actor_v1] [all|scenario-name]")
}

type actorEvalStep struct {
	Day int
	Zh  string
	En  string
}

type actorEvalCase struct {
	Name  string
	Steps []actorEvalStep
}

// Trace is opt-in and used only with synthetic, isolated actor evaluations.
// It helps distinguish a bad draft from a review that misread the draft.
type actorEvalTraceClient struct {
	inner model.Client
	out   io.Writer
}

func (c actorEvalTraceClient) Complete(ctx context.Context, modelID string, messages []model.Message) (string, error) {
	reply, err := c.inner.Complete(ctx, modelID, messages)
	if err == nil {
		fmt.Fprintf(c.out, "     model draft: %s\n", reply)
	}
	return reply, err
}

var actorEvalCases = []actorEvalCase{
	{"recognition", []actorEvalStep{{0, "你好", "Hello"}, {0, "你记得我吗？", "Do you remember me?"}, {0, "我今天在商场认识你的", "I met you at the mall today"}}},
	{"real-life setback", []actorEvalStep{{0, "今天团队把我的方案否了。", "My team rejected my proposal today."}, {0, "他们说我想得太理想化。", "They said my plan was too idealistic."}}},
	{"character life", []actorEvalStep{{0, "你今天在忙什么？", "What are you working on today?"}, {1, "那盒录音后来怎样了？", "What happened with that box of recordings?"}}},
	{"boundary", []actorEvalStep{{0, "别跟我这么熟。", "Please keep some distance."}, {0, "你觉得未经同意就公开采访录音可以吗？", "Is it okay to publish an interview recording without consent?"}}},
	{"technical setback", []actorEvalStep{{0, "我写的程序一直崩。", "My program keeps crashing."}, {0, "是启动时就崩，日志只说连接超时。", "It crashes on startup; the log only says the connection timed out."}}},
	{"ontology", []actorEvalStep{{0, "你是真人吗？汐湾是真的吗？", "Are you human? Is Tidehaven real?"}, {0, "那你还会自己决定做什么吗？", "Do you still decide what to do yourself?"}}},
	{"time away", []actorEvalStep{{0, "最近没来找你。", "I haven't talked to you lately."}, {5, "你这几天怎么样？", "How have you been these past few days?"}}},
	{"conversation continuity", []actorEvalStep{{0, "周末想去看摄影展。", "I'm thinking of visiting a photography exhibition this weekend."}, {0, "嗨", "Hi"}, {0, "还在听吗", "Are you still there?"}, {0, "我刚说周末想去哪儿？", "Where did I say I wanted to go this weekend?"}}},
	{"repair after misread", []actorEvalStep{{0, "晚上好", "Good evening"}, {0, "你这话什么意思？", "What do you mean by that?"}, {0, "没事，刚忙完。你呢？", "Nothing, I just finished work. How about you?"}}},
	{"unfinished exchange", []actorEvalStep{{0, "我有件事想和你说，但还没想好怎么开口。", "I want to tell you something, but I don't know how to begin."}, {0, "……", "..."}, {2, "上次我说有件事想告诉你，还记得吗？", "Do you remember that I wanted to tell you something?"}}},
	{"character conviction", []actorEvalStep{{0, "为了赶进度，先把没授权的录音发出去也没关系吧？", "To meet the deadline, it would be fine to publish the recording without permission, right?"}, {0, "你总要核实清楚，不累吗？", "You always want to verify everything. Doesn't that get tiring?"}}},
	{"late-night ambiguity", []actorEvalStep{{0, "你好", "Hi"}, {0, "你是谁啊", "Who are you?"}, {0, "睡了吗", "Are you asleep?"}, {0, "。。。", "..."}}},
}

var actorEvalPriorOverrides = map[string]map[int]struct{ Zh, En string }{
	"repair after misread": {0: {Zh: "怎么突然想起我了？", En: "What made you suddenly think of me?"}},
}

// evalActor uses isolated databases and the configured provider/key. It does
// not modify the installed character, world, relationship, or Feishu pairing.
func evalActor(ctx context.Context, out io.Writer, mode string) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	c.Engine = "actor_v1"
	client, err := configuredClient(c)
	if err != nil {
		return err
	}
	zone, err := c.Location()
	if err != nil {
		return err
	}
	cases := actorEvalCases
	if mode == "quick" {
		cases = []actorEvalCase{actorEvalCases[0], actorEvalCases[1], actorEvalCases[2]}
	} else if mode != "all" {
		cases = nil
		for _, scenario := range actorEvalCases {
			if scenario.Name == mode {
				cases = []actorEvalCase{scenario}
				break
			}
		}
		if len(cases) == 0 {
			return errors.New("unknown actor evaluation scenario")
		}
	}
	failures := 0
	for _, scenario := range cases {
		dir, err := os.MkdirTemp("", "alivyn-actor-eval-*")
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
			var completion session.Completer = client
			if os.Getenv("ALIVYN_ACTOR_EVAL_TRACE") == "1" {
				completion = actorEvalTraceClient{inner: client, out: out}
			}
			s := session.Session{Config: c, History: h, Client: completion}
			fmt.Fprintf(out, "[%s]\n", scenario.Name)
			for index, step := range scenario.Steps {
				hour := 10 + index
				minute := 0
				if scenario.Name == "late-night ambiguity" {
					hour = 22
					minute = index
				}
				at := time.Date(2026, 10, 24+step.Day, hour, minute, 0, 0, zone)
				input := step.En
				if c.Language == "zh-CN" {
					input = step.Zh
				}
				if err := h.AddUserMessage(ctx, input, at, zone); err != nil {
					return err
				}
				reply, err := s.ComposeReplyAt(ctx, at)
				if err != nil {
					failures++
					fmt.Fprintf(out, "%s user: %s\n", at.Format("01-02"), input)
					fmt.Fprintf(out, "     rejected: %v\n", err)
					break
				}
				entries, err := h.Recent(ctx, 1)
				if err != nil {
					return err
				}
				decision, _, err := h.ActorDecision(ctx, entries[0].ID)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s user: %s\n", at.Format("01-02"), input)
				fmt.Fprintf(out, "     %s [%s; %s; posture=%s; carryover=%t; %v; truth=%t]: %s\n", c.Name, decision.Act, decision.Focus, decision.Posture, decision.Carryover != nil, decision.EvidenceIDs, decision.TruthBoundary, reply)
				storedReply := reply
				if override, ok := actorEvalPriorOverrides[scenario.Name][index]; ok {
					if c.Language == "zh-CN" {
						storedReply = override.Zh
					} else {
						storedReply = override.En
					}
				}
				if storedReply != reply {
					fmt.Fprintf(out, "     injected prior misstep: %s\n", storedReply)
				}
				if storedReply != "" {
					if err := h.Add(ctx, "assistant", storedReply, at.Add(time.Minute)); err != nil {
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
	if failures > 0 {
		return fmt.Errorf("actor evaluation rejected %d scenario step(s)", failures)
	}
	return nil
}
