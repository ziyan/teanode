package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// What the fold used to lose: the thing the person actually told their
// agent.
//
// A new fact was put behind an older one whenever the vectors were near
// and a permissive name check passed, and the older wording was the one
// the page kept unless one of the two carried an English negation.
// Nothing about a rent that went up, a review that moved, or a standing
// order that became monthly carries a negation, so the newer statement
// went dormant, normal recall carried last year's, and the person was
// never told.
func TestAChangedAmountOrDateOrFrequencyIsNotATwin(t *testing.T) {
	for _, each := range []struct {
		name  string
		older string
		newer string
	}{
		{"a changed amount",
			"The rent is 4200 a month from March.",
			"The rent is 3100 a month from March."},
		{"an amount the name check would have waved through",
			"Alice invoices 4200 for the Portal work.",
			"Alice invoices 5600 for the Portal work."},
		{"a changed date",
			"The review is on 2026-09-14.",
			"The review is on 2026-09-21."},
		{"a changed month",
			"They move to the Osaka office in March.",
			"They move to the Osaka office in April."},
		{"a changed frequency",
			"The standing order to Alice runs monthly.",
			"The standing order to Alice runs weekly."},
		{"a frequency written as a count",
			"Alice and the Portal team meet twice a week.",
			"Alice and the Portal team meet three times a week."},
	} {
		if !differsInQuantity(each.older, each.newer) {
			t.Errorf("%s: the two disagree and were not seen to", each.name)
		}
		// And so the pair never reaches the fold at all: the newer is
		// left on the page, where recall carries it beside the older.
		if got := whatToFold(
			&models.AgentFact{Text: each.newer, Confidence: 1},
			&models.AgentFact{Text: each.older, Confidence: 1},
		); got != foldKeepBoth {
			t.Errorf("%s: both facts stay, not %d", each.name, got)
		}
	}
}

// Two writings of one sentence still fold, which is the whole point of
// having a fold. A conversation filed today and one filed a week ago
// both say the boat's name the same way, and a page that says it twice
// is a page every answer is drawn differently from.
func TestTheSameSentenceFiledTwiceStillFolds(t *testing.T) {
	for _, each := range []struct {
		name  string
		older string
		newer string
	}{
		{"the same words", "Kittiwake is the neighbour's boat.", "Kittiwake is the neighbour's boat."},
		{"a different case", "Kittiwake is the neighbour's boat.", "kittiwake IS the neighbour's boat."},
		{"a line broken differently", "Kittiwake is the\n neighbour's boat.", "Kittiwake is the neighbour's boat"},
		{"a curly apostrophe and no full stop", "Kittiwake is the neighbour’s boat", "Kittiwake is the neighbour's boat."},
		{"the same figure said twice", "The rent is 4200 a month.", "the rent is 4200 a month"},
	} {
		if got := whatToFold(
			&models.AgentFact{Text: each.newer, Confidence: 1},
			&models.AgentFact{Text: each.older, Confidence: 1},
		); got != foldTheNewerBehindTheOlder {
			t.Errorf("%s: the page says it once, not %d", each.name, got)
		}
	}
	// And a rewording is not folded with nobody watching. It may well be
	// the same statement, and it may be the next thing the page has to
	// say; the pass that asks a model decides, and until it does the
	// person hears both rather than only the older.
	if got := whatToFold(
		&models.AgentFact{Text: "Every spring they repaint Kittiwake, the neighbour's boat.", Confidence: 1},
		&models.AgentFact{Text: "Kittiwake is the neighbour's boat, and they repaint it every spring.", Confidence: 1},
	); got != foldKeepBoth {
		t.Errorf("a rewording is not folded unasked, got %d", got)
	}
}

// A negation is still the one thing that puts an older fact behind a
// newer one -- but only when the newer stands on ground at least as firm.
//
// The evidence check marks a fact whose quote is nowhere in what the run
// was shown as inferred, at half confidence, because the model may have
// composed it out of the gist. Letting that supersede something the
// person said had the agent's own paraphrase win an argument with its
// source, with nobody present to object.
func TestAnInferredContradictionDoesNotSupersedeWhatWasStated(t *testing.T) {
	yesterday := time.Now().Add(-24 * time.Hour)
	stated := &models.AgentFact{
		Text: "She prefers tea.", Confidence: 1, CreatedAt: yesterday,
	}
	inferred := &models.AgentFact{
		Text: "She no longer prefers tea.", Confidence: evidenceInferredConfidence,
		Inferred: true, CreatedAt: time.Now(),
	}
	if got := whatToFold(inferred, stated); got != foldKeepBoth {
		t.Fatalf("a guess does not overrule what was said: %d", got)
	}
	// Said, and the later statement wins as it always did.
	said := &models.AgentFact{
		Text: "She no longer prefers tea.", Confidence: 1, CreatedAt: time.Now(),
	}
	if got := whatToFold(said, stated); got != foldTheOlderBehindTheNewer {
		t.Fatalf("a later statement replaces the earlier one: %d", got)
	}
	// And the older one is never put behind an earlier statement, however
	// it is evidenced.
	older := &models.AgentFact{
		Text: "She prefers tea.", Confidence: 1, CreatedAt: yesterday.Add(-time.Hour),
	}
	if got := whatToFold(older, said); got != foldKeepBoth {
		t.Fatalf("an earlier statement does not replace a later one: %d", got)
	}
}

// A negation that also changes the figure is two changes, and the pair
// is not a twin at all: both rows stay rather than one of them carrying
// the other's amount away with it.
func TestANegationThatAlsoChangesTheAmountKeepsBoth(t *testing.T) {
	if !differsInQuantity("The standing order is 4200 monthly.", "The standing order is no longer 3100 monthly.") {
		t.Fatal("4200 and 3100 are not the same amount")
	}
}
