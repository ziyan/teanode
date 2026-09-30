package agent

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// recallForTurn puts what the agent already knows about this turn in
// front of the model, before the first round.
//
// Four searches: pages and facts by words, pages and facts by meaning.
// Their answers are fused by reciprocal rank -- each row scores the sum of
// 1/(60+rank) over the lists it appears in -- which needs no tuning
// because it compares positions rather than scores, and a full-text rank
// and a cosine are not on the same scale.
//
// Then the top pages are expanded: the page's opening and its most-used
// facts, so that "what does she work on" is answered from the page rather
// than from one sentence that happened to match.
//
// It costs one embedding call -- one for the turn, shared by the graph
// and the documents, which used to be one each -- and no round trip to
// the model, which is why it happens every turn rather than being asked
// for.
func (self *AskRun) recallForTurn(ctx context.Context) {
	// A job's turn is not the person speaking. Its message is a prompt
	// the code wrote -- a batch of twenty documents, a month's record, a
	// thread to summarize -- and what it needs from the graph is in that
	// prompt already, put there by the code that knows what the job is
	// about. Searched by its words it did the opposite of recalling:
	// nine thousand tokens of instructions, any word of which matches,
	// ranked the whole corpus of a third of a million documents, eleven
	// times at once, for seven minutes, while the model sat idle.
	if self.settings.Headless {
		return
	}
	// What has no vector yet is not given one here. Backfilling on the
	// interactive path put twenty embedding calls between the person
	// pressing return and the model being asked anything, for rows the
	// turn was not going to look at; the night's dreamEmbed stage
	// backfills two hundred at a time with nobody waiting.
	words := strings.TrimSpace(self.settings.Message)
	if words == "" {
		return
	}
	// What the person pointed at is part of what this turn is about.
	for _, reference := range self.settings.References {
		words += "\n" + reference.Subject
	}

	nodes, facts, sections := self.searchGraph(ctx, words, recallCandidates)
	nodes, facts, sections = self.followRetrievalPlan(ctx, nodes, facts, sections)
	self.writeRecalled(ctx, nodes, facts, sections)
	self.recallLessons(ctx, words)
	self.recallFromKnowledge(ctx, words)
}

// recallLinkedPages is how many pages linked to the top page one hop
// brings in, the strongest links first.
const recallLinkedPages = 5

// followRetrievalPlan applies what the depth judgement said of how to
// search for this message, at no cost beyond the searches themselves: no
// model is asked anything here, and the overlay's budget is the same.
//
// A message that refers to things indirectly, or needs two things found,
// gets the focused searches the judgement planned, fused with the
// message's own, and the pages most strongly linked to the top page those
// searches found, one hop along the graph: "who shares the car insurance" reaches the policy
// and, from it, the people on it. A message about a whole area has the
// pages whose overview sections it matched counted twice, since those are
// what describe an area, and is told that a survey reads all of it. A
// message the judgement said nothing about is searched as before.
func (self *AskRun) followRetrievalPlan(ctx context.Context, nodes []*models.AgentNode, facts []*models.AgentFact, sections map[string]string) ([]*models.AgentNode, []*models.AgentFact, map[string]string) {
	if len(self.plannedSearches) == 0 && !self.isBroadQuestion {
		return nodes, facts, sections
	}
	nodeLists := [][]*models.AgentNode{nodes}
	factLists := [][]*models.AgentFact{facts}
	// The hop starts from what the planned searches found first: the
	// message's own words are the vague ones, and their top page may be
	// anything.
	var hopFrom *models.AgentNode
	for _, search := range self.plannedSearches {
		plannedNodes, plannedFacts, plannedSections := self.searchGraph(ctx, search, recallCandidates)
		if hopFrom == nil && len(plannedNodes) > 0 {
			hopFrom = plannedNodes[0]
		}
		nodeLists = append(nodeLists, plannedNodes)
		factLists = append(factLists, plannedFacts)
		for nodeId, sectionId := range plannedSections {
			if _, isMatched := sections[nodeId]; !isMatched {
				sections[nodeId] = sectionId
			}
		}
	}
	if hopFrom != nil {
		nodeLists = append(nodeLists, self.linkedPages(ctx, hopFrom))
	}
	if self.isBroadQuestion {
		var described []*models.AgentNode
		for _, node := range nodes {
			if _, isMatched := sections[node.ID]; isMatched {
				described = append(described, node)
			}
		}
		nodeLists = append(nodeLists, described)
		self.Recall("This message asks about a whole area, and what is recalled here is a few pages of it. " +
			"The survey tool asks every overview in the area for its part of the answer and combines them.")
	}
	return fuseNodes(recallCandidates, nodeLists...), fuseFacts(recallCandidates, factLists...), sections
}

