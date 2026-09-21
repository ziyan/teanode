package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// recallWorld is an agent with a graph and a turn to write an overlay
// into. No model and no embedder: writeRecalled is given what a search
// found, so there is nothing here for either to do.
type recallWorld struct {
	run      *AskRun
	database db.Database
	agent    *models.Agent
}

func newRecallWorld(t *testing.T) *recallWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)

	configuration := config.Default()
	worker := New(&Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	world := &recallWorld{database: database}
	var owner *models.User
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if world.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(world.agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
	})
	world.run = &AskRun{
		agent:          worker,
		settings:       &AskSettings{Agent: world.agent, Owner: owner},
		promptMemories: map[string]bool{},
	}
	return world
}

// page makes a page with a summary and some facts on it.
func (self *recallWorld) page(t *testing.T, path, name, summary string, facts ...string) *models.AgentNode {
	t.Helper()
	var node *models.AgentNode
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		node, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: self.agent.ID, Path: path, Kind: models.NodeProject, Name: name, Summary: summary,
		})
		if err != nil {
			t.Fatalf("PutAgentNode(%q): %s", path, err)
		}
		for _, text := range facts {
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: self.agent.ID, NodeID: node.ID, Kind: models.FactPlain, Text: text,
				Audiences: []models.AgentAudience{models.AudienceAsk},
			}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
	})
	return node
}

// factsOf is a page's live facts in the order the store hands them over,
// which is by number.
func (self *recallWorld) factsOf(t *testing.T, node *models.AgentNode) []*models.AgentFact {
	t.Helper()
	var facts []*models.AgentFact
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if facts, err = tx.ListAgentFacts(self.agent.ID, node.ID, false, 200); err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
	})
	return facts
}

// markedFacts is the facts of a page that have a use recorded against
// them, in number order.
func (self *recallWorld) markedFacts(t *testing.T, node *models.AgentNode) []*models.AgentFact {
	t.Helper()
	var marked []*models.AgentFact
	for _, fact := range self.factsOf(t, node) {
		if fact.UsedAt != nil {
			marked = append(marked, fact)
		}
	}
	return marked
}

// wanted is how many of a page's facts have a use recorded against them.
func (self *recallWorld) wanted(t *testing.T, node *models.AgentNode) int {
	t.Helper()
	return len(self.markedFacts(t, node))
}

// numbersShown is the fact numbers an expanded page carries, in the
// order the block carries them. A loose fact is written with its path in
// front of the number, so it is not one of these.
func numbersShown(carried string) []int {
	var numbers []int
	for _, field := range strings.Fields(carried) {
		if !strings.HasPrefix(field, "#") {
			continue
		}
		if number, err := strconv.Atoi(field[1:]); err == nil {
			numbers = append(numbers, number)
		}
	}
	return numbers
}

// overlay is the whole of what the turn would carry, as one string.
func (self *recallWorld) overlay() string {
	return strings.Join(self.run.Recalled(), "\n")
}

// A page whose block does not fit is left alone entirely: its facts are
// not marked as used and the next page down is still tried.
//
// Both halves were wrong. The facts were marked as the block was built
// and the budget was checked afterwards, so a page that never reached the
// prompt still had `used_at` moved on every fact in it -- which feeds
// importance, decay, and what the index carries tomorrow. And a block
// that did not fit ended the loop, so one long page hid every shorter one
// behind it.
func TestAPageOverTheBudgetIsPassedOver(t *testing.T) {
	world := newRecallWorld(t)

	// Five facts of nine hundred characters and an opening of six
	// hundred: more than the whole recall budget on its own.
	long := strings.Repeat("what this project decided and why, at length. ", 20)
	big := world.page(t, "projects/big-one", "Big One", strings.Repeat("an opening at length. ", 40),
		long, long, long, long, long)
	small := world.page(t, "projects/small-one", "Small One", "A short one.", "Ships on Fridays.")

	world.run.writeRecalled(context.Background(), []*models.AgentNode{big, small}, nil)

	carried := world.overlay()
	if strings.Contains(carried, "projects/big-one") {
		t.Fatalf("the page over the budget is not carried:\n%s", carried)
	}
	if !strings.Contains(carried, "projects/small-one") || !strings.Contains(carried, "Ships on Fridays.") {
		t.Fatalf("and the smaller page behind it still is:\n%s", carried)
	}
	if count := world.wanted(t, big); count != 0 {
		t.Fatalf("nothing of a page that was not carried is marked as used, and %d was", count)
	}
	if count := world.wanted(t, small); count != 1 {
		t.Fatalf("what was carried is marked as used, and %d was", count)
	}
}

