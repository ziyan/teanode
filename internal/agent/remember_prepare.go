package agent

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// Model calls stay outside the transactions that write pages and facts.
func (self *Agent) prepareRememberedFacts(ctx context.Context, run *Run, answer *RememberAnswer, theirWords map[string]bool, selfPage *models.AgentNode) []*preparedFact {
	agentId := run.Agent.ID
	// What each fact would say, and what its page would be called.
	prepared := make([]*preparedFact, 0, len(answer.Facts))
	for index, wanted := range answer.Facts {
		if index >= rememberFacts {
			break
		}
		text := strings.TrimSpace(wanted.Text)
		if text == "" {
			continue
		}
		kind := models.AgentFactKind(strings.ToLower(strings.TrimSpace(wanted.Kind)))
		if !models.IsAgentFactKind(kind) {
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
		prepared = append(prepared, &preparedFact{
			AnswerIndex: index,
			AskedPath:   models.NormalizePath(wanted.Path),
			Text:        text, Kind: kind,
			// The digest marks each item "[id]", and a model that copies
			// the marker whole is answering as asked.
			MessageID: strings.Trim(strings.TrimSpace(wanted.MessageID), "[]"),
			Quote:     strings.TrimSpace(wanted.Quote),
			Happened:  whenHappened(run, wanted.Happened),
			PagePath:  pagePath, PageKind: pageKind, PageName: pageName,
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
		ready.Fact = &models.AgentFact{
			AgentID: agentId, NodeID: ready.Node.ID, Kind: ready.Kind,
			Text:       cutRunes(ready.Text, models.FactLength),
			HappenedAt: ready.Happened,
			Confidence: 1,
			Evidence: []models.Evidence{{
				Kind:  evidenceKind,
				ID:    ready.MessageID,
				Quote: cutRunes(ready.Quote, models.QuoteLength),
			}},
			Audiences: []models.AgentAudience{models.AudienceAsk},
		}
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
