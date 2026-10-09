package client

import (
	"context"
	"encoding/json"
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

	// Overview is how the thing works, in markdown sections the night
	// writes; asked for only when one page is read.
	Overview          string     `json:"overview"`
	OverviewWrittenAt *time.Time `json:"overviewWrittenAt"`
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

// AgentEdge joins two pages. Status is "stated" or "proposed": a link
// the nightly walk guessed is read as a guess and not as a fact.
type AgentEdge struct {
	Relation string `json:"relation"`
	Status   string `json:"status"`
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

	// LinkCount is how many links the page has, and NextLinkOffset where
	// the next part of them starts, zero when Edges reaches the last.
	LinkCount      int `json:"linkCount"`
	NextLinkOffset int `json:"nextLinkOffset"`
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

	// MoreNodeCount and MoreFactCount are how many more the search found
	// past these, at least that many where the flag says so, and
	// NextOffset the offset that reads them; zero is the last page.
	MoreNodeCount             int  `json:"moreNodeCount"`
	IsMoreNodeCountLowerBound bool `json:"isMoreNodeCountLowerBound"`
	MoreFactCount             int  `json:"moreFactCount"`
	IsMoreFactCountLowerBound bool `json:"isMoreFactCountLowerBound"`
	NextOffset                int  `json:"nextOffset"`
}

// AgentRecall is what a turn asking a question would have been carried
// from the graph: the pages, each with the facts recall would have put in
// front of the model.
type AgentRecall struct {
	// RetrievalMode is basic, or planned when a plan was followed.
	RetrievalMode string               `json:"retrievalMode"`
	Pages         []*AgentRecalledPage `json:"pages"`

	// Explanation is why, when it was asked for.
	Explanation *AgentRecallExplanation `json:"explanation,omitempty"`
}

// AgentRetrievalPlan is a retrieval plan to follow: at most two focused
// searches, and whether the question is about a whole area.
type AgentRetrievalPlan struct {
	Searches []string `json:"searches"`
	IsBroad  bool     `json:"isBroad"`
}

// AgentRecallExplanation is how one question's recall went: each query,
// what each of its lists found, and why each page and fact was carried or
// left out.
type AgentRecallExplanation struct {
	RetrievalMode      string               `json:"retrievalMode"`
	Plan               *AgentRetrievalPlan  `json:"plan"`
	Queries            []*AgentRecallQuery  `json:"queries"`
	Searches           []*AgentRecallSearch `json:"searches"`
	Pages              []*AgentRecallPage   `json:"pages"`
	Facts              []*AgentRecallFact   `json:"facts"`
	IsBroadNoteCarried bool                 `json:"isBroadNoteCarried"`

	TokenBudget int `json:"tokenBudget"`
	TokensSpent int `json:"tokensSpent"`
}

// AgentRecallQuery is one query a retrieval ran.
type AgentRecallQuery struct {
	QueryID   string `json:"queryId"`
	QueryKind string `json:"queryKind"`
	QueryText string `json:"queryText"`
}

// AgentRecallSearch is one list a query produced, and how much it found.
type AgentRecallSearch struct {
	QueryID    string `json:"queryId"`
	SearchName string `json:"searchName"`
	FoundCount int    `json:"foundCount"`
}

// AgentRecallRank is where a page or fact stood in one list.
type AgentRecallRank struct {
	QueryID    string `json:"queryId"`
	SearchName string `json:"searchName"`
	Rank       int    `json:"rank"`
}

// AgentRecallPage is one page the searches found and what became of it.
type AgentRecallPage struct {
	Path                   string             `json:"path"`
	FusedRank              int                `json:"fusedRank"`
	Ranks                  []*AgentRecallRank `json:"ranks"`
	HitFactCount           int                `json:"hitFactCount"`
	RecallDecision         string             `json:"recallDecision"`
	OverviewSectionHeading string             `json:"overviewSectionHeading"`
	SectionChoice          string             `json:"sectionChoice"`
	CarriedFactCount       int                `json:"carriedFactCount"`
	TokenCount             int                `json:"tokenCount"`
}

// AgentRecallFact is one fact the searches found and what became of it.
type AgentRecallFact struct {
	Reference      string             `json:"reference"`
	FusedRank      int                `json:"fusedRank"`
	Ranks          []*AgentRecallRank `json:"ranks"`
	RecallDecision string             `json:"recallDecision"`
}

// AgentRecalledPage is one of those pages.
type AgentRecalledPage struct {
	Path string `json:"path"`

	// Summary and Overview are the page's opening and the section of its
	// overview as recall carried them; empty where it carried none.
	Summary  string               `json:"summary"`
	Overview string               `json:"overview"`
	Facts    []*AgentRecalledFact `json:"facts"`
}

// AgentRecalledFact is a fact as the page cites it.
type AgentRecalledFact struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
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

	// CheckoutsKeptToProfile is how many checkouts under this source were
	// kept to what git says about them, and FilesKeptToProfile how many
	// files that was.
	CheckoutsKeptToProfile int `json:"checkoutsKeptToProfile"`
	FilesKeptToProfile     int `json:"filesKeptToProfile"`
}

