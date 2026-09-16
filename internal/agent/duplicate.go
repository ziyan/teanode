package agent

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Not making a second page for a thing that already has one.
//
// This is the failure the whole graph is vulnerable to, and it is quiet.
// Nothing errors: a conversation about "Alice" files onto
// people/alice-chen, a later one about "Alice Chen from platform" files
// onto people/alice-chen-from-platform, and the agent now knows two half
// people and will answer from whichever it happens to find. The graph
// looks healthy and is useless, which is worse than an empty one because
// nobody notices.
//
// So a page is looked for three ways before a new one is made: by the
// path itself, by what it is called, and by what it means. All three are
// cheap; the third costs one embedding call, which is the price of not
// having to tidy this up by hand later.

// The bounds.
const (
	// samePageFloor is how near two pages must mean for one to be the
	// other. Higher than the fact twin floor because a page is a whole
	// subject rather than a sentence: two projects at one company sit
	// closer together than two sentences about one project do.
	samePageFloor = 0.94

	// samePageCandidates is how many pages are ranked when looking for
	// one that already exists.
	samePageCandidates = 8
)

// findExistingPage is the page this one already is, or nil.
//
// Answers with a page only when it is the same *kind* of thing as well as
// near in meaning. A person and the project they run are talked about in
// the same words and are not each other, and merging them is the one
// mistake here that cannot be undone by hand.
func (self *Agent) findExistingPage(ctx context.Context, tx db.Transaction, agentId, path string, kind models.AgentNodeKind, name string) (*models.AgentNode, error) {
	// By path. The ordinary case, and free.
	if node, err := tx.GetAgentNode(agentId, path); err != nil || node != nil {
		return node, err
	}

	// By what it is called, within the same parent. "Alice Chen" filed
	// under people/ is the page called Alice Chen under people/, whatever
	// slug the writer chose.
	wanted := strings.TrimSpace(strings.ToLower(name))
	if wanted != "" {
		parent := models.ParentPath(path)
		siblings, err := tx.ListAgentNodesUnder(agentId, parent, 400)
		if err != nil {
			return nil, err
		}
		for _, sibling := range siblings {
			if sibling.Path == parent || sibling.Kind != kind {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(sibling.Name), name) {
				return sibling, nil
			}
			for _, alias := range sibling.Aliases {
				if strings.EqualFold(strings.TrimSpace(alias), name) {
					return sibling, nil
				}
			}
		}
	}

	// By what it means. Catches the case the other two cannot: a page
	// under a different parent, or called something else for the same
	// thing -- "the fleet team" and "Fleet".
	if wanted == "" {
		return nil, nil
	}
	vectors, modelName, ok := self.embed(ctx, agentId, "remember", []string{name})
	if !ok {
		return nil, nil
	}
	scores, err := tx.Nearest(db.AgentNodeTable, agentId, modelName, vectors[0], samePageCandidates, db.VectorQuery{
		Floor: samePageFloor,
	})
	if err != nil {
		return nil, err
	}
	candidates, err := tx.GetAgentNodes(agentId, idsOf(scores))
	if err != nil {
		return nil, err
	}
	for _, candidate := range orderNodes(candidates, idsOf(scores)) {
		if candidate.Kind != kind {
			continue
		}
		// A page that means the same and is called something else gains
		// the other name as an alias, so the next writer's spelling
		// lands on it by name rather than by another embedding call.
		if !strings.EqualFold(strings.TrimSpace(candidate.Name), name) && !hasAlias(candidate, name) {
			candidate.Aliases = append(candidate.Aliases, name)
			if _, err := tx.PutAgentNode(candidate); err != nil {
				return nil, err
			}
		}
		return candidate, nil
	}
	return nil, nil
}

// hasAlias says whether a page already answers to a name.
func hasAlias(node *models.AgentNode, name string) bool {
	for _, alias := range node.Aliases {
		if strings.EqualFold(strings.TrimSpace(alias), name) {
			return true
		}
	}
	return false
}

// resolvePage is the page a fact should go on: the one that is already
// there under any of its names, or a new one.
//
// Every writer of a fact goes through here rather than calling
// PutAgentNode, which is what keeps one thing to one page.
func (self *Agent) resolvePage(ctx context.Context, tx db.Transaction, agentId, path string, kind models.AgentNodeKind, name string) (*models.AgentNode, error) {
	path = models.NormalizePath(path)
	if path == "" {
		return nil, nil
	}
	if !models.IsAgentNodeKind(kind) {
		kind = kindOfPath(path)
	}
	if strings.TrimSpace(name) == "" {
		name = nameFromSlug(models.LastSegment(path))
	}
	existing, err := self.findExistingPage(ctx, tx, agentId, path, kind, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	return tx.PutAgentNode(&models.AgentNode{
		AgentID: agentId, Path: path, Kind: kind, Name: name,
	})
}
