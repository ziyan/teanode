package agent

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The graph as the loop uses it: what a prompt carries, what a turn's own
// words bring back, and how a page or a fact is given its meaning.
//
// What replaced what. The agent used to keep a flat list of memories and
// fold the top twenty into every prompt. Measured on a real server after
// five days and four hundred and fifty turns, it held two — the model was
// asked to file what it learned and, busy doing the person's actual work,
// did not. So writing moved out of the turn (see remember.go) and reading
// moved into it: the prompt carries an index of pages, and before the
// first round the turn's own words fetch whatever they touch.

// The bounds.
const (
	// indexTokens is how much of a prompt the index may take, and
	// recallTokens how much the overlay after the history may. Both are
	// estimates from the same counter the compactor uses.
	indexTokens  = 1500
	recallTokens = 1200

	// runIndexTokens is what a run with nobody present carries. Larger,
	// because such a run cannot ask for more.
	runIndexTokens = 1000

	// selfSummary is how much of the self page always goes in. It is the
	// one page worth its tokens on every turn: who the person is.
	selfSummary = 2000

	// recallCandidates is how many rows each of the four searches offers
	// the fusion below.
	recallCandidates = 20

	// recallPages is how many pages the overlay expands, recallFacts how
	// many loose facts it carries beside them, and pageFacts how many
	// facts of an expanded page are shown.
	recallPages = 5
	recallFacts = 10
	pageFacts   = 5

	// meaningFloorGraph is the least similarity worth calling a match.
	// Embedding models put unrelated text between a tenth and three
	// tenths apart; a quarter keeps out noise without losing a paraphrase.
	meaningFloorGraph = 0.25

	// twinFloor is how near two facts on one page must be for one to be
	// called the other said twice.
	twinFloor = 0.92

	// graphEmbedCharacters is how much of a page or a fact is embedded.
	graphEmbedCharacters = 4000

	// graphBackfill is how many rows without a vector are given one on a
	// turn, so an agent that has been learning for months catches up over
	// a few conversations rather than in one long pause.
	graphBackfill = 20

	// recallChunks is how many passages of the person's own files and
	// chat a turn brings back without being asked, recallChunkCharacters
	// how much of each, and recallKnowledgeTokens the whole budget for
	// them. Small: the point is to answer a question about their own work
	// from their own work, not to fill the prompt with a codebase.
	recallChunks          = 3
	recallChunkCharacters = 600
	recallKnowledgeTokens = 700

	// reciprocalRankConstant is the k of reciprocal rank fusion. Sixty is
	// the number the method was published with and needs no tuning: it is
	// what stops the first row of a short list from outweighing the whole
	// of a long one.
	reciprocalRankConstant = 60
)

// --- what a prompt carries --------------------------------------------

// graphIndex is the index a prompt carries: the self page in full, then
// as much of the rest of the graph as fits.
//
// Ordered by importance, which the nightly run recomputes and nothing
// else touches. That is deliberate. The list it replaced was ordered by
// when each memory was last used and every prompt marked what it carried,
// so the top of the prompt -- the part a provider can cache -- changed on
// every single turn and was never once a cache hit.
func (self *Agent) graphIndex(ctx context.Context, agent *models.Agent, owner *models.User, budget int) ([]string, []string) {
	var lines []string
	var carried []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.EnsureAgentRoots(agent.ID); err != nil {
			return err
		}
		nodes, err := tx.ListAgentIndex(agent.ID, 400)
		if err != nil {
			return err
		}
		spent := 0
		for _, node := range nodes {
			// The self page is not a line in the index; it is the block
			// above it, written by selfLines.
			if node.Path == models.PathSelf {
				continue
			}
			// A folder with nothing under it says nothing. The roots are
			// made for every agent whether or not anything is filed in
			// them, and seven empty headings at the top of every prompt
			// teach the model that the graph is empty.
			if node.Kind == models.NodeFolder && node.Summary == "" && node.ParentID == "" {
				continue
			}
			line := node.IndexLine(140)
			cost := llm.EstimateTokens(line)
			if spent+cost > budget {
				break
			}
			spent += cost
			lines = append(lines, line)
			carried = append(carried, node.ID)
		}
		return nil
	}); err != nil {
		log.Warningf("cannot read the graph of %q: %s", owner.Username, err)
	}
	return lines, carried
}

// carryIndex is the index for a turn, remembering which pages went in so
// that the recall after it does not send the same page twice.
func (self *AskRun) carryIndex(ctx context.Context, budget int) []string {
	lines, carried := self.agent.graphIndex(ctx, self.settings.Agent, self.settings.Owner, budget)
	self.mutex.Lock()
	if self.promptMemories == nil {
		self.promptMemories = map[string]bool{}
	}
	for _, id := range carried {
		self.promptMemories[id] = true
	}
	self.mutex.Unlock()
	return lines
}

