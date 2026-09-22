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
func supersedeWhatWasReplaced(tx db.Transaction, agentId string, supersedes []SupersededFact, filed map[string][]*models.AgentFact, shown map[string]string) error {
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
		// So a supersession has to be one of two things. Either this
		// answer filed something on the same page standing at least as
		// firmly as what it would replace -- a replacement -- or it
		// carries words from what the run was shown saying the line is
		// done with, which is a retraction. Neither, and the line stays
		// and the run says why.
		if replacementFor(fact, filed[node.ID]) == nil && !retractionHolds(superseded, shown) {
			log.Noticef("not superseding %s#%d: nothing was filed to replace it and nothing shown to retract it",
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

// replacementFor is the fact this answer filed that may stand in for one
// it asked to retire, or nil where it filed none that could.
func replacementFor(retiring *models.AgentFact, candidates []*models.AgentFact) *models.AgentFact {
	for _, candidate := range candidates {
		if candidate == nil || candidate.ID == retiring.ID {
			continue
		}
		if atLeastAsWellEvidenced(candidate, retiring) {
			return candidate
		}
	}
	return nil
}

// retractionHolds says whether a retirement that files nothing in its
// place is grounded in something the run was shown.
//
// The same test a fact's citation gets, and for the same reason: words
// that are not in the thing they are said to be from are words nobody
// said. A message named but never shown to this run is nothing at all.
func retractionHolds(superseded SupersededFact, shown map[string]string) bool {
	id := strings.TrimSpace(superseded.MessageID)
	quote := strings.TrimSpace(superseded.Quote)
	if id == "" || quote == "" {
		return false
	}
	source, known := shown[id]
	if !known {
		return false
	}
	// An empty source is a message the run knows it showed but kept no
	// text for, which is nothing to check rather than something to doubt.
	return source == "" || quoteOccursIn(quote, source)
}
