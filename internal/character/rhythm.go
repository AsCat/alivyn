package character

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/AsCat/alivyn/internal/model"
)

// Rhythm measures participation, not sentiment or intent. It never decides
// from keywords that someone is upset, needs advice, or should be ignored.
type Rhythm struct {
	ContributionMessages int `json:"contribution_messages"`
	ContributionChars    int `json:"contribution_chars"`
	RecentUserChars      int `json:"recent_user_chars"`
	RecentCharacterChars int `json:"recent_character_chars"`
	RecentQuestions      int `json:"recent_question_messages"`
}

func ConversationRhythm(messages []model.Message) Rhythm {
	var r Rhythm
	for i := len(messages) - 1; i >= 0 && messages[i].Role == "user"; i-- {
		r.ContributionMessages++
		r.ContributionChars += utf8.RuneCountInString(messages[i].Content)
	}
	start := max(0, len(messages)-8)
	for _, m := range messages[start:] {
		switch m.Role {
		case "user":
			r.RecentUserChars += utf8.RuneCountInString(m.Content)
		case "assistant":
			r.RecentCharacterChars += utf8.RuneCountInString(m.Content)
			if strings.ContainsAny(m.Content, "?？") {
				r.RecentQuestions++
			}
		}
	}
	return r
}

func rhythmMessage(c Context) model.Message {
	r := ConversationRhythm(c.Frame.Recent)
	data, _ := json.Marshal(r)
	text := "Conversation participation (counts, not an interpretation of the user's feelings): " + string(data) + ". Respond to the whole latest contribution as one thought. Do not invent an obligation to keep the conversation going."
	small := 80
	if c.Frame.Language == "zh-CN" {
		small = 20
	}
	if r.ContributionMessages > 0 && r.ContributionChars <= small {
		if c.Frame.Language == "zh-CN" {
			text += " 这一轮通常只需接住一个很短的意思，不必每次写成完整句子。同一个意思有时可以分两拍说；第一拍接住了，就停下，不为了改变条数补自己的近况、感想、笑话或新问题。只有这段话确实提出需要解释的问题、认真倾诉或明确邀请展开时才多说。不要把这个提示告诉对方，也不要省略必要内容。"
		} else {
			text += " For this small contribution, usually a few words (roughly 1–10) suffice. Stop after one thought; no appended life anecdote, reflection, joke or new question. Expand only if the contribution actually asks for an explanation, expresses a significant concern or invites elaboration. Do not mention these counts or omit necessary substance."
		}
	}
	if r.RecentQuestions > 0 {
		text += " You have recently asked questions. Let the other person choose whether to develop the topic; do not append another question just to solicit the next message. A necessary clarification or an explicit invitation to ask questions is different."
	}
	if r.RecentCharacterChars > max(40, 2*r.RecentUserChars) {
		text += " You have been doing substantially more of the talking. Give the other person room; avoid another unsolicited expansion."
	}
	text += " Your own relevant choice is also worth saying: you may bring this exchange to a close or return to another activity. This is not an obligation to announce an activity every turn. Follow the runtime output protocol in the character instructions; when leaving now, use its timed absence action so your words and availability agree."
	return model.Message{Role: "system", Content: text}
}
