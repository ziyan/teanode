package agent

import "testing"

// An alert's card says its first sentence whole as the headline and the rest
// below it, never a headline cut in the middle of a word.
func TestAnAlertCardsHeadlineIsItsFirstSentenceWhole(t *testing.T) {
	for _, example := range []struct{ alertText, title, body string }{
		{"Dining out has reached $512.40 this month, $12.40 over the $500 budget. At this pace it reaches $640.10 by the end of the month.", "Dining out has reached $512.40 this month, $12.40 over the $500 budget.", "At this pace it reaches $640.10 by the end of the month."},
		{"The parcel from the hardware shop is held at the depot.", "The parcel from the hardware shop is held at the depot.", ""},
		{"Rain is coming, e.g. a storm tonight! Bring the cushions in.", "Rain is coming, e.g. a storm tonight!", "Bring the cushions in."},
		{"  ", "Something to know", ""},
	} {
		title, body := alertCardHeadline(example.alertText)
		if title != example.title || body != example.body {
			t.Errorf("%q: got %q / %q", example.alertText, title, body)
		}
	}
}
