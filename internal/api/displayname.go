package api

import (
	"strings"
	"unicode"
)

// DisplayName is a name a program or a device gave itself, made safe to put
// in front of a person who is deciding whether to trust it.
//
// Control characters and invisible formatting characters are dropped: a
// right-to-left override (U+202E) can make "ppA edoNaeT" read as something
// else, and a zero-width space can make two names that look the same differ.
// Runs of spaces and line breaks become one space, so a name is one line.
// It is cut to longest characters, never through one.
func DisplayName(name string, longest int) string {
	cleaned := strings.Map(func(character rune) rune {
		switch {
		case unicode.IsSpace(character):
			return ' '
		case unicode.Is(unicode.Cc, character), unicode.Is(unicode.Cf, character):
			return -1
		}
		return character
	}, name)
	runes := []rune(strings.Join(strings.Fields(cleaned), " "))
	if len(runes) > longest {
		runes = runes[:longest]
	}
	return strings.TrimSpace(string(runes))
}
