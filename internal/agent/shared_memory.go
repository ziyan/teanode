package agent

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Reading what other people shared.
//
// A person shares a page, and everything under it, with a circle or with one
// person in a circle they share (db.PageShareOperation). The reader's agent,
// the dashboard, the command line and the prompt read the owner's rows as
// they are now, through the functions in this file and through nothing else.
// They are the only code that reads another agent's graph, and what a reader
// is not shown is left out here, once, so that every door leaves out the same
// things:
//
//   - a fact's quote, unless the owner chose to share the quotes, and where
//     it was read only in the owner's terms ("from Ada's mail"), never which
//     mail or which document;
//   - a link to a page that is not itself shared with the reader, and so the
//     name of every page of the owner's that is not;
//   - the night's reflections and lessons, which cite the owner's other pages
//     and say what nobody said;
//   - everything that is not about the page's words: its importance, its
//     contact card, its history, the identifiers of its rows.
//
// The summary and the overview are shown as they are written.
//
// Nothing read here is written anywhere. docs/decisions/
// 20261016-shared-knowledge-is-read-where-it-lives.md says why.

// sharedOpeningRunes is how much of a page's opening a listing carries.
const sharedOpeningRunes = 200

// sharedSearchLimit is the most pages and facts one search of shared pages
// returns of each.
const sharedSearchLimit = 20

// SharedMemoryIndexLines is the most roots the prompt's "Shared with you"
// section lists. The rest are counted and the call that lists them is named,
// so nothing is cut silently.
const SharedMemoryIndexLines = 20

// sharesOf are the shares among these that come from one owner.
func sharesOf(shares []*models.AgentPageShare, ownerUserId string) []*models.AgentPageShare {
	var kept []*models.AgentPageShare
	for _, share := range shares {
		if share.OwnerUserID == ownerUserId {
			kept = append(kept, share)
		}
	}
	return kept
}

// coveringShares are the shares among these that reach the page.
func coveringShares(shares []*models.AgentPageShare, pagePath string) []*models.AgentPageShare {
	var kept []*models.AgentPageShare
	for _, share := range shares {
		if share.Covers(pagePath) {
			kept = append(kept, share)
		}
	}
	return kept
}

// openingOf is the first sentence or line of a page's summary, cut to a
// listing's width with an ellipsis where it was cut.
func openingOf(summary string) string {
	summary = strings.TrimSpace(summary)
	if index := strings.IndexAny(summary, ".\n"); index > 0 {
		summary = summary[:index]
	}
	summary = strings.TrimSpace(summary)
	if utf8.RuneCountInString(summary) > sharedOpeningRunes {
		summary = strings.TrimSpace(string([]rune(summary)[:sharedOpeningRunes])) + "…"
	}
	return summary
}

// SharedPagesFor are the roots of the subtrees shared with the reader, with
// the owner of each named, by owner and then path. A root nested under another
// of the same owner's is not listed twice: reading the outer one reaches it.
// A share whose page has since been deleted or moved is skipped, and still
// exists for its owner to take back.
func SharedPagesFor(tx db.Transaction, readerUserId string) ([]*models.SharedRoot, error) {
	shares, err := tx.ListPageSharesReaching(readerUserId)
	if err != nil {
		return nil, err
	}
	return sharedRootsOf(tx, shares)
}

func sharedRootsOf(tx db.Transaction, shares []*models.AgentPageShare) ([]*models.SharedRoot, error) {
	roots := []*models.SharedRoot{}
	byKey := map[string]*models.SharedRoot{}
	for _, share := range shares {
		key := share.OwnerUserID + "\x00" + share.Path
		if existing := byKey[key]; existing != nil {
			existing.ShareIDs = append(existing.ShareIDs, share.ID)
			existing.IsQuoteShared = existing.IsQuoteShared || share.IsQuoteShared
			continue
		}
		isNested := false
		for _, other := range shares {
			if other.OwnerUserID == share.OwnerUserID && other.Path != share.Path && other.Covers(share.Path) {
				isNested = true
				break
			}
		}
		if isNested {
			continue
		}
		node, err := tx.GetAgentNode(share.AgentID, share.Path)
		if err != nil {
			return nil, err
		}
		if node == nil {
			continue
		}
		root := &models.SharedRoot{
			ShareIDs:    []string{share.ID},
			OwnerUserID: share.OwnerUserID, OwnerUsername: share.OwnerUsername, OwnerName: share.OwnerName, OwnerUser: share.OwnerUser,
			Path: node.Path, Name: node.Name, NodeKind: node.Kind,
			Opening:       openingOf(node.Summary),
			IsQuoteShared: share.IsQuoteShared,
		}
		byKey[key] = root
		roots = append(roots, root)
	}
	return roots, nil
}

