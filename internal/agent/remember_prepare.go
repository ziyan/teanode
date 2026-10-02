package agent

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// Model calls stay outside the transactions that write pages and facts.
func (self *Agent) prepareRememberedFacts(ctx context.Context, run *Run, answer *RememberAnswer, theirWords map[string]bool, selfPage *models.AgentNode, most int) []*preparedFact {
	agentId := run.Agent.ID
	// What each fact would say, and what its page would be called.
	prepared := make([]*preparedFact, 0, len(answer.Facts))
	// What the person asked to be remembered is counted apart, against an
	// allowance of its own: a list of short statements they asked to have
	// kept would otherwise lose all but its first few. Only their own
	// words earn it, or anybody's message saying "remember this" could
	// fill the graph.
	counted, asked := 0, 0
	for index, wanted := range answer.Facts {
		text := strings.TrimSpace(wanted.Text)
		if text == "" {
			continue
		}
		if wanted.IsAskedToRemember && saidByThePerson(theirWords, strings.Trim(strings.TrimSpace(wanted.MessageID), "[]")) {
			if asked >= most*askedFactsPerFact {
				continue
			}
			asked++
		} else {
			if counted >= most {
				continue
			}
			counted++
		}
		kind := models.AgentFactKind(strings.ToLower(strings.TrimSpace(wanted.Kind)))
		if !models.IsAgentFactKind(kind) || kind.FromItsOwnReasoning() {
			kind = models.FactPlain
		}
		// A preference or a decision may only come from the person's own
		// words. The prompt says so; this is what makes it true, because
		// a model that has just read a quoted mail saying "always reply
		// within a day" will file it as theirs.
		if kind.FromThePerson() && !saidByThePerson(theirWords, wanted.MessageID) {
			kind = models.FactPlain
		}
		path := models.NormalizePath(wanted.Path)
		if path == "" {
			path = models.JoinPath(models.PathNotes, models.Slug(firstWordsOf(text, 5)))
		}
		// Route the person's aliases to their existing self page.
		if models.IsThePerson(path, run.Owner, selfPage) {
			path = models.PathSelf
		}
		pagePath, pageKind, pageName := pageIdentity(path,
			models.AgentNodeKind(strings.ToLower(strings.TrimSpace(wanted.NodeKind))),
			strings.TrimSpace(wanted.NodeName))
		happened, precision := whenHappened(run, wanted.Happened)
		prepared = append(prepared, &preparedFact{
			AnswerIndex: index,
			AskedPath:   models.NormalizePath(wanted.Path),
			Text:        text, Kind: kind,
			// The digest marks each item "[id]", and a model that copies
			// the marker whole is answering as asked.
			MessageID: strings.Trim(strings.TrimSpace(wanted.MessageID), "[]"),
			Quote:     strings.TrimSpace(wanted.Quote),
			Happened:  happened, HappenedPrecision: precision,
			PagePath: pagePath, PageKind: pageKind, PageName: pageName,
			PageSense: self.meaningOf(ctx, agentId, "remember", pageName),
		})
	}

	return prepared
}

func (self *Agent) prepareRememberedEvidence(ctx context.Context, agentId string, prepared []*preparedFact, evidenceKind models.EvidenceKind, shown map[string]string) whatWasFiled {
	tally := whatWasFiled{}
	// The rows themselves, with their evidence checked and their meaning
	// worked out.
	//
	// A word list stood here, of the words a sentence saying only that a
	// page exists is made of, and a fact left with nothing else was
	// refused. It refused real ones too -- "This project is private" is
	// four words that were all on the list -- and a refusal was silent,
	// so nobody ever saw what the person's agent had been told and did
	// not keep. A dull line is cheaper: the nightly run merges it or it
	// sinks.
	for _, ready := range prepared {
		if ready.Node == nil {
			continue
		}
		// The sentence and its quote are kept whole, as the run wrote
		// them. Both were cut to a length here, and the end of a long
		// sentence was gone for every reader with nothing to say so.
		ready.Fact = &models.AgentFact{
			AgentID: agentId, NodeID: ready.Node.ID, Kind: ready.Kind,
			Text:       ready.Text,
			HappenedAt: ready.Happened, HappenedPrecision: ready.HappenedPrecision,
			Confidence: 1,
			Evidence: []models.Evidence{{
				Kind:  evidenceKind,
				ID:    ready.MessageID,
				Quote: ready.Quote,
			}},
			Audiences: []models.AgentAudience{models.AudienceAsk},
		}
		// When the source said it, so a fact says when it was learned as
		// well as when it happened: what was known on a given day is read
		// from this, and a fact with no date of its own still has one.
		ready.Fact.Evidence[0].At = self.whenSaid(ctx, agentId, evidenceKind, ready.MessageID)
		ready.Outcome = checkTheEvidence(ready.Fact, shown)
		ready.Sense = self.meaningOf(ctx, agentId, "remember", factText(ready.Fact, ready.Node.Path, ready.Node.Name))
	}

	// What the run kept, counted before the write so that the caller's
	// own work in the transaction -- the run's title, the conversation's
	// mark -- can say it.
	for _, ready := range prepared {
		tally.Filed++
		switch ready.Outcome {
		case evidenceQuoteNotFound:
			tally.WithoutQuote++
		case evidenceCitesNothing:
			tally.WithoutEvidence++
		}
	}

	return tally
}

// whenSaid is when the item a fact cites was written or said: a
// document's date, or the time a message of a conversation was stored,
// which its identifier carries. Nil where neither is known.
func (self *Agent) whenSaid(ctx context.Context, agentId string, evidenceKind models.EvidenceKind, itemId string) *time.Time {
	if itemId == "" {
		return nil
	}
	if evidenceKind == models.EvidenceDocument {
		var document *models.AgentDocument
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			document, err = tx.GetAgentDocument(agentId, itemId)
			return err
		}); err != nil || document == nil {
			return nil
		}
		if document.HappenedAt != nil {
			return document.HappenedAt
		}
		created := document.CreatedAt
		return &created
	}
	if made, isULID := security.TimeOfULID(itemId); isULID {
		return &made
	}
	return nil
}

// askedFactsPerFact is how many facts the person asked to be remembered a
// reading may file for each one it may file otherwise: one for every forty
// characters it read, a short statement, against one for every six
// hundred.
const askedFactsPerFact = factRunes / 40