// AgentKnowledgeSpecification says what a source reads.
type AgentKnowledgeSpecification struct {
	// Type is the source type this source is one of, and Settings what
	// was filled in for it.
	Type     string          `json:"type"`
	Settings json.RawMessage `json:"settings"`

	Computer  string   `json:"computer"`
	Path      string   `json:"path"`
	Format    string   `json:"format"`
	Include   []string `json:"include"`
	Exclude   []string `json:"exclude"`
	Tool      string   `json:"tool"`
	Start     string   `json:"start"`
	Depth     int      `json:"depth"`
	MailboxID string   `json:"mailboxId"`

	// ReadEveryCheckout reads the files of every checkout under this
	// source, the person's own and the ones they cloned alike.
	ReadEveryCheckout bool `json:"readEveryCheckout"`

	// CommitsPerPass is how many commits one pass over this source's
	// tree carries; zero is the pace the program on the machine reads
	// at.
	CommitsPerPass int `json:"commitsPerPass"`

	// OwnCommitsAtLeast is the fewest commits of the person's own a
	// checkout under this source must hold before its files are read;
	// zero is the floor the program on the machine reads at.
	OwnCommitsAtLeast int `json:"ownCommitsAtLeast"`
}

// AgentPassage is one passage a search of what was indexed found, with
// enough of the document it came from to cite it.
type AgentPassage struct {
	DocumentID string     `json:"documentId"`
	ExternalID string     `json:"externalId"`
	Title      string     `json:"title"`
	URL        string     `json:"url"`
	Kind       string     `json:"kind"`
	Author     string     `json:"author"`
	SourceID   string     `json:"sourceId"`
	Source     string     `json:"source"`
	HappenedAt *time.Time `json:"happenedAt"`
	Private    bool       `json:"private"`
	Number     int        `json:"number"`
	Text       string     `json:"text"`
	Score      float64    `json:"score"`
}

// AgentDefinition is one place an identifier in the words is defined.
type AgentDefinition struct {
	Symbol     string `json:"symbol"`
	Kind       string `json:"kind"`
	Line       int    `json:"line"`
	DocumentID string `json:"documentId"`
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
}

// AgentDocumentSearch is what a search of what was indexed found.
// Meaningful is false where the deployment can only search by words.
type AgentDocumentSearch struct {
	Passages    []*AgentPassage    `json:"passages"`
	Definitions []*AgentDefinition `json:"definitions"`
	Meaningful  bool               `json:"meaningful"`

	// Directories are where the passages ranked fall in a tree of files,
	// the most first; a search with Directory looks inside one.
	Directories []*AgentDirectoryHits `json:"directories"`

	// MoreCount is how many more passages the search found past these, at
	// least that many where IsMoreCountLowerBound says so, and NextOffset
	// the offset that reads them; zero is the last page.
	MoreCount             int  `json:"moreCount"`
	IsMoreCountLowerBound bool `json:"isMoreCountLowerBound"`
	NextOffset            int  `json:"nextOffset"`
}

