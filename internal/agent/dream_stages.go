package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

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
	// hebbianRise is what a link gains by having both its ends wanted
	// within the interval the pass is accounting for.
	hebbianRise = 0.25

	// edgeHalfLife is how long an untouched link takes to be worth half
	// what it was, and edgeDecayFloor the least one pass may leave it.
	//
	// This used to be a constant a pass -- four fifths kept each time the
	// quiet half ran -- which meant what it said only while the quiet
	// half ran once a night. Bootstrapping runs a night every few
	// minutes, and a day of catching up left every link nothing had
	// touched at the floor, so the weights said nothing about what
	// mattered. Thirty days is about the old fifth a night said in the
	// units it meant: time.
	//
	// The floor is per pass and the SQL has the same one on the stored
	// weight, so a night that was down for a year fades a link once by
	// this much rather than to nothing.
	edgeHalfLife   = 30 * 24 * time.Hour
	edgeDecayFloor = 0.05

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

// edgeDecayFactor is what a link keeps over an interval nothing touched
// it: half after edgeHalfLife, a quarter after twice that, never less
// than edgeDecayFloor in one pass.
func edgeDecayFactor(elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 1
	}
	factor := math.Pow(0.5, elapsed.Seconds()/edgeHalfLife.Seconds())
	if factor < edgeDecayFloor {
		return edgeDecayFloor
	}
	if factor > 1 {
		return 1
	}
	return factor
}

// dreamQuietHalf is the arithmetic half: links strengthened by use,
// weakened by disuse, and the least useful pages taken out of the index.
//
// No model runs here at all, which is why it can be thorough.
func (self *Agent) dreamQuietHalf(ctx context.Context, run *Run, record *models.AgentDream, now time.Time) {
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		// What the fade and the rise are accounting for is the stretch
		// since the last pass, read and written in the same transaction
		// so two nights cannot account for it twice.
		var since time.Time
		if _, err := tx.UpdateAgent(run.Agent.ID, func(agent *models.Agent) error {
			if agent.DecayedAt != nil {
				since = *agent.DecayedAt
			}
			agent.DecayedAt = &now
			return nil
		}); err != nil {
			return err
		}
		run.Agent.DecayedAt = &now
		// What was used together since the last pass is related in a way
		// nobody wrote down; what nothing has touched loses a little of
		// its claim, by how long it has been rather than by how often
		// this ran. The first pass has no interval to account for, so it
		// writes the watermark and fades nothing.
		if !since.IsZero() {
			strengthened, err := tx.StrengthenAgentEdges(run.Agent.ID, since, hebbianRise, edgeDecayFactor(now.Sub(since)))
			if err != nil {
				return err
			}
			record.Strengthened = int(strengthened)
		}

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
	said, err := self.dreamThink(ctx, run, budget, "Looked for a link between "+start.Path+" and "+end.Path, prompt, false)
	if err != nil {
		return false
	}
	extracted, err := llm.ExtractJSON(said)
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
		return tx.PutAgentEdge(walkedLink(run.Agent.ID, start.ID, end.ID, relation, answer.Note, through))
	}); err != nil {
		log.Debugf("cannot keep a link the dream found: %s", err)
		return false
	}
	record.Proposals = append(record.Proposals, models.DreamProposal{
		Kind: "linked", Path: start.Path, To: end.Path, Reason: answer.Note,
	})
	return true
}

