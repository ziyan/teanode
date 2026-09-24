package agent

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// FoldIntoWhatThePageSays merges a fact just written into the one already
// on the page that says the same thing, and reports which it became.
//
// Every writer needs this and for the same reason: nobody writing a fact
// has read the page first. A conversation that comes back to a subject is
// filed by two runs a day apart, neither knowing of the other, and both
// doing their job; a person adding a line by hand is not going to reread
// thirteen of them. Left to itself a page ends up saying one thing nine
// ways, which is worse than saying it once and worse than not saying it,
// because every answer drawn from it is a little different.
//
// The older one stays: its number is what anything else cites. It gains
// whatever evidence the new one brought, and the new one goes dormant
// behind it -- kept, searchable, out of the page -- so a fold the person
// disagrees with is there to be undone. What happened is in the page's
// history either way.
//
// What it will not do is guess. Folding with nobody watching is only
// safe where the two are the same sentence twice; anything that reads
// alike but says something different is left standing beside its twin,
// so that the newer is recalled as well as the older. See whatToFold.
func (self *Agent) FoldIntoWhatThePageSays(ctx context.Context, tx db.Transaction, written *models.AgentFact, node *models.AgentNode) (*models.AgentFact, error) {
	if written == nil || node == nil {
		return written, nil
	}
	return self.foldIntoWhatThePageSays(tx, written, node,
		self.meaningOf(ctx, written.AgentID, "remember", factText(written, node.Path, node.Name)))
}

// FoldByWordsIntoWhatThePageSays is the fold without the fact's meaning:
// the twin is found by names and words alone. For a caller that is
// already inside a transaction it cannot leave -- a dashboard request,
// which runs whole inside one -- and must not hold it, and the page's
// row lock, through a provider call.
func (self *Agent) FoldByWordsIntoWhatThePageSays(tx db.Transaction, written *models.AgentFact, node *models.AgentNode) (*models.AgentFact, error) {
	return self.foldIntoWhatThePageSays(tx, written, node, nil)
}

// foldIntoWhatThePageSays is the same with the fact's meaning already
// worked out, for a caller that did it before opening its transaction.
func (self *Agent) foldIntoWhatThePageSays(tx db.Transaction, written *models.AgentFact, node *models.AgentNode, sense *meaning) (*models.AgentFact, error) {
	if written == nil || node == nil {
		return written, nil
	}
	// Every near candidate is asked, not only the nearest: two lines in
	// the same words about two days sit at the same distance, and a third
	// saying of one of them must find its own day rather than stop at the
	// other.
	var twin *models.AgentFact
	choice := foldKeepBoth
	for _, candidate := range self.twinsOf(tx, written, node, sense) {
		if choice = whatToFold(written, candidate); choice != foldKeepBoth {
			twin = candidate
			break
		}
	}
	switch choice {
	case foldKeepBoth:
		return written, nil

	case foldTheOlderBehindTheNewer:
		if _, err := tx.FoldAgentFact(written.AgentID, twin.ID, written.ID,
			"a later statement of the same thing replaced it"); err != nil {
			return written, err
		}
		return written, nil
	}

	older, err := takeTheEvidenceOf(tx, twin, written)
	if err != nil {
		return written, err
	}
	if _, err := tx.FoldAgentFact(written.AgentID, written.ID, older.ID,
		"it says what another fact on the page already says"); err != nil {
		return written, err
	}
	return older, nil
}

// whatThePageAlreadySays is the fact the page states in these very
// words, or nil.
//
// Asked before a fact is written, where the fold behind it is asked
// after. The fold does the same job and cannot do it any earlier: the
// row has to exist before it can be put behind another one. So a
// re-statement cost a row and a number even when the words were
// identical -- AddAgentFact takes the page's next number, writes the
// line, and the fold puts it straight back, needlessly growing page history.
//
// Nothing is lost by not writing it. A second saying of a sentence
// carries exactly one thing the first does not -- where it was read --
// and that is evidence, which goes on the fact that is already there;
// the fold's own surviving branch does no more than that.
//
// Held to the same words, exactly as the fold is (see whatToFold): a
// rewording may be this sentence again or may be the next thing the page
// has to say, and telling those apart is a judgement, which belongs to
// the nightly pass that asks a model. And held to what the page still
// states: a line struck or folded away is not something the page says,
// so a run that reads it again is learning it rather than repeating it.
func whatThePageAlreadySays(tx db.Transaction, agentId string, node *models.AgentNode, said *models.AgentFact) (*models.AgentFact, error) {
	if node == nil || said == nil || strings.TrimSpace(said.Text) == "" {
		return nil, nil
	}
	facts, err := tx.ListAgentFacts(agentId, node.ID, false, alreadySaidCandidates)
	if err != nil {
		return nil, err
	}
	// By number ascending, which is what ListAgentFacts gives: the lowest
	// number wins, because that is the one anything else cites and the
	// one the fold would have kept.
	for _, fact := range facts {
		if isTheSameFact(fact, said) {
			return fact, nil
		}
	}
	return nil, nil
}