// Nothing is marked as used that the overlay does not carry, and the
// pages the search ranked highest are the ones it carries.
//
// The chooser and the overlay used to be bounded separately: the chooser
// offered up to fifteen blocks, five pages and ten loose facts, and the
// overlay kept the last ten lines. So a turn with plenty of matches
// dropped the five page blocks -- the best of what was found -- while
// `used_at` moved on every fact in them, which feeds importance, decay,
// and what the index carries tomorrow.
func TestTheOverlayCarriesEveryBlockThatWasMarkedAsUsed(t *testing.T) {
	world := newRecallWorld(t)

	// More pages and more loose facts than either bound, all small, so
	// it is the counting and not the token budget that decides.
	var nodes []*models.AgentNode
	for index := 0; index < 8; index++ {
		nodes = append(nodes, world.page(t,
			fmt.Sprintf("projects/page-%d", index), fmt.Sprintf("Page %d", index),
			"A short opening.", fmt.Sprintf("Ships on day %d.", index)))
	}
	var loose []*models.AgentFact
	for index := 0; index < 12; index++ {
		node := world.page(t, fmt.Sprintf("topics/loose-%d", index), fmt.Sprintf("Loose %d", index), "",
			fmt.Sprintf("A loose note number %d.", index))
		facts := world.factsOf(t, node)
		if len(facts) != 1 {
			t.Fatalf("a loose page has the one fact that was put on it, and has %d", len(facts))
		}
		loose = append(loose, facts[0])
	}

	world.run.writeRecalled(context.Background(), nodes, loose)

	carried := world.overlay()
	// The first page the search offered is the one the turn most wants,
	// so it is the one that must survive the budget.
	if !strings.Contains(carried, "projects/page-0") {
		t.Fatalf("the highest ranked page is carried:\n%s", carried)
	}
	for _, node := range nodes {
		if world.wanted(t, node) > 0 && !strings.Contains(carried, node.Path) {
			t.Fatalf("%q was marked as used and is not in the overlay:\n%s", node.Path, carried)
		}
	}
	if lines := len(world.run.Recalled()); lines > recallBlocks {
		t.Fatalf("the overlay carries at most %d lines, and carried %d", recallBlocks, lines)
	}
}

// Asking what a turn would be carried carries it, and leaves the graph
// exactly as it was.
//
// The evaluation replays a question set through this, so a run of it that
// moved `used_at` would feed importance and decay and change the thing it
// is measuring: the second run of the same set would be graded against a
// graph the first run had already rearranged.
func TestRecallingForAQuestionMarksNothingAsUsed(t *testing.T) {
	world := newRecallWorld(t)

	node := world.page(t, "projects/portal", "Portal", "The customer-facing portal.",
		"Runs on the Frankfurt cluster.")

	pages, err := world.run.agent.RecallForQuestion(context.Background(),
		world.agent, world.run.settings.Owner, "which cluster does the portal run on?")
	if err != nil {
		t.Fatalf("RecallForQuestion: %s", err)
	}
	carried := ""
	for _, page := range pages {
		for _, fact := range page.Facts {
			carried += page.Path + " " + fact.Text + "\n"
		}
	}
	if !strings.Contains(carried, "projects/portal") || !strings.Contains(carried, "Frankfurt") {
		t.Fatalf("the page the question is about is carried:\n%s", carried)
	}
	if count := world.wanted(t, node); count != 0 {
		t.Fatalf("an evaluation marks nothing as used, and %d fact was", count)
	}
}

