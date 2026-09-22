package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// whatWasFiled is what a filing run kept, and what the evidence check
// took off it on the way.
//
// The two counts are the measure of how often the model quotes something
// nobody said. They go in the run's own row rather than a log line,
// so the person reading the dream log can assess what the model retained.
type whatWasFiled struct {
	Filed           int
	WithoutQuote    int
	WithoutEvidence int
}

// Describe is what the check did, for the end of a run's title. Empty
// when every quote was where it was said to be, which is the usual case
// and does not need saying.
func (self *whatWasFiled) Describe() string {
	var parts []string
	if self.WithoutQuote > 0 {
		parts = append(parts, fmt.Sprintf("%d without their quote", self.WithoutQuote))
	}
	if self.WithoutEvidence > 0 {
		parts = append(parts, fmt.Sprintf("%d without evidence", self.WithoutEvidence))
	}
	return strings.Join(parts, ", ")
}

// preparedFact is one fact of an answer on its way to a page: worked out
// as far as it can be before anything at all is written.
//
// It exists so that everything slow happens first. An embedding is an
// HTTP call to another service, and both the page's meaning and the
// fact's are needed before either can be filed; made with the writing
// transaction open they would hold a database connection, and every row
// that transaction had locked, for as long as the provider took.
type preparedFact struct {
	// Text is what the model said, trimmed, and MessageID and Quote what
	// it cited for it.
	Text      string
	Kind      models.AgentFactKind
	MessageID string
	Quote     string
	Happened  *time.Time

	// AskedPath is the path the writer actually wrote, before this was
	// tidied into PagePath. A link in the same answer names a page by
	// that path rather than by whatever tidying it went through, so it is
	// kept to join the two.
	AskedPath string

	// The page this belongs on, as it would be called, and what that name
	// means -- which is how a page already there under another name is
	// found rather than a second one made beside it.
	PagePath  string
	PageKind  models.AgentNodeKind
	PageName  string
	PageSense *meaning

	// Node is that page once it has been opened.
	Node *models.AgentNode

	// Fact is the row to write, or nil where the page already said this
	// and there is nothing to write. Such an item still counts as filed:
	// the run did its job with it, and it turned out to be nothing new.
	Fact  *models.AgentFact
	Sense *meaning

	// Outcome is what the evidence check made of the citation.
	Outcome evidenceOutcome
}

// fileWhatWasLearned writes the run's answer onto the graph and says how
// much it kept.
//
// What the model was shown is handed in by id -- the batch's messages, or
// the documents as the reading rendered them -- because a fact may only
// cite something that was in front of it, and its quote may only be words
// that were there.
//
// Everything a window learned is written in one transaction, and finish
// is whatever the caller wants done in that same transaction once it has
// been: for a conversation, the mark saying how far it has been read.
// That is the point of the shape. Each fact used to be written on its
// own, a failure logged and stepped over, and the run reported success
// anyway -- so the caller moved its mark past the whole window and the
// messages behind it were marked read without ever having been read.
// Nothing came back for them, because the mark is a promise that
// everything behind it has been filed. The supersessions ran later still,
// in transactions of their own, so a replacement that failed left the
// page with the old line struck and nothing standing in its place.
//
// Facts, links, replacements and the read marker commit together. Page creation
// and contact binding happen earlier and may remain after a later failure.
func (self *Agent) fileWhatWasLearned(ctx context.Context, run *Run, answer *RememberAnswer, theirWords map[string]bool, evidenceKind models.EvidenceKind, shown map[string]string, finish func(tx db.Transaction, filed whatWasFiled) error) (whatWasFiled, error) {
	tally := whatWasFiled{}
	if answer == nil {
		answer = &RememberAnswer{}
	}
	agentId := run.Agent.ID
	// The person's own page, for telling a page about them from one about
	// somebody else: its aliases carry every name they have been found
	// under, which the account's own name need not.
	var selfPage *models.AgentNode
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		selfPage, err = tx.GetAgentNode(agentId, models.PathSelf)
		return err
	}); err != nil {
		return tally, err
	}

	prepared := self.prepareRememberedFacts(ctx, run, answer, theirWords, selfPage)

	if err := self.openRememberedPages(ctx, run, prepared); err != nil {
		return tally, err
	}

	// The pages this answer opened, under every path that names them, so
	// that a link in the same answer lands on the page its fact did.
	// Without this the links quietly halve: the page a fact asked for is
	// not always the page it got -- the owner's own name routes to
	// `self`, a bare path gains the folder its kind lives under, and a
	// page already there under another name keeps the path it has -- and
	// a link naming the path the writer wrote found nothing at it and was
	// dropped in silence. That is most of an authorship map, since most
	// of what a person's own checkouts contain was written by them.
	opened := make(map[string]*models.AgentNode, 2*len(prepared))
	for _, ready := range prepared {
		if ready.Node == nil {
			continue
		}
		opened[ready.AskedPath] = ready.Node
		opened[ready.PagePath] = ready.Node
	}

	tally = self.prepareRememberedEvidence(ctx, agentId, prepared, evidenceKind, shown)

	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := self.applyRememberedFacts(tx, run, answer, prepared, opened, selfPage, shown); err != nil {
			return err
		}
		if finish == nil {
			return nil
		}
		return finish(tx, tally)
	}); err != nil {
		return whatWasFiled{}, err
	}
	return tally, nil
}