// isTheSameFact says whether two statements are one statement.
//
// Identical words can describe separate occurrences. The kind must agree,
// and when both facts say when they happened, those dates must agree too.
//
// Where only one is dated the two are still taken as one statement, and
// the date is carried over by takeTheEvidenceOf. That is the conservative
// answer in both directions: a sentence re-stated without its date adds
// nothing by becoming a second row, and a sentence that arrives with a
// date the page did not have should leave the page knowing it.
//
// It is the one answer to that question: the check before writing, the
// fold after it (whatToFold) and the nightly merge (mergeSaidTwice) all
// ask it, so none of them can call two occurrences one.
func isTheSameFact(fact, said *models.AgentFact) bool {
	return saysItInTheSameWords(fact.Text, said.Text) && couldBeOneStatement(fact, said)
}

// couldBeOneStatement is the half of isTheSameFact that does not look at
// the words: the same kind, and the same date where both give one. The
// nightly merge asks only this half, because the model has already said
// the two wordings mean the same; what it cannot overrule is a second
// occurrence of an event on another day, or a state filed as an event.
func couldBeOneStatement(fact, said *models.AgentFact) bool {
	if fact.Kind != said.Kind {
		return false
	}
	if fact.HappenedAt != nil && said.HappenedAt != nil && !fact.HappenedAt.Equal(*said.HappenedAt) {
		return false
	}
	return true
}

// takeTheEvidenceOf puts what a second saying of a sentence brought onto
// the fact that already says it, and answers with that fact as it now
// stands.
//
// The fact that survives is the one already on the page, so where the
// second saying stood on firmer ground the first takes that with it.
// Otherwise re-filing a sentence the person stated, over a copy the
// agent had inferred, would leave the page saying at half confidence
// something it had been told.
func takeTheEvidenceOf(tx db.Transaction, standing, said *models.AgentFact) (*models.AgentFact, error) {
	return tx.UpdateAgentFact(standing.AgentID, standing.ID, func(older *models.AgentFact) error {
		older.Evidence = append(older.Evidence, said.Evidence...)
		if len(older.Evidence) > models.EvidenceCount {
			older.Evidence = older.Evidence[:models.EvidenceCount]
		}
		if atLeastAsWellEvidenced(said, older) {
			older.Inferred = said.Inferred
			if said.Confidence > older.Confidence {
				older.Confidence = said.Confidence
			}
		}
		// A second saying that knows when it happened, over one that did
		// not. The two are the same statement -- isTheSameFact would not
		// have matched them otherwise -- so this is the page learning a
		// date rather than changing one.
		if older.HappenedAt == nil && said.HappenedAt != nil {
			older.HappenedAt = said.HappenedAt
		}
		return nil
	})
}

// foldChoice is what the write boundary does with a new fact and the one
// already on the page that came back as its twin.
type foldChoice int

const (
	// foldKeepBoth leaves both rows on the page, which is the answer
	// whenever the two are not provably the same statement. It is the
	// zero value, so a path that does not decide keeps what it has.
	foldKeepBoth foldChoice = iota

	// foldTheNewerBehindTheOlder is the ordinary fold: the same sentence
	// filed twice. The older keeps its number, because that is what
	// anything else cites, and gains the newer's evidence.
	foldTheNewerBehindTheOlder

	// foldTheOlderBehindTheNewer is a negation: "she prefers tea" and
	// "she no longer prefers tea". The later statement is what the page
	// says and the earlier one stays behind it.
	foldTheOlderBehindTheNewer
)