// ReadSharedPage is the page at this path of one owner's graph as the reader
// is shown it, or nil when no share reaching the reader covers the path, the
// owner has no page there, or the owner is not who the share says. The
// reader cannot tell which: a page that is not shared with them is not found,
// and neither is a sibling of one that is.
func ReadSharedPage(tx db.Transaction, readerUserId, ownerUserId, pagePath string) (*models.SharedPage, error) {
	shares, err := tx.ListPageSharesReaching(readerUserId)
	if err != nil {
		return nil, err
	}
	return readSharedPageFrom(tx, sharesOf(shares, ownerUserId), models.NormalizePath(pagePath))
}

// PreviewSharedPage is exactly what a grantee would be shown of a page if the
// owner made the share: the same view ReadSharedPage makes, from the shares
// the grantee already has from the owner and the one being considered. Nil
// when the owner has no page there. The grantee must be somebody the owner
// could share with; the caller has checked.
func PreviewSharedPage(tx db.Transaction, ownerUserId string, candidate *models.AgentPageShare, pagePath string) (*models.SharedPage, error) {
	owned, err := tx.ListPageSharesBy(ownerUserId)
	if err != nil {
		return nil, err
	}
	var considered []*models.AgentPageShare
	for _, share := range owned {
		isSameGrantee := (candidate.GranteeUserID != "" && share.GranteeUserID == candidate.GranteeUserID) ||
			(candidate.GranteeCircleID != "" && share.GranteeCircleID == candidate.GranteeCircleID)
		// The share being considered replaces one the owner made already
		// with the same person or circle at the same path.
		if isSameGrantee && share.Path == candidate.Path {
			continue
		}
		if isSameGrantee {
			considered = append(considered, share)
		}
	}
	ownerAgent, err := tx.GetAgentByUser(ownerUserId)
	if err != nil || ownerAgent == nil {
		return nil, err
	}
	owner, err := tx.GetUser(ownerUserId)
	if err != nil || owner == nil {
		return nil, err
	}
	made := *candidate
	made.AgentID, made.OwnerUserID = ownerAgent.ID, ownerUserId
	made.OwnerUsername, made.OwnerName, made.OwnerUser = owner.Username, owner.DisplayName(), owner
	considered = append(considered, &made)
	return readSharedPageFrom(tx, considered, models.NormalizePath(pagePath))
}

// readSharedPageFrom makes the reader's view of a page from the shares that
// reach them, all of them from one owner.
func readSharedPageFrom(tx db.Transaction, shares []*models.AgentPageShare, pagePath string) (*models.SharedPage, error) {
	covering := coveringShares(shares, pagePath)
	if len(covering) == 0 {
		return nil, nil
	}
	agentId := covering[0].AgentID
	node, err := tx.GetAgentNode(agentId, pagePath)
	if err != nil || node == nil {
		return nil, err
	}
	isQuoteShared := false
	for _, share := range covering {
		isQuoteShared = isQuoteShared || share.IsQuoteShared
	}
	ownerName := covering[0].OwnerName
	page := &models.SharedPage{
		OwnerUserID: covering[0].OwnerUserID, OwnerUsername: covering[0].OwnerUsername, OwnerName: ownerName, OwnerUser: covering[0].OwnerUser,
		Path: node.Path, Name: node.Name, NodeKind: node.Kind, Aliases: node.Aliases,
		Opening: strings.TrimSpace(node.Summary), Overview: strings.TrimSpace(node.Overview), OverviewWrittenAt: node.OverviewWrittenAt,
		IsQuoteShared: isQuoteShared,
		Facts:         []*models.SharedFact{}, Edges: []*models.SharedEdge{}, Children: []*models.SharedChild{},
	}
	if page.Aliases == nil {
		page.Aliases = []string{}
	}

	facts, err := tx.ListAgentFacts(agentId, node.ID, false, everyFactOnPage)
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
		if fact.Kind.FromItsOwnReasoning() {
			continue
		}
		page.Facts = append(page.Facts, sharedFactOf(fact, ownerName, isQuoteShared))
	}

	edges, err := tx.ListAgentEdges(agentId, node.ID)
	if err != nil {
		return nil, err
	}
	for _, edge := range edges {
		if edge.Relation == models.EdgePartOf {
			continue
		}
		// A link is shown only when the page at its far end is shared with
		// the reader too. Otherwise the link would name a page they were
		// not given.
		farPath := edge.ToPath
		if edge.ToPath == node.Path {
			farPath = edge.FromPath
		}
		if len(coveringShares(shares, farPath)) == 0 {
			continue
		}
		page.Edges = append(page.Edges, &models.SharedEdge{
			FromPath: edge.FromPath, ToPath: edge.ToPath, FromName: edge.FromName, ToName: edge.ToName,
			Relation: edge.Relation, Note: edge.Note, IsProposed: edge.Proposed(),
		})
	}
	sort.SliceStable(page.Edges, func(first, second int) bool {
		left, right := page.Edges[first], page.Edges[second]
		if left.Relation != right.Relation {
			return left.Relation < right.Relation
		}
		if left.FromPath != right.FromPath {
			return left.FromPath < right.FromPath
		}
		return left.ToPath < right.ToPath
	})

	children, err := tx.ListAgentNodeChildren(agentId, node.ID)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		// Under a shared page is shared with it; the check is only for a
		// child filed somewhere the path does not say.
		if child.Dormant || !models.PathIsWithin(child.Path, node.Path) {
			continue
		}
		page.Children = append(page.Children, &models.SharedChild{
			Path: child.Path, Name: child.Name, NodeKind: child.Kind, Opening: openingOf(child.Summary),
		})
	}
	return page, nil
}

