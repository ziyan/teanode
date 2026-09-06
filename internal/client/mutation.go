package client

import "strings"

// IsMutationDocument says whether a GraphQL document would change anything:
// whether any operation in it is a mutation.
//
// A read-only client decides with this what to refuse, so it has to be right
// about documents it did not write — "teanode api graphql" sends whatever it
// was given. Comments and strings are skipped, so a keyword inside either
// neither hides a mutation nor fakes one, and only the keyword that opens an
// operation counts: a field called "mutation" inside a selection set is a
// field. A document that opens with a brace is the query shorthand.
func IsMutationDocument(document string) bool {
	tokens := tokenize(document)
	depth := 0
	for _, token := range tokens {
		switch token {
		case "{":
			depth++
		case "}":
			depth--
		default:
			if depth == 0 && token == "mutation" {
				return true
			}
		}
	}
	return false
}

// tokenize splits a document into the pieces IsMutationDocument looks at:
// braces and words, with comments and strings dropped and everything else —
// punctuation, arguments, variables — kept as opaque words that never match.
func tokenize(document string) []string {
	tokens := []string{}
	word := strings.Builder{}
	flush := func() {
		if word.Len() > 0 {
			tokens = append(tokens, word.String())
			word.Reset()
		}
	}

	runes := []rune(document)
	for index := 0; index < len(runes); index++ {
		character := runes[index]
		switch character {
		case '#':
			flush()
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
		case '"':
			flush()
			index = skipString(runes, index)
		case '{', '}':
			flush()
			tokens = append(tokens, string(character))
		case ' ', '\t', '\n', '\r', ',':
			flush()
		case '(', ')', ':', '$', '@', '!', '[', ']', '=':
			// Punctuation ends a word without becoming one worth matching,
			// but is kept so that "(mutation" can never form a word.
			flush()
			tokens = append(tokens, string(character))
		default:
			word.WriteRune(character)
		}
	}
	flush()
	return tokens
}

// skipString returns the index of the closing quote of the string starting
// at runes[start], handling block strings ("""...""") and escapes.
func skipString(runes []rune, start int) int {
	if start+2 < len(runes) && runes[start+1] == '"' && runes[start+2] == '"' {
		for index := start + 3; index+2 < len(runes); index++ {
			if runes[index] == '"' && runes[index+1] == '"' && runes[index+2] == '"' && runes[index-1] != '\\' {
				return index + 2
			}
		}
		return len(runes)
	}
	for index := start + 1; index < len(runes); index++ {
		switch runes[index] {
		case '\\':
			index++
		case '"':
			return index
		}
	}
	return len(runes)
}
