package agent

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// graphIndex is the index a prompt carries: the self page in full, then
// as much of the rest of the graph as fits.
//
// Ordered by importance, which the nightly run recomputes and nothing
// else touches. That is deliberate. The list it replaced was ordered by
// when each memory was last used and every prompt marked what it carried,
// so the top of the prompt -- the part a provider can cache -- changed on
// every single turn and was never once a cache hit.
func (self *Agent) graphIndex(ctx context.Context, agent *models.Agent, owner *models.User, budget int) ([]string, []string) {
	var lines []string
	var carried []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.EnsureAgentRoots(agent.ID); err != nil {
			return err
		}
		nodes, err := tx.ListAgentIndex(agent.ID, 400)
		if err != nil {
			return err
		}
		spent := 0
		for _, node := range nodes {
			// The self page is not a line in the index; it is the block
			// above it, written by selfLines.
			if node.Path == models.PathSelf {
				continue
			}
			// A folder with nothing under it says nothing. The roots are
			// made for every agent whether or not anything is filed in
			// them, and seven empty headings at the top of every prompt
			// teach the model that the graph is empty.
			if node.Kind == models.NodeFolder && node.Summary == "" && node.ParentID == "" {
				continue
			}
			line := node.IndexLine(140)
			cost := llm.EstimateTokens(line)
			if spent+cost > budget {
				break
			}
			spent += cost
			lines = append(lines, line)
			carried = append(carried, node.ID)
		}
		return nil
	}); err != nil {
		log.Warningf("cannot read the graph of %q: %s", owner.Username, err)
	}
	return lines, carried
}

// carryIndex is the index for a turn, remembering which pages went in so
// that the recall after it does not send the same page twice.
func (self *AskRun) carryIndex(ctx context.Context, budget int) []string {
	lines, carried := self.agent.graphIndex(ctx, self.settings.Agent, self.settings.Owner, budget)
	self.mutex.Lock()
	if self.promptMemories == nil {
		self.promptMemories = map[string]bool{}
	}
	for _, id := range carried {
		self.promptMemories[id] = true
	}
	self.mutex.Unlock()
	return lines
}

// selfLines is the self page: the card that is the person, then whatever
// the page itself says. Always first, and always in full.
//
// The card is read rather than copied. A person who changes their
// telephone number changes it in one place, and the agent is right about
// it on the next turn.
func (self *Agent) selfLines(ctx context.Context, agent *models.Agent, owner *models.User) []string {
	var lines []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if owner.ContactID != "" {
			card, err := self.contactOf(tx, owner)
			if err != nil {
				return err
			}
			if card != nil {
				lines = append(lines, cardLines(card)...)
			}
		}
		node, err := tx.GetAgentNode(agent.ID, models.PathSelf)
		if err != nil || node == nil {
			return err
		}
		if summary := strings.TrimSpace(node.Summary); summary != "" {
			lines = append(lines, cutRunes(summary, selfSummary))
		}
		facts, err := tx.ListAgentFactsLively(agent.ID, node.ID, 20)
		if err != nil {
			return err
		}
		byNumber(facts)
		for _, fact := range facts {
			lines = append(lines, "- "+fact.Line())
		}
		return nil
	}); err != nil {
		log.Warningf("cannot read the self page of %q: %s", owner.Username, err)
	}
	if len(lines) == 0 && owner.ContactID == "" {
		// Nothing is known and no card is chosen. Say so, with what to do
		// about it, rather than leaving the section out: a model that is
		// never told the page is empty answers as though it had read one.
		lines = append(lines, "No contact is marked as being them, so their own addresses are not known. If it comes up, offer to set one on the Contacts page.")
	}
	return lines
}

