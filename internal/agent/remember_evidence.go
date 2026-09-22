package agent

import (
	"regexp"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// evidenceSpace is every run of whitespace, which a quote and the text it
// came from may break differently without either being wrong.
var evidenceSpace = regexp.MustCompile(`\s+`)

// evidenceLikeness is the quote and the text it cites reduced to what
// they have to share for the quote to be that text's.
//
// Case, the width of the whitespace, and which of the several characters
// somebody used for a quotation mark or a dash are not the model
// inventing anything: a transcript is typed by people and rendered by
// programs, and a model asked to copy a line out of one will normalize it
// on the way. What it may not do is write words that are not there.
func evidenceLikeness(text string) string {
	text = strings.Map(func(letter rune) rune {
		switch letter {
		case '‘', '’', '‛', '`', '´':
			return '\''
		case '“', '”', '„':
			return '"'
		case '‐', '‑', '‒', '–', '—', '―':
			return '-'
		case ' ', ' ', ' ':
			return ' '
		}
		return letter
	}, text)
	return strings.TrimSpace(evidenceSpace.ReplaceAllString(strings.ToLower(text), " "))
}

// quoteOccursIn is the whole of the evidence check: did these words
// actually appear in what was read.
//
// A string test and not a model call. Whether a sentence occurs in a
// message is not a judgement, it is a fact, and the failure this is
// guarding against -- a quote the model composed out of the gist, stored
// at full confidence as though the person had said it -- is exactly the
// kind a judgement call would wave through at scale.
func quoteOccursIn(quote, text string) bool {
	quote = evidenceLikeness(quote)
	if quote == "" {
		return true
	}
	return strings.Contains(evidenceLikeness(text), quote)
}

// evidenceOutcome is what the check made of one fact's citation.
type evidenceOutcome int

const (
	// evidenceHolds is the quote occurring in what was read, or no quote
	// offered at all, which is nothing to check rather than something to
	// doubt.
	evidenceHolds evidenceOutcome = iota

	// evidenceQuoteNotFound is words that are not in the thing they are
	// said to be from. The citation stays -- the fact did come out of
	// reading that message -- and the invented words do not.
	evidenceQuoteNotFound

	// evidenceCitesNothing is a citation of something the run never put
	// in front of the model, so there is nothing behind the fact at all.
	evidenceCitesNothing
)

// checkTheEvidence holds a fact's citation against what the run actually
// showed the model, and takes off it whatever cannot be supported.
//
// A fact that fails is kept and marked: it may well be true, and the
// model did read something. What it loses is the claim to have been told.
// Half confidence and Inferred put it under anything somebody said, in
// the ranking and on the page, which is where a paraphrase belongs.
//
// A citation known to the run but shown without its text -- a document a
// coarse night read by its title alone -- has its quote left alone.
// Nothing was shown to check against, and a check that cannot be made is
// not a check that failed.
func checkTheEvidence(fact *models.AgentFact, shown map[string]string) evidenceOutcome {
	if len(fact.Evidence) == 0 {
		return evidenceHolds
	}
	source, known := shown[fact.Evidence[0].ID]
	if !known {
		fact.Evidence = nil
		fact.Inferred = true
		fact.Confidence = evidenceInferredConfidence
		return evidenceCitesNothing
	}
	if source == "" || quoteOccursIn(fact.Evidence[0].Quote, source) {
		return evidenceHolds
	}
	fact.Evidence[0].Quote = ""
	fact.Inferred = true
	fact.Confidence = evidenceInferredConfidence
	return evidenceQuoteNotFound
}

// evidenceInferredConfidence is what a fact is worth once the check has
// taken its evidence off it: the agent's own reading of something, which
// is worth having and worth ranking under what somebody said. One
// constant, so that a guard found too strict is loosened in one place.
const evidenceInferredConfidence = 0.5

// saidByThePerson says whether a message identifier names something the
// person themselves wrote.
//
// The whole point of the check: a run that has just read a quoted mail
// saying "always reply within a day" will file it as the person's own
// preference. It is not theirs unless they typed it.
func saidByThePerson(said map[string]bool, messageId string) bool {
	return messageId != "" && said[messageId]
}