// linkedPages is the pages most strongly linked to a page, strongest
// first, that are not dormant.
func (self *AskRun) linkedPages(ctx context.Context, page *models.AgentNode) []*models.AgentNode {
	agentId := self.settings.Agent.ID
	var linked []*models.AgentNode
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		edges, err := tx.ListAgentEdges(agentId, page.ID)
		if err != nil {
			return err
		}
		sort.SliceStable(edges, func(left, right int) bool { return edges[left].Weight > edges[right].Weight })
		var otherIds []string
		for _, edge := range edges {
			if len(otherIds) >= recallLinkedPages {
				break
			}
			otherId := edge.ToID
			if otherId == page.ID {
				otherId = edge.FromID
			}
			otherIds = append(otherIds, otherId)
		}
		found, err := tx.GetAgentNodes(agentId, otherIds)
		if err != nil {
			return err
		}
		byId := make(map[string]*models.AgentNode, len(found))
		for _, node := range found {
			byId[node.ID] = node
		}
		for _, otherId := range otherIds {
			if node := byId[otherId]; node != nil && !node.Dormant {
				linked = append(linked, node)
			}
		}
		return nil
	}); err != nil {
		log.Debugf("cannot follow the links of %q: %s", page.Path, err)
	}
	return linked
}

// recallFromKnowledge puts the two or three passages of the person's own
// files and chat that this turn's words touch in front of the model.
//
// Only where they score well: a question about their own work should be
// answered from their own work without a search, and a question about
// anything else should not drag three code files into the prompt. The
// tool is there for going further.
func (self *AskRun) recallFromKnowledge(ctx context.Context, words string) {
	if !FeatureAllowed(self.agent.settings.Configuration(), "knowledge") {
		return
	}
	var byWords []*models.AgentChunk
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		byWords, err = tx.SearchAgentChunks(self.settings.Agent.ID, nil, words, recallChunks*4)
		return err
	}); err != nil {
		log.Debugf("cannot search what %q indexed: %s", self.settings.Owner.Username, err)
	}
	byMeaning, indexed := self.SearchKnowledgeByMeaning(ctx, nil, words, recallChunks*2)
	var chunks []*models.AgentChunk
	if indexed {
		chunks = fuseChunks(recallChunks, byMeaning, byWords)
	} else {
		chunks = self.RankChunksByMeaning(ctx, words, byWords, recallChunks)
	}
	if len(chunks) == 0 {
		return
	}
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		ids := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			ids = append(ids, chunk.DocumentID)
		}
		documents, err := tx.GetAgentDocuments(self.settings.Agent.ID, ids)
		if err != nil {
			return err
		}
		byId := map[string]*models.AgentDocument{}
		for _, document := range documents {
			byId[document.ID] = document
		}
		spent := 0
		for _, chunk := range chunks {
			document := byId[chunk.DocumentID]
			if document == nil {
				continue
			}
			line := document.Cite() + "  [" + document.ID + "#" + strconv.Itoa(chunk.Number) + "]\n  " +
				cutRunes(chunk.Text, recallChunkCharacters)
			cost := llm.EstimateTokens(line)
			if spent+cost > recallKnowledgeTokens {
				break
			}
			spent += cost
			self.Recall(line)
		}
		return nil
	}); err != nil {
		log.Debugf("cannot recall from what was indexed: %s", err)
	}
}

