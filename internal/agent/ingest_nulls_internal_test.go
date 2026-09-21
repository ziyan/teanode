package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/computer"
)

// A NUL in what a source read loses the whole document.
//
// Postgres will not put 0x00 in a text column or in jsonb, and a NUL is
// valid UTF-8, so every check between the file and the insert passes and
// the insert is what fails. The document is then dropped with a warning
// and dropped again on the next pass, for ever. The byte means nothing in
// text somebody reads; what is around it does.
func TestADocumentIsKeptWhenItsTextCarriesANull(test *testing.T) {
	entry := computer.ScanEntry{
		Title: "ledger\x00.jsonl",
		Text:  "a line\x00and the next",
		URL:   "https://example.invalid/a\x00b",
		Metadata: map[string]any{
			"path":  "~/Drive/ledger\x00.jsonl",
			"depth": float64(3),
			"tags":  []any{"one\x00", "two"},
			"about": map[string]any{"who": "them\x00"},
		},
	}
	takeTheNullsOut(&entry)

	if entry.Title != "ledger.jsonl" {
		test.Errorf("the title keeps its words and loses the byte: %q", entry.Title)
	}
	if entry.Text != "a lineand the next" {
		test.Errorf("the text keeps its words and loses the byte: %q", entry.Text)
	}
	if entry.URL != "https://example.invalid/ab" {
		test.Errorf("the link keeps its words and loses the byte: %q", entry.URL)
	}
	if got := entry.Metadata["path"]; got != "~/Drive/ledger.jsonl" {
		test.Errorf("the metadata is jsonb and cannot hold one either: %q", got)
	}
	if got := entry.Metadata["depth"]; got != float64(3) {
		test.Errorf("what is not a string is left alone: %v", got)
	}
	if got := entry.Metadata["tags"].([]any); got[0] != "one" || got[1] != "two" {
		test.Errorf("inside a list as well: %v", got)
	}
	if got := entry.Metadata["about"].(map[string]any); got["who"] != "them" {
		test.Errorf("and inside a map: %v", got)
	}
}
