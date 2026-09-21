package agent

import (
	"strings"
	"unicode"
)

// sharesAName says whether two sentences name the same thing: a word that
// begins with a capital and is not the first word, or a run of digits.
// Where neither sentence has one, they are judged on the cosine alone.
func sharesAName(left, right string, itsOwn ...string) bool {
	leftNames, rightNames := properNouns(left), properNouns(right)
	// Every fact on a page shares the page's name. Ignore that name when
	// checking whether the sentences agree about other people or quantities.
	for _, name := range namesIn(itsOwn) {
		delete(leftNames, name)
		delete(rightNames, name)
	}
	if len(leftNames) == 0 || len(rightNames) == 0 {
		return true
	}
	for name := range leftNames {
		if rightNames[name] {
			return true
		}
	}
	return false
}

// negationTokens are the words that turn a sentence into its opposite,
// written as they appear once punctuation has become spaces: a whole
// word, the pair "no longer", or the contraction's own ending.
//
// Short and English-only on purpose. It is not a grammar; it is the list
// of ways the sentences a graph actually holds say "not any more".
var negationTokens = []string{
	" not ", " no longer ", " never ", " stopped ", " former ", " formerly ", "n't", "n’t",
}

// negates says whether exactly one of two sentences carries a negation.
//
// This is the guard in front of every fold. Similarity is a candidate
// generator and nothing more: "she prefers tea" and "she no longer
// prefers tea" share every proper noun, embed within a hair of each
// other, and are the two statements it matters most not to lose one of.
// A cosine cannot tell them apart and neither can the name check, so
// before two facts are folded into one they are asked this, and when the
// answer is yes both rows stay and the newer one supersedes the older.
//
// Both negated, or neither, is not the case this catches: "she never
// drinks tea" and "she has never drunk tea" are the same statement, and
// folding them is right.
func negates(left, right string) bool {
	return negated(left) != negated(right)
}

// negated says whether one sentence carries a negation token.
func negated(text string) bool {
	// Punctuation has become space and the whole is padded, so that a
	// token written with its spaces reaches the first and last words
	// too: "They stopped." ends with the word this is looking for.
	words := wordsOf(text)
	for _, token := range negationTokens {
		if strings.Contains(words, token) {
			return true
		}
	}
	return false
}

// saysItInTheSameWords says whether two sentences are one statement
// written out twice: the same once case, the width of the whitespace and
// the punctuation words are written with have been taken off.
//
// This, and not the cosine, is what lets a fold happen with nobody
// watching. Two sentences near each other in the vector space and
// sharing a name may be the same thing said twice, or they may be this
// month's figure and last month's; the vectors cannot tell, and the one
// that goes dormant is the one the person never hears again. So the
// automatic fold is held to the case where there is provably nothing to
// lose, and everything else is left for the pass that asks a model (see
// consolidatePage) or for the person.
func saysItInTheSameWords(left, right string) bool {
	return statementLikeness(left) == statementLikeness(right)
}

// statementLikeness is a sentence reduced to what two writings of it have
// to share to be the same writing: its words, lowercased, in order,
// without the punctuation a sentence is written with.
func statementLikeness(text string) string {
	words := strings.Fields(evidenceLikeness(text))
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if trimmed := strings.Trim(word, ".,;:!?()[]{}\"'"); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, " ")
}

// quantityWords are the words that say how much, how often, or when.
//
// Not a vocabulary of English, and English only, the same as the
// negation tokens beside it. It is the list of ways the sentences a
// graph actually holds change what they claim without changing any of
// the names in them.
var quantityWords = map[string]bool{
	"hourly": true, "daily": true, "nightly": true, "weekly": true,
	"fortnightly": true, "monthly": true, "quarterly": true, "yearly": true,
	"annually": true, "biweekly": true, "monthy": true,
	"second": true, "seconds": true, "minute": true, "minutes": true,
	"hour": true, "hours": true, "day": true, "days": true,
	"week": true, "weeks": true, "fortnight": true, "month": true, "months": true,
	"quarter": true, "quarters": true, "year": true, "years": true,
	"decade": true, "decades": true,
	"once": true, "twice": true, "thrice": true,
	"half": true, "double": true, "triple": true, "both": true,
	"one": true, "two": true, "three": true, "four": true, "five": true,
	"six": true, "seven": true, "eight": true, "nine": true, "ten": true,
	"eleven": true, "twelve": true, "twenty": true, "thirty": true,
	"forty": true, "fifty": true, "sixty": true, "seventy": true,
	"eighty": true, "ninety": true, "hundred": true, "thousand": true,
	"million": true, "billion": true,
	"january": true, "february": true, "march": true, "april": true,
	"may": true, "june": true, "july": true, "august": true,
	"september": true, "october": true, "november": true, "december": true,
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
	"spring": true, "summer": true, "autumn": true, "winter": true,
	"morning": true, "afternoon": true, "evening": true,
	"today": true, "yesterday": true, "tomorrow": true,
}

