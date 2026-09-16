package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The graph as the dashboard and the command line reach it: pages
// addressed by path, the facts on them, what the nightly run did, and the
// sources the person has pointed their agent at.
//
// Everything is the caller's own. A person reads and edits their own
// agent's pages and nobody else's; there is no permission that grants a
// view of somebody else's, because the whole of it is about them.

// AgentGraphQuery reads the graph.
type AgentGraphQuery interface {
	// The top of the graph, as the prompt carries it. Needs agent:use.
	AgentGraphIndex(ctx context.Context, arguments AgentGraphIndexArguments) ([]*models.AgentNode, error)

	// One page with its facts, its links and what is under it. Needs
	// agent:use.
	AgentGraphPage(ctx context.Context, arguments AgentGraphPageArguments) (*AgentGraphPageResult, error)

	// The pages directly under one, a page at a time, most important
	// first, with the total. Needs agent:use.
	AgentGraphChildren(ctx context.Context, arguments AgentGraphChildrenArguments) (*AgentGraphChildrenResult, error)

	// One page with everything one step away from it -- what it is under,
	// what is under it, and what it is linked to -- for an explorer that
	// moves a step at a time. Needs agent:use.
	AgentGraphNeighbours(ctx context.Context, arguments AgentGraphPageArguments) (*AgentGraphNeighboursResult, error)

	// Pages and facts by words. Needs agent:use.
	SearchAgentGraph(ctx context.Context, arguments SearchAgentGraphArguments) (*AgentGraphSearchResult, error)

	// What has been filed lately: what the agent page shows under
	// "Learned". Needs agent:use.
	ListAgentLearned(ctx context.Context, arguments ListAgentLearnedArguments) ([]*AgentLearnedFact, error)

	// What has happened to one page, newest first: every change, who
	// made it, and what it moved. Needs agent:use.
	ListAgentPageHistory(ctx context.Context, arguments ListAgentPageHistoryArguments) ([]*AgentPageRevision, error)

	// What the nightly run did, newest first. Needs agent:use.
	ListAgentDreams(ctx context.Context, arguments ListAgentDreamsArguments) ([]*models.AgentDream, error)

	// The places the person has pointed their agent at. Needs agent:use.
	ListAgentKnowledgeSources(ctx context.Context) ([]*models.AgentKnowledgeSource, error)
}

// AgentGraphMutation changes it.
type AgentGraphMutation interface {
	// Write a page: its summary, its name, whether it is pinned. Needs
	// agent:use.
	SaveAgentNode(ctx context.Context, arguments SaveAgentNodeArguments) (*models.AgentNode, error)

	// Put a page under another. Needs agent:use.
	MoveAgentNode(ctx context.Context, arguments MoveAgentNodeArguments) (*models.AgentNode, error)

	// Remove a page and everything under it. Needs agent:use.
	DeleteAgentNode(ctx context.Context, arguments DeleteAgentNodeArguments) (bool, error)

	// Put a fact on a page, or change one. Needs agent:use.
	SaveAgentFact(ctx context.Context, arguments SaveAgentFactArguments) (*models.AgentFact, error)

	// Strike a fact. What the person struck is shown to the next filing
	// run as an example of what not to keep, which is the only way that
	// run learns anything. Needs agent:use.
	DeleteAgentFact(ctx context.Context, arguments DeleteAgentFactArguments) (bool, error)

	// Name the contact that is the caller themselves. Needs agent:use.
	SetMyContact(ctx context.Context, arguments SetMyContactArguments) (bool, error)

	// Add, change or remove a knowledge source. Needs agent:use.
	SaveAgentKnowledgeSource(ctx context.Context, arguments SaveAgentKnowledgeSourceArguments) (*models.AgentKnowledgeSource, error)
	DeleteAgentKnowledgeSource(ctx context.Context, arguments DeleteAgentKnowledgeSourceArguments) (bool, error)

	// Read a source again now, rather than waiting for its time. Needs
	// agent:use.
	SyncAgentKnowledgeSource(ctx context.Context, arguments DeleteAgentKnowledgeSourceArguments) (bool, error)

	// Run the night now, within the agent's own hours, rather than
	// waiting for its next turn. Needs agent:use.
	DreamAgentNow(ctx context.Context) (bool, error)

	// Join two pages, or take the join away. What the agent's memory
	// tool does with `link`; here so the person can do it too. Needs
	// agent:use.
	LinkAgentNodes(ctx context.Context, arguments LinkAgentNodesArguments) (bool, error)
	UnlinkAgentNodes(ctx context.Context, arguments LinkAgentNodesArguments) (bool, error)

	// Let a directory the scan flagged as being about other people in.
	// Needs agent:use.
	AllowAgentKnowledgeDirectory(ctx context.Context, arguments AllowAgentKnowledgeDirectoryArguments) (*models.AgentKnowledgeSource, error)
}