// selfLines is the self page: the card that is the person, then whatever
// the page itself says. Always first, and always in full.
//
// The card is read rather than copied. A person who changes their
// telephone number changes it in one place, and the agent is right about
// it on the next turn.
func (self *Agent) selfLines(ctx context.Context, agent *models.Agent, owner *models.User) []string {
	var lines []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if owner.ContactID != "" {
			card, err := self.contactOf(tx, owner)
			if err != nil {
				return err
			}
			if card != nil {
				lines = append(lines, cardLines(card)...)
			}
		}
		node, err := tx.GetAgentNode(agent.ID, models.PathSelf)
		if err != nil || node == nil {
			return err
		}
		if summary := strings.TrimSpace(node.Summary); summary != "" {
			lines = append(lines, cutRunes(summary, selfSummary))
		}
		facts, err := tx.ListAgentFactsLively(agent.ID, node.ID, 20)
		if err != nil {
			return err
		}
		byNumber(facts)
		for _, fact := range facts {
			lines = append(lines, "- "+fact.Line())
		}
		return nil
	}); err != nil {
		log.Warningf("cannot read the self page of %q: %s", owner.Username, err)
	}
	if len(lines) == 0 && owner.ContactID == "" {
		// Nothing is known and no card is chosen. Say so, with what to do
		// about it, rather than leaving the section out: a model that is
		// never told the page is empty answers as though it had read one.
		lines = append(lines, "No contact is marked as being them, so their own addresses are not known. If it comes up, offer to set one on the Contacts page.")
	}
	return lines
}

// contactOf is the address book entry that is the person themselves.
func (self *Agent) contactOf(tx db.Transaction, owner *models.User) (*models.Contact, error) {
	books, err := tx.ListAddressBooks(owner.ID)
	if err != nil {
		return nil, err
	}
	for _, book := range books {
		contact, err := tx.GetContact(book.ID, owner.ContactID)
		if err != nil {
			return nil, err
		}
		if contact != nil {
			return contact, nil
		}
	}
	return nil, nil
}

// cardLines is the person's own card as prompt lines: what the agent
// needs to know it is them, and to tell their mail and their commits from
// somebody else's.
func cardLines(contact *models.Contact) []string {
	var lines []string
	if name := strings.TrimSpace(contact.Name); name != "" {
		lines = append(lines, name)
	}
	if organization := strings.TrimSpace(contact.Organization); organization != "" {
		lines = append(lines, "Works at "+organization+".")
	}
	if len(contact.Emails) > 0 {
		lines = append(lines, "Their own addresses: "+strings.Join(contact.Emails, ", ")+".")
	}
	if len(contact.Phones) > 0 {
		lines = append(lines, "Telephone: "+strings.Join(contact.Phones, ", ")+".")
	}
	if extra := contacts.NotableFields(contact); extra != "" {
		lines = append(lines, extra)
	}
	return lines
}

// memoryLines is what an unattended run of a kind is shown: the index,
// then the facts addressed to that run. Named as it was when a memory was
// a memory, because six callers say it and none of them care.
func memoryLines(tx db.Transaction, agentId string, audience models.AgentAudience, limit int, withIds bool) ([]string, error) {
	if err := tx.EnsureAgentRoots(agentId); err != nil {
		return nil, err
	}
	var lines []string
	nodes, err := tx.ListAgentIndex(agentId, 200)
	if err != nil {
		return nil, err
	}
	spent := 0
	var nodeIds []string
	for _, node := range nodes {
		if node.Kind == models.NodeFolder && node.Summary == "" && node.ParentID == "" {
			continue
		}
		line := node.IndexLine(140)
		cost := llm.EstimateTokens(line)
		if spent+cost > runIndexTokens {
			break
		}
		spent += cost
		lines = append(lines, line)
		nodeIds = append(nodeIds, node.ID)
	}
	facts, err := tx.ListAgentFactsForAudience(agentId, audience, limit)
	if err != nil {
		return nil, err
	}
	paths, err := pathsOfFacts(tx, agentId, facts)
	if err != nil {
		return nil, err
	}
	factIds := make([]string, 0, len(facts))
	for _, fact := range facts {
		line := fact.Line()
		if withIds {
			line += " (" + fact.Reference(paths[fact.NodeID]) + ")"
		}
		lines = append(lines, line)
		factIds = append(factIds, fact.ID)
	}
	if err := tx.TouchAgentNodes(nodeIds, time.Now()); err != nil {
		return nil, err
	}
	if err := tx.TouchAgentFacts(factIds, time.Now()); err != nil {
		return nil, err
	}
	return lines, nil
}

