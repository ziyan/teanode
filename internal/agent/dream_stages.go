package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/version"
)

// The two halves of a night.
//
// Consolidating what arrived is only part of what sleep is for. The
// literature on it -- and every working implementation of the idea --
// splits the night in two, and the split is not decoration:
//
//   - The quiet half strengthens what was used together, weakens
//     everything a little, and lets the weak fall out of reach. It is
//     arithmetic: no model runs, and it is what keeps a link's weight
//     meaning "worth something now" rather than "somebody once wrote
//     this".
//
//   - The other half is generative. It walks the graph from what matters,
//     puts two pages that are connected but not obviously so in front of
//     the model, and asks whether there is a real relation between them.
//     This is where a graph gains a link nobody typed -- that the person
//     on this project is the one who wrote that thing -- which is the
//     whole reason to have a graph rather than a list.
//
// And then rehearsal, which is neither: the agent asks itself the
// questions it expects tomorrow, tries to answer them from memory alone,
// and writes down what it could not answer. A gap found at three in the
// morning is cheap; the same gap found mid-conversation is the person
// watching their agent say it does not know.

// The bounds.
const (
	// hebbianRise is what a link gains by having both its ends wanted on
	// the same day, and hebbianDecay what every link keeps overnight.
	//
	// A fifth off a night sounds severe and is not: a link used twice a
	// week sits in equilibrium well above the floor, and one nothing has
	// touched in two months is down near it. That is the intended
	// difference, and a gentler number does not produce it.
	hebbianRise  = 0.25
	hebbianDecay = 0.8

	// indexTarget is how many pages the index is meant to hold. The bar
	// to stay in it rises as the graph grows past this.
	indexTarget = 400

	// retireUnusedFor is how long a page must have gone unwanted before
	// falling under the threshold takes it out of the index. Importance
	// alone is not enough: a page written yesterday has had no chance to
	// be used.
	retireUnusedFor = 45 * 24 * time.Hour

	// walks is how many paths a generative pass takes, and walkSteps how
	// far each goes. Five steps is far enough that the two ends are not
	// obviously related and near enough that they might be.
	walks     = 6
	walkSteps = 5

	// rehearsalQuestions is how many questions a night asks itself.
	rehearsalQuestions = 8

	// reviseBatch is how much of what an older build wrote one night goes
	// back over. Pacing, as everywhere here: what is not looked at
	// tonight is looked at tomorrow, and nothing is dropped.
	// Two thousand rather than five hundred: the pass asks no model, and
	// every build that ships makes every row an older build's again, so
	// a batch smaller than the graph never reached the newest rows while
	// builds were shipping daily.
	reviseBatch = 2000

	// emptyPageGrace is how long a page may stand with nothing on it
	// before the night takes it as never going to have anything.
	emptyPageGrace = 48 * time.Hour
)

// dreamQuietHalf is the arithmetic half: links strengthened by use,
// weakened by disuse, and the least useful pages taken out of the index.
//
// No model runs here at all, which is why it can be thorough.
func (self *Agent) dreamQuietHalf(ctx context.Context, run *Run, record *models.AgentDream) {
	now := time.Now()
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		// What was used together since the last night is related in a way
		// nobody wrote down; what nothing has touched loses a little of
		// its claim. Since the last night rather than since midnight,
		// because that is the stretch this run is accounting for.
		strengthened, err := tx.StrengthenAgentEdges(run.Agent.ID, now.Add(-dreamApart), hebbianRise, hebbianDecay)
		if err != nil {
			return err
		}
		record.Strengthened = int(strengthened)

		if _, err := tx.RecomputeAgentImportance(run.Agent.ID, now); err != nil {
			return err
		}
		dormantFacts, err := tx.RetireAgentFacts(run.Agent.ID, now.Add(-dormantAfter))
		if err != nil {
			return err
		}
		// The bar to stay in the index, from the graph rather than from a
		// constant somebody would have to re-tune.
		threshold, err := tx.AgentImportanceThreshold(run.Agent.ID, indexTarget)
		if err != nil {
			return err
		}
		dormantPages, err := tx.RetireAgentNodes(run.Agent.ID, threshold, now.Add(-retireUnusedFor))
		if err != nil {
			return err
		}
		record.Dormant = dormantFacts + dormantPages
		return nil
	}); err != nil {
		log.Warningf("cannot settle what matters: %s", err)
	}
}

