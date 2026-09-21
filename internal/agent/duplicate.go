package agent

import (
	"strings"
	"unicode"

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
	// other on meaning alone. Higher than the fact twin floor because a
	// page is a whole subject rather than a sentence: two projects at one
	// company sit closer together than two sentences about one project
	// do.
	//
	// It is also high enough that, on a real graph, it almost never
	// fires. Measured on one of several thousand pages, real duplicates
	// -- a subject and the same subject written a little differently, or
	// with the company's name in front of it -- scored between 0.75 and
	// 0.93, and every one of them had been filed as a second page. It
	// cannot simply be lowered, because what sits in the same band is not
	// duplicates: two adjacent months scored 0.886 and two sibling
	// platforms 0.761. Nearness alone does not separate a second page
	// about one subject from a sibling about another.
	samePageFloor = 0.94

	// sameNameFloor is how near two pages must mean when one's name is
	// also written inside the other's, which is a second signal and a
	// much better one.
	//
	// A subject inside the same subject with a qualifier in front of it
	// is one subject; "July 2025" and "June 2025" do not
	// contain each other, and neither do "Android client" and "iOS
	// client". But containment on its own is worse than useless in a
	// folder mirroring a checkout, where a word like "frontend" is inside
	// seven other repositories' names and a customer's name inside four.
	//
	// The two together separate cleanly, and this is where. Measured on a
	// real graph of several thousand pages: the nearest false pair the
	// two signals agree on scored 0.680, and the furthest true one --
	// a subject and the same subject with the company's name in front of
	// it -- scored 0.754. The line goes in the gap.
	sameNameFloor = 0.70

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
// sameName says whether a page above is called what this one is called,
// by the name the writer gave or by the last piece of the path.
//
// Both, because a writer may give the name and leave the slug to the
// path, or give a slug that says it and a name that says it differently.
func sameName(above *models.AgentNode, path, name string) bool {
	if wanted := strings.TrimSpace(name); wanted != "" &&
		strings.EqualFold(strings.TrimSpace(above.Name), wanted) {
		return true
	}
	return strings.EqualFold(models.LastSegment(above.Path), models.LastSegment(path))
}

func (self *Agent) findExistingPage(tx db.Transaction, agentId, path string, kind models.AgentNodeKind, name string, sense *meaning) (*models.AgentNode, error) {
	// By path. The ordinary case, and free.
	if node, err := tx.GetAgentNode(agentId, path); err != nil || node != nil {
		return node, err
	}

	// A page under a page of the same name is that page.
	//
	// projects/teanode/teanode is not a part of teanode. It is teanode,
	// filed one level too deep, and the thirteen facts on it are thirteen
	// facts missing from the hundred and seventy-one above it. The path
	// tier cannot catch this, because the path it is given does not exist
	// yet, and that is exactly the moment to catch it: once the page is
	// made, everything written to it goes to the wrong one.
	//
	// The graph had three of these. Nothing in it says what a name means,
	// so this asks only whether two names are the same word, which holds
	// in any language.
	if parent := models.ParentPath(path); parent != "" {
		above, err := tx.GetAgentNode(agentId, parent)
		if err != nil {
			return nil, err
		}
		if above != nil && above.Kind == kind && sameName(above, path, name) {
			return above, nil
		}
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
	// Asked down to the lower of the two floors, because a candidate that
	// only qualifies with the name inside it is not near enough to come
	// back at the other.
	scores, err := tx.Nearest(db.AgentNodeTable, agentId, sense.ModelName, sense.Vector, samePageCandidates, db.VectorQuery{
		Floor: sameNameFloor,
	})
	if err != nil {
		return nil, err
	}
	nearness := make(map[string]float64, len(scores))
	for _, score := range scores {
		nearness[score.ID] = score.Score
	}
	candidates, err := tx.GetAgentNodes(agentId, idsOf(scores))
	if err != nil {
		return nil, err
	}
	for _, candidate := range orderNodes(candidates, idsOf(scores)) {
		if candidate.Kind != kind {
			continue
		}
		near := nearness[candidate.ID]
		if near < samePageFloor && !(near >= sameNameFloor && oneNameIsInsideTheOther(candidate.Name, name)) {
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
	// The root of the graph is folders and the person's own page. A page
	// filed straight at the root sits beside people/ and projects/, where
	// nothing lists it under a folder and the dashboard can neither move
	// nor merge it, since those are hidden on a root. It goes under the
	// folder its kind belongs to instead.
	if !strings.Contains(path, "/") && path != models.PathSelf && kind != models.NodeFolder {
		path = models.JoinPath(folderOfKind(kind), path)
	}
	// A root folder said twice. A model reading a directory of people, in
	// a source that keeps them under a folder of its own, files the first
	// one at "people/people/alice-chen": the prompt's rule and the
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
// Only the front, and only an exact repeat: "people/people/alice-chen"
// becomes "people/alice-chen", while "people/alice-chen/people" is left as it
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

// oneNameIsInsideTheOther says whether every word of one page's name is
// among the other's.
//
// A subject is inside the same subject with a qualifier in front of it,
// and the same words in a different order are inside each other both
// ways, since order is not what this asks. Two names that merely share a
// word are not: each holds something the other does not.
//
// A name of one word is refused. A single common word can sit inside half
// a dozen names that have nothing to do with each other, and one word is
// too little to say two subjects are one. Nothing is listed anywhere:
// this compares two names to each other and knows no vocabulary, so it
// reads a name in any language the same way.
func oneNameIsInsideTheOther(one, other string) bool {
	first, second := wordsOfName(one), wordsOfName(other)
	if len(first) < 2 || len(second) < 2 {
		return false
	}
	return wordsWithin(first, second) || wordsWithin(second, first)
}

func wordsWithin(inner, outer map[string]bool) bool {
	for word := range inner {
		if !outer[word] {
			return false
		}
	}
	return true
}

// wordsOfName is a name as the set of words in it, lowered, with
// punctuation taken for a space so that "Alpha/Beta" is two words and
// "macOS client and gateway" is four.
func wordsOfName(name string) map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(name), func(letter rune) bool {
		return !unicode.IsLetter(letter) && !unicode.IsDigit(letter)
	}) {
		words[word] = true
	}
	return words
}
