package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

// requestTouchUp asks for a day's words to be touched up, as the API does.
func (self *journalFixture) requestTouchUp(t *testing.T, day, how string) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if err := tx.RequestJournalTouchUp(self.run.Owner.ID, models.JournalTarget{Day: day}, journal.Zone(self.run.Owner).String(), &how, time.Now()); err != nil {
			t.Fatalf("RequestJournalTouchUp: %s", err)
		}
	})
}

// requestNarrative asks for a day's narrative, as the API does.
func (self *journalFixture) requestNarrative(t *testing.T, day string) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if err := tx.RequestJournalNarrative(self.run.Owner.ID, models.JournalTarget{Day: day}, journal.Zone(self.run.Owner).String(), time.Now()); err != nil {
			t.Fatalf("RequestJournalNarrative: %s", err)
		}
	})
}

func (self *journalFixture) waiting(t *testing.T) (touchUps, narratives []models.JournalTarget) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if touchUps, err = tx.ListJournalTouchUpsWaiting(self.run.Owner.ID); err != nil {
			t.Fatal(err)
		}
		if narratives, err = tx.ListJournalNarrativesWaiting(self.run.Owner.ID); err != nil {
			t.Fatal(err)
		}
	})
	return touchUps, narratives
}

// A touch-up is made from the original and what the person asked for, and
// stored with the digest of both, so that it is known to be made from those
// words; the original is untouched, and nothing waits afterward.
func TestATouchUpIsStoredWithTheDigestOfTheOriginalAndHow(t *testing.T) {
	fixture := newJournalFixture(t, func(string) string { return "Planted the pear tree today." })
	day := fixture.dayAgo(1)
	original := "planted teh pear tree today"
	fixture.words(t, day, original)
	fixture.requestTouchUp(t, day, "fix the grammar")

	isMade, err := fixture.worker.touchUpJournalWords(t.Context(), fixture.run, models.JournalTarget{Day: day})
	if err != nil || !isMade {
		t.Fatalf("touchUpJournalWords: %v %v", isMade, err)
	}
	prompt := fixture.prompts()[0]
	if !strings.Contains(prompt, original) || !strings.Contains(prompt, "fix the grammar") || !strings.Contains(prompt, "Keep their meaning, their voice and their language") {
		t.Errorf("the prompt: %s", prompt)
	}
	row := fixture.day(t, day)
	if row.TouchedUpWords == nil || *row.TouchedUpWords != "Planted the pear tree today." {
		t.Fatalf("the touched-up words: %v", row.TouchedUpWords)
	}
	if row.TouchedUpFromDigest != models.JournalWordsDigest(original, "fix the grammar") || row.Words != original || row.TouchUpRequestedAt != nil {
		t.Errorf("the stored day: %+v", row)
	}
	if touchUps, _ := fixture.waiting(t); len(touchUps) != 0 {
		t.Errorf("still waiting: %v", touchUps)
	}
}

// With no instruction the default is stated in the prompt, and the answer is
// cleaned of the code fence a model sometimes wraps it in.
func TestATouchUpWithNoInstructionFixesGrammarAndFlow(t *testing.T) {
	fixture := newJournalFixture(t, func(string) string { return "```\nA clean line.\n```" })
	day := fixture.dayAgo(1)
	fixture.words(t, day, "a clean line")
	fixture.requestTouchUp(t, day, "")
	if _, err := fixture.worker.touchUpJournalWords(t.Context(), fixture.run, models.JournalTarget{Day: day}); err != nil {
		t.Fatal(err)
	}
	if prompt := fixture.prompts()[0]; !strings.Contains(prompt, "fix the grammar and the flow and keep it about the same length") {
		t.Errorf("the default instruction is not in the prompt: %s", prompt)
	}
	if row := fixture.day(t, day); row.TouchedUpWords == nil || *row.TouchedUpWords != "A clean line." || row.TouchedUpFromDigest != models.JournalWordsDigest("a clean line", "") {
		t.Errorf("the stored day: %+v", row)
	}
	if cleaned := cleanTouchedUpWords("```markdown\nOne.\n\nTwo.\n```"); cleaned != "One.\n\nTwo." {
		t.Errorf("a fence with a language: %q", cleaned)
	}
}

