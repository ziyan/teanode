package models

import (
	"regexp"
	"strings"
	"unicode"
)

// fillerWordPatterns are the words that carry nothing on their own: sounds of
// listening and hesitation, laughter, and the bare acknowledgements (okay,
// yeah) said while someone else talks. A plain yes or no is not among them;
// it answers something.
var fillerWordPatterns = regexp.MustCompile(`^(h+m+|m+|m+h+m+|u+h+|u+m+|e+r+m*|a+h+|o+h+|huh|aha|(ha)+h?|(he)+h?|okay|ok|yeah|yep|yup|alright|right)$`)

// fillerSyllables are the same for languages written without spaces: a passage
// made only of these, as many times over as they come, is filler.
var fillerSyllables = []string{"嗯", "啊", "哦", "噢", "呃", "唔", "哈", "嘿", "好", "えー", "あー", "うーん", "うん", "へえ", "ふーん", "ー", "음", "어", "아"}

// IsFillerTranscriptText says whether a passage holds only filler ("Mm-hmm.
// Okay. Okay."), which a reader skips and nobody needs to label. A passage
// with no words at all is not filler; it is empty. Nor is a question ("Right?"),
// however short: it asks something.
func IsFillerTranscriptText(text string) bool {
	if strings.ContainsAny(text, "?？") {
		return false
	}
	hasWord := false
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != 'ー'
	}) {
		hasWord = true
		if isSpacelessText(word) {
			if !isFillerSyllables(word) {
				return false
			}
			continue
		}
		if !fillerWordPatterns.MatchString(word) {
			return false
		}
	}
	return hasWord
}

// isSpacelessText is a word in a script written without spaces between words.
func isSpacelessText(word string) bool {
	for _, character := range word {
		if unicode.In(character, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

// isFillerSyllables says whether text is made only of fillerSyllables.
func isFillerSyllables(text string) bool {
	for text != "" {
		isMatched := false
		for _, syllable := range fillerSyllables {
			if strings.HasPrefix(text, syllable) {
				text = strings.TrimPrefix(text, syllable)
				isMatched = true
				break
			}
		}
		if !isMatched {
			return false
		}
	}
	return true
}
