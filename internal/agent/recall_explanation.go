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

// RecallExplanation is how one question's recall went.
type RecallExplanation struct {
	// Searches is each search's count of what it found, in the order they
	// ran.
	Searches []*RecallSearch `json:"searches"`

	// Pages is every page any search found, in the order recall considered
	// them: fused rank first.
	Pages []*RecallPageExplanation `json:"pages"`

	// Facts is every fact the searches found, fused, and what became of it.
	Facts []*RecallFactExplanation `json:"facts"`

	// TokenBudget is what the overlay may spend on the graph, and
	// TokensSpent what it did.
	TokenBudget int `json:"tokenBudget"`
	TokensSpent int `json:"tokensSpent"`

	// factsByWords and factsByMeaning are what the two fact searches
	// found, kept until the facts' pages are read.
	factsByWords, factsByMeaning []*models.AgentFact
}

// RecallSearch is one search: pages or facts, by words, by meaning, or by
// overview section, and how many it found.
type RecallSearch struct {
	SearchName string `json:"searchName"`
	FoundCount int    `json:"foundCount"`
}

// RecallPageExplanation is one page and what became of it.
type RecallPageExplanation struct {
	Path   string `json:"path"`
	NodeID string `json:"nodeId"`

	// FusedRank is the page's place once the searches were fused, from
	// one. WordsRank, MeaningRank and SectionRank are its place in each
	// search, zero where that search did not find it.
	FusedRank   int `json:"fusedRank"`
	WordsRank   int `json:"wordsRank"`
	MeaningRank int `json:"meaningRank"`
	SectionRank int `json:"sectionRank"`

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

	// WordsRank and MeaningRank are its place in each search, zero where
	// that search did not find it; FusedRank its place once fused.
	FusedRank   int `json:"fusedRank"`
	WordsRank   int `json:"wordsRank"`
	MeaningRank int `json:"meaningRank"`

	RecallDecision string `json:"recallDecision"`
}

// explainSearch records one search's count.
func (self *RecallExplanation) explainSearch(searchName string, foundCount int) {
	if self == nil {
		return
	}
	self.Searches = append(self.Searches, &RecallSearch{SearchName: searchName, FoundCount: foundCount})
}

// explainPages records the pages each search found and the fused order.
func (self *RecallExplanation) explainPages(fused, byWords, byMeaning, bySection []*models.AgentNode) {
	if self == nil {
		return
	}
	rankOf := func(nodes []*models.AgentNode) map[string]int {
		ranks := make(map[string]int, len(nodes))
		for index, node := range nodes {
			if _, seen := ranks[node.ID]; !seen {
				ranks[node.ID] = index + 1
			}
		}
		return ranks
	}
	wordsRanks, meaningRanks, sectionRanks := rankOf(byWords), rankOf(byMeaning), rankOf(bySection)
	for index, node := range fused {
		self.Pages = append(self.Pages, &RecallPageExplanation{
			Path: node.Path, NodeID: node.ID, FusedRank: index + 1,
			WordsRank: wordsRanks[node.ID], MeaningRank: meaningRanks[node.ID], SectionRank: sectionRanks[node.ID],
			RecallDecision: RecallDecisionPageLimit, SectionChoice: RecallSectionNone,
		})
	}
}

// explainFacts records the facts each search found and the fused order.
func (self *RecallExplanation) explainFacts(fused, byWords, byMeaning []*models.AgentFact, paths map[string]string) {
	if self == nil {
		return
	}
	rankOf := func(facts []*models.AgentFact) map[string]int {
		ranks := make(map[string]int, len(facts))
		for index, fact := range facts {
			if _, seen := ranks[fact.ID]; !seen {
				ranks[fact.ID] = index + 1
			}
		}
		return ranks
	}
	wordsRanks, meaningRanks := rankOf(byWords), rankOf(byMeaning)
	for index, fact := range fused {
		self.Facts = append(self.Facts, &RecallFactExplanation{
			Reference: fact.Reference(paths[fact.NodeID]), FusedRank: index + 1,
			WordsRank: wordsRanks[fact.ID], MeaningRank: meaningRanks[fact.ID], RecallDecision: RecallDecisionFactLimit,
		})
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
// the searches found. Nothing is marked as used.
func (self *Agent) ExplainRecall(ctx context.Context, found *models.Agent, owner *models.User, question string) ([]*RecalledPage, *RecallExplanation, error) {
	explanation := &RecallExplanation{Searches: []*RecallSearch{}, Pages: []*RecallPageExplanation{}, Facts: []*RecallFactExplanation{}}
	pages, err := self.recallForQuestion(ctx, found, owner, question, explanation)
	if err != nil {
		return nil, nil, err
	}
	return pages, explanation, nil
}
