package client

import (
	"context"
	"time"
)

// The graph from a terminal: pages addressed by path, the facts on them,
// what the nightly run did, and the sources the agent reads.

// AgentNode is one page of the graph.
type AgentNode struct {
	ID         string     `json:"id"`
	Path       string     `json:"path"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Aliases    []string   `json:"aliases"`
	Summary    string     `json:"summary"`
	ContactID  string     `json:"contactId"`
	Pinned     bool       `json:"pinned"`
	Importance float32    `json:"importance"`
	Dormant    bool       `json:"dormant"`
	UsedAt     *time.Time `json:"usedAt"`
	ModifiedAt time.Time  `json:"modifiedAt"`
}

// AgentFact is one sentence on a page.
type AgentFact struct {
	ID         string          `json:"id"`
	Number     int             `json:"number"`
	Kind       string          `json:"kind"`
	Text       string          `json:"text"`
	HappenedAt *time.Time      `json:"happenedAt"`
	Confidence float32         `json:"confidence"`
	Inferred   bool            `json:"inferred"`
	Evidence   []AgentEvidence `json:"evidence"`
	Audiences  []string        `json:"audiences"`
	Dormant    bool            `json:"dormant"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// AgentEvidence is where a fact came from.
type AgentEvidence struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Quote string `json:"quote"`
}

// AgentEdge joins two pages.
type AgentEdge struct {
	Relation string `json:"relation"`
	FromPath string `json:"fromPath"`
	ToPath   string `json:"toPath"`
}

// AgentGraphPage is a page and everything a reader of it wants.
type AgentGraphPage struct {
	Node     *AgentNode   `json:"node"`
	Facts    []*AgentFact `json:"facts"`
	Edges    []*AgentEdge `json:"edges"`
	Children []*AgentNode `json:"children"`
	Contact  *Contact     `json:"contact"`
}

// AgentLearnedFact is a fact with the page it sits on.
type AgentLearnedFact struct {
	Fact *AgentFact `json:"fact"`
	Path string     `json:"path"`
	Name string     `json:"name"`
}

// AgentGraphSearch is what words found.
type AgentGraphSearch struct {
	Nodes []*AgentNode        `json:"nodes"`
	Facts []*AgentLearnedFact `json:"facts"`
}

// AgentKnowledgeSource is a place the agent reads.
type AgentKnowledgeSource struct {
	ID             string                      `json:"id"`
	Kind           string                      `json:"kind"`
	Name           string                      `json:"name"`
	Specification  AgentKnowledgeSpecification `json:"specification"`
	RootPath       string                      `json:"rootPath"`
	Enabled        bool                        `json:"enabled"`
	Cron           string                      `json:"cron"`
	LastRunAt      *time.Time                  `json:"lastRunAt"`
	NextRunAt      *time.Time                  `json:"nextRunAt"`
	LastError      string                      `json:"lastError"`
	DocumentCount  int                         `json:"documentCount"`
	ChunkCount     int                         `json:"chunkCount"`
	RefusedCount   int                         `json:"refusedCount"`
	More           bool                        `json:"more"`
	UnknownAuthors []string                    `json:"unknownAuthors"`
}

// AgentKnowledgeSpecification says what a source reads.
type AgentKnowledgeSpecification struct {
	Computer  string   `json:"computer"`
	Path      string   `json:"path"`
	Format    string   `json:"format"`
	Include   []string `json:"include"`
	Exclude   []string `json:"exclude"`
	Tool      string   `json:"tool"`
	Start     string   `json:"start"`
	Depth     int      `json:"depth"`
	MailboxID string   `json:"mailboxId"`
}