// pathsOfFacts is the path of the page each fact sits on.
func pathsOfFacts(tx db.Transaction, agentId string, facts []*models.AgentFact) (map[string]string, error) {
	if len(facts) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.NodeID)
	}
	nodes, err := tx.GetAgentNodes(agentId, ids)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]string, len(nodes))
	for _, node := range nodes {
		paths[node.ID] = node.Path
	}
	return paths, nil
}

// correctionLines is what the person corrected, newest first.
func correctionLines(tx db.Transaction, agentId string, kinds []models.AgentFeedbackKind) ([]string, error) {
	feedback, err := tx.ListAgentFeedback(agentId, kinds, promptCorrections)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(feedback))
	for _, entry := range feedback {
		lines = append(lines, entry.Said)
	}
	return lines, nil
}

// promptCorrections is how many corrections a run is shown, and
// promptRunMemories how many facts addressed to it a run with nobody
// present carries. A run cannot ask for more, so it is shown more.
const (
	promptCorrections = 20
	promptRunMemories = 30
)

// --- recall -----------------------------------------------------------

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
// It costs one embedding call and no round trip to the model, which is
// why it happens every turn rather than being asked for.
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
	// Anything written before there was an embedding model, or before
	// this one, catches up a few at a time.
	if _, err := self.agent.EmbedGraph(ctx, self.settings.Agent, graphBackfill); err != nil {
		log.Warningf("cannot give the graph of %q its vectors: %s", self.settings.Owner.Username, err)
	}
	words := strings.TrimSpace(self.settings.Message)
	if words == "" {
		return
	}
	// What the person pointed at is part of what this turn is about.
	for _, reference := range self.settings.References {
		words += "\n" + reference.Subject
	}

	nodes, facts := self.searchGraph(ctx, words, recallCandidates)
	self.writeRecalled(ctx, nodes, facts)
	self.recallFromKnowledge(ctx, words)
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

// fuseChunks ranks what two searches found by reciprocal rank.
func fuseChunks(limit int, lists ...[]*models.AgentChunk) []*models.AgentChunk {
	scores := map[string]float64{}
	byId := map[string]*models.AgentChunk{}
	var order []string
	for _, list := range lists {
		for position, chunk := range list {
			if _, seen := byId[chunk.ID]; !seen {
				order = append(order, chunk.ID)
			}
			scores[chunk.ID] += 1 / float64(reciprocalRankConstant+position+1)
			byId[chunk.ID] = chunk
		}
	}
	sort.SliceStable(order, func(left, right int) bool { return scores[order[left]] > scores[order[right]] })
	ranked := make([]*models.AgentChunk, 0, limit)
	for _, id := range order {
		if len(ranked) >= limit {
			break
		}
		ranked = append(ranked, byId[id])
	}
	return ranked
}

// searchGraph is the fused search the turn and the tool both use.
func (self *AskRun) searchGraph(ctx context.Context, words string, limit int) ([]*models.AgentNode, []*models.AgentFact) {
	agentId := self.settings.Agent.ID

	var wordNodes []*models.AgentNode
	var wordFacts []*models.AgentFact
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		wordNodes, wordFacts, err = tx.SearchAgentGraph(agentId, words, limit)
		return err
	}); err != nil {
		log.Warningf("cannot search the graph of %q: %s", self.settings.Owner.Username, err)
	}
	meaningNodes, meaningFacts := self.agent.nearestInGraph(ctx, self.settings.Agent, words, limit)

	return fuseNodes(limit, meaningNodes, wordNodes), fuseFacts(limit, meaningFacts, wordFacts)
}

// SearchGraphByMeaning is what the memory tool's search adds to its own
// word search. Part of tools.GraphSearching.
func (self *AskRun) SearchGraphByMeaning(ctx context.Context, words string, limit int) ([]*models.AgentNode, []*models.AgentFact) {
	return self.agent.nearestInGraph(ctx, self.settings.Agent, words, limit)
}