// The arguments.

type AgentGraphIndexArguments struct {
	// Under narrows to a subtree; empty is the whole graph.
	Under string `json:"under" graphapi:"nullable"`
	First int    `json:"first" graphapi:"nullable"`
}

type AgentGraphPageArguments struct {
	Path string `json:"path"`
}

type AgentGraphChildrenArguments struct {
	Path   string `json:"path"`
	First  int    `json:"first" graphapi:"nullable"`
	Offset int    `json:"offset" graphapi:"nullable"`
}

// AgentGraphChild is one row of a folder: the page, and one line that
// tells it from the row above -- its opening where it has one, its first
// fact where it does not. A list of bare names is the thing a person
// cannot use, and most project pages have no opening yet.
type AgentGraphChild struct {
	Node *models.AgentNode `json:"node"`
	Hint string            `json:"hint"`
}

// AgentGraphChildrenResult is one page of a folder and how big the folder
// is.
type AgentGraphChildrenResult struct {
	Rows  []*AgentGraphChild `json:"rows"`
	Total int                `json:"total"`
}

// AgentGraphNeighbour is a page one step away and how it is reached: the
// relation for a link, "part_of" for the parent, "" for a child.
type AgentGraphNeighbour struct {
	Node     *models.AgentNode `json:"node"`
	Relation string            `json:"relation"`
	Outward  bool              `json:"outward"`
	Note     string            `json:"note"`
}

// AgentGraphNeighboursResult is a page and the pages one step from it.
type AgentGraphNeighboursResult struct {
	Node       *models.AgentNode      `json:"node"`
	Parent     *models.AgentNode      `json:"parent"`
	Neighbours []*AgentGraphNeighbour `json:"neighbours"`
	Children   int                    `json:"children"`
}

type SearchAgentGraphArguments struct {
	Query string `json:"query"`
	First int    `json:"first" graphapi:"nullable"`
}

type ListAgentLearnedArguments struct {
	// Days is how far back to look; zero is one day.
	Days  int `json:"days" graphapi:"nullable"`
	First int `json:"first" graphapi:"nullable"`
}

type ListAgentPageHistoryArguments struct {
	Path  string `json:"path"`
	First int    `json:"first" graphapi:"nullable"`
}

type ListAgentDreamsArguments struct {
	First int `json:"first" graphapi:"nullable"`
}

type SaveAgentNodeArguments struct {
	Path    string   `json:"path"`
	Kind    string   `json:"kind" graphapi:"nullable"`
	Name    string   `json:"name" graphapi:"nullable"`
	Summary string   `json:"summary" graphapi:"nullable"`
	Aliases []string `json:"aliases" graphapi:"nullable"`
	Pinned  *bool    `json:"pinned" graphapi:"nullable"`
}

type MoveAgentNodeArguments struct {
	Path  string `json:"path"`
	Under string `json:"under"`
}

type LinkAgentNodesArguments struct {
	Path     string `json:"path"`
	To       string `json:"to"`
	Relation string `json:"relation"`
	Note     string `json:"note" graphapi:"nullable"`
}

type DeleteAgentNodeArguments struct {
	Path string `json:"path"`
}

type SaveAgentFactArguments struct {
	Path      string   `json:"path"`
	Number    int      `json:"number" graphapi:"nullable"`
	Kind      string   `json:"kind" graphapi:"nullable"`
	Text      string   `json:"text"`
	Happened  string   `json:"happened" graphapi:"nullable"`
	Audiences []string `json:"audiences" graphapi:"nullable"`
}

type DeleteAgentFactArguments struct {
	Path   string `json:"path"`
	Number int    `json:"number"`
}

type SetMyContactArguments struct {
	// ContactID is the address book entry that is the caller; empty
	// clears it.
	ContactID string `json:"contactId" graphapi:"nullable"`
}