// searchGraph is the fused search the turn and the tool both use. Besides
// the pages and facts it says which overview section of a page the
// question's meaning matched best, by page id: a page found by that
// section is ranked as one found by meaning, and recall carries that
// section of it.
func (self *AskRun) searchGraph(ctx context.Context, words string, limit int) ([]*models.AgentNode, []*models.AgentFact, map[string]string) {
	agentId := self.settings.Agent.ID

	var wordNodes []*models.AgentNode
	var wordFacts []*models.AgentFact
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		wordNodes, wordFacts, err = tx.SearchAgentGraph(agentId, words, limit)
		return err
	}); err != nil {
		log.Warningf("cannot search the graph of %q: %s", self.settings.Owner.Username, err)
	}
	// A turn goes on with whatever it has: the words found what they
	// found, and half a search is better than none in front of somebody
	// waiting. Rehearsal is the caller that cannot do this; see
	// canAnswerFromMemory. The question is embedded once a turn and put
	// to both stores.
	question := self.meaningOfQuestion(ctx, "recall", words)
	meaningNodes, meaningFacts, err := self.agent.nearestInGraphTo(ctx, self.settings.Agent.ID, question, limit)
	if err != nil {
		log.Warningf("cannot rank the graph of %q by meaning: %s", self.settings.Owner.Username, err)
	}
	sectionNodes, sections := self.pagesOfNearestSections(ctx, question, limit)

	nodes, facts := fuseNodes(limit, meaningNodes, wordNodes, sectionNodes), fuseFacts(limit, meaningFacts, wordFacts)
	if explanation := self.explanation; explanation != nil {
		explanation.explainSearch("pages by words", len(wordNodes))
		explanation.explainSearch("pages by meaning", len(meaningNodes))
		explanation.explainSearch("pages by overview section", len(sectionNodes))
		explanation.explainSearch("facts by words", len(wordFacts))
		explanation.explainSearch("facts by meaning", len(meaningFacts))
		explanation.explainPages(nodes, wordNodes, meaningNodes, sectionNodes)
		explanation.factsByWords, explanation.factsByMeaning = wordFacts, meaningFacts
	}
	return nodes, facts, sections
}

