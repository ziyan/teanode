package llm

import "testing"

// A cut-off answer is told from a whole one, including when its strings
// hold brackets and escaped quotes, and when it arrives in a fence.
func TestACutOffAnswerIsNotCompleteJSON(t *testing.T) {
	for _, each := range []struct {
		text     string
		isCutOff bool
	}{
		{`{"facts": []}`, false},
		{`Here it is: {"facts": [{"text": "a } and a \" inside"}]} and that is all`, false},
		{"```json\n{\"facts\": []}\n```", false},
		{`{"facts":`, true},
		{`{"facts": [{"text": "one"}, {"text": "tw`, true},
		{`{"facts": [{"text": "a } inside"}]`, true},
		{"```json\n{\"facts\": [\n```", true},
		{`no object here`, false},
	} {
		if got := isCutOff(each.text); got != each.isCutOff {
			t.Errorf("%q: cut off %v, want %v", each.text, got, each.isCutOff)
		}
		if _, err := ExtractCompleteJSON(each.text); each.isCutOff && err == nil {
			t.Errorf("%q: a cut-off answer was taken as complete", each.text)
		}
	}
}