// writeRecalled expands what was found into the overlay the next round
// sees, under a token budget, skipping whatever the index already carries.
func (self *AskRun) writeRecalled(ctx context.Context, nodes []*models.AgentNode, facts []*models.AgentFact) {
	agentId := self.settings.Agent.ID
	spent := 0
	shown := map[string]bool{}
	var usedNodes, usedFacts []string

	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		paths, err := pathsOfFacts(tx, agentId, facts)
		if err != nil {
			return err
		}
		pages := 0
		for _, node := range nodes {
			if pages >= recallPages || self.inPrompt(node.ID) {
				continue
			}
			pageFactsFound, err := tx.ListAgentFacts(agentId, node.ID, false, pageFacts)
			if err != nil {
				return err
			}
			block := node.Path
			if node.Name != "" {
				block += " — " + node.Name
			}
			if summary := strings.TrimSpace(node.Summary); summary != "" {
				block += "\n  " + cutRunes(summary, 600)
			}
			for _, fact := range pageFactsFound {
				block += "\n  #" + strconv.Itoa(fact.Number) + " " + fact.Line()
				shown[fact.ID] = true
				usedFacts = append(usedFacts, fact.ID)
			}
			cost := llm.EstimateTokens(block)
			if spent+cost > recallTokens {
				break
			}
			spent += cost
			self.Recall(block)
			usedNodes = append(usedNodes, node.ID)
			pages++
		}
		// Then the loose facts: ones whose page did not make the cut but
		// which the turn's words hit directly.
		kept := 0
		for _, fact := range facts {
			if kept >= recallFacts || shown[fact.ID] {
				continue
			}
			line := fact.Reference(paths[fact.NodeID]) + " " + fact.Line()
			cost := llm.EstimateTokens(line)
			if spent+cost > recallTokens {
				break
			}
			spent += cost
			self.Recall(line)
			usedFacts = append(usedFacts, fact.ID)
			kept++
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

// fuseNodes and fuseFacts rank what several searches found by reciprocal
// rank: a row's score is the sum of 1/(k+position) over every list it
// appears in. A row that two searches both found beats one that only the
// best search found in first place, which is the property wanted: the
// words and the meaning agreeing is the strongest evidence there is.
func fuseNodes(limit int, lists ...[]*models.AgentNode) []*models.AgentNode {
	now := time.Now()
	scores := map[string]float64{}
	byId := map[string]*models.AgentNode{}
	for _, list := range lists {
		for position, node := range list {
			scores[node.ID] += 1 / float64(reciprocalRankConstant+position+1)
			byId[node.ID] = node
		}
	}
	for id, node := range byId {
		scores[id] *= decayOfNode(node, now)
	}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(left, right int) bool {
		if scores[ids[left]] != scores[ids[right]] {
			return scores[ids[left]] > scores[ids[right]]
		}
		return ids[left] < ids[right]
	})
	ranked := make([]*models.AgentNode, 0, limit)
	for _, id := range ids {
		if len(ranked) >= limit {
			break
		}
		ranked = append(ranked, byId[id])
	}
	return ranked
}

// fuseFacts ranks by reciprocal rank and then by age.
//
// The decay is a multiplier on the fused score rather than a filter: a
// five-year-old fact that answers the question is still the answer, and
// what is wanted is that where somebody works now comes before where they
// worked in 2019 when both match. A filter cannot do that, because both
// are true.
func fuseFacts(limit int, lists ...[]*models.AgentFact) []*models.AgentFact {
	now := time.Now()
	scores := map[string]float64{}
	byId := map[string]*models.AgentFact{}
	for _, list := range lists {
		for position, fact := range list {
			scores[fact.ID] += 1 / float64(reciprocalRankConstant+position+1)
			byId[fact.ID] = fact
		}
	}
	for id, fact := range byId {
		scores[id] *= decayOfFact(fact, now)
	}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(left, right int) bool {
		if scores[ids[left]] != scores[ids[right]] {
			return scores[ids[left]] > scores[ids[right]]
		}
		return ids[left] < ids[right]
	})
	ranked := make([]*models.AgentFact, 0, limit)
	for _, id := range ids {
		if len(ranked) >= limit {
			break
		}
		ranked = append(ranked, byId[id])
	}
	return ranked
}

// inPrompt says whether a page is already in the prompt's own index,
// which the turn's recall does not repeat.
func (self *AskRun) inPrompt(nodeId string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.promptMemories[nodeId]
}

// --- meaning ----------------------------------------------------------

// embedderFor is the embedding model as this deployment has it, the width
// to ask for, and whether there is one at all.
//
// The name carries the width where one was asked for, because two widths
// of one model are two spaces: a vector written at 512 must never be
// ranked against one the same model wrote at 1536.
func (self *Agent) embedderFor() (embedder llm.Embedder, model, modelName string, dimensions int, ok bool) {
	configuration := self.settings.Configuration()
	if self.settings.Registry == nil || configuration.Agent.Models.Embedding == "" {
		return nil, "", "", 0, false
	}
	found, name, err := self.settings.Registry.Embedding()
	if err != nil {
		log.Warningf("no embedding model for the graph: %s", err)
		return nil, "", "", 0, false
	}
	modelName = configuration.Agent.Models.Embedding
	dimensions = configuration.Agent.Models.EmbeddingDimensions
	if dimensions > 0 {
		modelName += "@" + strconv.Itoa(dimensions)
	}
	return found, name, modelName, dimensions, true
}

// embed is one call to the embedding model, at the configured width.
func (self *Agent) embed(ctx context.Context, agentId, kind string, texts []string) ([][]float32, string, bool) {
	embedder, model, modelName, dimensions, ok := self.embedderFor()
	if !ok || len(texts) == 0 {
		return nil, "", false
	}
	configuration := self.settings.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	vectors, usage, err := embedder.Embed(callContext, llm.EmbedRequest{
		Model: model, Inputs: texts, Dimensions: dimensions,
	})
	RecordUsage(self.settings.Database, agentId, "", modelName, kind, usage)
	if err != nil {
		log.Warningf("cannot embed: %s", err)
		return nil, modelName, false
	}
	return vectors, modelName, len(vectors) > 0
}

