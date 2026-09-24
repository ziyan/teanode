package models

import "testing"

// The hidden suggested replies come off an answer for anywhere but the
// dashboard, whole or half written, and nothing else does.
func TestStripSuggestedReplies(t *testing.T) {
	for _, each := range []struct{ given, want string }{
		{"Which one?\n<!--suggestions:[\"Red\",\"Blue\"]-->", "Which one?"},
		{"Which one?\n<!--suggestions:[\"Red\",\"Blu", "Which one?"},
		{"Which one?\n<!--sugg", "Which one?\n<!--sugg"},
		{"No marker here.", "No marker here."},
		{"An HTML comment <!-- note --> stays.", "An HTML comment <!-- note --> stays."},
	} {
		if got := StripSuggestedReplies(each.given); got != each.want {
			t.Errorf("%q: got %q, want %q", each.given, got, each.want)
		}
	}
}
