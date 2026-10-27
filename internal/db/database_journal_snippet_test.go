package db

import (
	"strings"
	"testing"
)

func TestJournalSnippetCentersOnTheFirstMatch(t *testing.T) {
	long := strings.Repeat("word ", 60) + "Harbor" + strings.Repeat(" more", 60)
	snippet := journalSnippet("harbor", "no match here", long)
	if !strings.HasPrefix(snippet, "…") || !strings.HasSuffix(snippet, "…") || !strings.Contains(snippet, "Harbor") {
		t.Errorf("a long text is cut on both sides around the match: %q", snippet)
	}
	if length := len([]rune(snippet)); length < 150 || length > 165 {
		t.Errorf("about 160 characters, not %d", length)
	}

	if got := journalSnippet("harbor", "Harbor at the start of a text that goes on "+strings.Repeat("and on ", 40)); !strings.HasPrefix(got, "Harbor at") || strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("a match at the start: %q", got)
	}
	if got := journalSnippet("harbor", "Line one\n\nthe   harbor\tends"); got != "Line one the harbor ends" {
		t.Errorf("white space is collapsed: %q", got)
	}
	// A character whose lowercase has another width must not move the cut.
	if got := journalSnippet("hafen", "İstanbul und der Hafen"); got != "İstanbul und der Hafen" {
		t.Errorf("a letter that changes width when lowercased: %q", got)
	}
	if got := journalSnippet("absent", "", "only text"); got != "only text" {
		t.Errorf("no match shows the start of the first text: %q", got)
	}
}
