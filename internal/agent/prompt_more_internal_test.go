package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A text cut for a prompt ends with an ellipsis and the call that reads
// the rest, and a text that fits is left as it is.
func TestACutTextSaysWhereTheRestIs(t *testing.T) {
	if got := cutWithMore("short", 10, "memory get things/shed"); got != "short" {
		t.Errorf("a text that fits: %q", got)
	}
	if got := cutWithMore("the shed holds the ladder", 9, "memory get things/shed"); got != "the shed… (more: memory get things/shed)" {
		t.Errorf("a cut text: %q", got)
	}
	if got := cutMarked("étagère étagère", 7); got != "étagère…" {
		t.Errorf("cut by character, marked: %q", got)
	}
}

// The self page carries selfFacts of its facts, and a page with more says
// how many more and how to list them; an opening past selfSummary says
// where the rest is.
func TestTheSelfPageBeyondItsLimitSaysThereIsMore(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")
	agentId := run.Agent.ID
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(agentId); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: models.PathSelf, Kind: models.NodePerson,
			Summary: strings.Repeat("They keep a garden. ", 150),
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for number := 1; number <= selfFacts+7; number++ {
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agentId, NodeID: node.ID, Kind: models.FactPlain, Confidence: 1,
				Text: fmt.Sprintf("They planted row %d.", number),
			}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
	})
	lines := worker.selfPageLines(t.Context(), run.Agent, run.Owner)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "… (more: memory get self)") {
		t.Errorf("a long opening says where the rest is: %q", joined)
	}
	if count := strings.Count(joined, "\n- They planted row"); count != selfFacts {
		t.Errorf("the self page carries %d facts, not %d", count, selfFacts)
	}
	if last := lines[len(lines)-1]; last != "(and 7 more facts on self, not shown here: memory get with path self and from 1 lists every one)" {
		t.Errorf("the self page ends with %q", last)
	}
}

// An index that leaves pages out for its budget says how many and how to
// list them.
func TestTheIndexSaysHowManyPagesItLeftOut(t *testing.T) {
	nodes := []*models.AgentNode{
		{Path: "things", Kind: models.NodeFolder},
		{Path: "things/shed", Kind: models.NodeThing, Summary: "Where the tools are kept."},
		{Path: "things/greenhouse", Kind: models.NodeThing, Summary: "Where the seedlings grow."},
		{Path: "things/pond", Kind: models.NodeThing, Summary: "Where the frogs live."},
	}
	lines, carried := indexLines(nodes, 12, false, true)
	if len(carried) != 1 || carried[0].Path != "things/shed" {
		t.Fatalf("the budget carried %v", lines)
	}
	if last := lines[len(lines)-1]; last != "(2 more pages are not listed here: the memory tool's index action lists every page, and get reads one)" {
		t.Errorf("the index ends with %q", last)
	}
	lines, _ = indexLines(nodes, 12, true, false)
	if last := lines[len(lines)-1]; last != "(at least 2 more pages are not listed here)" {
		t.Errorf("an index whose read stopped ends with %q", last)
	}
	if lines, _ = indexLines(nodes, 1000, false, true); strings.Contains(strings.Join(lines, "\n"), "more pages") {
		t.Errorf("an index that left nothing out says nothing: %q", lines)
	}
}

// What recall cuts to fit says where the rest is: a page's opening and a
// theme's reflection.
func TestARecalledTextCutShortSaysWhereTheRestIs(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")
	agentId := run.Agent.ID
	var page, theme *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if page, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: "things/shed", Kind: models.NodeThing, Name: "Shed",
			Summary: strings.Repeat("A shed with shelves for the tools. ", 40),
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if theme, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "themes/garden", Kind: models.NodeTopic, Name: "Garden"}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: agentId, NodeID: theme.ID, Kind: models.FactReflection, Inferred: true,
			Text: strings.Repeat("The garden tools are kept in order. ", 20),
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
	})
	turn := &AskRun{agent: worker, settings: &AskSettings{Agent: run.Agent, Owner: run.Owner}, promptMemories: map[string]bool{}}
	var blocks []*recalledBlock
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if blocks, err = turn.chooseRecalled(tx, []*models.AgentNode{page, theme}, nil, nil); err != nil {
			t.Fatalf("chooseRecalled: %s", err)
		}
	})
	if len(blocks) < 2 {
		t.Fatalf("recall carried %d blocks", len(blocks))
	}
	if !strings.Contains(blocks[0].Text, "… (more: memory get things/shed)") {
		t.Errorf("a cut opening says where the rest is: %q", blocks[0].Text)
	}
	if !strings.Contains(blocks[1].Text, "… (more: memory get themes/garden)") {
		t.Errorf("a cut reflection says where the rest is: %q", blocks[1].Text)
	}
}

// The filing run is shown the facts the conversation hit and then the
// newest, not the oldest, and is told how many more there are.
func TestTheFilingRunSeesTheFactsHitAndTheNewest(t *testing.T) {
	var facts []*models.AgentFact
	for number := 1; number <= 30; number++ {
		facts = append(facts, &models.AgentFact{ID: fmt.Sprint("fact-", number), Number: number})
	}
	chosen := factsForFiling(facts, map[string]bool{"fact-3": true}, 5)
	var numbers []int
	for _, fact := range chosen {
		numbers = append(numbers, fact.Number)
	}
	if fmt.Sprint(numbers) != "[3 27 28 29 30]" {
		t.Errorf("chosen %v", numbers)
	}
}

// A survey part cut for the combining call says so, and how much it left
// out.
func TestASurveyPartCutShortSaysSo(t *testing.T) {
	if got := surveyPartShown("short"); got != "short" {
		t.Errorf("a part that fits: %q", got)
	}
	got := surveyPartShown(strings.Repeat("a", surveyPartLength+42))
	if !strings.HasSuffix(got, "…\n(this part is cut here; 42 more characters of it are left out)") {
		t.Errorf("a cut part ends with %q", got[len(got)-80:])
	}
}