type SaveAgentKnowledgeSourceArguments struct {
	SourceID string `json:"sourceId" graphapi:"nullable"`
	Kind     string `json:"kind" graphapi:"nullable"`
	Name     string `json:"name" graphapi:"nullable"`
	Computer string `json:"computer" graphapi:"nullable"`
	Path     string `json:"path" graphapi:"nullable"`
	Format   string `json:"format" graphapi:"nullable"`
	RootPath string `json:"rootPath" graphapi:"nullable"`
	Cron     string `json:"cron" graphapi:"nullable"`
	Enabled  *bool  `json:"enabled" graphapi:"nullable"`

	// MailboxID is which mailbox a sent source reads.
	MailboxID string `json:"mailboxId" graphapi:"nullable"`
}

type DeleteAgentKnowledgeSourceArguments struct {
	SourceID string `json:"sourceId"`
}

type AllowAgentKnowledgeDirectoryArguments struct {
	SourceID string `json:"sourceId"`
	Name     string `json:"name"`
}

// AgentPageRevision is one change to a page, as somebody reads it.
//
// Flattened rather than passed through as it is stored, because what a
// revision holds depends on what it changed and a schema cannot say
// "whatever moved". Summary is the line; Before and After are the words
// where words moved, so a change to a summary can be read as a diff.
type AgentPageRevision struct {
	Revision int    `json:"revision"`
	Kind     string `json:"kind"`
	Actor    string `json:"actor"`

	// Summary is the change in a sentence: who did it and what they did.
	// Change is the same without the who, for a reader that shows the
	// actor beside it rather than in it.
	Summary string `json:"summary"`
	Change  string `json:"change"`

	// Before and After are the text either side of the change, empty for
	// a change that moved something other than words.
	Before string `json:"before"`
	After  string `json:"after"`

	// Path is the other page a change points at: where this one was
	// filed, or the far end of a link.
	Path string `json:"path"`

	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

// AgentGraphPageResult is a page and everything a reader of it wants.
type AgentGraphPageResult struct {
	Node     *models.AgentNode   `json:"node"`
	Facts    []*models.AgentFact `json:"facts"`
	Edges    []*models.AgentEdge `json:"edges"`
	Children []*models.AgentNode `json:"children"`

	// Contact is the address book entry this page is about, for a person.
	Contact *models.Contact `json:"contact" graphapi:"nullable"`
}

// AgentGraphSearchResult is what words found.
type AgentGraphSearchResult struct {
	Nodes []*models.AgentNode `json:"nodes"`
	Facts []*AgentLearnedFact `json:"facts"`
}

// AgentLearnedFact is a fact with the path of the page it is on, which is
// what a reader needs and the fact itself does not carry.
type AgentLearnedFact struct {
	Fact *models.AgentFact `json:"fact"`
	Path string            `json:"path"`
	Name string            `json:"name"`
}

// --- reading ----------------------------------------------------------

func (self *graph) AgentGraphIndex(ctx context.Context, arguments AgentGraphIndexArguments) ([]*models.AgentNode, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if err := tx.EnsureAgentRoots(found.ID); err != nil {
		return nil, err
	}
	limit := arguments.First
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	if under := models.NormalizePath(arguments.Under); under != "" {
		return tx.ListAgentNodesUnder(found.ID, under, limit)
	}
	return tx.ListAgentNodesUnder(found.ID, "", limit)
}

func (self *graph) AgentGraphPage(ctx context.Context, arguments AgentGraphPageArguments) (*AgentGraphPageResult, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	path := models.NormalizePath(arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("which page?")
	}
	tx := self.transaction(ctx)
	if err := tx.EnsureAgentRoots(found.ID); err != nil {
		return nil, err
	}
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, nil
	}
	result := &AgentGraphPageResult{Node: node}
	if result.Facts, err = tx.ListAgentFacts(found.ID, node.ID, false, 500); err != nil {
		return nil, err
	}
	if result.Edges, err = tx.ListAgentEdges(found.ID, node.ID); err != nil {
		return nil, err
	}
	if result.Children, err = tx.ListAgentNodeChildren(found.ID, node.ID); err != nil {
		return nil, err
	}
	// The card behind a person's page, and behind the self page the
	// person's own -- read live rather than copied, so a number changed
	// in one place is right everywhere.
	contactId := node.ContactID
	if node.Kind == models.NodeSelf {
		owner, err := tx.GetUser(found.UserID)
		if err != nil {
			return nil, err
		}
		if owner != nil {
			contactId = owner.ContactID
		}
	}
	if contactId != "" {
		result.Contact, err = self.contactOfUser(tx, found.UserID, contactId)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// contactOfUser finds one of the person's contacts across their books.
func (self *graph) contactOfUser(tx db.Transaction, userId, contactId string) (*models.Contact, error) {
	books, err := tx.ListAddressBooks(userId)
	if err != nil {
		return nil, err
	}
	for _, book := range books {
		contact, err := tx.GetContact(book.ID, contactId)
		if err != nil {
			return nil, err
		}
		if contact != nil {
			return contact, nil
		}
	}
	return nil, nil
}

func (self *graph) AgentGraphChildren(ctx context.Context, arguments AgentGraphChildrenArguments) (*AgentGraphChildrenResult, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if err := tx.EnsureAgentRoots(found.ID); err != nil {
		return nil, err
	}
	// An empty path is the top of the graph: the roots.
	parentId := ""
	if path := models.NormalizePath(arguments.Path); path != "" {
		node, err := tx.GetAgentNode(found.ID, path)
		if err != nil {
			return nil, err
		}
		if node == nil {
			return &AgentGraphChildrenResult{Rows: []*AgentGraphChild{}}, nil
		}
		parentId = node.ID
	}
	nodes, total, err := tx.ListAgentNodeChildrenPage(found.ID, parentId, arguments.First, arguments.Offset)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(nodes))
	for _, child := range nodes {
		if strings.TrimSpace(child.Summary) == "" {
			ids = append(ids, child.ID)
		}
	}
	lines, err := tx.FirstAgentFactLines(found.ID, ids)
	if err != nil {
		return nil, err
	}
	rows := make([]*AgentGraphChild, 0, len(nodes))
	for _, child := range nodes {
		hint := firstLine(child.Summary)
		if hint == "" {
			hint = lines[child.ID]
		}
		rows = append(rows, &AgentGraphChild{Node: child, Hint: hint})
	}
	return &AgentGraphChildrenResult{Rows: rows, Total: int(total)}, nil
}

// firstLine is the first line of some text with any markdown heading
// marks taken off, for a hint that has to fit on one line.
func firstLine(text string) string {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if line != "" {
			return line
		}
	}
	return ""
}

