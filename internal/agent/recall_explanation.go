package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/models"
)

// A recall explanation says why a question was given the memories it was
// given and not others: what each search found and in what order, where
// each page landed once the searches were fused, and for every page and
// loose fact whether it was carried and, if not, what kept it out. It is
// recorded only when asked for, by `teanode agent memory recall --explain`,
// so a turn pays nothing for it.

// Why a page or fact was carried, or what kept it out.
const (
	RecallDecisionCarried = "carried"

	// RecallDecisionPageLimit: the overlay had expanded as many pages as
	// it expands, or filled its blocks, before this one's turn came.
	RecallDecisionPageLimit = "page_limit"

	// RecallDecisionUnhitPageLimit: no fact on the page was found, and the
	// overlay had expanded as many such pages as it takes.
	RecallDecisionUnhitPageLimit = "unhit_page_limit"

	// RecallDecisionTokenBudget: the page or fact did not fit in what was
	// left of the overlay's tokens.
	RecallDecisionTokenBudget = "token_budget"

	// RecallDecisionAlreadyExpanded: the page was expanded once already.
	RecallDecisionAlreadyExpanded = "already_expanded"

	// RecallDecisionRetired: the fact is folded, struck or superseded, so
	// the page no longer says it.
	RecallDecisionRetired = "retired"

	// RecallDecisionShownOnPage: the fact was carried inside its page's
	// block rather than among the loose facts.
	RecallDecisionShownOnPage = "shown_on_page"

	// RecallDecisionFactLimit: the loose facts were full.
	RecallDecisionFactLimit = "fact_limit"

	// RecallDecisionOutranked: a query found it, but fusing every query's
	// lists left it out of the candidates.
	RecallDecisionOutranked = "outranked"
)

// Which section of a page's overview recall carried, and why.
const (
	RecallSectionNone             = "none"
	RecallSectionMatchedByMeaning = "matched_by_meaning"
	RecallSectionMatchedByWords   = "matched_by_words"
	RecallSectionFirst            = "first"

	// RecallSectionFirstGaveWay: the meaning search matched the first
	// section, and a later one the question's other words point at was
	// carried instead.
	RecallSectionFirstGaveWay = "first_gave_way_to_words"

	// RecallSectionLeftOutForBudget: the page was carried without its
	// section, which did not fit.
	RecallSectionLeftOutForBudget = "left_out_for_budget"
)

// RetrievalPlan is how a turn's graph retrieval searches beyond the
// message's own words: the focused searches the depth judgement planned,
// and whether the message asks about a whole area. A live turn gets it from
// the judgement; a replay is given it, so that it asks no model anything.
type RetrievalPlan struct {
	Searches []string `json:"searches"`
	IsBroad  bool     `json:"isBroad"`
}

// isEmpty says the plan changes nothing: retrieval is the basic one.
func (self *RetrievalPlan) isEmpty() bool {
	return self == nil || (len(self.Searches) == 0 && !self.IsBroad)
}

// The retrieval modes an explanation or an evaluation names: basic is the
// message's own search alone, planned follows a retrieval plan.
const (
	RetrievalModeBasic   = "basic"
	RetrievalModePlanned = "planned"
)

// What each query of a retrieval is.
const (
	RecallQueryMessage = "message" // the message's own words
	RecallQueryPlanned = "planned" // a focused search the plan named
	RecallQueryHop     = "hop"     // the pages linked to the top page the planned searches found
	RecallQueryBroad   = "broad"   // the pages whose overview sections a broad question matched, counted again
)