// dreamAssociate is the generative half: walking the graph and asking
// whether two pages that are connected but not adjacent have anything
// real to do with each other.
//
// What comes back is a link with a sentence on it, or nothing. Nothing is
// the common answer and the right one: a walk through a dense graph will
// find two pages with no relation at all most of the time, and a pass
// that invented one anyway would fill the graph with noise that looks
// exactly like knowledge.
func (self *Agent) dreamAssociate(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	var starts []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		starts, err = tx.ListAgentNodesForWalking(run.Agent.ID, walks)
		return err
	}); err != nil || len(starts) == 0 {
		return
	}
	for _, start := range starts {
		if ctx.Err() != nil || !budget.left() {
			return
		}
		var path []*models.AgentNode
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			path, err = tx.WalkAgentGraph(run.Agent.ID, start.ID, walkSteps)
			return err
		}); err != nil || len(path) < 2 {
			continue
		}
		if self.askAboutAWalk(ctx, run, record, budget, start, path) {
			record.Associated++
		}
	}
}

// askAboutAWalk puts one path to the model and writes the link it names.
func (self *Agent) askAboutAWalk(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget, start *models.AgentNode, path []*models.AgentNode) bool {
	end := path[len(path)-1]
	// Already joined directly? Then there is nothing here to find.
	var joined bool
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		edges, err := tx.ListAgentEdges(run.Agent.ID, start.ID)
		if err != nil {
			return err
		}
		for _, edge := range edges {
			if edge.ToID == end.ID || edge.FromID == end.ID {
				joined = true
			}
		}
		return nil
	}); err != nil || joined {
		return false
	}

	provider, model, err := run.Registry().ForWork(config.AgentWorkScan)
	if err != nil {
		return false
	}
	modelName := run.Registry().Configuration().Models.ForWork(config.AgentWorkScan)

	var through []string
	for _, node := range path {
		line := node.Path
		if node.Name != "" {
			line += " — " + node.Name
		}
		if summary := strings.TrimSpace(node.Summary); summary != "" {
			line += ": " + cutRunes(summary, 240)
		}
		through = append(through, line)
	}
	relations := make([]string, 0, len(models.AgentEdgeRelations))
	for _, relation := range models.AgentEdgeRelations {
		relations = append(relations, string(relation))
	}
	prompt, err := render("associate.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"From":       start.Path + " — " + start.Name + ": " + cutRunes(start.Summary, 400),
		"To":         end.Path + " — " + end.Name + ": " + cutRunes(end.Summary, 400),
		"Through":    through,
		"Relations":  strings.Join(relations, ", "),
	})
	if err != nil {
		return false
	}
	configuration := run.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model: model, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}}, MaxTokens: 400,
	})
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, "", modelName, string(models.AgentJobDream), response.Usage)
		budget.note(response.Usage)
	}
	if err != nil {
		return false
	}
	extracted, err := llm.ExtractJSON(response.Message.Content)
	if err != nil {
		return false
	}
	var answer struct {
		Related  bool   `json:"related"`
		Relation string `json:"relation"`
		Note     string `json:"note"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil || !answer.Related {
		return false
	}
	relation := models.AgentEdgeRelation(strings.ToLower(strings.TrimSpace(answer.Relation)))
	if !models.IsAgentEdgeRelation(relation) || strings.TrimSpace(answer.Note) == "" {
		return false
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		return tx.PutAgentEdge(&models.AgentEdge{
			AgentID: run.Agent.ID, FromID: start.ID, ToID: end.ID, Relation: relation,
			Note: answer.Note,
			// Below one: a link the agent worked out overnight starts
			// weaker than one somebody stated, and earns its weight by
			// being useful.
			Weight:   0.5,
			Evidence: []models.Evidence{{Kind: models.EvidenceDocument, Quote: strings.Join(through, " → ")}},
		})
	}); err != nil {
		log.Debugf("cannot keep a link the night found: %s", err)
		return false
	}
	record.Proposals = append(record.Proposals, models.DreamProposal{
		Kind: "linked", Path: start.Path, To: end.Path, Reason: answer.Note,
	})
	return true
}

// dreamRehearse asks the questions tomorrow is likely to bring, tries to
// answer them from memory alone, and writes down what it could not.
//
// The point is not the answers -- nobody reads them. It is the failures:
// a question the graph cannot answer is a gap, and a gap found at three
// in the morning costs a model call, where the same gap found in a
// conversation costs the person watching their agent say it does not
// know.
//
// It pays off exactly as far as tomorrow's questions are predictable from
// what is already known, which for somebody's own life is a good deal
// further than for a search engine.
func (self *Agent) dreamRehearse(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	var index []string
	var recent []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		nodes, err := tx.ListAgentIndex(run.Agent.ID, 80)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			index = append(index, node.IndexLine(140))
		}
		// What changed lately is what they are most likely to ask about.
		revisions, err := tx.ListAgentRevisionsSince(run.Agent.ID, time.Now().Add(-7*24*time.Hour), 40)
		if err != nil {
			return err
		}
		for _, revision := range revisions {
			if text := revision.TextAfter(); text != "" {
				recent = append(recent, text)
			}
		}
		return nil
	}); err != nil || len(index) == 0 {
		return
	}

	provider, model, err := run.Registry().ForWork(config.AgentWorkScan)
	if err != nil {
		return
	}
	modelName := run.Registry().Configuration().Models.ForWork(config.AgentWorkScan)
	prompt, err := render("rehearse.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Index":      index,
		"Recent":     recent,
		"Most":       rehearsalQuestions,
	})
	if err != nil {
		return
	}
	configuration := run.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model: model, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}}, MaxTokens: 1200,
	})
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, "", modelName, string(models.AgentJobDream), response.Usage)
		budget.note(response.Usage)
	}
	if err != nil {
		return
	}
	extracted, err := llm.ExtractJSON(response.Message.Content)
	if err != nil {
		return
	}
	var answer struct {
		Questions []string `json:"questions"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil {
		return
	}

	// Each question is asked of the graph the way a turn would ask it. A
	// question nothing comes back for is the gap.
	var gaps, asked []string
	for index, question := range answer.Questions {
		if index >= rehearsalQuestions || ctx.Err() != nil {
			break
		}
		question = strings.TrimSpace(question)
		if question == "" {
			continue
		}
		record.Rehearsed++
		if self.canAnswerFromMemory(ctx, run, budget, question) {
			asked = append(asked, "answered: "+question)
			continue
		}
		asked = append(asked, "gap: "+question)
		gaps = append(gaps, question)
	}
	record.Gaps = len(gaps)
	// The questions themselves are kept with the night, so the person
	// can see what their agent thought they would ask, not just a count.
	record.Notes = strings.Join(asked, "\n")
	if len(gaps) == 0 {
		return
	}
	// Written down rather than acted on. Filling a gap means deciding
	// what the answer is, and a run with nobody present inventing answers
	// to its own questions is how a graph fills with fiction.
	for _, gap := range gaps {
		record.Proposals = append(record.Proposals, models.DreamProposal{
			Kind: "gap", Reason: gap,
		})
	}
	log.Debugf("rehearsal found %d question(s) the graph cannot answer", len(gaps))
}