// neighbourLimit bounds what an explorer draws around one page. A page
// with three hundred children is a folder, and a folder is browsed as a
// list; the explorer shows the first few and says how many there are.
const neighbourLimit = 24

func (self *graph) AgentGraphNeighbours(ctx context.Context, arguments AgentGraphPageArguments) (*AgentGraphNeighboursResult, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	path := models.NormalizePath(arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("which page?")
	}
	tx := self.transaction(ctx)
	if err := tx.EnsureAgentRoots(found.ID); err != nil {
		return nil, err
	}
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil || node == nil {
		return nil, err
	}
	result := &AgentGraphNeighboursResult{Node: node, Neighbours: []*AgentGraphNeighbour{}}
	if node.ParentID != "" {
		if result.Parent, err = tx.GetAgentNodeByID(found.ID, node.ParentID); err != nil {
			return nil, err
		}
	}
	// Links first: they are the relations somebody stated, and the
	// explorer is for those. Children fill what room is left.
	edges, err := tx.ListAgentEdges(found.ID, node.ID)
	if err != nil {
		return nil, err
	}
	far := make([]string, 0, len(edges))
	for _, edge := range edges {
		if edge.FromID == node.ID {
			far = append(far, edge.ToID)
		} else {
			far = append(far, edge.FromID)
		}
	}
	farNodes, err := tx.GetAgentNodes(found.ID, far)
	if err != nil {
		return nil, err
	}
	byId := make(map[string]*models.AgentNode, len(farNodes))
	for _, other := range farNodes {
		byId[other.ID] = other
	}
	for _, edge := range edges {
		outward := edge.FromID == node.ID
		id := edge.ToID
		if !outward {
			id = edge.FromID
		}
		if other := byId[id]; other != nil && !other.Dormant {
			result.Neighbours = append(result.Neighbours, &AgentGraphNeighbour{
				Node: other, Relation: string(edge.Relation), Outward: outward, Note: edge.Note,
			})
		}
	}
	children, total, err := tx.ListAgentNodeChildrenPage(found.ID, node.ID, neighbourLimit, 0)
	if err != nil {
		return nil, err
	}
	result.Children = int(total)
	for _, child := range children {
		if len(result.Neighbours) >= neighbourLimit*2 {
			break
		}
		result.Neighbours = append(result.Neighbours, &AgentGraphNeighbour{Node: child, Relation: "", Outward: true})
	}
	return result, nil
}