// RecallExplanation is how one question's recall went.
type RecallExplanation struct {
	// RetrievalMode is basic or planned, and Plan the plan followed, if
	// any.
	RetrievalMode string         `json:"retrievalMode"`
	Plan          *RetrievalPlan `json:"plan" graphapi:"nullable"`

	// Queries is each query the retrieval ran, in order: the message
	// first, then the planned searches, the hop and the broad weighting.
	Queries []*RecallQuery `json:"queries"`

	// Searches is each list a query produced and how much it found.
	Searches []*RecallSearch `json:"searches"`

	// Pages is every page any query found: the ones fused into the
	// candidates first, by fused rank, then the ones fusion left out.
	Pages []*RecallPageExplanation `json:"pages"`

	// Facts is every fact any query found, in the same order.
	Facts []*RecallFactExplanation `json:"facts"`

	// IsBroadNoteCarried says the turn was told a survey reads the whole
	// area, as a broad question is.
	IsBroadNoteCarried bool `json:"isBroadNoteCarried"`

	// TokenBudget is what the overlay may spend on the graph, and
	// TokensSpent what it did.
	TokenBudget int `json:"tokenBudget"`
	TokensSpent int `json:"tokensSpent"`

	// factLists and fusedFacts are the facts' lists and their fusion,
	// kept until the facts' pages are read.
	factLists  []rankedFactList
	fusedFacts []*models.AgentFact
}

// RecallQuery is one query a retrieval ran.
type RecallQuery struct {
	QueryID   string `json:"queryId"`
	QueryKind string `json:"queryKind"`

	// QueryText is the words searched, or for the hop the page followed.
	QueryText string `json:"queryText"`
}

// RecallSearch is one list a query produced, and how much it found.
type RecallSearch struct {
	QueryID    string `json:"queryId"`
	SearchName string `json:"searchName"`
	FoundCount int    `json:"foundCount"`
}

// RecallRank is where a page or fact stood in one list.
type RecallRank struct {
	QueryID    string `json:"queryId"`
	SearchName string `json:"searchName"`
	Rank       int    `json:"rank"`
}

// RecallPageExplanation is one page and what became of it.
type RecallPageExplanation struct {
	Path   string `json:"path"`
	NodeID string `json:"nodeId"`

	// FusedRank is the page's place among the candidates once every
	// query's lists were fused, from one; zero where fusion left it out.
	// Ranks is its place in each list that found it.
	FusedRank int           `json:"fusedRank"`
	Ranks     []*RecallRank `json:"ranks"`

	// HitFactCount is how many of the facts the searches found are on it.
	HitFactCount int `json:"hitFactCount"`

	RecallDecision string `json:"recallDecision"`

	// OverviewSectionHeading is the section of the overview carried, and
	// SectionChoice why that one; see the RecallSection values.
	OverviewSectionHeading string `json:"overviewSectionHeading"`
	SectionChoice          string `json:"sectionChoice"`

	// CarriedFactCount is how many facts the page's block carried, and
	// TokenCount what the block cost.
	CarriedFactCount int `json:"carriedFactCount"`
	TokenCount       int `json:"tokenCount"`
}

// RecallFactExplanation is one fact the searches found and what became of
// it.
type RecallFactExplanation struct {
	// Reference is the fact as a page cites it, path#number.
	Reference string `json:"reference"`

	FusedRank int           `json:"fusedRank"`
	Ranks     []*RecallRank `json:"ranks"`

	RecallDecision string `json:"recallDecision"`
}

// rankedPageList and rankedFactList are one list a query produced.
type rankedPageList struct {
	queryId, searchName string
	nodes               []*models.AgentNode
}

type rankedFactList struct {
	queryId, searchName string
	facts               []*models.AgentFact
}

// explainQuery records a query and the lists it produced.
func (self *RecallExplanation) explainQuery(query *RecallQuery, pageLists []rankedPageList, factLists []rankedFactList) {
	if self == nil {
		return
	}
	self.Queries = append(self.Queries, query)
	for _, list := range pageLists {
		self.Searches = append(self.Searches, &RecallSearch{QueryID: list.queryId, SearchName: list.searchName, FoundCount: len(list.nodes)})
	}
	for _, list := range factLists {
		self.Searches = append(self.Searches, &RecallSearch{QueryID: list.queryId, SearchName: list.searchName, FoundCount: len(list.facts)})
	}
	self.factLists = append(self.factLists, factLists...)
}

