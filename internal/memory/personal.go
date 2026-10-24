package memory

import (
	"regexp"
	"strings"
)

// PersonalChange captures only short, explicit statements about the user.
// Ambiguous conversation is left in the transcript instead of being promoted
// to durable memory.
type PersonalChange struct {
	Key    string
	Value  string
	Forget bool
}

var (
	zhName   = regexp.MustCompile(`^(?:我叫|叫我|以后叫我)([\p{Han}\p{L}\p{N}·_-]{1,24})$`)
	zhLike   = regexp.MustCompile(`^我(喜欢|不喜欢|讨厌)([^，。！？,.!?]{1,24})$`)
	zhForget = regexp.MustCompile(`^忘记我(喜欢|不喜欢|讨厌)([^，。！？,.!?]{1,24})$`)
	enName   = regexp.MustCompile(`(?i)^(?:my name is|call me) ([a-z][a-z0-9 _'-]{0,23})$`)
	enLike   = regexp.MustCompile(`(?i)^i (like|dislike|hate) ([a-z0-9][a-z0-9 _'-]{0,23})$`)
	enForget = regexp.MustCompile(`(?i)^forget (?:that )?i (?:like|dislike|hate) ([a-z0-9][a-z0-9 _'-]{0,23})$`)
)

func ExtractPersonal(text string) (PersonalChange, bool) {
	text = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(text), "。.!！"))
	if text == "忘记我的名字" || strings.EqualFold(text, "forget my name") {
		return PersonalChange{Key: "name", Forget: true}, true
	}
	if parts := zhForget.FindStringSubmatch(text); parts != nil {
		return PersonalChange{Key: preferenceKey(parts[2]), Forget: true}, true
	}
	if parts := enForget.FindStringSubmatch(text); parts != nil {
		return PersonalChange{Key: preferenceKey(parts[1]), Forget: true}, true
	}
	if parts := zhName.FindStringSubmatch(text); parts != nil {
		return PersonalChange{Key: "name", Value: "用户希望被称为" + parts[1]}, true
	}
	if parts := enName.FindStringSubmatch(text); parts != nil {
		return PersonalChange{Key: "name", Value: "The user prefers to be called " + strings.TrimSpace(parts[1])}, true
	}
	if parts := zhLike.FindStringSubmatch(text); parts != nil {
		return PersonalChange{Key: preferenceKey(parts[2]), Value: text}, true
	}
	if parts := enLike.FindStringSubmatch(text); parts != nil {
		return PersonalChange{Key: preferenceKey(parts[2]), Value: text}, true
	}
	return PersonalChange{}, false
}

func preferenceKey(subject string) string {
	return "preference:" + strings.ToLower(strings.TrimSpace(subject))
}