// canAnswerFromMemory says whether the graph has anything for a question.
//
// By meaning, and deliberately not by words. The word search joins a
// question's words with OR so that a question phrased as a sentence finds
// something -- which is right for recall, where a near miss is still a
// help, and wrong here, where it answers "is any word of this question
// anywhere in the graph" and so answers yes to everything. A rehearsal
// that never finds a gap is a phase that costs a model call and reports
// nothing.
//
// The meaning search has a floor, so it answers the question actually
// being asked: is anything in this graph about this. An identifier the
// person would type verbatim is in the embedded text too, so asking by
// meaning does not lose the exact-match case.
func (self *Agent) canAnswerFromMemory(ctx context.Context, run *Run, budget *dreamBudget, question string) bool {
	// A fact answers a question; a page only says the subject exists.
	// Counting a page as an answer made every question answerable --
	// "what did I promise the Osaka team" matched the Osaka project at
	// a quarter's similarity -- and eight nights found no gap at all.
	// And a fact that is merely near is not an answer either: "what was
	// my next step for gogcli" is near "the checkout is at ~/gogcli",
	// which does not say. So the nearest facts are shown to the model
	// with the question, and it says whether they answer it.
	_, facts := self.nearestInGraph(ctx, run.Agent, question, 5)
	if len(facts) > 0 {
		return self.factsAnswer(ctx, run, budget, question, facts)
	}
	// No embedding model, or it failed: then there is no way to tell, and
	// reporting every question as a gap would be worse than reporting
	// none.
	if _, _, _, _, ok := self.embedderFor(); !ok {
		return true
	}
	return false
}

