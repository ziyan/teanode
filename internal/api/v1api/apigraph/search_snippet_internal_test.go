package apigraph

import "testing"

func TestASnippetLeavesOutTheMarksOfMarkdownLines(test *testing.T) {
	test.Parallel()
	for text, want := range map[string]string{
		"## A heading\nThe words under it.": "A heading The words under it.",
		"- one\n- two\n3. three":           "one two three",
		"> quoted words":                   "quoted words",
		"A line with # inside, kept":       "A line with # inside, kept",
		"#hashtag stays":                   "#hashtag stays",
	} {
		if got := snippetOf(text); got != want {
			test.Errorf("snippetOf(%q) = %q, want %q", text, got, want)
		}
	}
}