// A page the prompt's own index already names still has its facts
// expanded when the turn's words hit it.
//
// The index line says what a page is about. It is not what the page
// knows, and skipping the page for being in the index meant that asking
// about the one project the agent thinks most important was answered from
// a single line of description.
func TestAnIndexedPageStillGetsItsFacts(t *testing.T) {
	world := newRecallWorld(t)

	node := world.page(t, "projects/portal", "Portal", "The customer-facing portal.",
		"Runs on the Frankfurt cluster.")
	// As carryIndex leaves it: the prompt already carries this page's line.
	world.run.promptMemories[node.ID] = true

	world.run.writeRecalled(context.Background(), []*models.AgentNode{node}, nil)

	carried := world.overlay()
	if !strings.Contains(carried, "Runs on the Frankfurt cluster.") {
		t.Fatalf("a page in the index still gives up its facts:\n%s", carried)
	}
	// The opening is the one part the index line already has the gist of.
	if strings.Contains(carried, "The customer-facing portal.") {
		t.Fatalf("and not its opening, which the index line carries:\n%s", carried)
	}
	if count := world.wanted(t, node); count != 1 {
		t.Fatalf("the fact that was carried is marked as used, and %d was", count)
	}
}

// A page of many facts shows the ones the question hit, not the ones
// that happen to be oldest.
//
// The store hands a page's facts over by number and the chooser asked it
// for five, so an expanded page showed its five oldest whatever had been
// asked. That was harmless while a page held a handful. On the pages
// this graph grew into -- fifty to ninety live facts -- the sentence the
// search had matched was almost never among the first five, and the
// loose-fact loop that would have carried it afterwards had neither a
// block nor a token left by then, the page blocks having spent both. The
// page was carried and the answer was not: the evaluation set read "did
// not carry work/portal saying August 27" while work/portal was right
// there in the overlay.
func TestAnExpandedPageShowsTheFactsTheQuestionHit(t *testing.T) {
	world := newRecallWorld(t)

	var written []string
	for number := 1; number <= 12; number++ {
		written = append(written, fmt.Sprintf("The portal decided thing number %d.", number))
	}
	node := world.page(t, "projects/portal", "Portal", "The customer-facing portal.", written...)
	facts := world.factsOf(t, node)
	// Late enough on the page that nothing but the search would ever
	// reach it.
	hit := facts[9]

	world.run.writeRecalled(context.Background(), []*models.AgentNode{node}, []*models.AgentFact{hit})

	carried := world.overlay()
	if !strings.Contains(carried, hit.Text) {
		t.Fatalf("the fact the question hit is carried:\n%s", carried)
	}
	// The fifth fact is what the old chooser showed in its place: old
	// enough to be among the first five by number, and nothing the
	// question asked about.
	if strings.Contains(carried, facts[4].Text) {
		t.Fatalf("and a fact the search did not hit, past the five, is not:\n%s", carried)
	}
	if count := strings.Count(carried, hit.Text); count != 1 {
		t.Fatalf("a fact its page carried is not repeated as a loose fact, and was carried %d times", count)
	}
	// Nothing carried is left unmarked and nothing marked is left
	// uncarried: the overlay and `used_at` are the same list.
	marked := world.markedFacts(t, node)
	if len(marked) != pageFacts {
		t.Fatalf("the page carried %d facts and marked %d", pageFacts, len(marked))
	}
	for _, fact := range marked {
		if !strings.Contains(carried, fact.Text) {
			t.Fatalf("%q was marked as used and is not in the overlay:\n%s", fact.Text, carried)
		}
	}
	if numbers := numbersShown(carried); len(numbers) != len(marked) {
		t.Fatalf("the overlay shows %d facts of the page and %d were marked", len(numbers), len(marked))
	}
}