func (self *graph) SearchAgentGraph(ctx context.Context, arguments SearchAgentGraphArguments) (*AgentGraphSearchResult, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		return &AgentGraphSearchResult{Nodes: []*models.AgentNode{}, Facts: []*AgentLearnedFact{}}, nil
	}
	limit := arguments.First
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	tx := self.transaction(ctx)
	nodes, facts, err := tx.SearchAgentGraph(found.ID, query, limit)
	if err != nil {
		return nil, err
	}
	withPaths, err := self.factsWithPaths(tx, found.ID, facts)
	if err != nil {
		return nil, err
	}
	if nodes == nil {
		nodes = []*models.AgentNode{}
	}
	return &AgentGraphSearchResult{Nodes: nodes, Facts: withPaths}, nil
}

func (self *graph) ListAgentLearned(ctx context.Context, arguments ListAgentLearnedArguments) ([]*AgentLearnedFact, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	days := arguments.Days
	if days <= 0 {
		days = 1
	}
	if days > 365 {
		days = 365
	}
	limit := arguments.First
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	tx := self.transaction(ctx)
	facts, err := tx.ListAgentFactsLearnedSince(found.ID, time.Now().AddDate(0, 0, -days), limit)
	if err != nil {
		return nil, err
	}
	return self.factsWithPaths(tx, found.ID, facts)
}

