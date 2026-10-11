// Package emoji tells whether a piece of text is nothing but emoji, and
// splits it into the ones it holds.
//
// It reads the text as the emoji sequences of Unicode Technical Standard
// #51 rather than as general grapheme clusters, because the question is
// never "how many characters" but "is every one of them an emoji": a
// sequence that does not parse is refused where it stands. For text that
// is emoji the two splits agree -- a flag is two regional indicators, a
// keycap a base and its mark, a family the people joined by zero width
// joiners -- so what this calls one emoji is what a reader sees as one.
//
// The properties come from the Unicode Consortium's emoji-data.txt, made
// into tables.go by tables_generate.go.
package emoji

import (
	"errors"
	"unicode"
)

// The code points that join, select or mark inside an emoji sequence.
const (
	zeroWidthJoiner        = '\u200d'
	emojiPresentation      = '\ufe0f'
	textPresentation       = '\ufe0e'
	combiningEnclosingKey  = '\u20e3'
	tagFirst               = '\U000E0020'
	tagLast                = '\U000E007E'
	cancelTag              = '\U000E007F'
	regionalIndicatorFirst = '\U0001F1E6'
	regionalIndicatorLast  = '\U0001F1FF'
)

// ErrNotEmoji is what Split answers for text with anything in it that is
// not an emoji: a letter, a symbol drawn as text, a stray joiner.
var ErrNotEmoji = errors.New("not an emoji")

// Split is the emoji the text is made of, in order, or ErrNotEmoji when
// any part of it is not one. Empty text is no emoji and no error.
func Split(text string) ([]string, error) {
	runes := []rune(text)
	var found []string
	for start := 0; start < len(runes); {
		end, isEmoji := sequenceAt(runes, start)
		if !isEmoji {
			return nil, ErrNotEmoji
		}
		found = append(found, string(runes[start:end]))
		start = end
	}
	return found, nil
}

// sequenceAt reads one emoji from runes[start:], and says where it ends.
func sequenceAt(runes []rune, start int) (int, bool) {
	first := runes[start]
	// A flag is a pair of regional indicators, and one alone is a letter
	// in a box, not a flag.
	if isRegionalIndicator(first) {
		if start+1 < len(runes) && isRegionalIndicator(runes[start+1]) {
			return start + 2, true
		}
		return start, false
	}
	// A keycap: a digit, # or *, the emoji selector if it is there, and
	// the mark that draws the key around it. The digit alone is a digit.
	if isKeycapBase(first) {
		next := start + 1
		if next < len(runes) && runes[next] == emojiPresentation {
			next++
		}
		if next < len(runes) && runes[next] == combiningEnclosingKey {
			return next + 1, true
		}
		return start, false
	}
	end, isPresented := elementAt(runes, start)
	if end == start {
		return start, false
	}
	elementCount := 1
	for end+1 < len(runes) && runes[end] == zeroWidthJoiner {
		following, isFollowingPresented := elementAt(runes, end+1)
		if following == end+1 {
			return start, false
		}
		end = following
		isPresented = isPresented || isFollowingPresented
		elementCount++
	}
	// One code point that is an emoji only when asked to be -- a
	// copyright sign, an arrow, a heart written plainly -- is text unless
	// something says otherwise. Joined to others it is part of an emoji
	// whether or not each piece carries its own selector.
	if elementCount == 1 && !isPresented {
		return start, false
	}
	return end, true
}

// elementAt reads one piece of a sequence: an emoji, then a skin tone or
// the emoji selector, then a run of tags such as the ones that make a
// flag of a subdivision. It returns start when there is none there, and
// whether the piece asks to be drawn as an emoji.
func elementAt(runes []rune, start int) (int, bool) {
	base := runes[start]
	if !unicode.Is(emojiTable, base) || isKeycapBase(base) || isRegionalIndicator(base) {
		return start, false
	}
	isPresented := unicode.Is(emojiPresentationTable, base)
	next := start + 1
	switch {
	case next < len(runes) && runes[next] == emojiPresentation:
		isPresented = true
		next++
	case next < len(runes) && runes[next] == textPresentation:
		// Asked to be drawn as text, which is not a reaction anybody sees
		// as an emoji.
		return start, false
	case next < len(runes) && unicode.Is(emojiModifierTable, runes[next]) && unicode.Is(emojiModifierBaseTable, base):
		isPresented = true
		next++
	}
	if next < len(runes) && runes[next] >= tagFirst && runes[next] <= tagLast {
		for next < len(runes) && runes[next] >= tagFirst && runes[next] <= tagLast {
			next++
		}
		if next >= len(runes) || runes[next] != cancelTag {
			return start, false
		}
		isPresented = true
		next++
	}
	return next, isPresented
}

func isRegionalIndicator(character rune) bool {
	return character >= regionalIndicatorFirst && character <= regionalIndicatorLast
}

func isKeycapBase(character rune) bool {
	return character == '#' || character == '*' || (character >= '0' && character <= '9')
}