// differsInQuantity says whether two sentences disagree about any
// number, date or quantity word.
//
// Similar sentences can disagree on an amount, date, frequency or duration.
// Those differences must survive even when vector similarity is high.
//
// Compared as multisets, and it errs towards saying they differ: a false
// difference costs a page one extra line, and a false sameness costs the
// person something they told their agent.
func differsInQuantity(left, right string) bool {
	leftCounts, rightCounts := quantitiesIn(left), quantitiesIn(right)
	if len(leftCounts) != len(rightCounts) {
		return true
	}
	for word, count := range leftCounts {
		if rightCounts[word] != count {
			return true
		}
	}
	return false
}

// quantitiesIn is how often a sentence says each number and each word of
// amount, date or frequency.
func quantitiesIn(text string) map[string]int {
	counts := map[string]int{}
	for _, word := range strings.Fields(wordsOf(text)) {
		if digits := plainNumber(word); digits != "" {
			counts[digits]++
			continue
		}
		if quantityWords[word] {
			counts[word]++
		}
	}
	return counts
}

// plainNumber is a word that is nothing but digits, with the leading
// zeros off so that "09" and "9" are one number, or empty for a word
// that is not one.
func plainNumber(word string) string {
	for _, letter := range word {
		if !unicode.IsDigit(letter) {
			return ""
		}
	}
	if word == "" {
		return ""
	}
	trimmed := strings.TrimLeft(word, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

// wordsOf is a sentence with everything that is not a letter, a digit or
// an apostrophe turned into a space, and the whole lowercased. One
// function because the negation check and the quantity check have to cut
// a sentence into words the same way or they disagree about what a word
// is.
func wordsOf(text string) string {
	var words strings.Builder
	words.WriteByte(' ')
	for _, letter := range strings.ToLower(text) {
		if unicode.IsLetter(letter) || unicode.IsDigit(letter) || letter == '\'' || letter == '’' {
			words.WriteRune(letter)
			continue
		}
		words.WriteByte(' ')
	}
	words.WriteByte(' ')
	return words.String()
}

// namesIn is the words of some names, lowercased.
func namesIn(names []string) []string {
	var words []string
	for _, name := range names {
		for _, word := range strings.Fields(name) {
			if trimmed := strings.Trim(word, ".,;:!?()[]\"'"); trimmed != "" {
				words = append(words, strings.ToLower(trimmed))
			}
		}
	}
	return words
}

// properNouns is the names and numbers a sentence carries: a word that
// begins with a capital and is not the first word, or a run of digits.
//
// The first word is capitalized by position, so it is excluded to avoid
// treating ordinary sentence openings as different names. This also misses a
// person's name at the start; names mentioned later still constrain matching.
func properNouns(text string) map[string]bool {
	names := map[string]bool{}
	for index, word := range strings.Fields(text) {
		trimmed := strings.Trim(word, ".,;:!?()[]\"'")
		if trimmed == "" {
			continue
		}
		runes := []rune(trimmed)
		if runes[0] >= '0' && runes[0] <= '9' {
			names[strings.ToLower(trimmed)] = true
			continue
		}
		if index == 0 {
			continue
		}
		if strings.ToUpper(string(runes[0])) == string(runes[0]) && strings.ToLower(string(runes[0])) != string(runes[0]) {
			names[strings.ToLower(trimmed)] = true
		}
	}
	return names
}

// cutRunes shortens text to a number of characters without cutting one in
// half: a byte cut through a character embeds a replacement mark instead
// of the word it was part of.
func cutRunes(text string, characters int) string {
	runes := []rune(text)
	if len(runes) <= characters {
		return text
	}
	return string(runes[:characters])
}