// nearestInGraph is the pages and facts nearest in meaning to some words.
func (self *Agent) nearestInGraph(ctx context.Context, agent *models.Agent, words string, limit int) ([]*models.AgentNode, []*models.AgentFact) {
	vectors, modelName, ok := self.embed(ctx, agent.ID, "recall", []string{cutRunes(words, graphEmbedCharacters)})
	if !ok {
		return nil, nil
	}
	query := vectors[0]
	var nodes []*models.AgentNode
	var facts []*models.AgentFact
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		nodeScores, err := tx.Nearest(db.AgentNodeTable, agent.ID, modelName, query, limit, db.VectorQuery{Floor: meaningFloorGraph})
		if err != nil {
			return err
		}
		factScores, err := tx.Nearest(db.AgentFactTable, agent.ID, modelName, query, limit, db.VectorQuery{Floor: meaningFloorGraph})
		if err != nil {
			return err
		}
		if nodes, err = tx.GetAgentNodes(agent.ID, idsOf(nodeScores)); err != nil {
			return err
		}
		facts, err = tx.GetAgentFacts(agent.ID, idsOf(factScores))
		if err != nil {
			return err
		}
		// The reads come back in whatever order the table gave them; the
		// scores are the ranking.
		nodes = orderNodes(nodes, idsOf(nodeScores))
		facts = orderFacts(facts, idsOf(factScores))
		return nil
	}); err != nil {
		log.Warningf("cannot rank the graph by meaning: %s", err)
		return nil, nil
	}
	return nodes, facts
}

func idsOf(scores []db.Scored) []string {
	ids := make([]string, 0, len(scores))
	for _, score := range scores {
		ids = append(ids, score.ID)
	}
	return ids
}

func orderNodes(nodes []*models.AgentNode, ids []string) []*models.AgentNode {
	byId := make(map[string]*models.AgentNode, len(nodes))
	for _, node := range nodes {
		byId[node.ID] = node
	}
	ordered := make([]*models.AgentNode, 0, len(ids))
	for _, id := range ids {
		if node := byId[id]; node != nil {
			ordered = append(ordered, node)
		}
	}
	return ordered
}

func orderFacts(facts []*models.AgentFact, ids []string) []*models.AgentFact {
	byId := make(map[string]*models.AgentFact, len(facts))
	for _, fact := range facts {
		byId[fact.ID] = fact
	}
	ordered := make([]*models.AgentFact, 0, len(ids))
	for _, id := range ids {
		if fact := byId[id]; fact != nil {
			ordered = append(ordered, fact)
		}
	}
	return ordered
}

// factText is what is embedded of a fact: the sentence and the page it is
// on, so that "moved to the fleet team" carries whose move it was.
func factText(fact *models.AgentFact, path, name string) string {
	text := strings.TrimSpace(fact.Text)
	if name != "" {
		text = name + ": " + text
	} else if path != "" {
		text = path + ": " + text
	}
	return cutRunes(text, graphEmbedCharacters)
}

// nodeText is what is embedded of a page.
func nodeText(node *models.AgentNode) string {
	text := strings.TrimSpace(node.Name + "\n" + node.Summary)
	if len(node.Aliases) > 0 {
		text += "\n" + strings.Join(node.Aliases, " ")
	}
	if strings.TrimSpace(text) == "" {
		text = node.Path
	}
	return cutRunes(text, graphEmbedCharacters)
}

// NoteFact gives a fact its vector as it is written and answers whatever
// on the same page is near enough to be the same thing said twice.
//
// Within one page, and with a second test beside the cosine: the two must
// share a proper noun or a number where either has one. Two short
// sentences about two different people sit above nine tenths of each
// other, and a merge on that evidence alone loses one of them.
func (self *AskRun) NoteFact(ctx context.Context, fact *models.AgentFact) []*models.AgentFact {
	if fact == nil {
		return nil
	}
	agentId := self.settings.Agent.ID
	var path, name string
	var aliases []string
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNodeByID(agentId, fact.NodeID)
		if err != nil || node == nil {
			return err
		}
		path, name = node.Path, node.Name
		aliases = node.Aliases
		return nil
	}); err != nil {
		log.Warningf("cannot read the page a fact is on: %s", err)
	}
	vectors, modelName, ok := self.agent.embed(ctx, agentId, "ask", []string{factText(fact, path, name)})
	if !ok {
		return nil
	}
	var twins []*models.AgentFact
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.PutAgentFactVector(agentId, fact.ID, modelName, vectors[0]); err != nil {
			return err
		}
		scores, err := tx.Nearest(db.AgentFactTable, agentId, modelName, vectors[0], 6, db.VectorQuery{
			Where:     []string{`"fact_id" IN (SELECT "id" FROM "agent_fact" WHERE "node_id" = ? AND "id" <> ? AND NOT "dormant" AND "superseded_by" IS NULL)`},
			Arguments: []any{fact.NodeID, fact.ID},
			Floor:     twinFloor,
		})
		if err != nil {
			return err
		}
		candidates, err := tx.GetAgentFacts(agentId, idsOf(scores))
		if err != nil {
			return err
		}
		// The page's own name is not evidence either way; see sharesAName.
		itsOwn := append([]string{name}, aliases...)
		for _, candidate := range orderFacts(candidates, idsOf(scores)) {
			if sharesAName(fact.Text, candidate.Text, itsOwn...) {
				twins = append(twins, candidate)
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot keep a fact's vector: %s", err)
		return nil
	}
	return twins
}