// The job makes every touch-up and narrative that waits, looks again for what
// was asked for while it worked, and ends when nothing waits.
func TestTheJobLoopsUntilNothingWaits(t *testing.T) {
	var fixture *journalFixture
	fixture = newJournalFixture(t, func(prompt string) string {
		if strings.Contains(prompt, "'s day, ") && strings.Contains(prompt, "marker-two") {
			// The person asks for another day while this one is being written.
			fixture.requestNarrative(t, fixture.dayAgo(3))
		}
		if strings.Contains(prompt, "Touch up") {
			return "A touched-up first day."
		}
		return plainPage
	})
	fixture.words(t, fixture.dayAgo(1), "marker-one")
	fixture.words(t, fixture.dayAgo(2), "marker-two")
	fixture.words(t, fixture.dayAgo(3), "marker-three")
	fixture.requestTouchUp(t, fixture.dayAgo(1), "shorter")
	fixture.requestNarrative(t, fixture.dayAgo(2))

	if err := fixture.worker.runJournal(t.Context(), fixture.run); err != nil {
		t.Fatalf("runJournal: %s", err)
	}
	if touchUps, narratives := fixture.waiting(t); len(touchUps) != 0 || len(narratives) != 0 {
		t.Fatalf("still waiting: %v %v", touchUps, narratives)
	}
	if row := fixture.day(t, fixture.dayAgo(1)); row.TouchedUpWords == nil || *row.TouchedUpWords != "A touched-up first day." {
		t.Errorf("the touch-up: %+v", row)
	}
	for _, count := range []int{2, 3} {
		if row := fixture.day(t, fixture.dayAgo(count)); row.Narrative == "" || row.Headline != "A quiet day" {
			t.Errorf("the narrative of the day %d days ago: %+v", count, row)
		}
	}
	if len(fixture.dayPrompts()) != 2 {
		t.Errorf("%d days were written, not two", len(fixture.dayPrompts()))
	}
}

// Asked for again and again, the job stops after five rounds and hands what is
// left back to the queue, to start shortly, rather than lose it.
func TestTheJobHandsBackWhatIsStillAskedForAfterFiveRounds(t *testing.T) {
	var fixture *journalFixture
	fixture = newJournalFixture(t, func(prompt string) string {
		for count := 1; count < 12; count++ {
			if strings.Contains(prompt, fmt.Sprintf("marker-%02d", count)) {
				fixture.requestNarrative(t, fixture.dayAgo(count+1))
			}
		}
		return plainPage
	})
	for count := 1; count <= 12; count++ {
		fixture.words(t, fixture.dayAgo(count), fmt.Sprintf("marker-%02d", count))
	}
	fixture.requestNarrative(t, fixture.dayAgo(1))

	err := fixture.worker.runJournal(t.Context(), fixture.run)
	var deferral *Deferral
	if !errors.As(err, &deferral) || time.Until(deferral.Until) > time.Minute+time.Second {
		t.Fatalf("runJournal answered %v, not a short deferral", err)
	}
	if len(fixture.dayPrompts()) != journalRoundsMost {
		t.Errorf("%d days were written in %d rounds", len(fixture.dayPrompts()), journalRoundsMost)
	}
	if _, narratives := fixture.waiting(t); len(narratives) != 1 {
		t.Errorf("what is still asked for was lost: %v", narratives)
	}
}

// A person who no longer holds journal:use has nothing made for them.
func TestTheJobMakesNothingForAPersonWithoutTheJournal(t *testing.T) {
	fixture := newJournalFixture(t, nil, models.PermissionAgentUse)
	day := fixture.dayAgo(1)
	fixture.words(t, day, "marker")
	fixture.requestNarrative(t, day)
	if err := fixture.worker.runJournal(t.Context(), fixture.run); err != nil {
		t.Fatal(err)
	}
	if len(fixture.prompts()) != 0 {
		t.Errorf("the model was asked %d times", len(fixture.prompts()))
	}
}
