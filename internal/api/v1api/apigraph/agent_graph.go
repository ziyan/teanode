package apigraph

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/agent/indexed"
	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/api"
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

	// What a turn asking this question would be carried from the graph,
	// without asking it: the pages recall would expand and the facts on
	// each. Nothing is said to a model and nothing in the graph moves,
	// which is what makes it safe to replay a question set through.
	// Needs agent:use.
	RecallAgentMemory(ctx context.Context, arguments RecallAgentMemoryArguments) (*RecallAgentMemoryResult, error)

	// What has been filed lately: what the agent page shows under
	// "Learned". Needs agent:use.
	ListAgentLearned(ctx context.Context, arguments ListAgentLearnedArguments) ([]*AgentLearnedFact, error)

	// What has happened to one page, newest first: every change, who
	// made it, and what it moved. Needs agent:use.
	ListAgentPageHistory(ctx context.Context, arguments ListAgentPageHistoryArguments) ([]*AgentPageRevision, error)

	// What the nightly run did, newest first. Needs agent:use.
	ListAgentDreams(ctx context.Context, arguments ListAgentDreamsArguments) ([]*models.AgentDream, error)
	// AgentReadingProgress is how far the night has got through what was
	// indexed, and how long the rest takes at the pace of the last dreams.
	// Needs agent:use.
	AgentReadingProgress(ctx context.Context) (*reading.Progress, error)

	// The pictures and files behind citations the agent has already
	// written -- "work/mcx#3" -- so that a conversation can show the
	// evidence an answer rests on. Needs agent:use.
	AgentCitedAttachments(ctx context.Context, arguments AgentCitedAttachmentsArguments) ([]*AgentCitedAttachment, error)

	// The places the person has pointed their agent at. Needs agent:use.
	ListAgentKnowledgeSources(ctx context.Context) ([]*models.AgentKnowledgeSource, error)

	// What became of the pictures and files each of those places
	// carried: how many wait for a decision, how many the night decided
	// against, how many it opened and read. Needs agent:use.
	ListAgentSourceAttachments(ctx context.Context) ([]*AgentSourceAttachments, error)

	// The files the night decided against opening, newest first, with
	// the reason each was passed over. Needs agent:use.
	ListAgentDeclinedAttachments(ctx context.Context, arguments ListAgentDeclinedAttachmentsArguments) ([]*AgentAttachmentFile, error)

	// Passages from what those places indexed, by words: the same search
	// the agent's knowledge tool runs, so that a person can look through
	// their own documents without asking their agent to. Needs agent:use.
	SearchAgentDocuments(ctx context.Context, arguments SearchAgentDocumentsArguments) (*indexed.Found, error)

	// One of those documents, from an offset: the heading a search cites
	// and as much of the text as was asked for. Needs agent:use.
	ReadAgentDocument(ctx context.Context, arguments ReadAgentDocumentArguments) (*indexed.Extract, error)
}

