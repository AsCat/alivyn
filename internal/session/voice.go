package session

import (
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/relationship"
	"github.com/AsCat/alivyn/internal/store"
)

// A greeting has almost no semantic content. A tiny, context-aware response
// avoids asking the model to invent an occasion or a service-style question.
func greetingReply(c config.Config, stage relationship.Stage, recent []store.Entry, now time.Time) string {
	previousGreetings := 0
	previousAssistant := ""
	for i := len(recent) - 2; i >= 0; i-- {
		entry := recent[i]
		if entry.CreatedAt.After(now) || now.Sub(entry.CreatedAt) > 24*time.Hour {
			continue
		}
		if entry.Role == "user" && bareGreeting(entry.Content) {
			previousGreetings++
		}
		if entry.Role == "assistant" && previousAssistant == "" {
			previousAssistant = entry.Content
		}
	}
	if c.Language == "zh-CN" {
		if stage == relationship.Stranger {
			if previousGreetings == 0 {
				return "你好，你是？"
			}
			if strings.Contains(previousAssistant, "怎么称呼") {
				return "你好。"
			}
			return "你好，怎么称呼你？"
		}
		if previousGreetings == 0 {
			if stage == relationship.Close {
				return "你来啦。"
			}
			return "嗨。"
		}
		if strings.Contains(previousAssistant, "又来") || strings.Contains(previousAssistant, "又见面") {
			return "今天已经跟我说了好几次你好。"
		}
		return "欸，你又来啦。"
	}
	if stage == relationship.Stranger {
		if previousGreetings == 0 {
			return "Hi. Have we met?"
		}
		return "Hi. What should I call you?"
	}
	if previousGreetings == 0 {
		return "Hey."
	}
	if strings.Contains(previousAssistant, "again") || strings.Contains(previousAssistant, "back") {
		return "We've said hi a few times today."
	}
	return "Hey, you're back."
}

func cannedReplyReason(user, reply string) string {
	patterns := []struct{ phrase, reason string }{
		{"我在这儿", "a stock presence statement"},
		{"想聊点什么", "a generic invitation to chat"},
		{"有什么事吗", "a service-style question"},
		{"有什么可以帮", "a service offer"},
		{"随时陪", "an unconditional companionship promise"},
		{"愿意倾听", "a generic listening promise"},
		{"帮你梳理", "a generic support offer"},
		{"我理解你的感受", "a generic empathy formula"},
		{"你并不孤单", "a generic reassurance"},
		{"心里肯定", "an unsupported claim about the user's feelings"},
		{"有点堵", "an unsupported claim about the user's feelings"},
		{"不好受吧", "an unsupported claim about the user's feelings"},
		{"挺失落", "an unsupported claim about the user's feelings"},
		{"一定很难受", "an unsupported claim about the user's feelings"},
		{"方便说一下吗", "a service-style request to elaborate"},
		{"愿意说说吗", "a generic invitation to elaborate"},
		{"要不要说说", "a generic invitation to elaborate"},
		{"这不代表你不行", "an unsolicited reassurance"},
		{"，还是", "a list of possible causes the user did not mention"},
		{"I'm here for you", "a stock presence statement"},
		{"What can I help", "a service offer"},
		{"How can I help", "a service offer"},
		{"I understand how you feel", "a generic empathy formula"},
	}
	lower := strings.ToLower(reply)
	for _, p := range patterns {
		if p.phrase == "，还是" && strings.Contains(user, "还是") {
			continue
		}
		if strings.Contains(lower, strings.ToLower(p.phrase)) {
			if p.reason == "an unsupported claim about the user's feelings" && strings.Contains(user, strings.TrimSuffix(strings.TrimSuffix(p.phrase, "吧"), "肯定")) {
				continue
			}
			return p.reason
		}
	}
	return ""
}

func soundsOverfamiliar(reply string) bool {
	lower := strings.ToLower(reply)
	for _, phrase := range []string{"宝贝", "亲爱的", "想你了", "我一直在等你", "dear", "sweetheart", "missed you"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func revisionPrompt(language, reason string) string {
	if language == "zh-CN" {
		return "上一版回复有模板感，问题是" + reason + "。重写成一条自然的私人消息，保持当前关系阶段和人物背景。只根据用户已经说过的事实，别替用户断定心情，别列举未经提及的可能原因；信息少时宁可短。不要照搬上一版的套话，不要用“是……还是……”的二选一问法，也不要连问两句。只输出最终要发送的中文。"
	}
	return "The previous draft sounds like a generic assistant because of " + reason + ". Rewrite as one natural private message, respecting the current relationship stage and character background. Use only facts the user supplied. Do not guess their feelings, list invented possible causes, or end with a routine question. Be brief when there is little to say. Output only the final message."
}