// factsAnswer asks the model whether these facts answer the question.
// When it cannot be asked, near is taken as answered: a night that
// reports every question as a gap is worse than one that reports none.
func (self *Agent) factsAnswer(ctx context.Context, run *Run, budget *dreamBudget, question string, facts []*models.AgentFact) bool {
	if !budget.left() {
		return true
	}
	provider, model, err := run.Registry().ForWork(config.AgentWorkScan)
	if err != nil {
		return true
	}
	lines := make([]string, 0, len(facts))
	for _, fact := range facts {
		lines = append(lines, "- "+cutRunes(fact.Text, 400))
	}
	prompt, err := render("rehearse_check.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Question":   question,
		"Facts":      lines,
	})
	if err != nil {
		return true
	}
	configuration := run.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model: model, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}}, MaxTokens: 100,
	})
	if response != nil {
		modelName := run.Registry().Configuration().Models.ForWork(config.AgentWorkScan)
		RecordUsage(run.Database(), run.Agent.ID, "", modelName, string(models.AgentJobDream), response.Usage)
		budget.note(response.Usage)
	}
	if err != nil {
		return true
	}
	extracted, err := llm.ExtractJSON(response.Message.Content)
	if err != nil {
		return true
	}
	var answer struct {
		Answered bool `json:"answered"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil {
		return true
	}
	return answer.Answered
}

// dreamRevise goes back over what an older build of this program filed
// and applies what this one knows.
//
// A graph outlives the code that fills it. The first real ingest of a
// person's own machines filed twenty-eight facts of the shape "X is a
// project or work channel" -- true, worthless, and indistinguishable
// from knowledge at a glance -- because the prompt of the day invited
// them. The prompt is fixed and the rule is in code now, but that does
// nothing about what is already on the pages, and there is no way to
// find it except by knowing which build wrote it.
//
// So every row carries the build that wrote it, and this pass offers
// each one to the rules as they stand. What fails them is struck, which
// is a change like any other: the page's history says the nightly run
// did it and what the line used to say, so a person who disagrees can
// put it back.
//
// No model runs here. A rule worth applying to a graph unattended is one
// that can be stated in code; anything needing judgement belongs in the
// phase that asks.
func (self *Agent) dreamRevise(ctx context.Context, run *Run, record *models.AgentDream) {
	build := version.Version()
	var facts []*models.AgentFact
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		facts, err = tx.ListAgentFactsWrittenBefore(run.Agent.ID, build, reviseBatch)
		return err
	}); err != nil {
		log.Warningf("cannot list what an older build wrote: %s", err)
		return
	}
	// Every one is stamped whether or not it changed, including the ones
	// that could not be read: a row left unstamped is offered again
	// tomorrow and every night after.
	seen := make([]string, 0, len(facts))
	due := map[string]bool{}
	for _, fact := range facts {
		if ctx.Err() != nil {
			break
		}
		seen = append(seen, fact.ID)
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			tx.AsActor(models.ActorDream)
			node, err := tx.GetAgentNodeByID(run.Agent.ID, fact.NodeID)
			if err != nil || node == nil {
				return err
			}
			// Whatever happens to this line, the page it is on was filled
			// by an older build and is due a fresh reading: its opening
			// was written under rules that have changed, and the pass
			// that merges facts saying the same thing twice only visits
			// pages whose facts have moved. Without this, a page that an
			// old build filled with nine wordings of one sentence keeps
			// all nine for ever, because nothing ever touches it again.
			due[node.ID] = true
			// A line an older build worded badly is reworded, not
			// struck: "1 commits by 1 people, July 2026 to July 2026"
			// is a true thing said badly, and the page it is on may
			// belong to a source that is paused and will not say it
			// again.
			if reworded := reviseWording(fact.Text); reworded != fact.Text {
				if _, err := tx.UpdateAgentFact(run.Agent.ID, fact.ID, func(existing *models.AgentFact) error {
					existing.Text = reworded
					return nil
				}); err != nil {
					return err
				}
				record.Revised++
				return nil
			}
			if saysSomethingNew(fact.Text, node, run.Owner) {
				return nil
			}
			if err := tx.DeleteAgentFact(run.Agent.ID, fact.ID); err != nil {
				return err
			}
			record.Revised++
			// A page's opening is written from its facts. With the last
			// of them gone there is nothing left for it to have come
			// from, and what is up there was written about lines that no
			// longer exist -- so it goes too, in the page's history like
			// any other change.
			left, err := tx.ListAgentFacts(run.Agent.ID, node.ID, false, 1)
			if err != nil {
				return err
			}
			if len(left) == 0 && strings.TrimSpace(node.Summary) != "" {
				node.Summary = ""
				if _, err := tx.PutAgentNode(node); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			log.Debugf("cannot go back over %s: %s", fact.ID, err)
		}
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.MarkAgentFactsSeen(run.Agent.ID, seen); err != nil {
			return err
		}
		for nodeId := range due {
			if err := tx.MarkAgentNodeConsolidated(nodeId, time.Time{}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot record what was gone over: %s", err)
	}
	self.dreamForgetSaidTwice(ctx, run, record)
	self.dreamForgetEmptyPages(ctx, run, record)
	self.dreamClearPaddedOpenings(ctx, run, record)
}

// dreamClearPaddedOpenings takes the opening off a page where it only
// says the page matters, and makes the page due a fresh one. Every page
// whose opening another build wrote is looked at once and stamped.
func (self *Agent) dreamClearPaddedOpenings(ctx context.Context, run *Run, record *models.AgentDream) {
	build := version.Version()
	var nodes []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		nodes, err = tx.ListAgentNodesWithOpeningWrittenBefore(run.Agent.ID, build, reviseBatch)
		return err
	}); err != nil {
		log.Warningf("cannot list the openings an older build wrote: %s", err)
		return
	}
	seen := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if ctx.Err() != nil {
			return
		}
		seen = append(seen, node.ID)
		if !saysNothingOpening(node.Summary) {
			continue
		}
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			tx.AsActor(models.ActorDream)
			node.Summary = ""
			if _, err := tx.PutAgentNode(node); err != nil {
				return err
			}
			return tx.MarkAgentNodeConsolidated(node.ID, time.Time{})
		}); err != nil {
			log.Warningf("cannot clear the opening of %q: %s", node.Path, err)
			continue
		}
		record.Revised++
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.MarkAgentNodesSeen(run.Agent.ID, seen)
	}); err != nil {
		log.Warningf("cannot record which openings were gone over: %s", err)
	}
}

// dreamForgetSaidTwice strikes a fact whose page already says the same
// words on an earlier number. No judgement is involved -- the words are
// identical -- so no model is asked. Twelve pages carried "wrote 16 of
// the commits" twice, from a pass that keyed its lines by evidence and
// a pass before it that did not; the pass that merges near-duplicates
// would have got to them a page a night.
func (self *Agent) dreamForgetSaidTwice(ctx context.Context, run *Run, record *models.AgentDream) {
	var twice []*models.AgentFact
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		twice, err = tx.ListAgentFactsSaidTwice(run.Agent.ID, reviseBatch)
		return err
	}); err != nil {
		log.Warningf("cannot list what is said twice: %s", err)
		return
	}
	for _, fact := range twice {
		if ctx.Err() != nil {
			return
		}
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			tx.AsActor(models.ActorDream)
			return tx.DeleteAgentFact(run.Agent.ID, fact.ID)
		}); err != nil {
			log.Warningf("cannot strike the second copy of %s: %s", fact.ID, err)
			continue
		}
		record.Merged++
	}
}

// dreamForgetEmptyPages removes the pages that say nothing at all.
//
// A source that names a channel or a directory makes a page for it, and
// a run that meant to write something may make one and then not. Either
// way what is left is a name with nothing under it -- twenty-nine of
// them in a graph of two hundred pages -- and a page that has said
// nothing for two days is not going to. If the name comes up again the
// page is made again, with something on it this time.
func (self *Agent) dreamForgetEmptyPages(ctx context.Context, run *Run, record *models.AgentDream) {
	var empty []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		empty, err = tx.ListAgentNodesEmpty(run.Agent.ID, time.Now().Add(-emptyPageGrace), reviseBatch)
		return err
	}); err != nil {
		log.Warningf("cannot list the pages that say nothing: %s", err)
		return
	}
	for _, node := range empty {
		if ctx.Err() != nil {
			return
		}
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			tx.AsActor(models.ActorDream)
			_, err := tx.DeleteAgentNode(run.Agent.ID, node.Path)
			return err
		}); err != nil {
			log.Warningf("cannot remove the empty page %q: %s", node.Path, err)
			continue
		}
		record.Revised++
	}
}

// reviseWording is the old phrasing of a computed line, said the way the
// current build says it, so that the pages of a paused source read like
// the rest. It changes nothing it does not recognise.
func reviseWording(text string) string {
	if match := oneCommitBy.FindStringSubmatch(text); match != nil {
		text = match[1] + " commits by 1 person, " + match[2] + "."
	}
	if match := wroteOfThem.FindStringSubmatch(text); match != nil {
		text = match[1] + " wrote " + match[2] + " of the commits, " + match[3] + "."
	}
	if match := sameMonthTwice.FindStringSubmatch(text); match != nil && match[2] == match[3] {
		text = match[1] + match[2] + "."
	}
	return text
}

var (
	oneCommitBy    = regexp.MustCompile(`^(\d+) commits by 1 people, (.+)\.$`)
	wroteOfThem    = regexp.MustCompile(`^(.+) wrote (\d+) of them, (.+)\.$`)
	sameMonthTwice = regexp.MustCompile(`^(.*, )([A-Z][a-z]+ \d{4}) to ([A-Z][a-z]+ \d{4})\.$`)
)
