package models

import "testing"

func TestFillerIsOnlyTheWordsThatCarryNothing(test *testing.T) {
	for _, text := range []string{"Mm-hmm. Okay. Okay.", "Hmm", "Yeah.", "uh, um", "Hahaha!", "OK", "嗯", "哈哈哈", "嗯嗯，好", "うん", "えー", "Uh-huh."} {
		if !IsFillerTranscriptText(text) {
			test.Errorf("%q is filler", text)
		}
	}
	for _, text := range []string{"Yes.", "No.", "Right?", "Okay?", "So?", "Cool.", "对", "はい", "네", "42", "Okay, let's ship it.", "So the shelf is full.", "", "  ", "好的", "我知道", "Hmm, the crate is late."} {
		if IsFillerTranscriptText(text) {
			test.Errorf("%q is not filler", text)
		}
	}
}