// walkedLink is the link a walk suggests.
//
// Proposed, not stated: two pages being two steps apart and a model
// agreeing that they might be related is a hypothesis, and stored as a
// plain link it was indistinguishable from one the person drew. It is
// also worth less than a stated link from the start, and earns its weight
// by being useful.
//
// Its evidence is the walk itself, under its own kind. It used to be
// filed as a document, so anything that went looking for the document an
// id named found nothing and a reader was told the agent had read
// something it had not.
func walkedLink(agentId, fromId, toId string, relation models.AgentEdgeRelation, note string, through []string) *models.AgentEdge {
	return &models.AgentEdge{
		AgentID: agentId, FromID: fromId, ToID: toId, Relation: relation,
		Note:     strings.TrimSpace(note),
		Weight:   0.5,
		Status:   models.EdgeProposed,
		Evidence: []models.Evidence{{Kind: models.EvidenceDream, Quote: strings.Join(through, " → ")}},
	}
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

	prompt, err := render("rehearse.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Index":      index,
		"Recent":     recent,
		"Most":       rehearsalQuestions,
	})
	if err != nil {
		return
	}
	said, err := self.dreamThink(ctx, run, budget, "Rehearsed what might be asked", prompt, false)
	if err != nil {
		return
	}
	extracted, err := llm.ExtractJSON(said)
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
	// question nothing comes back for is the gap; one that could not be
	// tried at all is neither, and saying so is the point of the third
	// outcome.
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
		switch self.canAnswerFromMemory(ctx, run, budget, question) {
		case rehearsalAnswered:
			asked = append(asked, "answered: "+question)
		case rehearsalGap:
			asked = append(asked, "gap: "+question)
			gaps = append(gaps, question)
		default:
			// Not a gap: nothing was learned about this question, and
			// writing it down as one would have the person chasing an
			// answer their agent already has.
			record.Unknown++
			asked = append(asked, "could not tell: "+question)
		}
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

// rehearsalOutcome is what a night made of one question it asked itself.
//
// Three and not two. The phase used to answer yes or no, and every way of
// failing -- no budget left, no embedding model, a model that did not
// answer, an answer that did not parse -- came back as yes, because
// reporting every question as a gap would be worse than reporting none.
// That made a night whose model was unreachable read as a night with
// nothing missing, which is the opposite of what rehearsal is for.
type rehearsalOutcome int

const (
	// rehearsalUnknown is a question that could not be tried. The zero
	// value, so a path that forgets to say what happened says this.
	rehearsalUnknown rehearsalOutcome = iota

	// rehearsalAnswered is memory having the answer, said by the model
	// naming the facts it answered from.
	rehearsalAnswered

	// rehearsalGap is the graph having been asked and having nothing:
	// what the person will hear as "I don't know" tomorrow.
	rehearsalGap
)

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
func (self *Agent) canAnswerFromMemory(ctx context.Context, run *Run, budget *dreamBudget, question string) rehearsalOutcome {
	// A fact answers a question; a page only says the subject exists.
	// Counting a page as an answer made every question answerable --
	// "what did I promise the Rivermouth team" matched the Rivermouth project at
	// a quarter's similarity -- and eight nights found no gap at all.
	// And a fact that is merely near is not an answer either: "what was
	// my next step for gogcli" is near "the checkout is at ~/gogcli",
	// which does not say. So the nearest facts are shown to the model
	// with the question, and it says whether they answer it.
	_, facts, err := self.nearestInGraph(ctx, run.Agent, question, 5)
	// No embedding model, or one that did not answer, or a database that
	// did not: the graph was never really asked, and neither verdict
	// would be true of it. This used to ask instead whether an embedder
	// was configured, which answers a different question -- a provider
	// that timed out was configured, so an outage read as a graph full of
	// gaps and sent the person chasing answers their agent already had.
	if err != nil {
		log.Debugf("rehearsal could not ask the graph: %s", err)
		return rehearsalUnknown
	}
	// And only lines the pages still say, which the search now narrows to
	// as well. Kept here too because this is the one caller that does not
	// go on to filter them itself: recall drops a retired fact before it
	// reaches anybody, and rehearsal handed whatever came back straight to
	// its judge -- so a night could report that memory answers a question,
	// out of a statement the page had already retired.
	standing := facts[:0]
	for _, fact := range facts {
		if stillStands(fact) {
			standing = append(standing, fact)
		}
	}
	if len(standing) == 0 {
		return rehearsalGap
	}
	return self.factsAnswer(ctx, run, budget, question, standing)
}