// linkWhatWasLearned draws the links an answer asked for. A link whose
// either end is not a page the agent has is not a failure: the model
// named something it did not file, and there is nothing to join.
//
// opened is the pages this same answer just made, by every path that
// names them, because a link names a page the way its fact did and the
// fact does not always end up at the path it asked for.
func linkWhatWasLearned(tx db.Transaction, agentId string, links []RememberedLink, opened map[string]*models.AgentNode, owner *models.User, selfPage *models.AgentNode) error {
	for _, link := range links {
		from := pathOfLinkEnd(link.From, owner, selfPage)
		to := pathOfLinkEnd(link.To, owner, selfPage)
		relation := models.AgentEdgeRelation(strings.ToLower(strings.TrimSpace(link.Relation)))
		if from == "" || to == "" || !models.IsAgentEdgeRelation(relation) {
			continue
		}
		fromNode, err := pageOfLinkEnd(tx, agentId, from, opened)
		if err != nil {
			return fmt.Errorf("reading %q: %w", from, err)
		}
		toNode, err := pageOfLinkEnd(tx, agentId, to, opened)
		if err != nil {
			return fmt.Errorf("reading %q: %w", to, err)
		}
		if fromNode == nil || toNode == nil {
			continue
		}
		// A page joined to itself, which PutAgentEdge refuses with an
		// error rather than a shrug -- and an error here loses the whole
		// window, since everything a run learned is written in one
		// transaction. Two ends that are two spellings of the person now
		// both route to `self`, so this is reachable in a way it was not
		// when an unfound page simply meant no link.
		if fromNode.ID == toNode.ID {
			continue
		}
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agentId, FromID: fromNode.ID, ToID: toNode.ID, Relation: relation,
			Note: strings.TrimSpace(link.Note),
		}); err != nil {
			return fmt.Errorf("linking %s to %s: %w", from, to, err)
		}
	}
	return nil
}

// pathOfLinkEnd is one end of a link as a path: cleaned, and routed to
// `self` where it names the person whose agent this is. The same routing
// a fact's path gets, for the same reason -- what the agent knows about
// them lives on one page, and a link to a second page for them joins
// nothing to nothing.
func pathOfLinkEnd(path string, owner *models.User, selfPage *models.AgentNode) string {
	path = models.NormalizePath(path)
	if models.IsThePerson(path, owner, selfPage) {
		return models.PathSelf
	}
	return path
}

// pageOfLinkEnd is the page one end of a link names: the one this answer
// just opened under that path, or the one already there.
func pageOfLinkEnd(tx db.Transaction, agentId, path string, opened map[string]*models.AgentNode) (*models.AgentNode, error) {
	if node := opened[path]; node != nil {
		return node, nil
	}
	return tx.GetAgentNode(agentId, path)
}

func (self *Agent) openRememberedPages(ctx context.Context, run *Run, prepared []*preparedFact) error {
	agentId := run.Agent.ID
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorRemember)
		for _, ready := range prepared {
			// The page this belongs on, which is the one already there
			// under any of its names rather than a second one beside it.
			// This keeps aliases from fragmenting facts across separate pages.
			node, err := self.resolvePage(tx, agentId, ready.PagePath, ready.PageKind, ready.PageName, ready.PageSense)
			if err != nil {
				return fmt.Errorf("opening the page for %q: %w", ready.Text, err)
			}
			if node == nil {
				continue
			}
			ready.Node = node
			// A page about a person is not a contact. The graph files
			// whoever turns up in a commit log, a chat channel or a
			// document, and that is not the same set as the people
			// somebody keeps: it filed five hundred and eighteen cards
			// with a name and nothing else on them, among them a release
			// bot and a build account, and the address book stopped being
			// an address book.
			//
			// A card is made when a person makes one, or when they promote
			// somebody the mail has learned. What the graph knows about a
			// person lives on their page, where it can be wrong without
			// reaching a phone.
		}
		return nil
	})
}

// applyRememberedFacts uses the caller's transaction so the read marker and
// transcript title commit with facts, links and replacements.
func (self *Agent) applyRememberedFacts(tx db.Transaction, run *Run, answer *RememberAnswer, prepared []*preparedFact, opened map[string]*models.AgentNode, selfPage *models.AgentNode, shown map[string]string) error {
	agentId := run.Agent.ID
	tx.AsActor(models.ActorRemember)
	// What this answer actually put on each page, which is what a
	// supersession has to point at before anything is struck.
	filed := map[string][]*models.AgentFact{}
	for _, ready := range prepared {
		if ready.Fact == nil {
			continue
		}
		// A sentence the page already states does not go on it
		// again. What the second saying brought that the first did
		// not is its evidence, and that goes on the fact that is
		// there. See whatThePageAlreadySays for why this is asked
		// before the row is written rather than after.
		standing, err := whatThePageAlreadySays(tx, agentId, ready.Node, ready.Fact)
		if err != nil {
			return fmt.Errorf("reading what %q already says: %w", ready.Node.Path, err)
		}
		if standing != nil {
			if _, err := takeTheEvidenceOf(tx, standing, ready.Fact); err != nil {
				return fmt.Errorf("giving what %q brought to the fact that says it: %w", ready.Text, err)
			}
			continue
		}
		written, err := tx.AddAgentFact(ready.Fact)
		if err != nil {
			return fmt.Errorf("filing %q: %w", ready.Text, err)
		}
		filed[ready.Node.ID] = append(filed[ready.Node.ID], written)
		if _, err := self.foldIntoWhatThePageSays(tx, written, ready.Node, ready.Sense); err != nil {
			return fmt.Errorf("folding %q into the page: %w", ready.Text, err)
		}
	}
	if err := linkWhatWasLearned(tx, agentId, answer.Links, opened, run.Owner, selfPage); err != nil {
		return err
	}
	if err := supersedeWhatWasReplaced(tx, agentId, answer.Supersedes, filed, shown); err != nil {
		return err
	}

	return nil
}
