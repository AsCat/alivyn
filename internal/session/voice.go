package session

import (
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/relationship"
	"github.com/AsCat/alivyn/internal/store"
)

func previousAssistant(recent []store.Entry) string {
	for i := len(recent) - 2; i >= 0; i-- {
		if recent[i].Role == "assistant" {
			return recent[i].Content
		}
	}
	return ""
}

// Short social beats are authored for the default character's first-contact
// ritual. They use actual conversation state and never fabricate recognition
// or an offline life. Custom backgrounds keep the model-generated path.
func anchoredSocialReply(c config.Config, state relationship.State, recent []store.Entry, hasName bool) (string, bool) {
	if strings.TrimSpace(c.Background) != "" || hasName || (state.Stage != relationship.Stranger && state.Stage != relationship.Acquaintance) {
		return "", false
	}
	latest := strings.ToLower(strings.TrimSpace(strings.TrimRight(recent[len(recent)-1].Content, "。.!！?？")))
	previous := previousAssistant(recent)
	if !relationship.OfflineClaim(latest) && state.MeaningfulTurns > 1 {
		return "", false
	}
	if c.Language == "zh-CN" {
		if relationship.OfflineClaim(latest) {
			return "那不是我。我们只在这儿聊过。", true
		}
		if latest == "是我" || latest == "是我啊" || latest == "是我呀" {
			if strings.Contains(previous, "猜谜") {
				return "你倒是给我点提示。", true
			}
			return "你这是让我猜谜？", true
		}
		if strings.Contains(latest, "不记得我") || strings.Contains(latest, "认不出我") || strings.Contains(latest, "忘了我") {
			if strings.Contains(previous, "猜谜") || strings.Contains(previous, "提示") || strings.Contains(previous, "名字") {
				return "你连名字都没说，我怎么认你。", true
			}
			return "你还没告诉我你是谁呢。", true
		}
		return "", false
	}
	if relationship.OfflineClaim(latest) {
		return "That wasn't me. We've only talked here.", true
	}
	if latest == "it's me" || latest == "its me" {
		return "You're making me guess?", true
	}
	if strings.Contains(latest, "remember me") {
		return "You haven't told me who you are yet.", true
	}
	return "", false
}

// A greeting has almost no semantic content. A tiny, context-aware response
// avoids asking the model to invent an occasion or a service-style question.
func greetingReply(c config.Config, state relationship.State, recent []store.Entry, now time.Time) string {
	previousGreetings := 0
	previousAssistant := ""
	askedIdentity := false
	for i := len(recent) - 2; i >= 0; i-- {
		entry := recent[i]
		if entry.Role == "assistant" && (strings.Contains(entry.Content, "你是？") || strings.Contains(entry.Content, "怎么称呼你") || strings.Contains(entry.Content, "What should I call you") || strings.Contains(entry.Content, "Have we met")) {
			askedIdentity = true
		}
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
		if state.Stage == relationship.Stranger {
			if askedIdentity {
				return "你好。"
			}
			if previousGreetings == 0 && state.MeaningfulTurns == 0 {
				return "你好，你是？"
			}
			return "你好，怎么称呼你？"
		}
		if previousGreetings == 0 {
			if state.Stage == relationship.Close {
				return "你来啦。"
			}
			return "嗨。"
		}
		if strings.Contains(previousAssistant, "又来") || strings.Contains(previousAssistant, "又见面") {
			return "今天已经跟我说了好几次你好。"
		}
		return "欸，你又来啦。"
	}
	if state.Stage == relationship.Stranger {
		if askedIdentity {
			return "Hi."
		}
		if previousGreetings == 0 && state.MeaningfulTurns == 0 {
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
		{"我这边", "a system-status voice instead of a personal reply"},
		{"这个我得澄清", "a formal correction instead of a personal reply"},
		{"没有留下能认出", "a database-style explanation of recognition"},
		{"给我提一句，我就能对上", "a scripted claim of recognition"},
		{"你是不是把我跟别的什么搞混了", "an accusatory question"},
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
	if !asksAboutNature(user) {
		for _, phrase := range []string{"没有线下形态", "我是一个ai", "我是 ai", "我没有实体", "as an ai", "i have no physical form"} {
			if strings.Contains(lower, strings.ToLower(phrase)) {
				return "an unprompted technical identity explanation"
			}
		}
	}
	return ""
}

func socialReplyIssue(user, reply string, recent []store.Entry) string {
	if !relationship.SocialProbe(user) {
		return ""
	}
	if strings.HasPrefix(reply, "抱歉") || strings.HasPrefix(strings.ToLower(reply), "sorry") {
		return "an unnecessary apology to a playful recognition probe"
	}
	if strings.Contains(reply, "比如") && strings.Contains(reply, "或者") {
		return "a scripted list of identity clues"
	}
	if relationship.OfflineClaim(user) {
		if strings.Contains(reply, "我今天没去") || strings.Contains(reply, "I didn't go there today") {
			return "a misleading suggestion that you could visit that place on another day"
		}
		if strings.Contains(reply, "搞混") || strings.Contains(reply, "弄混") {
			return "an unnecessarily accusatory correction"
		}
	}
	previous := previousAssistant(recent)
	if (strings.Contains(user, "不记得我") || strings.Contains(user, "认不出我")) &&
		(strings.Contains(previous, "提示") || strings.Contains(previous, "线索")) &&
		(strings.Contains(reply, "提示") || strings.Contains(reply, "线索")) {
		return "repeating the same request for a clue"
	}
	return ""
}

func asksAboutNature(user string) bool {
	lower := strings.ToLower(user)
	for _, phrase := range []string{"你是ai", "你是 ai", "你是机器人", "你是真人吗", "你有身体吗", "你能出门吗", "你能去商场吗", "are you ai", "are you human", "do you have a body"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
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

func revisionPrompt(language, reason string, socialProbe bool) string {
	if language == "zh-CN" {
		if socialProbe {
			return "上一版回复显得像在处理身份工单，问题是" + reason + "。重新只写一条短消息，接住对方可能在逗你或试探你记不记得他的语气。仅用真实对话，不能假装认得；如果刚才已经要过提示，这次别重复问。若他说线下见过你，不确认不存在的见面，也不要说自己“今天没去”某地；不讲 AI 形态、记忆系统或做正式澄清。不要道歉，不要说“我这边”“没有线索”“搞混了”；最多一个问句。只输出要发送的话。"
		}
		return "上一版回复有模板感，问题是" + reason + "。重写成一条自然的私人消息，保持当前关系阶段和人物背景。只根据用户已经说过的事实，别替用户断定心情，别列举未经提及的可能原因；信息少时宁可短。不要照搬上一版的套话，不要用“是……还是……”的二选一问法，也不要连问两句。只输出最终要发送的中文。"
	}
	if socialProbe {
		return "The prior draft sounds like an identity support ticket because of " + reason + ". Rewrite as one brief human-sounding text that catches the person's playful or probing tone. Do not pretend to recognize them without evidence, or confirm an offline encounter. Avoid explaining your architecture, memory system, or lack of a body unless directly asked. Ask at most one question. Output only the message."
	}
	return "The previous draft sounds like a generic assistant because of " + reason + ". Rewrite as one natural private message, respecting the current relationship stage and character background. Use only facts the user supplied. Do not guess their feelings, list invented possible causes, or end with a routine question. Be brief when there is little to say. Output only the final message."
}
