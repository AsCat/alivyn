package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/AsCat/alivyn/internal/store"
)

// runDemo drives the real local memory and contact runtime with a private,
// disposable clock. It never modifies the user's identity or conversation.
func runDemo(ctx context.Context, out io.Writer, language string) error {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "alivyn-demo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := store.OpenHistory(ctx, dir)
	if err != nil {
		return err
	}
	defer h.Close()
	at := func(day, hour int) time.Time {
		return time.Date(2026, 10, day, hour, 0, 0, 0, zone)
	}
	fmt.Fprintln(out, demoText(language, "Alivyn · a time-advanced story (disposable local data; no key needed)", "意续 · 一段可以快进的相处片段（独立临时数据，无需密钥）"))
	for _, step := range []struct {
		at   time.Time
		text string
	}{
		{at(26, 10), demoText(language, "I have an interview on Wednesday afternoon", "周三下午有面试")},
		{at(27, 12), demoText(language, "My interview moved to Friday", "面试改到周五了")},
	} {
		if err := h.AddUserMessage(ctx, step.text, step.at, zone); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s %s%s\n", step.at.Format("01-02 15:04"), demoText(language, "You: ", "你："), step.text)
	}
	facts, err := h.ActiveFacts(ctx)
	if err != nil {
		return err
	}
	if len(facts) != 1 || facts[0].Topic != "interview" || facts[0].HappensAt.In(zone).Day() != 30 {
		return fmt.Errorf("demo memory did not retain the revised Friday interview")
	}
	fmt.Fprintln(out, demoText(language, "          Memory: interview moved to Friday; old follow-up canceled.", "          记忆：面试已改到周五；旧提醒取消。"))
	if _, err := h.ProcessDue(ctx, at(28, 16), zone, language); err != nil {
		return err
	}
	messages, err := h.Outbox(ctx, 10)
	if err != nil {
		return err
	}
	if len(messages) != 0 {
		return fmt.Errorf("demo unexpectedly contacted the user on Wednesday")
	}
	fmt.Fprintln(out, demoText(language, "10-28 16:00 Wednesday: stayed quiet; no old follow-up.", "10-28 16:00 周三：保持安静，没有发送旧提醒。"))
	if _, err := h.ProcessDue(ctx, at(30, 16), zone, language); err != nil {
		return err
	}
	messages, err = h.Outbox(ctx, 10)
	if err != nil {
		return err
	}
	if len(messages) != 1 || messages[0].Topic != "interview" {
		return fmt.Errorf("demo Friday follow-up was not delivered once")
	}
	fmt.Fprintf(out, "%s%s\n", demoText(language, "10-30 16:00 Alivyn reaches out: ", "10-30 16:00 意续主动说："), messages[0].Body)
	if err := h.Heartbeat(ctx, at(30, 17)); err != nil {
		return err
	}
	if err := h.ResumeRuntime(ctx, at(31, 10)); err != nil {
		return err
	}
	if _, err := h.ProcessDue(ctx, at(31, 10), zone, language); err != nil {
		return err
	}
	if err := h.ResumeRuntime(ctx, at(31, 11)); err != nil {
		return err
	}
	if _, err := h.ProcessDue(ctx, at(31, 11), zone, language); err != nil {
		return err
	}
	messages, err = h.Outbox(ctx, 10)
	if err != nil {
		return err
	}
	if len(messages) != 2 || messages[0].Topic != "time_gap" {
		return fmt.Errorf("demo time-gap contact was not delivered exactly once")
	}
	fmt.Fprintf(out, "%s%s\n", demoText(language, "10-31 10:00 After waking, Alivyn says: ", "10-31 10:00 开机后意续说："), messages[0].Body)
	fmt.Fprintln(out, demoText(language, "10-31 11:00 Checked again: no repeated greeting.", "10-31 11:00 再次检查：没有重复打招呼。"))
	fmt.Fprintln(out, demoText(language, "These proactive messages went only to the local test outbox; chat channels are not connected yet.", "以上主动消息只写入本地测试投递箱；真实聊天渠道尚未接入。"))
	return nil
}

func demoText(language, en, zh string) string {
	if language == "en" {
		return en
	}
	return zh
}