// AgentGraphMutation changes it.
type AgentGraphMutation interface {
	// Write a page: its summary, its name, whether it is pinned. Needs
	// agent:use.
	SaveAgentNode(ctx context.Context, arguments SaveAgentNodeArguments) (*models.AgentNode, error)

	// Put a page under another. Needs agent:use.
	MoveAgentNode(ctx context.Context, arguments MoveAgentNodeArguments) (*models.AgentNode, error)

	// Fold one page into another: facts, links and children move over,
	// the name becomes an alias, and the page goes. Needs agent:use.
	MergeAgentNodes(ctx context.Context, arguments MergeAgentNodesArguments) (*models.AgentNode, error)

	// Remove a page and everything under it. Needs agent:use.
	DeleteAgentNode(ctx context.Context, arguments DeleteAgentNodeArguments) (bool, error)

	// Put a fact on a page, or change one. Needs agent:use.
	SaveAgentFact(ctx context.Context, arguments SaveAgentFactArguments) (*models.AgentFact, error)

	// Put a fact on another page, for a sentence filed under the wrong
	// name. Needs agent:use.
	MoveAgentFact(ctx context.Context, arguments MoveAgentFactArguments) (*models.AgentFact, error)

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
	DreamAgentNow(ctx context.Context, arguments DreamAgentNowArguments) (bool, error)

	// Put back into the night's queue everything marked read in the last
	// so many minutes, for a night that marked what it never read. Says
	// how many. Needs agent:use.
	RereadAgentDocuments(ctx context.Context, arguments RereadAgentDocumentsArguments) (int, error)

	// Join two pages, or take the join away. What the agent's memory
	// tool does with `link`; here so the person can do it too. Needs
	// agent:use.
	LinkAgentNodes(ctx context.Context, arguments LinkAgentNodesArguments) (bool, error)
	UnlinkAgentNodes(ctx context.Context, arguments LinkAgentNodesArguments) (bool, error)
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
	// Children is how many pages are filed under this one: what makes a
	// row something to walk into, and the number on it.
	Children int `json:"children"`
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
	// Weight is the link's, one for a link somebody stated, less for one
	// the night proposed, more for one that keeps being used together;
	// a child has none.
	Weight float64 `json:"weight"`
	// Status is "stated" or "proposed", so the drawing can say which
	// lines are the agent's guesses. Empty for a page filed under
	// another, which is not a link anybody made.
	Status string `json:"status"`
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

// RecallAgentMemoryArguments are the question to recall for, as a person
// would have typed it.
type RecallAgentMemoryArguments struct {
	Question string `json:"question"`
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

type MergeAgentNodesArguments struct {
	Path string `json:"path"`
	Into string `json:"into"`
}

type MoveAgentNodeArguments struct {
	Path  string `json:"path"`
	Under string `json:"under"`
}

// DreamAgentNowArguments is how the night is asked for. Bootstrap, when
// given, switches bootstrapping on or off: the night running at every
// tick with wider limits until nothing waits to be read.
type DreamAgentNowArguments struct {
	Bootstrap *bool `json:"bootstrap" graphapi:"nullable"`
}

type RereadAgentDocumentsArguments struct {
	Minutes int `json:"minutes"`
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

type MoveAgentFactArguments struct {
	Path   string `json:"path"`
	Number int    `json:"number"`
	To     string `json:"to"`
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

	// MaxAttachmentBytes is the largest file this source carries off the
	// person's machine. Not given leaves it alone; zero puts it back to
	// the server's own limit, which is what nearly every source wants.
	MaxAttachmentBytes *int64 `json:"maxAttachmentBytes" graphapi:"nullable"`

	// ReadEveryCheckout reads the files of every checkout under this
	// source, including the ones barely any of whose history is the
	// person's own work. Not given leaves it alone.
	ReadEveryCheckout *bool `json:"readEveryCheckout" graphapi:"nullable"`

	// CommitsPerPass is how many commits one pass over this source's
	// tree carries. Not given leaves it alone; zero puts it back to the
	// pace the program on the machine reads at.
	CommitsPerPass *int `json:"commitsPerPass" graphapi:"nullable"`

	// OwnCommitsAtLeast is the fewest commits of the person's own a
	// checkout under this source must hold before its files are read.
	// Not given leaves it alone; zero puts it back to the floor the
	// program on the machine reads at.
	OwnCommitsAtLeast *int `json:"ownCommitsAtLeast" graphapi:"nullable"`
}

type DeleteAgentKnowledgeSourceArguments struct {
	SourceID string `json:"sourceId"`
}

// SearchAgentDocumentsArguments is what to look for in what was indexed.
type SearchAgentDocumentsArguments struct {
	// Query is words, a name, or an identifier out of a log: an
	// identifier is looked up exactly as well as searched for.
	Query string `json:"query"`

	// First is how many passages; zero is indexed.SearchLimit.
	First int `json:"first" graphapi:"nullable"`

	// SourceID narrows the search to one source, by its identifier or by
	// its name, because a person types the name and a script has the
	// identifier.
	SourceID string `json:"sourceId" graphapi:"nullable"`
}

// ReadAgentDocumentArguments is which document to read and how much of it.
type ReadAgentDocumentArguments struct {
	// DocumentID is the document, or the "document#passage" a search
	// cites.
	DocumentID string `json:"documentId"`

	// From is where in the text to start, in characters, and First how
	// many to return; zero is indexed.ReadLimit. A read that does not
	// reach the end says where the next one starts.
	From  int `json:"from" graphapi:"nullable"`
	First int `json:"first" graphapi:"nullable"`
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

	// Folded is what the page used to say and no longer states: a fact
	// the agent decided repeated another, standing behind the one that
	// absorbed it. Kept out of Facts so the page reads as what it says
	// now, and shown all the same, because a merge nobody can see is a
	// merge nobody can disagree with.
	Folded []*AgentFoldedFact `json:"folded"`

	// Attachments are the pictures and files the facts on this page cite
	// as evidence, one row per document however many facts cite it. The
	// evidence itself carries only an identifier, and a fact whose
	// evidence is a screenshot is worth little to a person who cannot see
	// the screenshot.
	Attachments []*AgentAttachmentFile `json:"attachments"`

	// Contact is the address book entry this page is about, for a person.
	Contact *models.Contact `json:"contact" graphapi:"nullable"`
}

// AgentGraphSearchResult is what words found.
type AgentGraphSearchResult struct {
	Nodes []*models.AgentNode `json:"nodes"`
	Facts []*AgentLearnedFact `json:"facts"`
}

// RecallAgentMemoryResult is what a turn would have been carried.
type RecallAgentMemoryResult struct {
	Pages []*RecalledAgentPage `json:"pages"`
}

// RecalledAgentPage is one page of that, with the facts recall would have
// put in front of the model from it.
type RecalledAgentPage struct {
	Path  string               `json:"path"`
	Facts []*RecalledAgentFact `json:"facts"`
}

// RecalledAgentFact is a fact as the page cites it: its number and what
// it says. Enough to grade a question on, and not the whole row, because
// what is being asked is what the model was told.
type RecalledAgentFact struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

// AgentFoldedFact is a fact that stands behind another, with the number
// of the one it was folded into -- which is how a page cites a fact and
// what the identifier it actually carries cannot be shown as.
type AgentFoldedFact struct {
	Fact *models.AgentFact `json:"fact"`
	Into int               `json:"into"`
}

// AgentAttachmentFile is one picture or file a record came with, as a
// person sees it rather than as it is stored.
//
// One type for both places a person meets these -- the evidence under a
// fact, and the list of what the night decided against -- because they
// are the same file and a reader should not have to learn it twice.
type AgentAttachmentFile struct {
	DocumentID string `json:"documentId"`

	// Name is what the file was called where it came from, and
	// ContentType what sort of file it is, which is what decides whether
	// it is shown or offered to be saved.
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Bytes       int64  `json:"bytes"`

	// Channel and Thread are where it was posted, from the record it
	// arrived with: the two things that tell one screenshot from the
	// fifty thousand beside it.
	Channel string `json:"channel"`
	Thread  string `json:"thread"`

	// Path is where the bytes are served from, ready to put in an
	// address. Empty for a document whose bytes were never kept, which is
	// how a reader knows there is nothing to open.
	Path string `json:"path"`

	// Declined is why the night decided against opening it, and empty
	// where it did not.
	Declined string `json:"declined"`

	HappenedAt *time.Time `json:"happenedAt"`
}

// AgentCitedAttachmentsArguments names the citations to resolve.
type AgentCitedAttachmentsArguments struct {
	// Citations are references as the agent writes them into an answer:
	// a page's path, a hash, and the fact's number, as in work/mcx#3.
	Citations []string `json:"citations"`
}

// AgentCitedAttachment is one citation and the files the fact behind it
// was read from.
//
// Only the citations that have one are answered for. A conversation cites
// far more than it was read out of pictures, and a row per citation
// saying "nothing" would be most of the answer.
type AgentCitedAttachment struct {
	Citation string                 `json:"citation"`
	Files    []*AgentAttachmentFile `json:"files"`
}

// ListAgentDeclinedAttachmentsArguments names whose files to list.
type ListAgentDeclinedAttachmentsArguments struct {
	// SourceID is one source; empty is every source of the agent's.
	SourceID string `json:"sourceId" graphapi:"nullable"`

	// First is how many, newest first; zero is a hundred.
	First int `json:"first" graphapi:"nullable"`
}

// AgentSourceAttachments is what became of one source's files.
type AgentSourceAttachments struct {
	SourceID string `json:"sourceId"`

	// Undecided is waiting for the night to decide about it, Declined
	// was decided against, and Described was opened and read.
	Undecided int64 `json:"undecided"`
	Declined  int64 `json:"declined"`
	Described int64 `json:"described"`
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
	if result.Folded, err = self.foldedOn(tx, found.ID, node.ID); err != nil {
		return nil, err
	}
	if result.Edges, err = tx.ListAgentEdges(found.ID, node.ID); err != nil {
		return nil, err
	}
	if result.Attachments, err = self.attachmentsCitedBy(tx, found.ID, result.Facts); err != nil {
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

// citedDocumentsLimit is how many distinct things one page's facts may
// be looked up for at once.
const citedDocumentsLimit = 500

// attachmentsCitedBy is the pictures and files the given facts cite as
// evidence, one row per document.
//
// One read for the whole page rather than one per fact: a page of five
// hundred facts citing the same thread would otherwise be five hundred
// queries. Everything that is not an attachment is dropped -- a fact
// citing a chat unit or a conversation has nothing to show.
func (self *graph) attachmentsCitedBy(tx db.Transaction, agentId string, facts []*models.AgentFact) ([]*AgentAttachmentFile, error) {
	documentIds := citedDocumentIds(facts)
	files, err := attachmentFilesOf(tx, agentId, documentIds)
	if err != nil {
		return nil, err
	}
	attachments := []*AgentAttachmentFile{}
	for _, documentId := range documentIds {
		if file := files[documentId]; file != nil {
			attachments = append(attachments, file)
		}
	}
	return attachments, nil
}

// citedDocumentIds is what a set of facts name as evidence, in the order
// they name them and without repeats.
//
// A filing run writes the identifier as the prompt showed it, which is in
// brackets; documentEvidence trims them the same way.
func citedDocumentIds(facts []*models.AgentFact) []string {
	seen := map[string]bool{}
	var documentIds []string
	for _, fact := range facts {
		for _, evidence := range fact.Evidence {
			id := strings.Trim(strings.TrimSpace(evidence.ID), "[]")
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			documentIds = append(documentIds, id)
			// A crowded page may cite thousands of things, and the
			// reader shows fifty facts at a time. Bounded so that one
			// page cannot turn into one enormous statement.
			if len(documentIds) >= citedDocumentsLimit {
				return documentIds
			}
		}
	}
	return documentIds
}

// attachmentFilesOf is the files among these documents, as a person sees
// them, by identifier. Anything that is not an attachment is left out: a
// chat unit or a commit has nothing to show.
func attachmentFilesOf(tx db.Transaction, agentId string, documentIds []string) (map[string]*AgentAttachmentFile, error) {
	files := map[string]*AgentAttachmentFile{}
	if len(documentIds) == 0 {
		return files, nil
	}
	documents, err := tx.GetAgentDocuments(agentId, documentIds)
	if err != nil {
		return nil, err
	}
	for _, document := range documents {
		if document.Kind != models.DocumentAttachment {
			continue
		}
		files[document.ID] = attachmentFileOf(document)
	}
	return files, nil
}

// attachmentFileOf is one attachment document as a person sees it.
func attachmentFileOf(document *models.AgentDocument) *AgentAttachmentFile {
	file := &AgentAttachmentFile{
		DocumentID:  document.ID,
		Name:        document.Cite(),
		ContentType: document.ContentType(),
		Bytes:       document.Bytes,
		Channel:     document.Channel(),
		Thread:      document.Thread(),
		Declined:    document.Declined(),
		HappenedAt:  document.HappenedAt,
	}
	// An address only where there are bytes behind it: a document filed
	// while the person's machine was busy has no key until the next pass
	// of its source fills one in, and a link to nothing is worse than no
	// link.
	if document.StorageKey != "" {
		file.Path = api.AgentDocumentFilePath(document.ID)
	}
	return file
}

// foldedOn is the page's facts that stand behind another fact, each with
// the number of the one that absorbed it.
//
// The number and not the identifier, because a page cites #3 and nothing
// in the dashboard shows a row identifier. A pointer at a fact that has
// since gone is dropped rather than shown as #0: there is nothing left
// to send the reader to.
func (self *graph) foldedOn(tx db.Transaction, agentId, nodeId string) ([]*AgentFoldedFact, error) {
	facts, err := tx.ListAgentFacts(agentId, nodeId, true, 500)
	if err != nil {
		return nil, err
	}
	numbers := map[string]int{}
	for _, fact := range facts {
		numbers[fact.ID] = fact.Number
	}
	folded := []*AgentFoldedFact{}
	for _, fact := range facts {
		if fact.SupersededBy == "" {
			continue
		}
		into, known := numbers[fact.SupersededBy]
		if !known {
			continue
		}
		folded = append(folded, &AgentFoldedFact{Fact: fact, Into: into})
	}
	return folded, nil
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
	all := make([]string, 0, len(nodes))
	for _, child := range nodes {
		all = append(all, child.ID)
	}
	counts, err := tx.CountAgentNodeChildren(found.ID, all)
	if err != nil {
		return nil, err
	}
	rows := make([]*AgentGraphChild, 0, len(nodes))
	for _, child := range nodes {
		hint := firstLine(child.Summary)
		if hint == "" {
			hint = lines[child.ID]
		}
		rows = append(rows, &AgentGraphChild{Node: child, Hint: hint, Children: counts[child.ID]})
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
	// Links first: they are the relations, stated or proposed, and the
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
				Weight: float64(edge.Weight), Status: string(edge.Status),
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

func (self *graph) RecallAgentMemory(ctx context.Context, arguments RecallAgentMemoryArguments) (*RecallAgentMemoryResult, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	question := strings.TrimSpace(arguments.Question)
	if question == "" {
		return &RecallAgentMemoryResult{Pages: []*RecalledAgentPage{}}, nil
	}
	recalled, err := worker.RecallForQuestion(ctx, self.transaction(ctx), found, principal.User, question)
	if err != nil {
		return nil, err
	}
	result := &RecallAgentMemoryResult{Pages: make([]*RecalledAgentPage, 0, len(recalled))}
	for _, page := range recalled {
		carried := &RecalledAgentPage{Path: page.Path, Facts: make([]*RecalledAgentFact, 0, len(page.Facts))}
		for _, fact := range page.Facts {
			carried.Facts = append(carried.Facts, &RecalledAgentFact{Number: fact.Number, Text: fact.Text})
		}
		result.Pages = append(result.Pages, carried)
	}
	return result, nil
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

func (self *graph) AgentReadingProgress(ctx context.Context) (*reading.Progress, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	return reading.For(self.transaction(ctx), self.config.Current(), found, principal.User)
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

// citationsResolved is how many citations one call may look up.
//
// A conversation shown in the drawer is a page of messages at a time and
// an answer cites a handful, so this is well clear of a real transcript
// and stops a caller from asking for a thousand page reads in one
// statement.
const citationsResolved = 200

// AgentCitedAttachments is the files behind citations an answer made.
//
// An assistant's line carries text and nothing else -- only a person's own
// message may carry a file -- so an answer read out of a screenshot cannot
// show the screenshot. It does not have to: the agent already cites what
// it used, and has since the graph was built. This resolves those
// citations the way the Knowledge page resolves a fact's evidence, so the
// conversation can show the thing itself under the message. Nothing the
// model does has to change, and it works for every citation already
// written.
func (self *graph) AgentCitedAttachments(ctx context.Context, arguments AgentCitedAttachmentsArguments) ([]*AgentCitedAttachment, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	cited := []*AgentCitedAttachment{}
	// One read per distinct page and per fact, and one for all the
	// documents at the end: a transcript citing the same page twenty
	// times is one page read, not twenty.
	nodes := map[string]*models.AgentNode{}
	facts := map[string][]*models.AgentFact{}
	var order []string
	var all []*models.AgentFact
	for _, citation := range arguments.Citations {
		if len(order) >= citationsResolved {
			break
		}
		path, number := splitCitation(citation)
		if path == "" {
			continue
		}
		if _, asked := facts[citation]; asked {
			continue
		}
		facts[citation] = nil
		node, known := nodes[path]
		if !known {
			if node, err = tx.GetAgentNode(found.ID, path); err != nil {
				return nil, err
			}
			nodes[path] = node
		}
		if node == nil {
			continue
		}
		// The caller's own agent throughout, so a path copied from
		// somewhere else reads their own graph or nothing.
		fact, err := tx.GetAgentFact(found.ID, node.ID, number)
		if err != nil {
			return nil, err
		}
		if fact == nil {
			continue
		}
		facts[citation] = []*models.AgentFact{fact}
		order = append(order, citation)
		all = append(all, fact)
	}
	files, err := attachmentFilesOf(tx, found.ID, citedDocumentIds(all))
	if err != nil {
		return nil, err
	}
	for _, citation := range order {
		var behind []*AgentAttachmentFile
		for _, documentId := range citedDocumentIds(facts[citation]) {
			if file := files[documentId]; file != nil {
				behind = append(behind, file)
			}
		}
		if len(behind) == 0 {
			continue
		}
		cited = append(cited, &AgentCitedAttachment{Citation: citation, Files: behind})
	}
	return cited, nil
}

// splitCitation reads a citation the way the agent writes one: the page's
// path, then the fact's number after a hash. Anything else is nothing.
func splitCitation(citation string) (string, int) {
	citation = strings.TrimSpace(citation)
	hash := strings.LastIndex(citation, "#")
	if hash <= 0 {
		return "", 0
	}
	number, err := strconv.Atoi(strings.TrimSpace(citation[hash+1:]))
	if err != nil || number <= 0 {
		return "", 0
	}
	return models.NormalizePath(citation[:hash]), number
}

// ListAgentSourceAttachments is what became of each source's files.
//
// Apart from the source itself because these are counted rather than
// kept: a night deciding about a file writes the decision on the
// document, and a number on the source's row would be one more thing to
// keep right.
func (self *graph) ListAgentSourceAttachments(ctx context.Context) ([]*AgentSourceAttachments, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := self.transaction(ctx).CountAgentAttachmentsBySource(found.ID)
	if err != nil {
		return nil, err
	}
	attachments := make([]*AgentSourceAttachments, 0, len(counts))
	for sourceId, count := range counts {
		attachments = append(attachments, &AgentSourceAttachments{
			SourceID:  sourceId,
			Undecided: count.Undecided,
			Declined:  count.Declined,
			Described: count.Described,
		})
	}
	// In a settled order, so that a card refreshing itself does not
	// reorder rows under somebody reading them.
	sort.Slice(attachments, func(first, second int) bool {
		return attachments[first].SourceID < attachments[second].SourceID
	})
	return attachments, nil
}

// ListAgentDeclinedAttachments is what the night passed over, and why.
//
// Shown rather than kept quiet: the decision is the agent's own judgement
// about the person's files, and a judgement nobody can read is one nobody
// can disagree with.
func (self *graph) ListAgentDeclinedAttachments(ctx context.Context, arguments ListAgentDeclinedAttachmentsArguments) ([]*AgentAttachmentFile, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	sourceId := strings.TrimSpace(arguments.SourceID)
	if sourceId != "" {
		// Their own source, so that an identifier from somewhere else
		// lists nothing rather than somebody else's files.
		source, err := tx.GetAgentSource(found.ID, sourceId)
		if err != nil {
			return nil, err
		}
		if source == nil {
			return nil, api.ErrNotFound
		}
	}
	limit := arguments.First
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	documents, err := tx.ListAgentAttachmentsDeclined(found.ID, sourceId, limit)
	if err != nil {
		return nil, err
	}
	files := make([]*AgentAttachmentFile, 0, len(documents))
	for _, document := range documents {
		files = append(files, attachmentFileOf(document))
	}
	return files, nil
}

// SearchAgentDocuments is the knowledge tool's search, run for the person
// rather than for their agent.
//
// It is indexed.Search either way: the tool prints the rows into a turn
// and this hands them back as data, and neither can find something the
// other cannot. What the deployment has decides how they are ranked --
// with an embedding model, what the words find and what the question
// means, fused; without one, the words alone, which the result says.
func (self *graph) SearchAgentDocuments(ctx context.Context, arguments SearchAgentDocumentsArguments) (*indexed.Found, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	var sourceIds []string
	if named := strings.TrimSpace(arguments.SourceID); named != "" {
		source, err := self.agentSourceNamed(tx, found.ID, named)
		if err != nil {
			return nil, err
		}
		sourceIds = []string{source.ID}
	}
	// Nil where agents are off, which searches by words rather than
	// refusing: what was indexed is still theirs to look through.
	var meaning indexed.Meaning
	if worker := self.agentWorker(); worker != nil {
		meaning = worker.KnowledgeMeaning(found.ID)
	}
	return indexed.Search(ctx, tx, meaning, found.ID, indexed.Query{
		Words: arguments.Query, SourceIds: sourceIds, Limit: arguments.First,
	})
}

// ReadAgentDocument reads one of the caller's own documents.
//
// A document is found under the caller's agent, so an identifier out of
// somebody else's search is not there: not found, not somebody else's
// text.
func (self *graph) ReadAgentDocument(ctx context.Context, arguments ReadAgentDocumentArguments) (*indexed.Extract, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	extract, err := indexed.Read(self.transaction(ctx), found.ID, arguments.DocumentID, arguments.From, arguments.First)
	if err != nil {
		return nil, err
	}
	if extract == nil {
		return nil, api.ErrNotFound
	}
	return extract, nil
}

// agentSourceNamed is a source by its identifier or by its name, which is
// what the command line's --source takes.
func (self *graph) agentSourceNamed(tx db.Transaction, agentId, wanted string) (*models.AgentKnowledgeSource, error) {
	source, err := tx.GetAgentSource(agentId, wanted)
	if err != nil {
		return nil, err
	}
	if source == nil {
		if source, err = tx.GetAgentSourceByName(agentId, wanted); err != nil {
			return nil, err
		}
	}
	if source == nil {
		return nil, api.ErrNotFound
	}
	return source, nil
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
	// Nothing said about the audiences and an empty list said on purpose
	// are two different requests, and they arrive differently: an omitted
	// argument and an explicit null both reach here as a nil slice, while
	// an empty list arrives non-nil. Editing a fact used to rewrite its
	// audiences from the argument either way, so anything that sent the
	// text alone — the dashboard's fact editor did — quietly narrowed the
	// fact to the conversation.
	var audiences []models.AgentAudience
	if arguments.Audiences != nil {
		audiences = []models.AgentAudience{models.AudienceAsk}
		for _, name := range arguments.Audiences {
			audience := models.AgentAudience(strings.TrimSpace(name))
			if audience != models.AudienceAsk && models.IsAgentAudience(audience) {
				audiences = append(audiences, audience)
			}
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
			if audiences != nil {
				fact.Audiences = audiences
			}
			// Edited by hand, so it is theirs now rather than something
			// the agent worked out: an inference they corrected is a
			// fact.
			fact.Inferred = false
			fact.Confidence = 1
			fact.Evidence = append(fact.Evidence, models.Evidence{Kind: models.EvidencePerson})
			return nil
		})
	}
	// A new fact with nothing said about its audiences is read by the
	// conversation, which is where every fact starts.
	if audiences == nil {
		audiences = []models.AgentAudience{models.AudienceAsk}
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
	// By words, not by meaning: this request runs whole inside one
	// transaction and has the page's row lock, and a provider call inside
	// that held every other writer to the page for as long as it took.
	folded, err := worker.FoldByWordsIntoWhatThePageSays(tx, written, node)
	if err != nil {
		// The fact is written; only the tidying failed.
		log.Warningf("cannot fold %s#%d into what the page already says: %s", path, written.Number, err)
		return written, nil
	}
	return folded, nil
}

// MoveAgentFact puts a fact on another page.
//
// The alternative a person has without it is to strike the sentence and
// type it again on the right page, which throws away the words it came
// from and the day it was learned. Moving keeps both; only the number
// changes, because a number belongs to the page it is on.
func (self *graph) MoveAgentFact(ctx context.Context, arguments MoveAgentFactArguments) (*models.AgentFact, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	path, to := models.NormalizePath(arguments.Path), models.NormalizePath(arguments.To)
	if path == "" || to == "" {
		return nil, fmt.Errorf("move a fact from where to where? give path and to")
	}
	tx := self.writing(ctx)
	node, err := tx.GetAgentNode(found.ID, path)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("there is no page at %s", path)
	}
	fact, err := tx.GetAgentFact(found.ID, node.ID, arguments.Number)
	if err != nil {
		return nil, err
	}
	if fact == nil {
		return nil, fmt.Errorf("there is no %s#%d", path, arguments.Number)
	}
	// The destination is not made on the way: a typo in a path would
	// otherwise take the sentence somewhere nobody looks.
	destination, err := tx.GetAgentNode(found.ID, to)
	if err != nil {
		return nil, err
	}
	if destination == nil {
		return nil, fmt.Errorf("there is no page at %s", to)
	}
	return tx.MoveAgentFact(found.ID, fact.ID, destination.ID)
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
		// Theirs, and one that exists. A source naming a mailbox is read
		// by the ingest run with no check of its own -- it opens the Sent
		// folder of whatever identifier it was handed -- so naming a
		// stranger's here would file their sent mail into this agent's
		// pages.
		if _, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID); err != nil {
			return nil, err
		}
		source.Specification.MailboxID = arguments.MailboxID
	}
	if arguments.MaxAttachmentBytes != nil {
		if *arguments.MaxAttachmentBytes < 0 {
			return nil, fmt.Errorf("the largest file this source may carry cannot be negative; zero is the server's own limit")
		}
		source.Specification.MaxAttachmentBytes = *arguments.MaxAttachmentBytes
	}
	if arguments.ReadEveryCheckout != nil {
		source.Specification.ReadEveryCheckout = *arguments.ReadEveryCheckout
	}
	if arguments.CommitsPerPass != nil {
		if *arguments.CommitsPerPass < 0 {
			return nil, fmt.Errorf("the commits one pass carries cannot be negative; zero is the pace the program on the machine reads at")
		}
		source.Specification.CommitsPerPass = *arguments.CommitsPerPass
	}
	if arguments.OwnCommitsAtLeast != nil {
		if *arguments.OwnCommitsAtLeast < 0 {
			return nil, fmt.Errorf("the commits of your own a checkout needs cannot be negative; zero is the floor the program on the machine reads at")
		}
		source.Specification.OwnCommitsAtLeast = *arguments.OwnCommitsAtLeast
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
	// Only when the source is new. An empty cron means "only when they
	// ask" (migration 0068), and filling it in on every save made that
	// impossible to keep: pausing a source, resuming it, or renaming it
	// each handed it a nightly schedule it had been deliberately set
	// without.
	if arguments.SourceID == "" && source.Cron == "" {
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

func (self *graph) DreamAgentNow(ctx context.Context, arguments DreamAgentNowArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	// The night is due when it has not run for six hours; forgetting when
	// it last ran makes it due at the next tick. The hours the person set
	// still hold: a night asked for at noon runs when its hours begin.
	_, err = self.writing(ctx).UpdateAgent(found.ID, func(agent *models.Agent) error {
		if arguments.Bootstrap != nil {
			agent.DreamBootstrap = *arguments.Bootstrap
			if !*arguments.Bootstrap {
				return nil
			}
		}
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

func (self *graph) MergeAgentNodes(ctx context.Context, arguments MergeAgentNodesArguments) (*models.AgentNode, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	path := models.NormalizePath(arguments.Path)
	for _, root := range models.AgentRoots {
		if root.Path == path {
			return nil, fmt.Errorf("%s is one of the places things are filed, and cannot be merged away", path)
		}
	}
	return self.writing(ctx).MergeAgentNodes(found.ID, path, models.NormalizePath(arguments.Into))
}

func (self *graph) RereadAgentDocuments(ctx context.Context, arguments RereadAgentDocumentsArguments) (int, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return 0, err
	}
	if arguments.Minutes <= 0 {
		return 0, fmt.Errorf("how far back? give minutes")
	}
	put, err := self.writing(ctx).UnmarkAgentDocumentsDigested(found.ID, time.Now().Add(-time.Duration(arguments.Minutes)*time.Minute))
	return int(put), err
}