// pagesOfNearestSections is the pages whose overview sections are nearest
// the question, best first, and the best section of each by page id.
func (self *AskRun) pagesOfNearestSections(ctx context.Context, question *meaning, limit int) ([]*models.AgentNode, map[string]string) {
	sections := map[string]string{}
	if question == nil {
		return nil, sections
	}
	agentId := self.settings.Agent.ID
	sectionIds, err := self.agent.nearestOverviewSectionsTo(ctx, agentId, question, limit)
	if err != nil {
		log.Warningf("cannot rank the overview sections of %q by meaning: %s", self.settings.Owner.Username, err)
		return nil, sections
	}
	var order []string
	for _, sectionId := range sectionIds {
		nodeId := nodeOfOverviewSection(sectionId)
		if _, seen := sections[nodeId]; seen {
			continue
		}
		sections[nodeId] = sectionId
		order = append(order, nodeId)
	}
	var nodes []*models.AgentNode
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.GetAgentNodes(agentId, order)
		if err != nil {
			return err
		}
		byId := make(map[string]*models.AgentNode, len(found))
		for _, node := range found {
			byId[node.ID] = node
		}
		for _, nodeId := range order {
			if node := byId[nodeId]; node != nil && !node.Dormant {
				nodes = append(nodes, node)
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot read the pages of the nearest overview sections: %s", err)
		return nil, sections
	}
	return nodes, sections
}

// SearchGraphByMeaning is what the memory tool's search adds to its own
// word search. Part of tools.GraphSearching.
func (self *AskRun) SearchGraphByMeaning(ctx context.Context, words string, limit int) ([]*models.AgentNode, []*models.AgentFact) {
	nodes, facts, err := self.agent.nearestInGraphTo(ctx, self.settings.Agent.ID, self.meaningOfQuestion(ctx, "recall", words), limit)
	if err != nil {
		log.Warningf("cannot rank the graph of %q by meaning: %s", self.settings.Owner.Username, err)
	}
	return nodes, facts
}

// recalledBlock is one piece of the overlay: the text the next round
// sees, the page it came from, and the facts it carried.
//
// Choosing the blocks is kept apart from writing them so that what a turn
// would carry can be asked for without taking a turn, which is what
// `teanode agent memory evaluate` replays a question set through.
type recalledBlock struct {
	// NodeID is the page this block expands. Empty for the block of loose
	// facts, whose pages did not make the cut and so were not used.
	NodeID string

	// Path is the page the block expands; empty for the block of loose
	// facts, which are from many pages: see FactPaths.
	Path string

	// Text is what goes into the overlay.
	Text string

	// Summary is the page's opening as Text carries it, if it does.
	Summary string

	// Overview is the section of the page's overview Text carries, if it
	// carries one.
	Overview string

	// Facts are the facts the block carried, in the order it carried
	// them.
	Facts []*models.AgentFact

	// FactPaths is the page of each fact, by fact id, for the block of
	// loose facts, which are from many pages.
	FactPaths map[string]string
}

// stillStands says whether a fact the search found is one the page still
// says: not folded away behind another, not struck, and not superseded by
// a later statement of the same thing.
func stillStands(fact *models.AgentFact) bool {
	return fact != nil && !fact.Dormant && fact.SupersededBy == ""
}

// chooseRecalled picks what the overlay carries under the token budget:
// the top pages expanded, then the loose facts the words hit directly.
//
// A page is built, measured, and only then kept. It used to be counted as
// it was built, so a block that turned out not to fit still marked every
// fact in it as wanted: `used_at` moved on facts the model never saw,
// which feeds importance, decay and what the index carries tomorrow.
// Recall is supposed to record what the prompt carried, not what it
// considered.
//
// Which facts a page gives up is decided by the question and not by age;
// see factsToShow for what that cost before.
func (self *AskRun) chooseRecalled(tx db.Transaction, nodes []*models.AgentNode, facts []*models.AgentFact, sections map[string]string) ([]*recalledBlock, error) {
	agentId := self.settings.Agent.ID
	paths, err := pathsOfFacts(tx, agentId, facts)
	if err != nil {
		return nil, err
	}
	explanation := self.explanation
	if explanation != nil {
		explanation.explainFacts(facts, explanation.factsByWords, explanation.factsByMeaning, paths)
		explanation.TokenBudget = recallTokens
	}
	// Which of the search's facts sit on which page, in the order the
	// search put them. That order is the ranking, and nothing here ranks
	// it again.
	// What the search found, by the page it is on, in the order it
	// found it. A vector is not taken away when a fact is folded into
	// another, struck, or superseded by a later statement -- the row
	// stays searchable on purpose, so that "what did it used to say"
	// can be answered -- so the search hands back sentences the page no
	// longer says, and only this side knows to leave them out. Carrying
	// one inside a page block would put words in the page's mouth that
	// a person reading the page would not find there.
	hitOnPage := map[string][]*models.AgentFact{}
	for _, fact := range facts {
		if stillStands(fact) {
			hitOnPage[fact.NodeID] = append(hitOnPage[fact.NodeID], fact)
		}
	}
	// Held back for the facts below, but only where there are facts to
	// hold it for: a question the fact search answered with nothing
	// leaves the pages the whole of it, as they had before.
	reservedBlocks, reservedTokens := 0, 0
	if len(facts) > 0 {
		// One block: the loose facts go in together below.
		reservedBlocks, reservedTokens = 1, recallFactTokens
	}
	pageBlocks := recallGraphBlocks - reservedBlocks
	pageTokens := recallTokens - reservedTokens

	spent := 0
	shown := map[string]bool{}
	expanded := map[string]bool{}
	blocks := []*recalledBlock{}
	pages, unhitPages := 0, 0
	for _, node := range nodes {
		// The overlay's budget as well as the page count: a block the
		// overlay would drop is a block whose facts must not be marked
		// as wanted.
		if pages >= recallPages || len(blocks) >= pageBlocks {
			break
		}
		explained := explanation.page(node.ID)
		if explained != nil {
			explained.HitFactCount = len(hitOnPage[node.ID])
		}
		if expanded[node.ID] {
			if explained != nil {
				explained.RecallDecision = RecallDecisionAlreadyExpanded
			}
			continue
		}
		isHit := len(hitOnPage[node.ID]) > 0
		if !isHit && unhitPages >= recallPagesUnhit {
			if explained != nil {
				explained.RecallDecision = RecallDecisionUnhitPageLimit
			}
			continue
		}
		considered, err := tx.ListAgentFacts(agentId, node.ID, false, pageFactsConsidered)
		if err != nil {
			return nil, err
		}
		// A theme's facts are the night's reflections on it, and what
		// it carries is the ones that stand now rather than the one or
		// two the words happened to hit: they are what the theme adds
		// over its members, which are pages of their own.
		isTheme := models.IsThemePath(node.Path) || node.Path == models.PathReflections
		pageFactsFound := factsToShow(considered, hitOnPage[node.ID])
		if isTheme {
			if reflections := reflectionsToShow(considered, hitOnPage[node.ID]); len(reflections) > 0 {
				pageFactsFound = reflections
			}
		}
		text := node.Path
		if node.Name != "" {
			text += " — " + node.Name
		}
		// Indexed is not expanded. A page the prompt's own index
		// names is carried there as a line about what the page is,
		// which is not what it knows -- so the facts go in all the
		// same when the turn's words hit the page, and only the
		// opening, which the index line already has the gist of, is
		// left out.
		summary := ""
		if opening := strings.TrimSpace(node.Summary); opening != "" && !self.inPrompt(node.ID) {
			summary = cutRunes(opening, 600)
			text += "\n  " + summary
		}
		factLines := ""
		for _, fact := range pageFactsFound {
			line := fact.Line()
			if isTheme {
				line = cutRunes(line, recallReflectionLength)
			}
			factLines += "\n  #" + strconv.Itoa(fact.Number) + " " + line
		}
		// A section of the overview, which says in a paragraph what no
		// single fact says: the one the question matched, else the first,
		// which says what the thing is and how it works. Only where the
		// page still fits with it; a page that does not is carried
		// without it before it is passed over.
		overview, sectionChoice := chooseOverviewSection(node, sections[node.ID], self.settings.Message, recallOverviewLength)
		cost := 0
		if overview != "" {
			cost = llm.EstimateTokens(text + "\n  " + overview + factLines)
			if spent+cost > pageTokens {
				overview, sectionChoice = "", RecallSectionLeftOutForBudget
			}
		}
		if overview != "" {
			text += "\n  " + overview + factLines
		} else {
			text += factLines
			cost = llm.EstimateTokens(text)
		}
		if spent+cost > pageTokens {
			if explained != nil {
				explained.RecallDecision = RecallDecisionTokenBudget
			}
			// A smaller page further down may still fit, so this one
			// is passed over rather than ending the loop -- but once
			// what is left could not hold a page at all there is no
			// sense reading the rest of them out of the store.
			if pageTokens-spent < recallTokens/8 {
				break
			}
			continue
		}
		spent += cost
		if explained != nil {
			explained.RecallDecision = RecallDecisionCarried
			explained.SectionChoice = sectionChoice
			if overview != "" {
				heading, _, _ := strings.Cut(strings.TrimPrefix(overview, "## "), "\n")
				explained.OverviewSectionHeading = heading
			}
			explained.CarriedFactCount = len(pageFactsFound)
			explained.TokenCount = cost
		}
		blocks = append(blocks, &recalledBlock{NodeID: node.ID, Path: node.Path, Text: text, Summary: summary, Overview: overview, Facts: pageFactsFound})
		for _, fact := range pageFactsFound {
			shown[fact.ID] = true
		}
		expanded[node.ID] = true
		pages++
		if !isHit {
			unhitPages++
		}
	}
	// Then the loose facts: ones whose page did not make the cut but
	// which the turn's words hit directly. Together, as one block: one
	// block each spent the overlay's lines three facts in, and the rest
	// of what the search found -- often the answer -- never went in.
	loose := &recalledBlock{FactPaths: map[string]string{}}
	var looseLines []string
	for _, fact := range facts {
		if len(loose.Facts) >= recallFacts || len(blocks) >= recallGraphBlocks {
			break
		}
		explainedFact := explanation.fact(fact.Reference(paths[fact.NodeID]))
		if shown[fact.ID] || !stillStands(fact) {
			if explainedFact != nil && !stillStands(fact) {
				explainedFact.RecallDecision = RecallDecisionRetired
			}
			continue
		}
		line := fact.Reference(paths[fact.NodeID]) + " " + fact.Line()
		cost := llm.EstimateTokens(line)
		if spent+cost > recallTokens {
			if explainedFact != nil {
				explainedFact.RecallDecision = RecallDecisionTokenBudget
			}
			// Passed over, not the end of the loop, for the reason the
			// pages above are: one long sentence ended the whole of
			// this and took every shorter fact behind it with it, and
			// the facts here are in the order the search ranked them,
			// so what was lost was the best of what it found.
			continue
		}
		spent += cost
		if explainedFact != nil {
			explainedFact.RecallDecision = RecallDecisionCarried
		}
		looseLines = append(looseLines, line)
		loose.Facts = append(loose.Facts, fact)
		loose.FactPaths[fact.ID] = paths[fact.NodeID]
	}
	if len(loose.Facts) > 0 {
		loose.Text = strings.Join(looseLines, "\n")
		blocks = append(blocks, loose)
	}
	if explanation != nil {
		explanation.TokensSpent = spent
		// A fact carried in its page's block, whether or not the loose
		// facts reached it.
		for _, fact := range facts {
			if explained := explanation.fact(fact.Reference(paths[fact.NodeID])); explained != nil && shown[fact.ID] {
				explained.RecallDecision = RecallDecisionShownOnPage
			}
		}
	}
	return blocks, nil
}

// factsToShow picks which of a page's facts the overlay shows: the ones
// the question hit, in the order the search ranked them, up to pageFacts,
// and then the page's first facts where fewer than pageFactsLeast were
// hit. The chosen are laid out by number, because selection is about relevance
// and presentation is about reading as a page -- a block whose `#n`
// references jump about is one the model cites back crookedly.
//
// It used to be the first pageFacts by number, since that is all that
// was read. Harmless while a page held a handful of sentences; on a page
// of eighty the one the search had matched was almost never among the
// oldest five, and the loose-fact loop that carries the matches
// afterwards had spent its blocks and its tokens on those same pages. So
// the page arrived in the prompt and the sentence that answered the
// question did not.
func factsToShow(considered, hit []*models.AgentFact) []*models.AgentFact {
	stored := make(map[string]*models.AgentFact, len(considered))
	for _, fact := range considered {
		stored[fact.ID] = fact
	}
	chosen := make([]*models.AgentFact, 0, pageFacts)
	taken := make(map[string]bool, pageFacts)
	keep := func(fact *models.AgentFact) {
		taken[fact.ID] = true
		chosen = append(chosen, fact)
	}
	for _, fact := range hit {
		if len(chosen) >= pageFacts {
			break
		}
		if taken[fact.ID] {
			continue
		}
		// A page long enough to run past the window read above must
		// still give up the sentence that was matched, so the search's
		// own copy of it stands in where the store's was not read.
		if found := stored[fact.ID]; found != nil {
			keep(found)
		} else {
			keep(fact)
		}
	}
	for _, fact := range considered {
		if len(chosen) >= pageFactsLeast {
			break
		}
		if !taken[fact.ID] {
			keep(fact)
		}
	}
	sort.SliceStable(chosen, func(first, second int) bool {
		return chosen[first].Number < chosen[second].Number
	})
	return chosen
}

// reflectionsToShow is the reflections a theme carries: the ones the
// question hit first, then the rest, up to recallReflections, laid out
// by number. Superseded ones were never read, so these are the ones
// that stand.
func reflectionsToShow(considered, hit []*models.AgentFact) []*models.AgentFact {
	isReflection := map[string]bool{}
	var reflections []*models.AgentFact
	for _, fact := range considered {
		if fact.Kind == models.FactReflection {
			isReflection[fact.ID] = true
			reflections = append(reflections, fact)
		}
	}
	chosen := make([]*models.AgentFact, 0, recallReflections)
	taken := map[string]bool{}
	for _, group := range [][]*models.AgentFact{hit, reflections} {
		for _, fact := range group {
			if len(chosen) >= recallReflections {
				break
			}
			if isReflection[fact.ID] && !taken[fact.ID] {
				taken[fact.ID] = true
				chosen = append(chosen, fact)
			}
		}
	}
	sort.SliceStable(chosen, func(first, second int) bool {
		return chosen[first].Number < chosen[second].Number
	})
	return chosen
}

// writeRecalled expands what was found into the overlay the next round
// sees, and marks what it carried as used.
func (self *AskRun) writeRecalled(ctx context.Context, nodes []*models.AgentNode, facts []*models.AgentFact, sections map[string]string) {
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		blocks, err := self.chooseRecalled(tx, nodes, facts, sections)
		if err != nil {
			return err
		}
		var usedNodes, usedFacts []string
		for _, block := range blocks {
			self.Recall(block.Text)
			if block.NodeID != "" {
				usedNodes = append(usedNodes, block.NodeID)
			}
			for _, fact := range block.Facts {
				usedFacts = append(usedFacts, fact.ID)
			}
		}
		now := time.Now()
		if err := tx.TouchAgentNodes(usedNodes, now); err != nil {
			return err
		}
		return tx.TouchAgentFacts(usedFacts, now)
	}); err != nil {
		log.Warningf("cannot recall for %q: %s", self.settings.Owner.Username, err)
	}
}