// factsAnswer asks the model whether these facts answer the question.
//
// Every way of not getting an answer is unknown rather than either
// verdict: what a night could not try it does not get to report on.
//
// And a yes has to say which of the facts it answered from. A model asked
// "do these notes answer this" agrees more readily than it should, and
// the numbers make it point at the line it means -- an answer that can
// name nothing is one that liked the subject rather than found the
// answer, and those are the gaps worth knowing about.
func (self *Agent) factsAnswer(ctx context.Context, run *Run, budget *dreamBudget, question string, facts []*models.AgentFact) rehearsalOutcome {
	if !budget.left() {
		return rehearsalUnknown
	}
	lines := make([]string, 0, len(facts))
	for number, fact := range facts {
		lines = append(lines, fmt.Sprintf("%d. %s", number+1, cutRunes(fact.Text, 400)))
	}
	prompt, err := render("rehearse_check.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Question":   question,
		"Facts":      lines,
	})
	if err != nil {
		return rehearsalUnknown
	}
	said, err := self.dreamThink(ctx, run, budget, "Judged whether memory answers: "+cutRunes(question, 80), prompt, false)
	if err != nil {
		return rehearsalUnknown
	}
	return rehearsalVerdict(said, len(lines))
}

// rehearsalVerdict is what the judge's answer says, given how many facts
// it was shown.
//
// Apart from the call so the rules can be read and tested without a
// model: an answer that is not an object says nothing, a no is a gap,
// and a yes counts only when it points at one of the facts in front of
// it by number.
//
// The field has to be there. Read into a plain bool it was false when
// absent, so any object at all that was not the one asked for -- the
// tool call a model sometimes writes instead, an object of its own
// design, an error the provider wrapped in JSON -- was counted as the
// model having looked and found nothing. That is a gap the graph never
// had, written down and put in front of the person as something their
// agent cannot answer.
func rehearsalVerdict(said string, shown int) rehearsalOutcome {
	extracted, err := llm.ExtractJSON(said)
	if err != nil {
		return rehearsalUnknown
	}
	var answer struct {
		Answered *bool `json:"answered"`
		Facts    []int `json:"facts"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil {
		return rehearsalUnknown
	}
	if answer.Answered == nil {
		return rehearsalUnknown
	}
	if !*answer.Answered {
		return rehearsalGap
	}
	for _, number := range answer.Facts {
		if number >= 1 && number <= shown {
			return rehearsalAnswered
		}
	}
	return rehearsalUnknown
}

// dreamRevise goes back over what an older build of this program filed
// and applies what this one knows.
//
// A graph outlives the code that fills it. A line an early build worded
// badly -- "1 commits by 1 people, July 2026 to July 2026" -- is still
// on the page years later, and there is no way to find it except by
// knowing which build wrote it. So every row carries the build that
// wrote it, and this pass offers each one to the rules as they stand.
//
// Nothing here deletes. A row is reworded or its evidence re-kinded,
// which the page's history records like any other change, and a row this
// build has no opinion about is stamped and left alone. A pass that
// struck what a word list called empty stood here, and took real lines
// with it, unannounced; judging what a person's agent already filed is
// not work to do unattended.
//
// No model runs here either. A rule worth applying to a graph unattended
// is one that can be stated in code; anything needing judgement belongs
// in the phase that asks.
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
			// Filed by a digest that borrowed the conversation's evidence
			// shape: the id names a document, and the page said "from
			// conversation" over a chat thread.
			if rekinded := self.documentEvidence(tx, run.Agent.ID, fact); rekinded {
				record.Revised++
				return nil
			}
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
			// A word list stood here, of the words that say only what a
			// page is, and a fact left with nothing else was struck
			// from the page it was on. It could not tell a thin line
			// from a plain one -- "This project is private" is all
			// list words -- and a struck line is one a person was never
			// told had gone. What an older build filed stays filed.
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
	self.dreamMergeThePerson(ctx, run, record)
	self.dreamForgetSaidTwice(ctx, run, record)
	self.dreamForgetEmptyPages(ctx, run, record)
}

// dreamMergeThePerson folds a page under people that names the person
// themselves into self. The filing routes such a page to self, and the
// memory tool does too, but a page made before either rule existed, or by
// a model naming them a little differently, sat beside self as a second
// person -- and a conversation asked to consolidate the two crowned the
// duplicate as the canonical one instead.
func (self *Agent) dreamMergeThePerson(ctx context.Context, run *Run, record *models.AgentDream) {
	var people []*models.AgentNode
	var selfPage *models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if selfPage, err = tx.GetAgentNode(run.Agent.ID, models.PathSelf); err != nil {
			return err
		}
		people, err = tx.ListAgentNodesUnder(run.Agent.ID, models.PathPeople, 500)
		return err
	}); err != nil {
		log.Warningf("cannot list the people: %s", err)
		return
	}
	for _, node := range people {
		if !models.IsThePerson(node.Path, run.Owner, selfPage) {
			continue
		}
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			tx.AsActor(models.ActorDream)
			_, err := tx.MergeAgentNodes(run.Agent.ID, node.Path, models.PathSelf)
			return err
		}); err != nil {
			log.Warningf("cannot fold %q into self: %s", node.Path, err)
			continue
		}
		log.Noticef("folded %q into self: it was the person", node.Path)
		record.Merged++
	}
}

// dreamForgetSaidTwice folds a fact whose page already says the same
// words on an earlier number into that earlier one. No judgement is
// involved -- the words are identical -- so no model is asked. Twelve
// pages carried "wrote 16 of the commits" twice, from a pass that keyed
// its lines by evidence and a pass before it that did not; the pass that
// merges near-duplicates would have got to them a page a night.
//
// A fold and not a deletion, the same as the write-time one: the second
// copy stays behind the first, so the pair reads as one decision the
// person can look at rather than as a row that vanished.
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
			kept, err := firstSayingIt(tx, run.Agent.ID, fact)
			if err != nil || kept == nil {
				// Gone, or folded by another pass between the listing
				// and here. Nothing to stand behind, so nothing to do.
				return err
			}
			_, err = tx.FoldAgentFact(run.Agent.ID, fact.ID, kept.ID,
				"the page already said it in the same words")
			return err
		}); err != nil {
			log.Warningf("cannot fold the second copy of %s: %s", fact.ID, err)
			continue
		}
		record.Merged++
	}
}

// firstSayingIt is the fact the page already states in the same words,
// on a lower number than the one given.
func firstSayingIt(tx db.Transaction, agentId string, fact *models.AgentFact) (*models.AgentFact, error) {
	facts, err := tx.ListAgentFacts(agentId, fact.NodeID, false, 500)
	if err != nil {
		return nil, err
	}
	saying := strings.ToLower(strings.TrimSpace(fact.Text))
	for _, candidate := range facts {
		if candidate.ID == fact.ID || candidate.Number >= fact.Number {
			continue
		}
		if strings.ToLower(strings.TrimSpace(candidate.Text)) == saying {
			return candidate, nil
		}
	}
	return nil, nil
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

// documentEvidence corrects a fact whose evidence says conversation but
// names a document, and says whether it did.
func (self *Agent) documentEvidence(tx db.Transaction, agentId string, fact *models.AgentFact) bool {
	changed := false
	for index := range fact.Evidence {
		evidence := &fact.Evidence[index]
		if evidence.Kind != models.EvidenceConversation {
			continue
		}
		id := strings.Trim(strings.TrimSpace(evidence.ID), "[]")
		document, err := tx.GetAgentDocument(agentId, id)
		if err != nil || document == nil {
			continue
		}
		evidence.Kind, evidence.ID = models.EvidenceDocument, id
		changed = true
	}
	if !changed {
		return false
	}
	if _, err := tx.UpdateAgentFact(agentId, fact.ID, func(existing *models.AgentFact) error {
		existing.Evidence = fact.Evidence
		return nil
	}); err != nil {
		log.Debugf("cannot correct the evidence of %s: %s", fact.ID, err)
		return false
	}
	return true
}