// What a page shows is chosen by what the question hit and shown in
// number order.
//
// Selection is by relevance, presentation is by number. A block whose
// `#n` references jump about stops reading like a page, and those
// numbers are how the model cites back to the person what it was given.
func TestAnExpandedPageShowsItsFactsInNumberOrder(t *testing.T) {
	world := newRecallWorld(t)

	var written []string
	for number := 1; number <= 12; number++ {
		written = append(written, fmt.Sprintf("The portal decided thing number %d.", number))
	}
	node := world.page(t, "projects/portal", "Portal", "The customer-facing portal.", written...)
	facts := world.factsOf(t, node)
	// The search ranked the tenth fact above the seventh. Neither the
	// ranking nor the page's own numbering is allowed to be lost: the
	// ranking picks the five, the numbering lays them out.
	hits := []*models.AgentFact{facts[9], facts[6]}

	world.run.writeRecalled(context.Background(), []*models.AgentNode{node}, hits)

	carried := world.overlay()
	shown := numbersShown(carried)
	if want := []int{1, 2, 3, 7, 10}; fmt.Sprint(shown) != fmt.Sprint(want) {
		t.Fatalf("the page shows %v and should show %v:\n%s", shown, want, carried)
	}
}

// A page the search hit no fact on keeps what it always did: its first
// facts, by number.
func TestAPageTheSearchDidNotHitShowsItsFirstFacts(t *testing.T) {
	world := newRecallWorld(t)

	var written []string
	for number := 1; number <= 12; number++ {
		written = append(written, fmt.Sprintf("The portal decided thing number %d.", number))
	}
	node := world.page(t, "projects/portal", "Portal", "The customer-facing portal.", written...)

	world.run.writeRecalled(context.Background(), []*models.AgentNode{node}, nil)

	carried := world.overlay()
	shown := numbersShown(carried)
	if want := []int{1, 2, 3, 4, 5}; fmt.Sprint(shown) != fmt.Sprint(want) {
		t.Fatalf("the page shows %v and should show %v:\n%s", shown, want, carried)
	}
	if count := world.wanted(t, node); count != pageFacts {
		t.Fatalf("the %d facts it carried are marked as used, and %d were", pageFacts, count)
	}
}

// A sentence the page no longer says is not carried, however well it
// matches the question.
//
// A fact keeps its vector when it is struck or superseded: the row stays
// searchable so that what a page used to say can still be found. The
// search therefore hands back sentences that have been taken back, and
// recall is the side that knows to leave them out. Showing one inside a
// page's block would put words in the page's mouth that a person reading
// the page would not find there -- and the fact that reads best against
// a question is often exactly the one that was corrected.
func TestWhatThePageNoLongerSaysIsNotCarried(t *testing.T) {
	world := newRecallWorld(t)

	node := world.page(t, "projects/portal", "Portal", "The customer-facing portal.",
		"Ships on Fridays.", "Runs on the Frankfurt cluster.")
	facts := world.factsOf(t, node)
	if len(facts) != 2 {
		t.Fatalf("two facts to start with, got %d", len(facts))
	}
	var struck *models.AgentFact
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		// The row as the search would hand it over: read after the
		// strike, so it carries the mark the filter reads.
		if struck, err = tx.StrikeAgentFact(world.agent.ID, facts[0].ID, "the person said it was wrong"); err != nil {
			t.Fatalf("StrikeAgentFact: %s", err)
		}
	})
	if !struck.Dormant {
		t.Fatal("striking a fact marks it dormant")
	}

	// The search found it anyway, which is what its vector still being
	// there means, and offered it as the page's best match.
	world.run.writeRecalled(context.Background(), []*models.AgentNode{node}, []*models.AgentFact{struck})

	carried := world.overlay()
	if strings.Contains(carried, "Ships on Fridays.") {
		t.Fatalf("a struck fact is not carried:\n%s", carried)
	}
	if !strings.Contains(carried, "Runs on the Frankfurt cluster.") {
		t.Fatalf("and what the page does still say is:\n%s", carried)
	}
	for _, fact := range world.markedFacts(t, node) {
		if fact.ID == struck.ID {
			t.Fatal("a fact that was not carried is not marked as used")
		}
	}
}

