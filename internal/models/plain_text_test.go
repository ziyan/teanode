package models

import "testing"

// Everything only the dashboard and the app read comes off an answer for
// anywhere else, and code that shows a comment as an example keeps it.
func TestStripHiddenMarkers(t *testing.T) {
	for name, testCase := range map[string]struct{ given, want string }{
		"suggested replies":       {"Which one?\n<!--suggestions:[\"Red\",\"Blue\"]-->", "Which one?"},
		"another comment":         {"Saved. <!-- kept: 3 --> Anything else?", "Saved.  Anything else?"},
		"a comment cut off":       {"Working on it <!--sugg", "Working on it"},
		"a comment over lines":    {"Done.\n<!--\nnote\n-->\nBye.", "Done.\n\nBye."},
		"a comment in a fence":    {"Use:\n```html\n<!-- a note -->\n```\nOkay.", "Use:\n```html\n<!-- a note -->\n```\nOkay."},
		"a comment in a span":     {"Write `<!-- a note -->` to comment.", "Write `<!-- a note -->` to comment."},
		"a comment after a fence": {"```\ncode\n```\n<!--suggestions:[\"A\",\"B\"]-->", "```\ncode\n```"},
		"nothing hidden":          {"No marker here.", "No marker here."},
	} {
		if got := StripHiddenMarkers(testCase.given); got != testCase.want {
			t.Errorf("%s: StripHiddenMarkers(%q) = %q, want %q", name, testCase.given, got, testCase.want)
		}
	}
}

// A surface with no Markdown shows an answer's words: not what is hidden,
// not a link's target, not the marks around bold and code.
func TestPlainText(t *testing.T) {
	for name, testCase := range map[string]struct{ given, want string }{
		"suggested replies": {"Done ✅\n<!--suggestions:[\"Thanks\",\"Not now\"]-->", "Done ✅"},
		"a link":            {"See [the invoice](mail:01abc) from the plumber.", "See the invoice from the plumber."},
		"a picture":         {"Here it is: ![the driveway](https://example.org/a.jpg)", "Here it is: the driveway"},
		"bold and code":     {"**Paid** `INV-1`", "Paid INV-1"},
		"lines kept":        {"One\nTwo", "One\nTwo"},
	} {
		if got := PlainText(testCase.given); got != testCase.want {
			t.Errorf("%s: PlainText(%q) = %q, want %q", name, testCase.given, got, testCase.want)
		}
	}
}