// contactOf is the address book entry that is the person themselves.
func (self *Agent) contactOf(tx db.Transaction, owner *models.User) (*models.Contact, error) {
	books, err := tx.ListAddressBooks(owner.ID)
	if err != nil {
		return nil, err
	}
	for _, book := range books {
		contact, err := tx.GetContact(book.ID, owner.ContactID)
		if err != nil {
			return nil, err
		}
		if contact != nil {
			return contact, nil
		}
	}
	return nil, nil
}

// cardLines is the person's own card as prompt lines: what the agent
// needs to know it is them, and to tell their mail and their commits from
// somebody else's.
func cardLines(contact *models.Contact) []string {
	var lines []string
	if name := strings.TrimSpace(contact.Name); name != "" {
		lines = append(lines, name)
	}
	if organization := strings.TrimSpace(contact.Organization); organization != "" {
		lines = append(lines, "Works at "+organization+".")
	}
	if len(contact.Emails) > 0 {
		lines = append(lines, "Their own addresses: "+strings.Join(contact.Emails, ", ")+".")
	}
	if len(contact.Phones) > 0 {
		lines = append(lines, "Telephone: "+strings.Join(contact.Phones, ", ")+".")
	}
	if extra := contacts.NotableFields(contact); extra != "" {
		lines = append(lines, extra)
	}
	return lines
}

// memoryLines is what an unattended run of a kind is shown: the index,
// then the facts addressed to that run. Named as it was when a memory was
// a memory, because six callers say it and none of them care.
func memoryLines(tx db.Transaction, agentId string, audience models.AgentAudience, limit int, withIds bool) ([]string, error) {
	if err := tx.EnsureAgentRoots(agentId); err != nil {
		return nil, err
	}
	var lines []string
	nodes, err := tx.ListAgentIndex(agentId, 200)
	if err != nil {
		return nil, err
	}
	spent := 0
	var nodeIds []string
	for _, node := range nodes {
		if node.Kind == models.NodeFolder && node.Summary == "" && node.ParentID == "" {
			continue
		}
		line := node.IndexLine(140)
		cost := llm.EstimateTokens(line)
		if spent+cost > runIndexTokens {
			break
		}
		spent += cost
		lines = append(lines, line)
		nodeIds = append(nodeIds, node.ID)
	}
	facts, err := tx.ListAgentFactsForAudience(agentId, audience, limit)
	if err != nil {
		return nil, err
	}
	paths, err := pathsOfFacts(tx, agentId, facts)
	if err != nil {
		return nil, err
	}
	factIds := make([]string, 0, len(facts))
	for _, fact := range facts {
		line := fact.Line()
		if withIds {
			line += " (" + fact.Reference(paths[fact.NodeID]) + ")"
		}
		lines = append(lines, line)
		factIds = append(factIds, fact.ID)
	}
	if err := tx.TouchAgentNodes(nodeIds, time.Now()); err != nil {
		return nil, err
	}
	if err := tx.TouchAgentFacts(factIds, time.Now()); err != nil {
		return nil, err
	}
	return lines, nil
}

// pathsOfFacts is the path of the page each fact sits on.
func pathsOfFacts(tx db.Transaction, agentId string, facts []*models.AgentFact) (map[string]string, error) {
	if len(facts) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.NodeID)
	}
	nodes, err := tx.GetAgentNodes(agentId, ids)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]string, len(nodes))
	for _, node := range nodes {
		paths[node.ID] = node.Path
	}
	return paths, nil
}

// correctionLines is what the person corrected, newest first.
func correctionLines(tx db.Transaction, agentId string, kinds []models.AgentFeedbackKind) ([]string, error) {
	feedback, err := tx.ListAgentFeedback(agentId, kinds, promptCorrections)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(feedback))
	for _, entry := range feedback {
		lines = append(lines, entry.Said)
	}
	return lines, nil
}

// promptCorrections is how many corrections a run is shown, and
// promptRunMemories how many facts addressed to it a run with nobody
// present carries. A run cannot ask for more, so it is shown more.
const (
	promptCorrections = 20
	promptRunMemories = 30
)