// explainPages records every page the lists found: the fused ones first,
// by fused rank, then the rest, each with its place in every list.
func (self *RecallExplanation) explainPages(fused []*models.AgentNode, lists []rankedPageList) {
	if self == nil {
		return
	}
	explained := map[string]*RecallPageExplanation{}
	add := func(node *models.AgentNode, fusedRank int) {
		if explained[node.ID] != nil {
			return
		}
		decision := RecallDecisionPageLimit
		if fusedRank == 0 {
			decision = RecallDecisionOutranked
		}
		page := &RecallPageExplanation{Path: node.Path, NodeID: node.ID, FusedRank: fusedRank, Ranks: []*RecallRank{},
			RecallDecision: decision, SectionChoice: RecallSectionNone}
		explained[node.ID] = page
		self.Pages = append(self.Pages, page)
	}
	for index, node := range fused {
		add(node, index+1)
	}
	for _, list := range lists {
		for _, node := range list.nodes {
			add(node, 0)
		}
	}
	for _, list := range lists {
		for index, node := range list.nodes {
			page := explained[node.ID]
			page.Ranks = append(page.Ranks, &RecallRank{QueryID: list.queryId, SearchName: list.searchName, Rank: index + 1})
		}
	}
}

// explainFacts records every fact the lists found, once the facts' pages
// are known: the fused ones first, then the rest.
func (self *RecallExplanation) explainFacts(paths map[string]string) {
	if self == nil {
		return
	}
	explained := map[string]*RecallFactExplanation{}
	add := func(fact *models.AgentFact, fusedRank int) {
		if explained[fact.ID] != nil {
			return
		}
		decision := RecallDecisionFactLimit
		if fusedRank == 0 {
			decision = RecallDecisionOutranked
		}
		explainedFact := &RecallFactExplanation{Reference: fact.Reference(paths[fact.NodeID]), FusedRank: fusedRank,
			Ranks: []*RecallRank{}, RecallDecision: decision}
		explained[fact.ID] = explainedFact
		self.Facts = append(self.Facts, explainedFact)
	}
	for index, fact := range self.fusedFacts {
		add(fact, index+1)
	}
	for _, list := range self.factLists {
		for _, fact := range list.facts {
			add(fact, 0)
		}
	}
	for _, list := range self.factLists {
		for index, fact := range list.facts {
			explained[fact.ID].Ranks = append(explained[fact.ID].Ranks, &RecallRank{QueryID: list.queryId, SearchName: list.searchName, Rank: index + 1})
		}
	}
}

// page is the explanation of a page, or nil.
func (self *RecallExplanation) page(nodeId string) *RecallPageExplanation {
	if self == nil {
		return nil
	}
	for _, page := range self.Pages {
		if page.NodeID == nodeId {
			return page
		}
	}
	return nil
}

// fact is the explanation of a fact by its reference, or nil.
func (self *RecallExplanation) fact(reference string) *RecallFactExplanation {
	if self == nil {
		return nil
	}
	for _, fact := range self.Facts {
		if fact.Reference == reference {
			return fact
		}
	}
	return nil
}

// ExplainRecall answers what recall would carry for a question, as
// RecallForQuestion does, and why: the explanation of every page and fact
// the searches found. A plan given is followed as a live turn follows the
// depth judgement's, without asking any model for one; nil is basic
// recall. Nothing is marked as used.
func (self *Agent) ExplainRecall(ctx context.Context, found *models.Agent, owner *models.User, question string, plan *RetrievalPlan) ([]*RecalledPage, *RecallExplanation, error) {
	explanation := &RecallExplanation{RetrievalMode: RetrievalModeBasic, Queries: []*RecallQuery{}, Searches: []*RecallSearch{},
		Pages: []*RecallPageExplanation{}, Facts: []*RecallFactExplanation{}}
	if !plan.isEmpty() {
		explanation.RetrievalMode, explanation.Plan = RetrievalModePlanned, plan
	}
	pages, err := self.recallForQuestion(ctx, found, owner, question, plan, explanation)
	if err != nil {
		return nil, nil, err
	}
	return pages, explanation, nil
}
