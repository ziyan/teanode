package agent

import (
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// supersedeWhatWasReplaced strikes the facts an answer says it has
// replaced.
//
// In the same transaction as the facts that replace them, because a
// strike that lands without its replacement is the one shape of this
// that loses something: the page stops saying the old thing and never
// starts saying the new one.
//
// filed is what each of the answer's facts became, by its place in the
// answer; theirWords, when given, is the messages the person wrote, which
// are the only ones a retraction may quote. Nil for documents, where
// every item shown is a source.
func supersedeWhatWasReplaced(tx db.Transaction, agentId string, supersedes []SupersededFact, filed map[int]*models.AgentFact, shown map[string]string, theirWords map[string]bool) error {
	for _, superseded := range supersedes {
		path := models.NormalizePath(superseded.Path)
		if path == "" || superseded.Number <= 0 {
			continue
		}
		node, err := tx.GetAgentNode(agentId, path)
		if err != nil {
			return fmt.Errorf("reading %q: %w", path, err)
		}
		if node == nil {
			continue
		}
		fact, err := tx.GetAgentFact(agentId, node.ID, superseded.Number)
		if err != nil {
			return fmt.Errorf("reading %s#%d: %w", path, superseded.Number, err)
		}
		if fact == nil {
			continue
		}
		// Nothing is retired on the strength of being named.
		//
		// The automatic fold already refuses to put an older statement
		// behind a newer one that stands on softer ground -- a fact whose
		// quote could not be found in what the run was shown is marked
		// inferred at half confidence exactly so that it cannot win an
		// argument with its source. This path went round that. An answer
		// naming a fact in `supersedes` struck it whatever else the answer
		// did, so a run that invented a citation could still retire the
		// line the person had stated, and an answer carrying nothing but
		// `supersedes` could empty a page without filing a word.
		//
		// So a supersession has to be one of two things. Either it names
		// the fact in this answer that replaces the line, and that fact
		// can -- see replacementHolds -- or it carries the person's own
		// words saying the line is done with, which is a retraction.
		// Neither, and the line stays and the run says why: a duplicate
		// costs a line on a page, a wrong retirement costs the truth.
		var replacement *models.AgentFact
		if superseded.ReplacedBy != nil {
			replacement = filed[*superseded.ReplacedBy-1]
		}
		if !replacementHolds(fact, replacement) && !retractionHolds(superseded, shown, theirWords) {
			log.Noticef("not superseding %s#%d: no replacement that can stand in for it was named, and nothing the person said retracts it",
				path, superseded.Number)
			continue
		}
		// Marked, never deleted: what it said is still readable, and
		// a page that was rewritten can be read back.
		//
		// Struck rather than updated to dormant. An ordinary update
		// that happens to set dormant files nothing in the page's
		// history -- that is what the nightly retirement pass wants
		// -- so a run that took a line off a page this way left no
		// trace of having done it, and a judgement the person cannot
		// see is one they cannot undo.
		if _, err := tx.StrikeAgentFact(agentId, fact.ID, "a later conversation replaced it"); err != nil {
			return fmt.Errorf("superseding %s#%d: %w", path, superseded.Number, err)
		}
	}
	return nil
}

// replacementHolds says whether the fact an answer named can stand in
// for the line it asked to retire: on the same page, another fact than
// the line itself, standing on ground at least as firm, of the same kind,
// and, where both are dated, not about an earlier day. Anything else is
// an answer naming the wrong fact, and both stay.
func replacementHolds(retiring, replacement *models.AgentFact) bool {
	if replacement == nil || replacement.ID == retiring.ID || replacement.NodeID != retiring.NodeID {
		return false
	}
	if replacement.Kind != retiring.Kind {
		return false
	}
	if replacement.HappenedAt != nil && retiring.HappenedAt != nil && replacement.HappenedAt.Before(*retiring.HappenedAt) {
		return false
	}
	return atLeastAsWellEvidenced(replacement, retiring)
}

// retractionHolds says whether a retirement that files nothing in its
// place is grounded in something the run was shown.
//
// The same test a fact's citation gets, and for the same reason: words
// that are not in the thing they are said to be from are words nobody
// said. A message named but never shown to this run is nothing at all.
//
// In a conversation the words must be the person's. The agent's own reply
// is in the transcript too, and a quote of its "Quite." retired a fact
// with nothing in its place. And there must be words: a message the run
// kept no text for has nothing in it to retract anything with.
func retractionHolds(superseded SupersededFact, shown map[string]string, theirWords map[string]bool) bool {
	id := strings.Trim(strings.TrimSpace(superseded.MessageID), "[]")
	quote := strings.TrimSpace(superseded.Quote)
	if id == "" || quote == "" {
		return false
	}
	if theirWords != nil && !theirWords[id] {
		return false
	}
	source, known := shown[id]
	if !known || strings.TrimSpace(source) == "" {
		return false
	}
	return quoteOccursIn(quote, source)
}
