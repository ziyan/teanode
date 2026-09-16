package db

import "strings"

// The words of a search that carry meaning.
//
// Postgres's `simple` dictionary is what these searches index with,
// because it is the only one that does not stem -- and stemming a graph
// of names and paths does more harm than good. But `simple` also drops
// nothing, so *the*, *what* and *is* are lexemes like any other.
//
// That makes both ways of joining the words wrong on their own. Joining
// with AND asks a page called Portal to also hold *what*, *is* and *the*,
// so every question phrased as a sentence finds nothing. Joining with OR
// matches anything whose summary happens to contain *the*, so everything
// finds something -- which is worse, because a search that always answers
// is one nobody can tell is broken.
//
// So the words that say nothing are dropped here, before the query is
// built, and what is left is joined with OR and ranked.

// searchStopWords are the words a question is made of rather than about.
//
// Short on purpose. Anything that could be part of a name, a project or a
// place stays: "will" is a person, "may" is a month, "can" is a thing on
// a shelf. The cost of keeping one is a slightly worse ranking; the cost
// of dropping one is a page nobody can find by its own name.
var searchStopWords = map[string]bool{
	"a": true, "about": true, "after": true, "all": true, "am": true, "an": true,
	"and": true, "any": true, "anything": true, "are": true, "as": true, "at": true,
	"be": true, "been": true, "being": true, "but": true, "by": true,
	"did": true, "do": true, "does": true, "doing": true, "done": true,
	"for": true, "from": true, "get": true, "got": true, "had": true, "has": true,
	"have": true, "he": true, "her": true, "hers": true, "him": true, "his": true,
	"how": true, "i": true, "if": true, "in": true, "into": true, "is": true,
	"it": true, "its": true, "me": true, "mine": true, "my": true, "of": true,
	"on": true, "once": true, "one": true, "or": true, "our": true, "ours": true,
	"out": true, "over": true, "she": true, "so": true, "some": true,
	"something": true, "that": true, "the": true, "their": true, "theirs": true,
	"them": true, "then": true, "there": true, "these": true, "they": true,
	"this": true, "those": true, "to": true, "up": true, "us": true, "was": true,
	"we": true, "were": true, "what": true, "when": true, "where": true,
	"which": true, "who": true, "whom": true, "whose": true, "why": true,
	"with": true, "you": true, "your": true, "yours": true,
}

// SearchText is the part of a query worth searching for.
//
// Everything that is not a word the question is made of, in the order it
// was typed. Empty when the query was nothing but those, and the caller
// then searches for what was typed rather than for nothing: a person who
// types "who" alone means it.
func SearchText(query string) string {
	fields := strings.FieldsFunc(query, func(letter rune) bool {
		switch {
		case letter >= 'a' && letter <= 'z', letter >= 'A' && letter <= 'Z':
			return false
		case letter >= '0' && letter <= '9':
			return false
		case letter == '-' || letter == '_' || letter == '/' || letter == '.':
			// Kept: a path, an identifier and a version are one word each.
			return false
		case letter > 127:
			// Kept: whatever this is, it is not one of the words above.
			return false
		}
		return true
	})
	kept := make([]string, 0, len(fields))
	for _, field := range fields {
		if searchStopWords[strings.ToLower(field)] {
			continue
		}
		kept = append(kept, field)
	}
	if len(kept) == 0 {
		return strings.TrimSpace(query)
	}
	return strings.Join(kept, " ")
}