// The fact a question matched outright is carried even when the pages
// above it would have spent the whole budget.
//
// The page search and the fact search are the fuzzy and the precise
// halves of one answer, and the fuzzy half used to be served first out of
// a single budget. Once the timeline filled in, a hundred month pages --
// long, prose, holding every ordinary word a question is made of --
// ranked above every real page and spent all twelve hundred tokens. The
// sentence that answered the question sat at the top of the fact search
// and never reached the prompt.
func TestAMatchedFactIsCarriedPastPagesThatWouldSpendItAll(t *testing.T) {
	world := newRecallWorld(t)

	// Pages that each fit and together do not, which is what a month
	// page is: prose about everything that happened.
	opening := strings.Repeat("a month of work. ", 12)
	line := strings.Repeat("what happened, at length. ", 6)
	var months []*models.AgentNode
	for index := 0; index < 5; index++ {
		months = append(months, world.page(t,
			fmt.Sprintf("time/2019/%02d", index+1), fmt.Sprintf("Month %d", index+1),
			opening, line, line, line, line, line))
	}

	answer := world.page(t, "work/webserver", "Webserver", "The web service.",
		"The API is served on a separate port because port 80 is taken.")
	matched := world.factsOf(t, answer)

	world.run.writeRecalled(context.Background(), months, matched)

	carried := world.overlay()
	if !strings.Contains(carried, "a separate port") {
		t.Fatalf("the fact the question matched is carried:\n%s", carried)
	}
	if count := len(world.markedFacts(t, answer)); count != 1 {
		t.Fatalf("and marked as used, and %d was", count)
	}
}

// A fact too long for what is left does not take the facts behind it
// with it. They are in the order the search ranked them, so what stood
// behind a long one was still the best of what the search found.
func TestALongFactDoesNotEndTheFactsBehindIt(t *testing.T) {
	world := newRecallWorld(t)

	// Long facts, each inside the thousand characters a fact may have,
	// until between them there is not room for another.
	long := strings.Repeat("a sentence somebody wrote at length. ", 27)
	var loose []*models.AgentFact
	for index := 0; index < 5; index++ {
		page := world.page(t, fmt.Sprintf("topics/long-%d", index), fmt.Sprintf("Long %d", index), "", long)
		loose = append(loose, world.factsOf(t, page)...)
	}
	answer := world.page(t, "work/webserver", "Webserver", "",
		"The API is served on a separate port because port 80 is taken.")
	loose = append(loose, world.factsOf(t, answer)...)

	world.run.writeRecalled(context.Background(), nil, loose)

	carried := world.overlay()
	if !strings.Contains(carried, "a separate port") {
		t.Fatalf("the short fact behind the long ones is carried:\n%s", carried)
	}
}

// A question is still answered from the graph once the evaluation carries
// the index a turn would.
//
// What changed is what a page costs: recall skips the opening of a page
// the prompt already holds and spends the room on its facts. The
// evaluation held none, so every page it recalled paid for its opening
// too, and a question was graded against less than a turn would have had.
//
// This does not test that. What a page cost is not in what RecallForQuestion
// answers -- a RecalledPage is a path and its facts -- so there is nothing
// here to assert it by, and a test written anyway would be a test of
// nothing. What it does hold is the other half: carrying the index changes
// what a page costs and never whether it is found.
func TestRecallingForAQuestionStillFindsTheAnswer(t *testing.T) {
	world := newRecallWorld(t)

	world.page(t, "projects/portal", "Portal", "The customer-facing portal.",
		"Runs on the Frankfurt cluster.")

	pages, err := world.run.agent.RecallForQuestion(context.Background(),
		world.agent, world.run.settings.Owner, "which cluster does the portal run on?")
	if err != nil {
		t.Fatalf("RecallForQuestion: %s", err)
	}
	// The page is still recalled, and its fact with it: carrying the index
	// changes what a page costs, never whether it is found.
	carried := ""
	for _, page := range pages {
		for _, fact := range page.Facts {
			carried += page.Path + " " + fact.Text + "\n"
		}
	}
	if !strings.Contains(carried, "Frankfurt") {
		t.Fatalf("the fact the question is about is still carried:\n%s", carried)
	}
}