// ResolvePage is the page a fact belongs on. Part of tools.Remembering;
// the work is in duplicate.go.
func (self *AskRun) ResolvePage(ctx context.Context, tx db.Transaction, path string, kind models.AgentNodeKind, name string) (*models.AgentNode, error) {
	return self.agent.resolvePage(ctx, tx, self.settings.Agent.ID, path, kind, name)
}

// NoteNode gives a page its vector.
func (self *AskRun) NoteNode(ctx context.Context, node *models.AgentNode) {
	if node == nil {
		return
	}
	agentId := self.settings.Agent.ID
	vectors, modelName, ok := self.agent.embed(ctx, agentId, "ask", []string{nodeText(node)})
	if !ok {
		return
	}
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.PutAgentNodeVector(agentId, node.ID, modelName, vectors[0])
	}); err != nil {
		log.Warningf("cannot keep a page's vector: %s", err)
	}
}

// sharesAName says whether two sentences name the same thing: a word that
// begins with a capital and is not the first word, or a run of digits.
// Where neither sentence has one, they are judged on the cosine alone.
func sharesAName(left, right string, itsOwn ...string) bool {
	leftNames, rightNames := properNouns(left), properNouns(right)
	// The page's own name is dropped from both sides rather than counted
	// on either. Every fact on a page is about the page, so counting it
	// would make "Kittiwake was repainted by Alice" and "...by Bob" share
	// a name and merge; dropping it leaves what the two sentences
	// actually disagree about, which for those is Alice and Bob and for
	// two wordings of the same thing is nothing at all.
	for _, name := range namesIn(itsOwn) {
		delete(leftNames, name)
		delete(rightNames, name)
	}
	if len(leftNames) == 0 || len(rightNames) == 0 {
		return true
	}
	for name := range leftNames {
		if rightNames[name] {
			return true
		}
	}
	return false
}

// namesIn is the words of some names, lowercased.
func namesIn(names []string) []string {
	var words []string
	for _, name := range names {
		for _, word := range strings.Fields(name) {
			if trimmed := strings.Trim(word, ".,;:!?()[]\"'"); trimmed != "" {
				words = append(words, strings.ToLower(trimmed))
			}
		}
	}
	return words
}

// properNouns is the names and numbers a sentence carries: a word that
// begins with a capital and is not the first word, or a run of digits.
//
// The first word is left out on purpose, and it costs something. It is
// capitalized because it is first, so counting it would make "Repainted
// every spring" and "Gets a coat of paint each spring" two different
// facts; leaving it out means "Alice wrote the API" and "Bob wrote the
// API" are judged on the cosine alone, and could be merged. That is the
// trade taken, and it is survivable because merging only ever happens
// within one page: a page is about one thing, so a second person in a
// sentence is almost always mentioned rather than the subject, and a
// mentioned name is mid-sentence and counted.
func properNouns(text string) map[string]bool {
	names := map[string]bool{}
	for index, word := range strings.Fields(text) {
		trimmed := strings.Trim(word, ".,;:!?()[]\"'")
		if trimmed == "" {
			continue
		}
		runes := []rune(trimmed)
		if runes[0] >= '0' && runes[0] <= '9' {
			names[strings.ToLower(trimmed)] = true
			continue
		}
		if index == 0 {
			continue
		}
		if strings.ToUpper(string(runes[0])) == string(runes[0]) && strings.ToLower(string(runes[0])) != string(runes[0]) {
			names[strings.ToLower(trimmed)] = true
		}
	}
	return names
}

