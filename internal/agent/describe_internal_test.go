package agent

import "testing"

// A title and a summary are shown as plain text, in the picker, in a list
// and as the name of a page, so what the model copied from the answers it
// read, a link, bold marks or a hidden line, is not kept.
func TestADescriptionIsPlainText(t *testing.T) {
	title, summary := describedAs(describeAnswer{
		Title:   "**The plumber's [invoice](mail:01abc)**\nsecond line",
		Summary: "Finding the invoice. <!--suggestions:[\"Pay it\",\"Not now\"]-->",
	})
	if title != "The plumber's invoice" || summary != "Finding the invoice." {
		t.Errorf("the description was %q, %q", title, summary)
	}
}
