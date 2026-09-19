package agent

import (
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
//
// The meaning of the name is handed in rather than worked out here: it
// is an HTTP call to another service, and this runs inside the
// transaction that writes the fact.
func (self *Agent) findExistingPage(tx db.Transaction, agentId, path string, kind models.AgentNodeKind, name string, sense *meaning) (*models.AgentNode, error) {
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
	if wanted == "" || sense == nil {
		return nil, nil
	}
	scores, err := tx.Nearest(db.AgentNodeTable, agentId, sense.ModelName, sense.Vector, samePageCandidates, db.VectorQuery{
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
		//
		// Up to the bound and no further. A page may answer to
		// models.AliasCount names and PutAgentNode refuses one that
		// answers to more, so appending past it did not add a name: it
		// made every write of that page fail validation, from here and
		// from everywhere else, leaving the page unwritable for good. The
		// match still stands -- it is the page, it is simply not learning
		// another name for itself.
		if !strings.EqualFold(strings.TrimSpace(candidate.Name), name) && !hasAlias(candidate, name) &&
			len(candidate.Aliases) < models.AliasCount {
			candidate.Aliases = append(candidate.Aliases, name)
			if _, err := tx.PutAgentNode(candidate); err != nil {
				return nil, err
			}
		}
		return candidate, nil
	}
	return nil, nil
}

// pageIdentity is what a writer asked for, normalized: the path, the kind
// it is if the writer did not say, and the name a new page would take.
//
// Worked out before the transaction as well as inside it, because the
// name is what the search by meaning embeds and that call belongs outside
// the transaction.
func pageIdentity(path string, kind models.AgentNodeKind, name string) (string, models.AgentNodeKind, string) {
	path = models.NormalizePath(path)
	if path == "" {
		return "", kind, ""
	}
	if !models.IsAgentNodeKind(kind) {
		kind = kindOfPath(path)
	}
	// The root of the graph is folders and the person's own page. A
	// model that files a fact at "mc" -- a chat channel's name cut to a
	// slug -- made a page beside people/ and projects/ that nothing lists
	// under a folder and the dashboard cannot move or merge, since those
	// are hidden on a root. It goes under the folder its kind belongs to.
	if !strings.Contains(path, "/") && path != models.PathSelf && kind != models.NodeFolder {
		path = models.JoinPath(folderOfKind(kind), path)
	}
	// A root folder said twice. A model reading a directory of people, in
	// a source that keeps them under a folder of its own, files the first
	// one at "people/people/ran-liao": the prompt's rule and the
	// document's own shelf, one after the other. Nine pages arrived that
	// way in one night, three of them a second copy of somebody who
	// already had a page, so their facts stood apart and a question about
	// either found half of them. The segments after the first are the
	// person's to arrange, so only the repeat at the front is taken.
	path = withoutTheRepeatedFolder(path)
	name = strings.TrimSpace(name)
	if name == "" {
		name = nameFromSlug(models.LastSegment(path))
	}
	return path, kind, name
}

// withoutTheRepeatedFolder is a path whose first segment is a root folder
// and whose second is the same folder, with the repeat taken out.
//
// Only the front, and only an exact repeat: "people/people/ran-liao"
// becomes "people/ran-liao", while "people/ran-liao/people" is left as it
// is, because a page named for a folder deeper down is somebody's own
// arrangement and not this mistake.
func withoutTheRepeatedFolder(path string) string {
	first, rest, found := strings.Cut(path, "/")
	if !found || !isRootFolder(first) {
		return path
	}
	second, under, found := strings.Cut(rest, "/")
	if second != first {
		return path
	}
	if !found {
		return first
	}
	return models.JoinPath(first, under)
}

// isRootFolder says whether a segment names one of the folders the graph
// is arranged under, which is the closed set kindOfPath reads.
func isRootFolder(segment string) bool {
	switch segment {
	case models.PathPeople, models.PathProjects, models.PathPlaces,
		models.PathThings, models.PathTime, models.PathTopics,
		models.PathNotes, "organizations":
		return true
	}
	return false
}

// folderOfKind is the root folder a page of a kind lives under, the
// inverse of kindOfPath: a topic under topics/, a thing under things/, and
// what is neither under notes/, where anything with no better home goes.
func folderOfKind(kind models.AgentNodeKind) string {
	switch kind {
	case models.NodePerson:
		return models.PathPeople
	case models.NodeProject:
		return models.PathProjects
	case models.NodePlace:
		return models.PathPlaces
	case models.NodeThing:
		return models.PathThings
	case models.NodePeriod:
		return models.PathTime
	case models.NodeTopic, models.NodeOrganization:
		return models.PathTopics
	}
	return models.PathNotes
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
func (self *Agent) resolvePage(tx db.Transaction, agentId, path string, kind models.AgentNodeKind, name string, sense *meaning) (*models.AgentNode, error) {
	path, kind, name = pageIdentity(path, kind, name)
	if path == "" {
		return nil, nil
	}
	existing, err := self.findExistingPage(tx, agentId, path, kind, name, sense)
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