// EmbedGraph gives vectors to pages and facts that have none, or whose
// vector an older model made, and says how many it wrote.
func (self *Agent) EmbedGraph(ctx context.Context, agent *models.Agent, limit int) (int, error) {
	_, _, modelName, _, ok := self.embedderFor()
	if !ok {
		return 0, nil
	}
	var nodes []*models.AgentNode
	var facts []*models.AgentFact
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if nodes, err = tx.ListAgentNodesWithoutVector(agent.ID, modelName, limit); err != nil {
			return err
		}
		facts, err = tx.ListAgentFactsWithoutVector(agent.ID, modelName, limit)
		return err
	}); err != nil {
		return 0, err
	}
	if len(nodes) == 0 && len(facts) == 0 {
		return 0, nil
	}

	paths := map[string]string{}
	names := map[string]string{}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		ids := make([]string, 0, len(facts))
		for _, fact := range facts {
			ids = append(ids, fact.NodeID)
		}
		found, err := tx.GetAgentNodes(agent.ID, ids)
		if err != nil {
			return err
		}
		for _, node := range found {
			paths[node.ID] = node.Path
			names[node.ID] = node.Name
		}
		return nil
	}); err != nil {
		return 0, err
	}

	texts := make([]string, 0, len(nodes)+len(facts))
	for _, node := range nodes {
		texts = append(texts, nodeText(node))
	}
	for _, fact := range facts {
		texts = append(texts, factText(fact, paths[fact.NodeID], names[fact.NodeID]))
	}
	vectors, _, ok := self.embed(ctx, agent.ID, "embed", texts)
	if !ok {
		return 0, nil
	}
	written := 0
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		for index, node := range nodes {
			if index >= len(vectors) || len(vectors[index]) == 0 {
				continue
			}
			if err := tx.PutAgentNodeVector(agent.ID, node.ID, modelName, vectors[index]); err != nil {
				return err
			}
			written++
		}
		for index, fact := range facts {
			position := len(nodes) + index
			if position >= len(vectors) || len(vectors[position]) == 0 {
				continue
			}
			if err := tx.PutAgentFactVector(agent.ID, fact.ID, modelName, vectors[position]); err != nil {
				return err
			}
			written++
		}
		return nil
	}); err != nil {
		return written, err
	}
	return written, nil
}

// --- what came before -------------------------------------------------

// MigrateMemories moves an agent's flat memories onto the graph, once.
//
// Done here rather than in the migration because a page and a fact need
// identifiers this package makes, and because doing it lazily means the
// old table is untouched: a deployment that goes back a release finds its
// memories exactly as they were.
func (self *Agent) MigrateMemories(ctx context.Context, agent *models.Agent) error {
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.EnsureAgentRoots(agent.ID); err != nil {
			return err
		}
		memories, err := tx.ListAgentMemories(agent.ID, "", 1000)
		if err != nil || len(memories) == 0 {
			return err
		}
		notes, err := tx.GetAgentNode(agent.ID, models.PathNotes)
		if err != nil || notes == nil {
			return err
		}
		existing, err := tx.ListAgentFacts(agent.ID, notes.ID, true, 2000)
		if err != nil {
			return err
		}
		already := map[string]bool{}
		for _, fact := range existing {
			for _, evidence := range fact.Evidence {
				if evidence.Kind == models.EvidenceMemory {
					already[evidence.ID] = true
				}
			}
		}
		moved := 0
		for _, memory := range memories {
			if already[memory.ID] {
				continue
			}
			text := memory.Line()
			if len(memory.Tags) > 0 {
				text += " (" + strings.Join(memory.Tags, ", ") + ")"
			}
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agent.ID, NodeID: notes.ID, Kind: models.FactPlain,
				Text: cutRunes(text, models.FactLength), Confidence: 1,
				Evidence:  []models.Evidence{{Kind: models.EvidenceMemory, ID: memory.ID, At: &memory.CreatedAt}},
				Audiences: memory.AppliesTo,
			}); err != nil {
				return err
			}
			moved++
		}
		if moved > 0 {
			log.Noticef("moved %d memories of agent %s onto the graph", moved, agent.ID)
		}
		return nil
	})
}

// EnsureVectorIndexes builds the vector index for every table and every
// model that has written a vector. Called at start, and again whenever a
// model writes its first vector, so that the index exists before a corpus
// arrives rather than having to be built over one.
func (self *Agent) EnsureVectorIndexes(ctx context.Context) error {
	database := self.settings.Database
	if !database.VectorIndexing() {
		return nil
	}
	var widths map[string]int
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		widths, err = tx.ListVectorModels()
		return err
	}); err != nil {
		return err
	}
	// The model this deployment is configured with, whether or not it has
	// written anything yet: building the index first is the whole point.
	if _, _, modelName, dimensions, ok := self.embedderFor(); ok && dimensions > 0 {
		if _, known := widths[modelName]; !known {
			if widths == nil {
				widths = map[string]int{}
			}
			widths[modelName] = dimensions
		}
	}
	tables := []db.VectorTable{db.AgentNodeTable, db.AgentFactTable, db.AgentChunkTable, db.MailEmbeddingTable}
	for model, dimension := range widths {
		for _, table := range tables {
			if err := database.EnsureVectorIndex(table, model, dimension); err != nil {
				// A table that does not exist yet is not a failure: the
				// chunk table arrives with knowledge sources.
				log.Debugf("no vector index for %s on %s: %s", model, table.Table, err)
			}
		}
	}
	return nil
}

