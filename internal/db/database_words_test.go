package db

import "testing"

// A question is mostly words the question is made of. Searching for
// those finds everything, and requiring them finds nothing.
func TestSearchTextKeepsWhatTheQuestionIsAbout(t *testing.T) {
	for _, row := range []struct {
		query string
		want  string
	}{
		{"what is the portal?", "portal"},
		{"who did the payload angle deviation feature?", "payload angle deviation feature"},
		// A citation splits at the hash, which is what the text search
		// parser would have done with it anyway.
		{"projects/portal#2", "projects/portal 2"},
		{"Alice Chen", "Alice Chen"},
		{"v1.2.3 and node-red", "v1.2.3 node-red"},
		// Nothing but the words a question is made of: what was typed is
		// what is searched for, because somebody who types "who" alone
		// means it.
		{"who", "who"},
		{"what is this", "what is this"},
		// Not English, and not this list's business either.
		{"払い戻しはいつですか", "払い戻しはいつですか"},
	} {
		if got := SearchText(row.query); got != row.want {
			t.Errorf("SearchText(%q) = %q, want %q", row.query, got, row.want)
		}
	}
}

// A name that could be mistaken for one of those words is kept: the cost
// of dropping one is a page nobody can find by its own name.
func TestSearchTextKeepsWordsThatAreAlsoNames(t *testing.T) {
	for _, word := range []string{"Will", "May", "Can", "Mark", "Bill", "Rose", "Art"} {
		if got := SearchText(word); got != word {
			t.Errorf("SearchText(%q) = %q; a name is not a stop word", word, got)
		}
	}
}