// RecalledPage is one page recall would carry and the facts it would
// carry from it.
type RecalledPage struct {
	Path string

	// Summary is the page's opening as the overlay carried it, or empty
	// where it carried none: a page the prompt's index already names.
	Summary string

	// Overview is the section of the page's overview the overlay
	// carried, or empty where it carried none.
	Overview string

	Facts []*models.AgentFact
}

// RecallForQuestion answers what the graph would put in front of the
// model for a question, without asking it anything.
//
// The same two steps a turn takes -- the fused search, then the choice of
// blocks under the token budget -- so that what this reports is what a
// turn would carry and not a second implementation of it that drifts.
// What it deliberately leaves out is the turn's bookkeeping: no `used_at`
// is moved and no vectors are backfilled, because an evaluation that
// changed importance and decay as it ran would be measuring its own last
// pass.
func (self *Agent) RecallForQuestion(ctx context.Context, found *models.Agent, owner *models.User, question string) ([]*RecalledPage, error) {
	return self.recallForQuestion(ctx, found, owner, question, nil)
}

// recallForQuestion is RecallForQuestion, recording why into the
// explanation where one is given.
func (self *Agent) recallForQuestion(ctx context.Context, found *models.Agent, owner *models.User, question string, explanation *RecallExplanation) ([]*RecalledPage, error) {
	if self == nil || found == nil || owner == nil {
		return nil, ErrUnavailable
	}
	words := strings.TrimSpace(question)
	if words == "" {
		return []*RecalledPage{}, nil
	}
	run := &AskRun{
		agent:          self,
		settings:       &AskSettings{Agent: found, Owner: owner, Message: words},
		promptMemories: map[string]bool{},
		explanation:    explanation,
	}
	run.ctx = ctx
	// The index a turn would have carried, built and thrown away.
	//
	// Only the record of which pages went into it is wanted: recall skips
	// a page the prompt already holds and spends the room on its facts
	// instead. Without this the index is empty, every page recalled here
	// carries its opening as well, and less of the budget is left for the
	// facts -- so a question graded this way is graded against less than a
	// turn would have had. The answer is not wrong, it is pessimistic, and
	// a measurement that is quietly pessimistic is worse than one that is
	// wrong in a way somebody notices.
	//
	// It costs what the index costs, which is a read of the top pages, and
	// it is the price of the number meaning what it says.
	_ = run.carryIndex(ctx, indexTokens)
	nodes, facts, sections := run.searchGraph(ctx, words, recallCandidates)
	var blocks []*recalledBlock
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		blocks, err = run.chooseRecalled(tx, nodes, facts, sections)
		return err
	}); err != nil {
		return nil, err
	}
	// A loose fact can sit on a page that was expanded too, when it was
	// not among the five facts the page showed, so the blocks are
	// gathered by path rather than listed one for one.
	pages := []*RecalledPage{}
	byPath := map[string]*RecalledPage{}
	pageOf := func(path string) *RecalledPage {
		page := byPath[path]
		if page == nil {
			page = &RecalledPage{Path: path, Facts: []*models.AgentFact{}}
			byPath[path] = page
			pages = append(pages, page)
		}
		return page
	}
	for _, block := range blocks {
		if block.NodeID == "" {
			for _, fact := range block.Facts {
				page := pageOf(block.FactPaths[fact.ID])
				page.Facts = append(page.Facts, fact)
			}
			continue
		}
		page := pageOf(block.Path)
		page.Summary = block.Summary
		page.Overview = block.Overview
		page.Facts = append(page.Facts, block.Facts...)
	}
	return pages, nil
}