// whatToFold decides between a new fact and its twin, and its whole job
// is to refuse.
//
// Vector similarity and shared names do not prove that two facts agree.
// Different amounts, dates, frequencies or responsibilities must remain distinct.
//
// So an automatic fold now needs the two to be the same fact written
// twice -- see isTheSameFact: the same words, the same kind, and the same
// day where both say one -- where there is provably nothing to lose. The
// same sentence about a second occurrence a year later is a second fact. A paraphrase is left standing beside its twin; the nightly
// pass that puts a page to a model (consolidatePage) is where a judgment
// like that belongs, and until it runs the person hears both rather than
// only the older.
func whatToFold(written, twin *models.AgentFact) foldChoice {
	// "She prefers tea" and "she no longer prefers tea" share every name
	// and sit on top of each other in the vector space, so neither the
	// cosine nor the name check can keep them apart -- and they are the
	// pair it matters most not to lose one of. Both rows stay, and the
	// newer statement is the one the page states.
	if negates(written.Text, twin.Text) {
		if !laterThan(written, twin) {
			return foldKeepBoth
		}
		// And only where the newer one stands on ground at least as firm.
		// A fact whose quote could not be found in what the run was shown
		// is marked inferred at half confidence precisely because the
		// model may have composed it; letting that supersede something
		// the person said would have the agent's own paraphrase win an
		// argument with its source, with no one present to object.
		if !atLeastAsWellEvidenced(written, twin) {
			return foldKeepBoth
		}
		return foldTheOlderBehindTheNewer
	}
	if isTheSameFact(twin, written) {
		return foldTheNewerBehindTheOlder
	}
	return foldKeepBoth
}

// atLeastAsWellEvidenced says whether one fact stands on ground at least
// as firm as another's: stated where the other is stated, and no less
// sure of itself.
func atLeastAsWellEvidenced(fact, than *models.AgentFact) bool {
	if fact.Inferred && !than.Inferred {
		return false
	}
	return fact.Confidence >= than.Confidence
}

// laterThan says whether one fact is the later statement of the two: by
// when it was true where both say, and by when it was filed otherwise.
//
// Prefer the occurrence date so learning an older event later does not make
// that event newer than a correction already on the page.
func laterThan(fact, than *models.AgentFact) bool {
	if fact.HappenedAt != nil && than.HappenedAt != nil {
		return fact.HappenedAt.After(*than.HappenedAt)
	}
	return fact.CreatedAt.After(than.CreatedAt)
}

// twinsOf is the facts already on this page that a new one may be a
// second saying of, nearest first.
//
// A candidate and not a verdict: whether the two are really one
// statement is whatToFold's to decide, and this only narrows the page
// down to what is worth asking about.
//
// Written after the fact rather than before it so that the vector is the
// one the store holds, and so that a deployment with no embedding model
// keeps everything rather than silently dropping what it cannot compare.
func (self *Agent) twinsOf(tx db.Transaction, fact *models.AgentFact, node *models.AgentNode, sense *meaning) []*models.AgentFact {
	if sense == nil {
		return nil
	}
	if err := tx.PutAgentFactVector(fact.AgentID, fact.ID, sense.ModelName, sense.Vector); err != nil {
		log.Debugf("cannot keep a fact's vector: %s", err)
		return nil
	}
	scores, err := tx.Nearest(db.AgentFactTable, fact.AgentID, sense.ModelName, sense.Vector, 6, db.VectorQuery{
		Where:     []string{`"fact_id" IN (SELECT "id" FROM "agent_fact" WHERE "node_id" = ? AND "id" <> ? AND NOT "dormant" AND "superseded_by" IS NULL)`},
		Arguments: []any{fact.NodeID, fact.ID},
		Floor:     twinFloor,
	})
	if err != nil {
		log.Debugf("cannot look for a twin: %s", err)
		return nil
	}
	candidates, err := tx.GetAgentFacts(fact.AgentID, idsOf(scores))
	if err != nil {
		return nil
	}
	var twins []*models.AgentFact
	// The page's own name is not evidence either way; see sharesAName.
	itsOwn := append([]string{node.Name}, node.Aliases...)
	for _, candidate := range orderFacts(candidates, idsOf(scores)) {
		if !sharesAName(fact.Text, candidate.Text, itsOwn...) {
			continue
		}
		// A line that says a different amount, a different date or a
		// different how-often is not this one said twice, however near
		// the two sit: it is the next thing the page has to say, and the
		// reason the person was talking to their agent at all. Not this
		// fact's twin, and the search goes on to the next candidate
		// rather than stopping at it.
		if differsInQuantity(fact.Text, candidate.Text) {
			continue
		}
		twins = append(twins, candidate)
	}
	return twins
}
