package tools

import (
	"strings"
	"unicode"
)

// answerWordsApart is how many words may stand between the words of
// "agent skill approve", for the flags of a command line between them.
const answerWordsApart = 6

// AnswersSkillShareCall says a command line, as typed into a shell, looks like
// `teanode agent skill approve` or `teanode agent skill decline`, the commands
// that answer a call somebody's agent made through a skill the person shared,
// which waits for the person's own word and spends their money when approved.
//
// A courtesy and not the guard. It is a guess over words and not a parse of
// the shell: quotes, separators and substitutions are turned into spaces, and
// the words agent, skill and approve or decline must come in that order with a
// few words between them. It errs toward asking, so that a shell that looks
// like it is answering a call asks the person first, as money. What it cannot
// see, a command assembled from pieces or typed into a terminal a key at a
// time, it cannot hold, and it does not have to: the server refuses to approve
// a call, or to lend money, to anything but the dashboard's session or the
// phone app's own sign-in (AnswerSkillShareCall, ShareSkill), whoever typed
// the command and however they wrote it.
func AnswersSkillShareCall(commandLine string) bool {
	words := strings.FieldsFunc(strings.ToLower(commandLine), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '-' && character != '_'
	})
	for index, word := range words {
		if word != "agent" {
			continue
		}
		skillIndex := indexWithin(words, index+1, "skill")
		if skillIndex < 0 {
			continue
		}
		if indexWithin(words, skillIndex+1, "approve", "decline") >= 0 {
			return true
		}
	}
	return false
}

// indexWithin is the index of the first of the wanted words among the
// answerWordsApart words from start, or -1.
func indexWithin(words []string, start int, wanted ...string) int {
	for index := start; index < len(words) && index < start+answerWordsApart; index++ {
		for _, each := range wanted {
			if words[index] == each {
				return index
			}
		}
	}
	return -1
}
