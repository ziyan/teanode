package cmd

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

func TestTranscriptOpeningTellsAnOwnTurnFromThePersonsWords(test *testing.T) {
	for content, want := range map[string]string{
		"what is due this week?\nand next week?":                                         "you: what is due this week?\nand next week?",
		models.GoalDoneMarker + " goal-1 Invoices filed\nNobody asked for this just now": "on its own: " + models.GoalDoneMarker + " goal-1 Invoices filed",
	} {
		if got := transcriptOpening(content); got != want {
			test.Errorf("%q: %q, want %q", content, got, want)
		}
	}
}