// sharedFactOf is one fact as a reader is shown it.
func sharedFactOf(fact *models.AgentFact, ownerName string, isQuoteShared bool) *models.SharedFact {
	shown := &models.SharedFact{
		Number: fact.Number, FactKind: fact.Kind, Text: strings.TrimSpace(fact.Text),
		HappenedAt: fact.HappenedAt, HappenedPrecision: fact.HappenedPrecision,
		SaidAt: fact.SaidAt(), IsInferred: fact.Inferred,
		EvidenceSource: sharedSourceLine(ownerName, fact.Evidence),
	}
	if isQuoteShared {
		for _, evidence := range fact.Evidence {
			if strings.TrimSpace(evidence.Quote) != "" && isReadableEvidence(evidence.Kind) {
				shown.EvidenceQuote = strings.TrimSpace(evidence.Quote)
				break
			}
		}
	}
	return shown
}

// isReadableEvidence says the evidence is something somebody read or said,
// as opposed to the agent's own walk through its pages (a page cited by path
// names a page the reader may not have been given).
func isReadableEvidence(kind models.EvidenceKind) bool {
	return kind != "" && kind != models.EvidenceMemory && kind != models.EvidenceDream
}

// sharedSourceWords is how each kind of evidence is named to a reader.
var sharedSourceWords = map[models.EvidenceKind]string{
	models.EvidenceConversation:        "conversation",
	models.EvidenceConversationMessage: "conversation",
	models.EvidenceMail:                "mail",
	models.EvidenceDocument:            "documents",
	models.EvidenceCommit:              "commits",
	models.EvidenceChat:                "chat",
	models.EvidenceContact:             "contacts",
	models.EvidenceRepository:          "repositories",
	models.EvidencePerson:              "own words",
	models.EvidenceCalendarEvent:       "calendar",
	models.EvidenceReminder:            "reminders",
	models.EvidenceFinanceTransaction:  "finances",
	models.EvidenceFinanceReceipt:      "receipts",
	models.EvidenceRecordingPassage:    "recordings",
}