// cutRunes shortens text to a number of characters without cutting one in
// half: a byte cut through a character embeds a replacement mark instead
// of the word it was part of.
func cutRunes(text string, characters int) string {
	runes := []rune(text)
	if len(runes) <= characters {
		return text
	}
	return string(runes[:characters])
}

// --- knowledge ---------------------------------------------------------

// SearchKnowledgeByMeaning is the passages nearest some words, and
// whether the database ranked them itself.
//
// Part of tools.KnowledgeSearching. False means this deployment has no
// vector index, and the caller should fall back to re-ranking what the
// words found: reading half a million vectors into memory to sort them is
// not a search.
func (self *AskRun) SearchKnowledgeByMeaning(ctx context.Context, sourceIds []string, words string, limit int) ([]*models.AgentChunk, bool) {
	if !self.agent.settings.Database.VectorIndexing() {
		return nil, false
	}
	vectors, modelName, ok := self.agent.embed(ctx, self.settings.Agent.ID, "search", []string{cutRunes(words, graphEmbedCharacters)})
	if !ok {
		return nil, false
	}
	narrow := db.VectorQuery{Floor: meaningFloorGraph}
	if len(sourceIds) > 0 {
		narrow.Where = append(narrow.Where, `"source_id" = ANY(?)`)
		narrow.Arguments = append(narrow.Arguments, pq.Array(sourceIds))
	}
	var chunks []*models.AgentChunk
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		scores, err := tx.Nearest(db.AgentChunkTable, self.settings.Agent.ID, modelName, vectors[0], limit, narrow)
		if err != nil {
			return err
		}
		found, err := tx.GetAgentChunks(self.settings.Agent.ID, idsOf(scores))
		if err != nil {
			return err
		}
		chunks = orderChunks(found, idsOf(scores))
		return nil
	}); err != nil {
		log.Warningf("cannot rank what %q indexed by meaning: %s", self.settings.Owner.Username, err)
		return nil, false
	}
	return chunks, true
}

// RankChunksByMeaning puts a set the words found into the order the
// meaning wants. What a deployment with no vector index does instead of a
// vector search: it finds everything the words find, in a better order,
// and misses a paraphrase that shares no word with the question.
func (self *AskRun) RankChunksByMeaning(ctx context.Context, words string, chunks []*models.AgentChunk, limit int) []*models.AgentChunk {
	if len(chunks) <= 1 {
		return chunks
	}
	_, _, modelName, _, ok := self.agent.embedderFor()
	if !ok {
		return chunks
	}
	vectors, _, ok := self.agent.embed(ctx, self.settings.Agent.ID, "search", []string{cutRunes(words, graphEmbedCharacters)})
	if !ok {
		return chunks
	}
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	var ordered []*models.AgentChunk
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		scores, err := tx.Nearest(db.AgentChunkTable, self.settings.Agent.ID, modelName, vectors[0], limit, db.VectorQuery{
			Where:     []string{`"chunk_id" = ANY(?)`},
			Arguments: []any{pq.Array(ids)},
			Floor:     meaningFloorGraph,
		})
		if err != nil {
			return err
		}
		ordered = orderChunks(chunks, idsOf(scores))
		return nil
	}); err != nil {
		log.Debugf("cannot re-rank by meaning: %s", err)
		return chunks
	}
	// Anything the meaning had nothing to say about keeps its place at
	// the end rather than being dropped: the words did find it.
	seen := map[string]bool{}
	for _, chunk := range ordered {
		seen[chunk.ID] = true
	}
	for _, chunk := range chunks {
		if len(ordered) >= limit {
			break
		}
		if !seen[chunk.ID] {
			ordered = append(ordered, chunk)
		}
	}
	return ordered
}

func orderChunks(chunks []*models.AgentChunk, ids []string) []*models.AgentChunk {
	byId := make(map[string]*models.AgentChunk, len(chunks))
	for _, chunk := range chunks {
		byId[chunk.ID] = chunk
	}
	ordered := make([]*models.AgentChunk, 0, len(ids))
	for _, id := range ids {
		if chunk := byId[id]; chunk != nil {
			ordered = append(ordered, chunk)
		}
	}
	return ordered
}

// --- writing as they do -------------------------------------------------

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

// byNumber puts facts back in the order they are numbered, for reading,
// after a query chose which of them to show.
func byNumber(facts []*models.AgentFact) {
	sort.Slice(facts, func(left, right int) bool { return facts[left].Number < facts[right].Number })
}