// inPrompt says whether a page is already in the prompt's own index,
// which the turn's recall does not repeat.
func (self *AskRun) inPrompt(nodeId string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.promptMemories[nodeId]
}

// exemplarsFor is the person's own past messages nearest in meaning to
// the one being answered: how they actually write about something like
// this.
//
// Their own words rather than a description of them. A voice described in
// fields says "friendly, brief"; five of their own replies say how they
// open, how much they explain, and whether they sign off at all.
//
// Empty where they have not pointed the agent at their sent mail, which
// is the common case and costs nothing.
func (self *Agent) exemplarsFor(ctx context.Context, agent *models.Agent, message *MessageContext, most int) []string {
	if message == nil || !FeatureAllowed(self.settings.Configuration(), "knowledge") {
		return nil
	}
	var sourceIds []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		sources, err := tx.ListAgentSources(agent.ID)
		if err != nil {
			return err
		}
		for _, source := range sources {
			if source.Kind == models.SourceSent && source.Enabled {
				sourceIds = append(sourceIds, source.ID)
			}
		}
		return nil
	}); err != nil || len(sourceIds) == 0 {
		return nil
	}

	words := message.Subject + "\n" + cutRunes(message.Text, 1500)
	vectors, modelName, ok := self.embed(ctx, agent.ID, "reply", []string{words})
	if !ok {
		return nil
	}
	var chunks []*models.AgentChunk
	var documents map[string]*models.AgentDocument
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		scores, err := tx.Nearest(db.AgentChunkTable, agent.ID, modelName, vectors[0], most, db.VectorQuery{
			Where:     []string{`"source_id" = ANY(?)`},
			Arguments: []any{pq.Array(sourceIds)},
			Floor:     meaningFloorGraph,
		})
		if err != nil {
			return err
		}
		found, err := tx.GetAgentChunks(agent.ID, idsOf(scores))
		if err != nil {
			return err
		}
		chunks = orderChunks(found, idsOf(scores))
		ids := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			ids = append(ids, chunk.DocumentID)
		}
		list, err := tx.GetAgentDocuments(agent.ID, ids)
		if err != nil {
			return err
		}
		documents = map[string]*models.AgentDocument{}
		for _, document := range list {
			documents[document.ID] = document
		}
		return nil
	}); err != nil {
		log.Debugf("cannot find how they have written about this: %s", err)
		return nil
	}
	var exemplars []string
	for _, chunk := range chunks {
		document := documents[chunk.DocumentID]
		if document == nil {
			continue
		}
		when := ""
		if document.HappenedAt != nil {
			when = document.HappenedAt.Format("Jan 2006") + ": "
		}
		exemplars = append(exemplars, when+cutRunes(strings.TrimSpace(chunk.Text), 1200))
	}
	return exemplars
}