// sharedSourceLine says where a fact was read in the owner's terms, "from
// Ada's mail" or "from Ada's mail and conversation", and never which mail:
// the reader is told what kind of thing the owner's agent read, not given the
// thing. Empty when the fact has none of a kind that can be named.
func sharedSourceLine(ownerName string, evidence []models.Evidence) string {
	var words []string
	seen := map[string]bool{}
	for _, each := range evidence {
		word := sharedSourceWords[each.Kind]
		if word == "" || seen[word] {
			continue
		}
		seen[word] = true
		words = append(words, word)
	}
	if len(words) == 0 {
		return ""
	}
	possessive := ownerName + "'s"
	switch len(words) {
	case 1:
		return "from " + possessive + " " + words[0]
	case 2:
		return "from " + possessive + " " + words[0] + " and " + words[1]
	}
	return "from " + possessive + " " + strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// SearchSharedPages finds, by words, the pages and facts of shared pages the
// reader may read, each marked with its owner. The search runs in each
// owner's graph restricted to the roots shared with the reader, so a page
// outside them cannot be found by a word on it. By words alone, whatever the
// reader's own search does: the vectors are the owner's.
func SearchSharedPages(tx db.Transaction, readerUserId, query string, limit int) ([]*models.SharedHit, error) {
	if limit <= 0 {
		limit = sharedSearchLimit
	}
	shares, err := tx.ListPageSharesReaching(readerUserId)
	if err != nil {
		return nil, err
	}
	var hits []*models.SharedHit
	for _, ownerShares := range groupSharesByOwner(shares) {
		roots := make([]string, 0, len(ownerShares))
		for _, share := range ownerShares {
			roots = append(roots, share.Path)
		}
		agentId := ownerShares[0].AgentID
		nodes, facts, err := tx.SearchAgentGraphWithin(agentId, roots, query, limit)
		if err != nil {
			return nil, err
		}
		owner := ownerShares[0]
		for _, node := range nodes {
			hits = append(hits, &models.SharedHit{
				OwnerUserID: owner.OwnerUserID, OwnerUsername: owner.OwnerUsername, OwnerName: owner.OwnerName,
				Path: node.Path, Name: node.Name, Opening: openingOf(node.Summary),
			})
		}
		if len(facts) == 0 {
			continue
		}
		nodeIds := make([]string, 0, len(facts))
		for _, fact := range facts {
			nodeIds = append(nodeIds, fact.NodeID)
		}
		onPages, err := tx.GetAgentNodes(agentId, nodeIds)
		if err != nil {
			return nil, err
		}
		pathOf := make(map[string]*models.AgentNode, len(onPages))
		for _, node := range onPages {
			pathOf[node.ID] = node
		}
		for _, fact := range facts {
			node := pathOf[fact.NodeID]
			if node == nil || fact.Kind.FromItsOwnReasoning() {
				continue
			}
			hits = append(hits, &models.SharedHit{
				OwnerUserID: owner.OwnerUserID, OwnerUsername: owner.OwnerUsername, OwnerName: owner.OwnerName,
				Path: node.Path, Name: node.Name, FactNumber: fact.Number, Text: strings.TrimSpace(fact.Text),
			})
		}
	}
	return hits, nil
}

// groupSharesByOwner is the shares in groups of one owner each, the owners in
// the order the shares came.
func groupSharesByOwner(shares []*models.AgentPageShare) [][]*models.AgentPageShare {
	var order []string
	byOwner := map[string][]*models.AgentPageShare{}
	for _, share := range shares {
		if _, seen := byOwner[share.OwnerUserID]; !seen {
			order = append(order, share.OwnerUserID)
		}
		byOwner[share.OwnerUserID] = append(byOwner[share.OwnerUserID], share)
	}
	groups := make([][]*models.AgentPageShare, 0, len(order))
	for _, ownerUserId := range order {
		groups = append(groups, byOwner[ownerUserId])
	}
	return groups
}

// SharedIndexLines is the "Shared with you" section of the index a prompt
// carries: each shared root with its opening and whose it is, at most
// SharedMemoryIndexLines of them, and then how many more there are and the
// call that lists them, so that the section never ends without saying it
// does. Empty when nothing is shared with the reader.
//
// The order is by owner and path and changes only when a share does, so the
// section is as steady as the rest of the index is and the prompt's front
// stays a cache hit between turns.
func SharedIndexLines(tx db.Transaction, readerUserId string) ([]string, error) {
	roots, err := SharedPagesFor(tx, readerUserId)
	if err != nil || len(roots) == 0 {
		return nil, err
	}
	lines := []string{"Shared with you by other people (read only; `memory` get with owner:<username> reads one, and the answer is cited as path#number (shared by name)):"}
	for index, root := range roots {
		if index >= SharedMemoryIndexLines {
			lines = append(lines, fmt.Sprintf("(%d more; memory index owner:<username> lists them)", len(roots)-SharedMemoryIndexLines))
			break
		}
		line := "- " + root.Path
		if name := strings.TrimSpace(root.Name); name != "" && !strings.EqualFold(name, models.LastSegment(root.Path)) {
			line += " - " + name
		}
		if root.Opening != "" {
			line += ": " + cutMarked(root.Opening, 120)
		}
		line += fmt.Sprintf(" (shared by %s, owner:%s)", root.OwnerName, root.OwnerUsername)
		lines = append(lines, line)
	}
	return lines, nil
}