// factsWithPaths gives each fact the path of the page it sits on: what a
// reader needs to make sense of it, and what the fact itself does not
// carry.
func (self *graph) factsWithPaths(tx db.Transaction, agentId string, facts []*models.AgentFact) ([]*AgentLearnedFact, error) {
	rows := make([]*AgentLearnedFact, 0, len(facts))
	if len(facts) == 0 {
		return rows, nil
	}
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.NodeID)
	}
	nodes, err := tx.GetAgentNodes(agentId, ids)
	if err != nil {
		return nil, err
	}
	byId := make(map[string]*models.AgentNode, len(nodes))
	for _, node := range nodes {
		byId[node.ID] = node
	}
	for _, fact := range facts {
		row := &AgentLearnedFact{Fact: fact}
		if node := byId[fact.NodeID]; node != nil {
			row.Path = node.Path
			row.Name = node.Name
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// ListAgentPageHistory is what has happened to one page.
//
// A page whose facts were written by a nightly run and a page the person
// wrote themselves look identical until somebody asks this: it is the
// only place the graph says where a sentence came from.
func (self *graph) ListAgentPageHistory(ctx context.Context, arguments ListAgentPageHistoryArguments) ([]*AgentPageRevision, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	path := models.NormalizePath(arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("which page?")
	}
	limit := arguments.First
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	tx := self.transaction(ctx)
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil || node == nil {
		return []*AgentPageRevision{}, err
	}
	revisions, err := tx.ListAgentRevisions(found.ID, node.ID, limit)
	if err != nil {
		return nil, err
	}
	rows := make([]*AgentPageRevision, 0, len(revisions))
	for _, revision := range revisions {
		rows = append(rows, &AgentPageRevision{
			Revision: revision.Revision, Kind: string(revision.Kind), Actor: string(revision.Actor),
			Summary: revision.Describe(), Change: revision.Change(),
			Before: revision.TextBefore(), After: revision.TextAfter(),
			Path: revision.PathAfter(), Reason: revision.Reason, CreatedAt: revision.CreatedAt,
		})
	}
	return rows, nil
}

func (self *graph) ListAgentDreams(ctx context.Context, arguments ListAgentDreamsArguments) ([]*models.AgentDream, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	limit := arguments.First
	if limit <= 0 || limit > 100 {
		limit = 14
	}
	return self.transaction(ctx).ListAgentDreams(found.ID, limit)
}

func (self *graph) ListAgentKnowledgeSources(ctx context.Context) ([]*models.AgentKnowledgeSource, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	sources, err := self.transaction(ctx).ListAgentSources(found.ID)
	if err != nil {
		return nil, err
	}
	if sources == nil {
		sources = []*models.AgentKnowledgeSource{}
	}
	return sources, nil
}

// --- writing ----------------------------------------------------------

// writing is the transaction for a change the person is making, marked as
// theirs.
//
// It matters on every one of these: a page's history is only worth
// keeping if it can tell a sentence the person wrote from one a nightly
// run put there, and unmarked writes are recorded as the agent's.
func (self *graph) writing(ctx context.Context) db.Transaction {
	tx := self.transaction(ctx)
	tx.AsActor(models.ActorPerson)
	return tx
}

func (self *graph) SaveAgentNode(ctx context.Context, arguments SaveAgentNodeArguments) (*models.AgentNode, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	path := models.NormalizePath(arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("a page needs a path")
	}
	tx := self.writing(ctx)
	if err := tx.EnsureAgentRoots(found.ID); err != nil {
		return nil, err
	}
	existing, err := tx.GetAgentNode(found.ID, path)
	if err != nil {
		return nil, err
	}
	node := &models.AgentNode{AgentID: found.ID, Path: path}
	if existing != nil {
		node = existing
	} else {
		node.Kind = models.NodeTopic
		node.Name = models.LastSegment(path)
	}
	if kind := models.AgentNodeKind(strings.TrimSpace(arguments.Kind)); kind != "" && models.IsAgentNodeKind(kind) {
		node.Kind = kind
	}
	if arguments.Name != "" {
		node.Name = arguments.Name
	}
	// A summary the person wrote is theirs: an empty one clears the page
	// rather than leaving what the nightly run put there.
	node.Summary = arguments.Summary
	if arguments.Aliases != nil {
		node.Aliases = arguments.Aliases
	}
	if arguments.Pinned != nil {
		node.Pinned = *arguments.Pinned
	}
	return tx.PutAgentNode(node)
}

func (self *graph) MoveAgentNode(ctx context.Context, arguments MoveAgentNodeArguments) (*models.AgentNode, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.writing(ctx).MoveAgentNode(found.ID,
		models.NormalizePath(arguments.Path), models.NormalizePath(arguments.Under))
}

func (self *graph) DeleteAgentNode(ctx context.Context, arguments DeleteAgentNodeArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	path := models.NormalizePath(arguments.Path)
	// The roots are the shape of the graph rather than pages about
	// anything; removing one would leave the agent filing into nothing.
	for _, root := range models.AgentRoots {
		if root.Path == path {
			return false, fmt.Errorf("%s is one of the places things are filed, and cannot be removed", path)
		}
	}
	removed, err := self.writing(ctx).DeleteAgentNode(found.ID, path)
	return removed > 0, err
}

func (self *graph) SaveAgentFact(ctx context.Context, arguments SaveAgentFactArguments) (*models.AgentFact, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	path := models.NormalizePath(arguments.Path)
	text := strings.TrimSpace(arguments.Text)
	if path == "" || text == "" {
		return nil, fmt.Errorf("a fact needs a page and something to say")
	}
	tx := self.writing(ctx)
	if err := tx.EnsureAgentRoots(found.ID); err != nil {
		return nil, err
	}
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil {
		return nil, err
	}
	if node == nil {
		if node, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: found.ID, Path: path, Kind: models.NodeTopic,
			Name: models.LastSegment(path),
		}); err != nil {
			return nil, err
		}
	}
	kind := models.AgentFactKind(strings.TrimSpace(arguments.Kind))
	if !models.IsAgentFactKind(kind) {
		kind = models.FactPlain
	}
	var happened *time.Time
	if said := strings.TrimSpace(arguments.Happened); said != "" {
		for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
			if when, err := time.Parse(layout, said); err == nil {
				happened = &when
				break
			}
		}
	}
	audiences := []models.AgentAudience{models.AudienceAsk}
	for _, name := range arguments.Audiences {
		audience := models.AgentAudience(strings.TrimSpace(name))
		if audience != models.AudienceAsk && models.IsAgentAudience(audience) {
			audiences = append(audiences, audience)
		}
	}

	if arguments.Number > 0 {
		existing, err := tx.GetAgentFact(found.ID, node.ID, arguments.Number)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, fmt.Errorf("there is no %s#%d", path, arguments.Number)
		}
		return tx.UpdateAgentFact(found.ID, existing.ID, func(fact *models.AgentFact) error {
			fact.Text = text
			fact.Kind = kind
			fact.HappenedAt = happened
			fact.Audiences = audiences
			// Edited by hand, so it is theirs now rather than something
			// the agent worked out: an inference they corrected is a
			// fact.
			fact.Inferred = false
			fact.Confidence = 1
			fact.Evidence = append(fact.Evidence, models.Evidence{Kind: models.EvidencePerson})
			return nil
		})
	}
	written, err := tx.AddAgentFact(&models.AgentFact{
		AgentID: found.ID, NodeID: node.ID, Kind: kind, Text: text,
		HappenedAt: happened, Confidence: 1, Audiences: audiences,
		Evidence: []models.Evidence{{Kind: models.EvidencePerson}},
	})
	if err != nil {
		return nil, err
	}
	// And the same check the agent's own writers make: a page that
	// already says this keeps saying it once. Somebody adding a line by
	// hand has not reread the page either, and a page saying one thing
	// nine ways is worse than saying it once. The older keeps its number,
	// what came back is the fact that stands, and the page's history says
	// what happened.
	worker := self.agentWorker()
	if worker == nil {
		return written, nil
	}
	folded, err := worker.FoldIntoWhatThePageSays(ctx, tx, written, node)
	if err != nil {
		// The fact is written; only the tidying failed.
		log.Warningf("cannot fold %s#%d into what the page already says: %s", path, written.Number, err)
		return written, nil
	}
	return folded, nil
}