// AgentDocumentExtract is a document and a slice of its text. Next is
// where the read that carries on from this one starts, and zero at the
// end of the document.
type AgentDocumentExtract struct {
	DocumentID string     `json:"documentId"`
	ExternalID string     `json:"externalId"`
	Title      string     `json:"title"`
	URL        string     `json:"url"`
	Kind       string     `json:"kind"`
	Author     string     `json:"author"`
	SourceID   string     `json:"sourceId"`
	Source     string     `json:"source"`
	HappenedAt *time.Time `json:"happenedAt"`
	Private    bool       `json:"private"`
	From       int        `json:"from"`
	Text       string     `json:"text"`
	Total      int        `json:"total"`
	Next       int        `json:"next"`
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

	OverviewsWritten   int `json:"overviewsWritten"`
	ThemesMade         int `json:"themesMade"`
	ThemesUpdated      int `json:"themesUpdated"`
	ReflectionsWritten int `json:"reflectionsWritten"`

	Tokens    int64  `json:"tokens"`
	LastError string `json:"lastError"`

	// Cost is what its model calls cost, in Currency.
	Cost      float64 `json:"cost"`
	Currency  string  `json:"currency"`
	Proposals []struct {
		Kind   string `json:"kind"`
		Path   string `json:"path"`
		To     string `json:"to"`
		Reason string `json:"reason"`
	} `json:"proposals"`
}

const nodeFields = `{ id path kind name aliases summary contactId pinned importance dormant usedAt modifiedAt }`

// pageNodeFields is nodeFields and the overview, for a page read on its
// own: an index of four hundred pages has no use for four hundred
// overviews.
const pageNodeFields = `{ id path kind name aliases summary contactId pinned importance dormant usedAt modifiedAt overview overviewWrittenAt }`
const factFields = `{ id number kind text happenedAt confidence inferred evidence { kind id quote } audiences dormant createdAt }`
const sourceFields = `{ id kind name specification { type settings computer path format include exclude tool start depth mailboxId readEveryCheckout commitsPerPass ownCommitsAtLeast } rootPath enabled cron lastRunAt nextRunAt lastError documentCount chunkCount refusedCount more unknownAuthors checkoutsKeptToProfile filesKeptToProfile }`
const revisionFields = `{ revision kind actor summary change before after path reason createdAt }`
const passageFields = `{ documentId externalId title url kind author sourceId source happenedAt private number text score }`
const extractFields = `{ documentId externalId title url kind author sourceId source happenedAt private from text total next }`
const dreamFields = `{ id jobId startedAt finishedAt digested filed merged rewritten moved dormant embedded backlog coarse strengthened associated rehearsed gaps unknown revised overviewsWritten themesMade themesUpdated reflectionsWritten tokens lastError cost currency proposals { kind path to reason } }`