// AgentDream is what the nightly run did.
type AgentDream struct {
	ID         string     `json:"id"`
	JobID      string     `json:"jobId"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	Digested   int        `json:"digested"`
	Filed      int        `json:"filed"`
	Merged     int        `json:"merged"`
	Rewritten  int        `json:"rewritten"`
	Moved      int        `json:"moved"`
	Dormant    int        `json:"dormant"`
	Embedded   int        `json:"embedded"`
	Backlog    int        `json:"backlog"`
	Coarse     bool       `json:"coarse"`

	Revised      int `json:"revised"`
	Strengthened int `json:"strengthened"`
	Associated   int `json:"associated"`
	Rehearsed    int `json:"rehearsed"`
	Gaps         int `json:"gaps"`
	Unknown      int `json:"unknown"`

	Tokens    int64  `json:"tokens"`
	LastError string `json:"lastError"`
	Proposals []struct {
		Kind   string `json:"kind"`
		Path   string `json:"path"`
		To     string `json:"to"`
		Reason string `json:"reason"`
	} `json:"proposals"`
}

const nodeFields = `{ id path kind name aliases summary contactId pinned importance dormant usedAt modifiedAt }`
const factFields = `{ id number kind text happenedAt confidence inferred evidence { kind id quote } audiences dormant createdAt }`
const sourceFields = `{ id kind name specification { computer path format include exclude tool start depth mailboxId } rootPath enabled cron lastRunAt nextRunAt lastError documentCount chunkCount refusedCount more unknownAuthors }`
const revisionFields = `{ revision kind actor summary change before after path reason createdAt }`
const dreamFields = `{ id jobId startedAt finishedAt digested filed merged rewritten moved dormant embedded backlog coarse strengthened associated rehearsed gaps unknown revised tokens lastError proposals { kind path to reason } }`

// The documents.
const (
	DocumentAgentGraphIndex = `query ($under: String, $first: Int) {
		AgentGraphIndex(under: $under, first: $first) ` + nodeFields + `
	}`
	DocumentAgentGraphPage = `query ($path: String!) {
		AgentGraphPage(path: $path) {
			node ` + nodeFields + `
			facts ` + factFields + `
			edges { relation fromPath toPath }
			children ` + nodeFields + `
			contact { id name emails phones organization }
		}
	}`
	DocumentSearchAgentGraph = `query ($query: String!, $first: Int) {
		SearchAgentGraph(query: $query, first: $first) {
			nodes ` + nodeFields + `
			facts { fact ` + factFields + ` path name }
		}
	}`
	DocumentListAgentLearned = `query ($days: Int, $first: Int) {
		ListAgentLearned(days: $days, first: $first) { fact ` + factFields + ` path name }
	}`
	DocumentSaveAgentNode = `mutation ($path: String!, $kind: String, $name: String, $summary: String, $aliases: [String!], $pinned: Boolean) {
		SaveAgentNode(path: $path, kind: $kind, name: $name, summary: $summary, aliases: $aliases, pinned: $pinned) ` + nodeFields + `
	}`
	DocumentMergeAgentNodes = `mutation ($path: String!, $into: String!) { MergeAgentNodes(path: $path, into: $into) { id path name } }`
	DocumentMoveAgentNode   = `mutation ($path: String!, $under: String!) {
		MoveAgentNode(path: $path, under: $under) ` + nodeFields + `
	}`
	DocumentDeleteAgentNode = `mutation ($path: String!) { DeleteAgentNode(path: $path) }`
	DocumentSaveAgentFact   = `mutation ($path: String!, $number: Int, $kind: String, $text: String!, $happened: String, $audiences: [String!]) {
		SaveAgentFact(path: $path, number: $number, kind: $kind, text: $text, happened: $happened, audiences: $audiences) ` + factFields + `
	}`
	DocumentMoveAgentFact = `mutation ($path: String!, $number: Int!, $to: String!) {
		MoveAgentFact(path: $path, number: $number, to: $to) ` + factFields + `
	}`
	DocumentDeleteAgentFact = `mutation ($path: String!, $number: Int!) { DeleteAgentFact(path: $path, number: $number) }`
	DocumentSetMyContact    = `mutation ($contactId: String) { SetMyContact(contactId: $contactId) }`

	DocumentListAgentKnowledgeSources = `query { ListAgentKnowledgeSources ` + sourceFields + ` }`
	DocumentSaveAgentKnowledgeSource  = `mutation ($sourceId: String, $kind: String, $name: String, $computer: String, $path: String, $format: String, $rootPath: String, $cron: String, $enabled: Boolean, $mailboxId: String) {
		SaveAgentKnowledgeSource(sourceId: $sourceId, kind: $kind, name: $name, computer: $computer, path: $path, format: $format, rootPath: $rootPath, cron: $cron, enabled: $enabled, mailboxId: $mailboxId) ` + sourceFields + `
	}`
	DocumentDeleteAgentKnowledgeSource = `mutation ($sourceId: String!) { DeleteAgentKnowledgeSource(sourceId: $sourceId) }`
	DocumentSyncAgentKnowledgeSource   = `mutation ($sourceId: String!) { SyncAgentKnowledgeSource(sourceId: $sourceId) }`
	DocumentDreamAgentNow              = `mutation ($bootstrap: Boolean) { DreamAgentNow(bootstrap: $bootstrap) }`
	DocumentRereadAgentDocuments       = `mutation ($minutes: Int!) { RereadAgentDocuments(minutes: $minutes) }`
	DocumentLinkAgentNodes             = `mutation ($path: String!, $to: String!, $relation: String!, $note: String) { LinkAgentNodes(path: $path, to: $to, relation: $relation, note: $note) }`
	DocumentUnlinkAgentNodes           = `mutation ($path: String!, $to: String!, $relation: String!) { UnlinkAgentNodes(path: $path, to: $to, relation: $relation) }`
	DocumentListAgentDreams            = `query ($first: Int) { ListAgentDreams(first: $first) ` + dreamFields + ` }`
	DocumentListAgentPageHistory       = `query ($path: String!, $first: Int) { ListAgentPageHistory(path: $path, first: $first) ` + revisionFields + ` }`
)

// AgentGraphIndex is the pages, under a path or the whole graph.
func AgentGraphIndex(ctx context.Context, connection *Client, under string, first int) ([]*AgentNode, error) {
	var result struct {
		AgentGraphIndex []*AgentNode `json:"AgentGraphIndex"`
	}
	variables := map[string]any{"first": first}
	if under != "" {
		variables["under"] = under
	}
	if err := connection.Execute(ctx, DocumentAgentGraphIndex, variables, &result); err != nil {
		return nil, err
	}
	return result.AgentGraphIndex, nil
}

// AgentGraphPageOf is one page with its facts and links.
func AgentGraphPageOf(ctx context.Context, connection *Client, path string) (*AgentGraphPage, error) {
	var result struct {
		AgentGraphPage *AgentGraphPage `json:"AgentGraphPage"`
	}
	if err := connection.Execute(ctx, DocumentAgentGraphPage, map[string]any{"path": path}, &result); err != nil {
		return nil, err
	}
	return result.AgentGraphPage, nil
}

// SearchAgentGraph finds pages and facts by words.
func SearchAgentGraph(ctx context.Context, connection *Client, query string, first int) (*AgentGraphSearch, error) {
	var result struct {
		SearchAgentGraph *AgentGraphSearch `json:"SearchAgentGraph"`
	}
	if err := connection.Execute(ctx, DocumentSearchAgentGraph, map[string]any{"query": query, "first": first}, &result); err != nil {
		return nil, err
	}
	return result.SearchAgentGraph, nil
}

// ListAgentLearned is what has been filed lately.
func ListAgentLearned(ctx context.Context, connection *Client, days, first int) ([]*AgentLearnedFact, error) {
	var result struct {
		ListAgentLearned []*AgentLearnedFact `json:"ListAgentLearned"`
	}
	if err := connection.Execute(ctx, DocumentListAgentLearned, map[string]any{"days": days, "first": first}, &result); err != nil {
		return nil, err
	}
	return result.ListAgentLearned, nil
}

// SaveAgentNode writes a page.
func SaveAgentNode(ctx context.Context, connection *Client, fields map[string]any) (*AgentNode, error) {
	var result struct {
		SaveAgentNode *AgentNode `json:"SaveAgentNode"`
	}
	if err := connection.Execute(ctx, DocumentSaveAgentNode, fields, &result); err != nil {
		return nil, err
	}
	return result.SaveAgentNode, nil
}

// MoveAgentNode files a page under another.
func MoveAgentNode(ctx context.Context, connection *Client, path, under string) (*AgentNode, error) {
	var result struct {
		MoveAgentNode *AgentNode `json:"MoveAgentNode"`
	}
	if err := connection.Execute(ctx, DocumentMoveAgentNode, map[string]any{"path": path, "under": under}, &result); err != nil {
		return nil, err
	}
	return result.MoveAgentNode, nil
}

// DeleteAgentNode removes a page and what is under it.
func DeleteAgentNode(ctx context.Context, connection *Client, path string) error {
	var result struct {
		DeleteAgentNode bool `json:"DeleteAgentNode"`
	}
	return connection.Execute(ctx, DocumentDeleteAgentNode, map[string]any{"path": path}, &result)
}

// SaveAgentFact puts a fact on a page, or changes one.
func SaveAgentFact(ctx context.Context, connection *Client, fields map[string]any) (*AgentFact, error) {
	var result struct {
		SaveAgentFact *AgentFact `json:"SaveAgentFact"`
	}
	if err := connection.Execute(ctx, DocumentSaveAgentFact, fields, &result); err != nil {
		return nil, err
	}
	return result.SaveAgentFact, nil
}

// MoveAgentFact puts one fact on another page, where it takes a new
// number.
func MoveAgentFact(ctx context.Context, connection *Client, path string, number int, to string) (*AgentFact, error) {
	var result struct {
		MoveAgentFact *AgentFact `json:"MoveAgentFact"`
	}
	if err := connection.Execute(ctx, DocumentMoveAgentFact, map[string]any{"path": path, "number": number, "to": to}, &result); err != nil {
		return nil, err
	}
	return result.MoveAgentFact, nil
}

// DeleteAgentFact strikes one.
func DeleteAgentFact(ctx context.Context, connection *Client, path string, number int) error {
	var result struct {
		DeleteAgentFact bool `json:"DeleteAgentFact"`
	}
	return connection.Execute(ctx, DocumentDeleteAgentFact, map[string]any{"path": path, "number": number}, &result)
}

// SetMyContact names the contact that is the caller.
func SetMyContact(ctx context.Context, connection *Client, contactId string) error {
	var result struct {
		SetMyContact bool `json:"SetMyContact"`
	}
	variables := map[string]any{}
	if contactId != "" {
		variables["contactId"] = contactId
	}
	return connection.Execute(ctx, DocumentSetMyContact, variables, &result)
}

// ListAgentKnowledgeSources is what the agent reads.
func ListAgentKnowledgeSources(ctx context.Context, connection *Client) ([]*AgentKnowledgeSource, error) {
	var result struct {
		ListAgentKnowledgeSources []*AgentKnowledgeSource `json:"ListAgentKnowledgeSources"`
	}
	if err := connection.Execute(ctx, DocumentListAgentKnowledgeSources, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentKnowledgeSources, nil
}

// SaveAgentKnowledgeSource adds one, or changes one.
func SaveAgentKnowledgeSource(ctx context.Context, connection *Client, fields map[string]any) (*AgentKnowledgeSource, error) {
	var result struct {
		SaveAgentKnowledgeSource *AgentKnowledgeSource `json:"SaveAgentKnowledgeSource"`
	}
	if err := connection.Execute(ctx, DocumentSaveAgentKnowledgeSource, fields, &result); err != nil {
		return nil, err
	}
	return result.SaveAgentKnowledgeSource, nil
}

// DeleteAgentKnowledgeSource stops one and forgets what it found.
func DeleteAgentKnowledgeSource(ctx context.Context, connection *Client, sourceId string) error {
	var result struct {
		DeleteAgentKnowledgeSource bool `json:"DeleteAgentKnowledgeSource"`
	}
	return connection.Execute(ctx, DocumentDeleteAgentKnowledgeSource, map[string]any{"sourceId": sourceId}, &result)
}

// SyncAgentKnowledgeSource reads one again now.
func SyncAgentKnowledgeSource(ctx context.Context, connection *Client, sourceId string) error {
	var result struct {
		SyncAgentKnowledgeSource bool `json:"SyncAgentKnowledgeSource"`
	}
	return connection.Execute(ctx, DocumentSyncAgentKnowledgeSource, map[string]any{"sourceId": sourceId}, &result)
}

// AgentPageRevision is one change to a page.
type AgentPageRevision struct {
	Revision  int       `json:"revision"`
	Kind      string    `json:"kind"`
	Actor     string    `json:"actor"`
	Summary   string    `json:"summary"`
	Change    string    `json:"change"`
	Before    string    `json:"before"`
	After     string    `json:"after"`
	Path      string    `json:"path"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListAgentPageHistory is what has happened to one page, newest first.
func ListAgentPageHistory(ctx context.Context, connection *Client, path string, first int) ([]*AgentPageRevision, error) {
	var result struct {
		ListAgentPageHistory []*AgentPageRevision `json:"ListAgentPageHistory"`
	}
	if err := connection.Execute(ctx, DocumentListAgentPageHistory, map[string]any{"path": path, "first": first}, &result); err != nil {
		return nil, err
	}
	return result.ListAgentPageHistory, nil
}

// ListAgentDreams is what the nightly run did, newest first.
func ListAgentDreams(ctx context.Context, connection *Client, first int) ([]*AgentDream, error) {
	var result struct {
		ListAgentDreams []*AgentDream `json:"ListAgentDreams"`
	}
	if err := connection.Execute(ctx, DocumentListAgentDreams, map[string]any{"first": first}, &result); err != nil {
		return nil, err
	}
	return result.ListAgentDreams, nil
}

// DreamAgentNow asks for the night to run at the next tick. bootstrap,
// when given, switches bootstrapping on or off: the night at every tick
// with wider limits until nothing waits to be read.
func DreamAgentNow(ctx context.Context, connection *Client, bootstrap *bool) error {
	var result struct {
		DreamAgentNow bool `json:"DreamAgentNow"`
	}
	variables := map[string]any{}
	if bootstrap != nil {
		variables["bootstrap"] = *bootstrap
	}
	return connection.Execute(ctx, DocumentDreamAgentNow, variables, &result)
}

// LinkAgentNodes joins two pages.
func LinkAgentNodes(ctx context.Context, connection *Client, path, to, relation, note string) error {
	var result struct {
		LinkAgentNodes bool `json:"LinkAgentNodes"`
	}
	return connection.Execute(ctx, DocumentLinkAgentNodes, map[string]any{"path": path, "to": to, "relation": relation, "note": note}, &result)
}

// UnlinkAgentNodes takes a join away.
func UnlinkAgentNodes(ctx context.Context, connection *Client, path, to, relation string) error {
	var result struct {
		UnlinkAgentNodes bool `json:"UnlinkAgentNodes"`
	}
	return connection.Execute(ctx, DocumentUnlinkAgentNodes, map[string]any{"path": path, "to": to, "relation": relation}, &result)
}

// MergeAgentNodes folds one page into another.
func MergeAgentNodes(ctx context.Context, connection *Client, path, into string) (*AgentNode, error) {
	var result struct {
		MergeAgentNodes *AgentNode `json:"MergeAgentNodes"`
	}
	if err := connection.Execute(ctx, DocumentMergeAgentNodes, map[string]any{"path": path, "into": into}, &result); err != nil {
		return nil, err
	}
	return result.MergeAgentNodes, nil
}

// RereadAgentDocuments puts back what a night marked read in the last so
// many minutes.
func RereadAgentDocuments(ctx context.Context, connection *Client, minutes int) (int, error) {
	var result struct {
		RereadAgentDocuments int `json:"RereadAgentDocuments"`
	}
	if err := connection.Execute(ctx, DocumentRereadAgentDocuments, map[string]any{"minutes": minutes}, &result); err != nil {
		return 0, err
	}
	return result.RereadAgentDocuments, nil
}