// DeleteAgentFact strikes a fact, and records that it was struck.
//
// The record is the point. The run that files what a conversation taught
// gets no other correction: it never sees whether what it filed was
// wanted. What the person struck is shown to the next one as an example
// of what not to keep.
func (self *graph) DeleteAgentFact(ctx context.Context, arguments DeleteAgentFactArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.writing(ctx)
	node, err := tx.GetAgentNode(found.ID, models.NormalizePath(arguments.Path))
	if err != nil || node == nil {
		return false, err
	}
	fact, err := tx.GetAgentFact(found.ID, node.ID, arguments.Number)
	if err != nil || fact == nil {
		return false, err
	}
	if _, err := tx.CreateAgentFeedback(&models.AgentFeedback{
		AgentID: found.ID, Kind: models.FeedbackUnlearned, Said: fact.Text,
	}); err != nil {
		return false, err
	}
	return true, tx.DeleteAgentFact(found.ID, fact.ID)
}

func (self *graph) SetMyContact(ctx context.Context, arguments SetMyContactArguments) (bool, error) {
	person, _, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.writing(ctx)
	contactId := strings.TrimSpace(arguments.ContactID)
	if contactId != "" {
		// Theirs, and one that exists: the card that is somebody is on
		// their account, so pointing it at a stranger's would be a way
		// to read one.
		contact, err := self.contactOfUser(tx, person.UserID(), contactId)
		if err != nil {
			return false, err
		}
		if contact == nil {
			return false, fmt.Errorf("there is no contact %q in their address books", contactId)
		}
	}
	return true, tx.SetUserContact(person.UserID(), contactId)
}

// --- sources ----------------------------------------------------------

func (self *graph) SaveAgentKnowledgeSource(ctx context.Context, arguments SaveAgentKnowledgeSourceArguments) (*models.AgentKnowledgeSource, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.writing(ctx)
	source := &models.AgentKnowledgeSource{AgentID: found.ID, Enabled: true}
	if arguments.SourceID != "" {
		existing, err := tx.GetAgentSource(found.ID, arguments.SourceID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, fmt.Errorf("there is no source %q", arguments.SourceID)
		}
		source = existing
	} else {
		source.Kind = models.AgentKnowledgeKind(strings.TrimSpace(arguments.Kind))
		source.Specification.Format = models.FormatFiles
	}
	if kind := models.AgentKnowledgeKind(strings.TrimSpace(arguments.Kind)); kind != "" {
		source.Kind = kind
	}
	if arguments.Name != "" {
		source.Name = arguments.Name
	}
	if arguments.Computer != "" {
		source.Specification.Computer = arguments.Computer
	}
	if arguments.Path != "" {
		source.Specification.Path = arguments.Path
	}
	if arguments.Format != "" {
		source.Specification.Format = arguments.Format
	}
	if arguments.MailboxID != "" {
		source.Specification.MailboxID = arguments.MailboxID
	}
	if arguments.RootPath != "" {
		source.RootPath = models.NormalizePath(arguments.RootPath)
	}
	if arguments.Cron != "" {
		source.Cron = arguments.Cron
	}
	if arguments.Enabled != nil {
		source.Enabled = *arguments.Enabled
	}
	if source.Name == "" {
		source.Name = models.Slug(source.Specification.Path)
		if source.Name == "" {
			source.Name = string(source.Kind)
		}
	}
	if source.Cron == "" {
		source.Cron = "17 3 * * *"
	}
	// Due now when it is new or has just been switched on: the person
	// asked for it, so the first pass should start rather than wait for
	// the small hours.
	if arguments.SourceID == "" || (arguments.Enabled != nil && *arguments.Enabled) {
		now := time.Now()
		source.NextRunAt = &now
	}
	return tx.PutAgentSource(source)
}