// The documents.
const (
	DocumentAgentGraphIndex = `query ($under: String, $first: Int) {
		AgentGraphIndex(under: $under, first: $first) ` + nodeFields + `
	}`
	DocumentAgentGraphPage = `query ($path: String!, $linkOffset: Int, $linkLimit: Int) {
		AgentGraphPage(path: $path, linkOffset: $linkOffset, linkLimit: $linkLimit) {
			node ` + pageNodeFields + `
			facts ` + factFields + `
			edges { relation status fromPath toPath }
			children ` + nodeFields + `
			contact { id name emails phones organization }
			linkCount nextLinkOffset
		}
	}`
	DocumentSearchAgentGraph = `query ($query: String!, $first: Int, $offset: Int) {
		SearchAgentGraph(query: $query, first: $first, offset: $offset) {
			nodes ` + nodeFields + `
			facts { fact ` + factFields + ` path name }
			moreNodeCount isMoreNodeCountLowerBound moreFactCount isMoreFactCountLowerBound nextOffset
		}
	}`
	DocumentEvaluateAgentAnswer = `mutation ($question: String!, $expectedAnswer: String!, $outdatedAnswer: String, $answerFrom: String!, $plannedSearches: [String!], $isBroad: Boolean) {
		EvaluateAgentAnswer(question: $question, expectedAnswer: $expectedAnswer, outdatedAnswer: $outdatedAnswer, answerFrom: $answerFrom, plannedSearches: $plannedSearches, isBroad: $isBroad) {
			answerText answerVerdict verdictReason factCount passageCount cost currency answerDurationMS
		}
	}`
	DocumentRecallAgentMemory = `query ($question: String!, $plannedSearches: [String!], $isBroad: Boolean) {
		RecallAgentMemory(question: $question, plannedSearches: $plannedSearches, isBroad: $isBroad) {
			retrievalMode
			pages { path summary overview facts { number text } }
		}
	}`
	DocumentAgentOverviewState = `query ($path: String!) {
		AgentOverviewState(path: $path) {
			path overviewWrittenAt isOverviewStale childCount childShownCount childWithoutOverviewCount
			memberCount memberShownCount memberWithoutOverviewCount linkCount linkShownCount
		}
	}`
	DocumentJudgeAgentRetrievalPlan = `query ($question: String!) {
		JudgeAgentRetrievalPlan(question: $question) { depth depthReason plannedSearches isBroad cost currency }
	}`
	DocumentExplainAgentRecall = `query ($question: String!, $plannedSearches: [String!], $isBroad: Boolean) {
		RecallAgentMemory(question: $question, isExplained: true, plannedSearches: $plannedSearches, isBroad: $isBroad) {
			retrievalMode
			pages { path summary overview facts { number text } }
			explanation {
				retrievalMode plan { searches isBroad }
				queries { queryId queryKind queryText }
				searches { queryId searchName foundCount }
				pages { path fusedRank ranks { queryId searchName rank } hitFactCount recallDecision overviewSectionHeading sectionChoice carriedFactCount tokenCount }
				facts { reference fusedRank ranks { queryId searchName rank } recallDecision }
				isBroadNoteCarried tokenBudget tokensSpent
			}
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
	DocumentSaveAgentKnowledgeSource  = `mutation ($sourceId: String, $kind: String, $name: String, $computer: String, $path: String, $format: String, $rootPath: String, $cron: String, $enabled: Boolean, $mailboxId: String, $readEveryCheckout: Boolean, $commitsPerPass: Int, $ownCommitsAtLeast: Int, $type: String, $settings: JSON) {
		SaveAgentKnowledgeSource(type: $type, settings: $settings, sourceId: $sourceId, kind: $kind, name: $name, computer: $computer, path: $path, format: $format, rootPath: $rootPath, cron: $cron, enabled: $enabled, mailboxId: $mailboxId, readEveryCheckout: $readEveryCheckout, commitsPerPass: $commitsPerPass, ownCommitsAtLeast: $ownCommitsAtLeast) ` + sourceFields + `
	}`
	DocumentSearchAgentDocuments = `query ($query: String!, $first: Int, $offset: Int, $sourceId: String, $directory: String, $computerName: String) {
		SearchAgentDocuments(query: $query, first: $first, offset: $offset, sourceId: $sourceId, directory: $directory, computerName: $computerName) {
			passages ` + passageFields + `
			definitions { symbol kind line documentId externalId title }
			directories { directory sourceId source passageCount }
			meaningful moreCount isMoreCountLowerBound nextOffset
		}
	}`
	DocumentReadAgentDocument = `query ($documentId: String!, $from: Int, $first: Int) {
		ReadAgentDocument(documentId: $documentId, from: $from, first: $first) ` + extractFields + `
	}`
	DocumentDeleteAgentKnowledgeSource = `mutation ($sourceId: String!) { DeleteAgentKnowledgeSource(sourceId: $sourceId) }`
	DocumentSyncAgentKnowledgeSource   = `mutation ($sourceId: String!) { SyncAgentKnowledgeSource(sourceId: $sourceId) }`
	DocumentDreamAgentNow              = `mutation ($bootstrap: Boolean) { DreamAgentNow(bootstrap: $bootstrap) }`
	DocumentRewriteAgentOverview       = `mutation ($path: String!) { RewriteAgentOverview(path: $path) }`
	DocumentRereadAgentDocuments       = `mutation ($minutes: Int!) { RereadAgentDocuments(minutes: $minutes) }`
	DocumentLinkAgentNodes             = `mutation ($path: String!, $to: String!, $relation: String!, $note: String) { LinkAgentNodes(path: $path, to: $to, relation: $relation, note: $note) }`
	DocumentUnlinkAgentNodes           = `mutation ($path: String!, $to: String!, $relation: String!) { UnlinkAgentNodes(path: $path, to: $to, relation: $relation) }`
	DocumentListAgentDreams            = `query ($first: Int) { ListAgentDreams(first: $first) ` + dreamFields + ` }`
	DocumentAgentReadingProgress       = `query { AgentReadingProgress { waiting read perHour hoursLeft bootstrapping } }`
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

// AgentGraphPageOf is one page with its facts and every link.
func AgentGraphPageOf(ctx context.Context, connection *Client, path string) (*AgentGraphPage, error) {
	return AgentGraphPageWithLinks(ctx, connection, path, 0, 0)
}

// AgentGraphPageWithLinks is one page with its facts and linkLimit of its
// links past linkOffset, or every link past it when linkLimit is zero.
func AgentGraphPageWithLinks(ctx context.Context, connection *Client, path string, linkOffset, linkLimit int) (*AgentGraphPage, error) {
	var result struct {
		AgentGraphPage *AgentGraphPage `json:"AgentGraphPage"`
	}
	variables := map[string]any{"path": path}
	if linkOffset > 0 {
		variables["linkOffset"] = linkOffset
	}
	if linkLimit > 0 {
		variables["linkLimit"] = linkLimit
	}
	if err := connection.Execute(ctx, DocumentAgentGraphPage, variables, &result); err != nil {
		return nil, err
	}
	return result.AgentGraphPage, nil
}

// SearchAgentGraph finds pages and facts by words, past the first offset
// of each.
func SearchAgentGraph(ctx context.Context, connection *Client, query string, first, offset int) (*AgentGraphSearch, error) {
	var result struct {
		SearchAgentGraph *AgentGraphSearch `json:"SearchAgentGraph"`
	}
	if err := connection.Execute(ctx, DocumentSearchAgentGraph, map[string]any{"query": query, "first": first, "offset": offset}, &result); err != nil {
		return nil, err
	}
	return result.SearchAgentGraph, nil
}

// RecallAgentMemory is what a turn asking this question would have been
// carried from the graph. It asks nothing of a model and changes nothing.
func RecallAgentMemory(ctx context.Context, connection *Client, question string, plan *AgentRetrievalPlan) (*AgentRecall, error) {
	var result struct {
		RecallAgentMemory *AgentRecall `json:"RecallAgentMemory"`
	}
	if err := connection.Execute(ctx, DocumentRecallAgentMemory, recallVariables(question, plan), &result); err != nil {
		return nil, err
	}
	return result.RecallAgentMemory, nil
}

// AgentOverviewState is how a page's overview stands: how much of what it
// could be written from its prompt shows, and whether that has changed
// since it was written.
type AgentOverviewState struct {
	Path                       string     `json:"path"`
	OverviewWrittenAt          *time.Time `json:"overviewWrittenAt"`
	IsOverviewStale            bool       `json:"isOverviewStale"`
	ChildCount                 int        `json:"childCount"`
	ChildShownCount            int        `json:"childShownCount"`
	ChildWithoutOverviewCount  int        `json:"childWithoutOverviewCount"`
	MemberCount                int        `json:"memberCount"`
	MemberShownCount           int        `json:"memberShownCount"`
	MemberWithoutOverviewCount int        `json:"memberWithoutOverviewCount"`
	LinkCount                  int        `json:"linkCount"`
	LinkShownCount             int        `json:"linkShownCount"`
}

// GetAgentOverviewState reads how a page's overview stands.
func GetAgentOverviewState(ctx context.Context, connection *Client, path string) (*AgentOverviewState, error) {
	var result struct {
		AgentOverviewState *AgentOverviewState `json:"AgentOverviewState"`
	}
	if err := connection.Execute(ctx, DocumentAgentOverviewState, map[string]any{"path": path}, &result); err != nil {
		return nil, err
	}
	return result.AgentOverviewState, nil
}

// AgentJudgedPlan is what the depth judgement said of a question on its
// own, and what the judgement cost.
type AgentJudgedPlan struct {
	Depth           string   `json:"depth"`
	DepthReason     string   `json:"depthReason"`
	PlannedSearches []string `json:"plannedSearches"`
	IsBroad         bool     `json:"isBroad"`
	Cost            float64  `json:"cost"`
	Currency        string   `json:"currency"`
}

// JudgeAgentRetrievalPlan asks the depth judgement what plan a live turn
// would follow for a question: one call to the fast model.
func JudgeAgentRetrievalPlan(ctx context.Context, connection *Client, question string) (*AgentJudgedPlan, error) {
	var result struct {
		JudgeAgentRetrievalPlan *AgentJudgedPlan `json:"JudgeAgentRetrievalPlan"`
	}
	if err := connection.Execute(ctx, DocumentJudgeAgentRetrievalPlan, map[string]any{"question": question}, &result); err != nil {
		return nil, err
	}
	return result.JudgeAgentRetrievalPlan, nil
}

// recallVariables are a recall's question and, where one is given, the
// retrieval plan to follow.
func recallVariables(question string, plan *AgentRetrievalPlan) map[string]any {
	variables := map[string]any{"question": question}
	if plan != nil {
		if len(plan.Searches) > 0 {
			variables["plannedSearches"] = plan.Searches
		}
		if plan.IsBroad {
			variables["isBroad"] = true
		}
	}
	return variables
}

// ExplainAgentRecall is RecallAgentMemory and why: what each search found,
// and why each page and fact was carried or left out.
func ExplainAgentRecall(ctx context.Context, connection *Client, question string, plan *AgentRetrievalPlan) (*AgentRecall, error) {
	var result struct {
		RecallAgentMemory *AgentRecall `json:"RecallAgentMemory"`
	}
	if err := connection.Execute(ctx, DocumentExplainAgentRecall, recallVariables(question, plan), &result); err != nil {
		return nil, err
	}
	return result.RecallAgentMemory, nil
}

// AgentAnswerEvaluation is one question of a memory evaluation, answered
// and graded.
type AgentAnswerEvaluation struct {
	AnswerText       string  `json:"answerText"`
	AnswerVerdict    string  `json:"answerVerdict"`
	VerdictReason    string  `json:"verdictReason"`
	FactCount        int     `json:"factCount"`
	PassageCount     int     `json:"passageCount"`
	Cost             float64 `json:"cost"`
	Currency         string  `json:"currency"`
	AnswerDurationMS int     `json:"answerDurationMS"`
}

// EvaluateAgentAnswer answers a question from memory, sources or both,
// and grades the answer against the expected one.
func EvaluateAgentAnswer(ctx context.Context, connection *Client, question, expectedAnswer, outdatedAnswer, answerFrom string, plan *AgentRetrievalPlan) (*AgentAnswerEvaluation, error) {
	var result struct {
		EvaluateAgentAnswer *AgentAnswerEvaluation `json:"EvaluateAgentAnswer"`
	}
	variables := map[string]any{"question": question, "expectedAnswer": expectedAnswer, "answerFrom": answerFrom}
	if outdatedAnswer != "" {
		variables["outdatedAnswer"] = outdatedAnswer
	}
	if plan != nil {
		if len(plan.Searches) > 0 {
			variables["plannedSearches"] = plan.Searches
		}
		if plan.IsBroad {
			variables["isBroad"] = true
		}
	}
	if err := connection.Execute(ctx, DocumentEvaluateAgentAnswer, variables, &result); err != nil {
		return nil, err
	}
	return result.EvaluateAgentAnswer, nil
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

// AgentDirectoryHits is a directory a search's passages are in.
type AgentDirectoryHits struct {
	Directory    string `json:"directory"`
	SourceID     string `json:"sourceId"`
	Source       string `json:"source"`
	PassageCount int    `json:"passageCount"`
}

// AgentDocumentQuery is what to search for, and where.
type AgentDocumentQuery struct {
	Words         string
	First, Offset int
	SourceID      string

	// Directory narrows to what a source of files read under it, on
	// ComputerName where several have the path.
	Directory    string
	ComputerName string
}

// SearchAgentDocuments finds passages in what the sources indexed: the
// same search the agent's own knowledge tool runs, past the first offset
// passages of its ranking.
func SearchAgentDocuments(ctx context.Context, connection *Client, query *AgentDocumentQuery) (*AgentDocumentSearch, error) {
	var result struct {
		SearchAgentDocuments *AgentDocumentSearch `json:"SearchAgentDocuments"`
	}
	variables := map[string]any{"query": query.Words, "first": query.First, "offset": query.Offset}
	for name, value := range map[string]string{"sourceId": query.SourceID, "directory": query.Directory, "computerName": query.ComputerName} {
		if value != "" {
			variables[name] = value
		}
	}
	if err := connection.Execute(ctx, DocumentSearchAgentDocuments, variables, &result); err != nil {
		return nil, err
	}
	return result.SearchAgentDocuments, nil
}

// ReadAgentDocument reads one indexed document from an offset.
func ReadAgentDocument(ctx context.Context, connection *Client, documentId string, from, first int) (*AgentDocumentExtract, error) {
	var result struct {
		ReadAgentDocument *AgentDocumentExtract `json:"ReadAgentDocument"`
	}
	if err := connection.Execute(ctx, DocumentReadAgentDocument, map[string]any{
		"documentId": documentId, "from": from, "first": first,
	}, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentDocument, nil
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

// AgentReadingProgress is how far the night has got through what was
// indexed, and how long the rest takes at the pace of the last dreams.
type AgentReadingProgress struct {
	Waiting       int64   `json:"waiting"`
	Read          int64   `json:"read"`
	PerHour       float64 `json:"perHour"`
	HoursLeft     float64 `json:"hoursLeft"`
	Bootstrapping bool    `json:"bootstrapping"`
}

// ReadAgentReadingProgress asks how far the reading has got.
func ReadAgentReadingProgress(ctx context.Context, connection *Client) (*AgentReadingProgress, error) {
	var result struct {
		AgentReadingProgress *AgentReadingProgress `json:"AgentReadingProgress"`
	}
	if err := connection.Execute(ctx, DocumentAgentReadingProgress, nil, &result); err != nil {
		return nil, err
	}
	return result.AgentReadingProgress, nil
}

// DreamAgentNow queues a dream now, whatever the agent's hours. bootstrap,
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

// RewriteAgentOverview has the next night write a page's overview again,
// whether or not what it is written from has changed.
func RewriteAgentOverview(ctx context.Context, connection *Client, path string) error {
	var result struct {
		RewriteAgentOverview bool `json:"RewriteAgentOverview"`
	}
	return connection.Execute(ctx, DocumentRewriteAgentOverview, map[string]any{"path": path}, &result)
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

// DocumentSpeakFirstNow has the agent start a conversation now.
const DocumentSpeakFirstNow = `mutation ($speakFirstReason: String!) { SpeakFirstNow(speakFirstReason: $speakFirstReason) }`

// SpeakFirstNow has the agent start a conversation in the main one now,
// for a reason: onboarding, memory_check or idea.
func SpeakFirstNow(ctx context.Context, connection *Client, speakFirstReason string) error {
	var result struct {
		SpeakFirstNow bool `json:"SpeakFirstNow"`
	}
	return connection.Execute(ctx, DocumentSpeakFirstNow, map[string]any{"speakFirstReason": speakFirstReason}, &result)
}

// DocumentListAgentEvaluationQuestions reads the memory check's questions.
const DocumentListAgentEvaluationQuestions = `query ($questionStates: [String!]) { ListAgentEvaluationQuestions(questionStates: $questionStates) { id createdAt questionKind questionText expectedAnswer outdatedAnswer questionState isAnswerFiledAfter answeredAt } }`

// DocumentImportAgentEvaluationQuestions adds questions from a file.
const DocumentImportAgentEvaluationQuestions = `mutation ($questions: [ImportedEvaluationQuestionInput!]!) { ImportAgentEvaluationQuestions(questions: $questions) }`

// AgentEvaluationQuestion is a memory check question as the API gives it.
type AgentEvaluationQuestion struct {
	ID                 string     `json:"id"`
	CreatedAt          time.Time  `json:"createdAt"`
	QuestionKind       string     `json:"questionKind"`
	QuestionText       string     `json:"questionText"`
	ExpectedAnswer     string     `json:"expectedAnswer"`
	OutdatedAnswer     string     `json:"outdatedAnswer,omitempty"`
	QuestionState      string     `json:"questionState"`
	IsAnswerFiledAfter bool       `json:"isAnswerFiledAfter"`
	AnsweredAt         *time.Time `json:"answeredAt,omitempty"`
}

// ImportedEvaluationQuestion is one question to import.
type ImportedEvaluationQuestion struct {
	QuestionKind   string `json:"questionKind,omitempty"`
	QuestionText   string `json:"questionText"`
	ExpectedAnswer string `json:"expectedAnswer"`
	OutdatedAnswer string `json:"outdatedAnswer,omitempty"`
}

// ListAgentEvaluationQuestions is the memory check's questions, newest
// first, in the states given or in all of them.
func ListAgentEvaluationQuestions(ctx context.Context, connection *Client, questionStates []string) ([]*AgentEvaluationQuestion, error) {
	var result struct {
		ListAgentEvaluationQuestions []*AgentEvaluationQuestion `json:"ListAgentEvaluationQuestions"`
	}
	variables := map[string]any{}
	if len(questionStates) > 0 {
		variables["questionStates"] = questionStates
	}
	if err := connection.Execute(ctx, DocumentListAgentEvaluationQuestions, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentEvaluationQuestions, nil
}

// ImportAgentEvaluationQuestions adds questions as confirmed, skipping
// those already on record, and says how many were added.
func ImportAgentEvaluationQuestions(ctx context.Context, connection *Client, questions []ImportedEvaluationQuestion) (int, error) {
	var result struct {
		ImportAgentEvaluationQuestions int `json:"ImportAgentEvaluationQuestions"`
	}
	if err := connection.Execute(ctx, DocumentImportAgentEvaluationQuestions, map[string]any{"questions": questions}, &result); err != nil {
		return 0, err
	}
	return result.ImportAgentEvaluationQuestions, nil
}

// DocumentListAgentEvaluationRuns reads the memory check's runs.
const DocumentListAgentEvaluationRuns = `query ($first: Int) { ListAgentEvaluationRuns(first: $first) { id startedAt finishedAt questionCount cost sourceScores { answerFrom answeredCount scorePercent scorePercentWithoutFiledAfter filedAfterCount verdictCounts } } }`

// DocumentEvaluateAgentMemoryNow starts a run now.
const DocumentEvaluateAgentMemoryNow = `mutation { EvaluateAgentMemoryNow { id startedAt } }`

// AgentEvaluationRun is a run of the memory check as the API gives it.
type AgentEvaluationRun struct {
	ID            string                   `json:"id"`
	StartedAt     time.Time                `json:"startedAt"`
	FinishedAt    *time.Time               `json:"finishedAt,omitempty"`
	QuestionCount int                      `json:"questionCount"`
	Cost          float64                  `json:"cost"`
	SourceScores  []*EvaluationSourceScore `json:"sourceScores"`
}

// EvaluationSourceScore is how the answers from one source scored.
type EvaluationSourceScore struct {
	AnswerFrom                    string         `json:"answerFrom"`
	AnsweredCount                 int            `json:"answeredCount"`
	ScorePercent                  float64        `json:"scorePercent"`
	ScorePercentWithoutFiledAfter float64        `json:"scorePercentWithoutFiledAfter"`
	FiledAfterCount               int            `json:"filedAfterCount"`
	VerdictCounts                 map[string]int `json:"verdictCounts"`
}

// ListAgentEvaluationRuns is the memory check's runs, newest first.
func ListAgentEvaluationRuns(ctx context.Context, connection *Client, first int) ([]*AgentEvaluationRun, error) {
	var result struct {
		ListAgentEvaluationRuns []*AgentEvaluationRun `json:"ListAgentEvaluationRuns"`
	}
	if err := connection.Execute(ctx, DocumentListAgentEvaluationRuns, map[string]any{"first": first}, &result); err != nil {
		return nil, err
	}
	return result.ListAgentEvaluationRuns, nil
}

// EvaluateAgentMemoryNow starts a run of the memory check now, or returns
// the one under way.
func EvaluateAgentMemoryNow(ctx context.Context, connection *Client) (*AgentEvaluationRun, error) {
	var result struct {
		EvaluateAgentMemoryNow *AgentEvaluationRun `json:"EvaluateAgentMemoryNow"`
	}
	if err := connection.Execute(ctx, DocumentEvaluateAgentMemoryNow, nil, &result); err != nil {
		return nil, err
	}
	return result.EvaluateAgentMemoryNow, nil
}
