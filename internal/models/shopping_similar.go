package models

import (
	"strings"
	"unicode"
)

// How close a word has to be to count, and what it is worth: a word that is
// the same scores 1, one that is begun by the other or misspelled a little
// less, so the same word always ranks first.
const (
	shoppingWordSame          = 1.0
	shoppingWordClose         = 0.8
	shoppingWordPrefixLeast   = 3
	shoppingWordTypoLeast     = 5
	shoppingWordTwoTyposLeast = 8
	shoppingCharactersLeast   = 2
)

// Words that say nothing about what an item is.
var shoppingFillerWords = map[string]bool{
	"a": true, "an": true, "and": true, "for": true, "in": true, "of": true, "or": true, "the": true, "to": true, "with": true,
}

// ShoppingItemSimilarity says how alike what somebody is typing and an item's
// description read, from 0 when they share no word to 1 when they are the same
// words in any order. Case, punctuation, a plural, a word only begun and a
// small misspelling make no difference to whether they match; extra words on
// either side lower the score, so "milk" is nearer "Milk" than "Whole milk".
func ShoppingItemSimilarity(typed, description string) float64 {
	typedWords, descriptionWords := shoppingWords(typed), shoppingWords(description)
	if len(typedWords) == 0 || len(descriptionWords) == 0 {
		return 0
	}
	if strings.Join(typedWords, "") == strings.Join(descriptionWords, "") {
		return 1
	}
	isTaken := make([]bool, len(descriptionWords))
	matched := 0.0
	for _, typedWord := range typedWords {
		best, bestIndex := 0.0, -1
		for index, descriptionWord := range descriptionWords {
			if isTaken[index] {
				continue
			}
			if score := shoppingWordScore(typedWord, descriptionWord); score > best {
				best, bestIndex = score, index
			}
		}
		if bestIndex >= 0 {
			isTaken[bestIndex] = true
			matched += best
		}
	}
	return matched / float64(max(len(typedWords), len(descriptionWords)))
}

// shoppingWords are the words of a description in the form they are compared
// in: lower case, singular, without filler and without numbers on their own.
func shoppingWords(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
	words := make([]string, 0, len(fields))
	for _, field := range fields {
		if !shoppingFillerWords[field] && strings.IndexFunc(field, unicode.IsLetter) >= 0 {
			words = append(words, shoppingSingular(field))
		}
	}
	return words
}

// shoppingSingular takes the English plural off a word, so "eggs", "tomatoes"
// and "berries" meet "egg", "tomato" and "berry". Another language's words
// mostly do not end this way, and a word cut short here is only ever compared
// with another cut the same way.
func shoppingSingular(word string) string {
	switch {
	case len(word) < 4 || strings.HasSuffix(word, "ss") || strings.HasSuffix(word, "us"):
		return word
	case strings.HasSuffix(word, "ies"):
		return strings.TrimSuffix(word, "ies") + "y"
	case strings.HasSuffix(word, "oes"), strings.HasSuffix(word, "shes"), strings.HasSuffix(word, "ches"), strings.HasSuffix(word, "xes"):
		return strings.TrimSuffix(word, "es")
	default:
		return strings.TrimSuffix(word, "s")
	}
}

func shoppingWordScore(first, second string) float64 {
	if first == second {
		return shoppingWordSame
	}
	firstCharacters, secondCharacters := []rune(first), []rune(second)
	shorter, longer := first, second
	shorterLength := len(firstCharacters)
	if len(secondCharacters) < shorterLength {
		shorter, longer, shorterLength = second, first, len(secondCharacters)
	}
	// These scripts write no spaces, so a word is a whole phrase: what is
	// asked for may be anywhere in it.
	if isShoppingWordUnspaced(first) || isShoppingWordUnspaced(second) {
		if shorterLength >= shoppingCharactersLeast && strings.Contains(longer, shorter) {
			return shoppingWordClose
		}
		return 0
	}
	if shorterLength >= shoppingWordPrefixLeast && strings.HasPrefix(longer, shorter) {
		return shoppingWordClose
	}
	if shorterLength < shoppingWordTypoLeast || firstCharacters[0] != secondCharacters[0] {
		return 0
	}
	typosAllowed := 1
	if shorterLength >= shoppingWordTwoTyposLeast {
		typosAllowed = 2
	}
	if shoppingEditDistance(firstCharacters, secondCharacters) <= typosAllowed {
		return shoppingWordClose
	}
	return 0
}

func isShoppingWordUnspaced(word string) bool {
	return strings.IndexFunc(word, func(character rune) bool {
		return unicode.In(character, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Thai)
	}) >= 0
}

// shoppingEditDistance counts the letters added, dropped or changed between
// two words.
func shoppingEditDistance(first, second []rune) int {
	previous := make([]int, len(second)+1)
	for index := range previous {
		previous[index] = index
	}
	for firstIndex, firstCharacter := range first {
		current := make([]int, len(second)+1)
		current[0] = firstIndex + 1
		for secondIndex, secondCharacter := range second {
			cost := 1
			if firstCharacter == secondCharacter {
				cost = 0
			}
			current[secondIndex+1] = min(previous[secondIndex+1]+1, current[secondIndex]+1, previous[secondIndex]+cost)
		}
		previous = current
	}
	return previous[len(second)]
}