func (self *graph) DeleteAgentKnowledgeSource(ctx context.Context, arguments DeleteAgentKnowledgeSourceArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	return true, self.writing(ctx).DeleteAgentSource(found.ID, arguments.SourceID)
}

func (self *graph) SyncAgentKnowledgeSource(ctx context.Context, arguments DeleteAgentKnowledgeSourceArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.writing(ctx)
	source, err := tx.GetAgentSource(found.ID, arguments.SourceID)
	if err != nil || source == nil {
		return false, err
	}
	now := time.Now()
	source.NextRunAt = &now
	source.Enabled = true
	_, err = tx.PutAgentSource(source)
	return err == nil, err
}

func (self *graph) AllowAgentKnowledgeDirectory(ctx context.Context, arguments AllowAgentKnowledgeDirectoryArguments) (*models.AgentKnowledgeSource, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.writing(ctx)
	source, err := tx.GetAgentSource(found.ID, arguments.SourceID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fmt.Errorf("there is no source %q", arguments.SourceID)
	}
	name := strings.TrimSpace(arguments.Name)
	for _, allowed := range source.Allowed {
		if strings.EqualFold(allowed, name) {
			return source, nil
		}
	}
	source.Allowed = append(source.Allowed, name)
	// Read it again now, so the person sees the effect of saying yes.
	now := time.Now()
	source.NextRunAt = &now
	return tx.PutAgentSource(source)
}

func (self *graph) DreamAgentNow(ctx context.Context) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	// The night is due when it has not run for six hours; forgetting when
	// it last ran makes it due at the next tick. The hours the person set
	// still hold: a night asked for at noon runs when its hours begin.
	_, err = self.writing(ctx).UpdateAgent(found.ID, func(agent *models.Agent) error {
		agent.DreamedAt = nil
		return nil
	})
	return err == nil, err
}

func (self *graph) LinkAgentNodes(ctx context.Context, arguments LinkAgentNodesArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	fromNode, toNode, relation, err := self.endsOfLink(ctx, found.ID, arguments)
	if err != nil {
		return false, err
	}
	err = self.writing(ctx).PutAgentEdge(&models.AgentEdge{
		AgentID: found.ID, FromID: fromNode.ID, ToID: toNode.ID, Relation: relation,
		Note: strings.TrimSpace(arguments.Note),
	})
	return err == nil, err
}

func (self *graph) UnlinkAgentNodes(ctx context.Context, arguments LinkAgentNodesArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	fromNode, toNode, relation, err := self.endsOfLink(ctx, found.ID, arguments)
	if err != nil {
		return false, err
	}
	err = self.writing(ctx).DeleteAgentEdge(found.ID, fromNode.ID, toNode.ID, relation)
	return err == nil, err
}

// endsOfLink is the two pages a link joins and what the join is called,
// or why it cannot be made.
func (self *graph) endsOfLink(ctx context.Context, agentId string, arguments LinkAgentNodesArguments) (*models.AgentNode, *models.AgentNode, models.AgentEdgeRelation, error) {
	from, to := models.NormalizePath(arguments.Path), models.NormalizePath(arguments.To)
	if from == "" || to == "" {
		return nil, nil, "", fmt.Errorf("link what to what? give path and to")
	}
	relation := models.AgentEdgeRelation(strings.ToLower(strings.TrimSpace(arguments.Relation)))
	if relation == "" {
		relation = models.EdgeRelatedTo
	}
	if !models.IsAgentEdgeRelation(relation) {
		names := make([]string, 0, len(models.AgentEdgeRelations))
		for _, known := range models.AgentEdgeRelations {
			names = append(names, string(known))
		}
		return nil, nil, "", fmt.Errorf("%q is not a relation; use one of %s", arguments.Relation, strings.Join(names, ", "))
	}
	tx := self.transaction(ctx)
	fromNode, err := tx.GetAgentNode(agentId, from)
	if err != nil {
		return nil, nil, "", err
	}
	if fromNode == nil {
		return nil, nil, "", fmt.Errorf("there is no page at %s", from)
	}
	toNode, err := tx.GetAgentNode(agentId, to)
	if err != nil {
		return nil, nil, "", err
	}
	if toNode == nil {
		return nil, nil, "", fmt.Errorf("there is no page at %s", to)
	}
	return fromNode, toNode, relation, nil
}
