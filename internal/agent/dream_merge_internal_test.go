package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// mergeWorld is a page with numbered facts on it, for the merge the
// nightly rewrite makes.
type mergeWorld struct {
	database db.Database
	agent    *models.Agent
	page     *models.AgentNode
	facts    []*models.AgentFact
}

func newMergeWorld(t *testing.T, texts ...string) *mergeWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)

	world := &mergeWorld{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if world.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if world.page, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: world.agent.ID, Path: "things/marigold", Kind: models.NodeThing, Name: "Marigold",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for _, text := range texts {
			fact, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: world.agent.ID, NodeID: world.page.ID, Kind: models.FactPlain, Text: text,
			})
			if err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			world.facts = append(world.facts, fact)
		}
	})
	return world
}

// merge applies what a rewrite answered, and says how many pairs it made.
func (self *mergeWorld) merge(t *testing.T, same [][]int) int {
	t.Helper()
	merged := 0
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if merged, err = mergeSaidTwice(tx, self.agent.ID, self.facts, same); err != nil {
			t.Fatalf("mergeSaidTwice: %s", err)
		}
	})
	return merged
}

// stated is the numbers the page says now, in order.
func (self *mergeWorld) stated(t *testing.T) []int {
	t.Helper()
	var numbers []int
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(self.agent.ID, self.page.ID, false, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		for _, fact := range facts {
			numbers = append(numbers, fact.Number)
		}
	})
	return numbers
}

// behind is what every dormant row points at, by number.
func (self *mergeWorld) behind(t *testing.T) map[int]int {
	t.Helper()
	behind := map[int]int{}
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(self.agent.ID, self.page.ID, true, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		byId := map[string]*models.AgentFact{}
		for _, fact := range facts {
			byId[fact.ID] = fact
		}
		for _, fact := range facts {
			if fact.SupersededBy == "" {
				continue
			}
			into := byId[fact.SupersededBy]
			if into == nil {
				t.Fatalf("#%d stands behind a row that is not on the page", fact.Number)
			}
			behind[fact.Number] = into.Number
		}
	})
	return behind
}

// Two pairs that share a fact fold into the row that is still there.
//
// The pairs used to be applied against the numbering the model was shown,
// taken before any of them were made. [[5,3],[5,7]] folded 5 behind 3 and
// then folded 7 behind 5 -- a live statement put behind a row that had
// already left the page, so the page stated neither and a citation of
// either led nowhere.
func TestOverlappingMergesFoldIntoWhatIsLeft(t *testing.T) {
	world := newMergeWorld(t,
		"first", "second", "the boat is moored at Blakeney",
		"fourth", "she is moored at Blakeney", "sixth", "her mooring is at Blakeney")

	if merged := world.merge(t, [][]int{{5, 3}, {5, 7}}); merged != 2 {
		t.Fatalf("both pairs are made, not %d", merged)
	}

	// #3 is the lowest number of the three and is what everything else
	// stands behind: a citation of #3 still points at the statement.
	behind := world.behind(t)
	if behind[5] != 3 || behind[7] != 3 {
		t.Fatalf("both fold into the row still on the page: %v", behind)
	}
	for _, number := range world.stated(t) {
		if number == 5 || number == 7 {
			t.Fatalf("what was folded is off the page: %v", world.stated(t))
		}
	}
}

// And a chain of pairs ends at one row rather than at a row that has
// itself gone.
func TestChainedMergesEndAtOneRow(t *testing.T) {
	world := newMergeWorld(t,
		"she was repainted in spring", "a coat of paint each spring", "repainted every spring")

	if merged := world.merge(t, [][]int{{1, 2}, {2, 3}}); merged != 2 {
		t.Fatalf("both pairs are made, not %d", merged)
	}

	behind := world.behind(t)
	if behind[2] != 1 || behind[3] != 1 {
		t.Fatalf("the chain ends at #1, not %v", behind)
	}
	stated := world.stated(t)
	if len(stated) != 1 || stated[0] != 1 {
		t.Fatalf("the page states one of the three: %v", stated)
	}
	// The first pair said #1 was the better of it and #2, so the row
	// left carries #1's wording -- and the second pair, whose better half
	// was #2, follows #2 to that same row rather than reinstating what it
	// used to say.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(world.agent.ID, world.page.ID, false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("ListAgentFacts: %v %s", facts, err)
		}
		if facts[0].Text != "she was repainted in spring" {
			t.Fatalf("the surviving row keeps the wording the pairs chose: %q", facts[0].Text)
		}
	})
}

// A pair whose rows have both already gone is passed over rather than
// folded again.
func TestAMergeOfTwoRowsThatAreGoneIsSkipped(t *testing.T) {
	world := newMergeWorld(t, "one wording", "another wording", "a third wording")

	if merged := world.merge(t, [][]int{{1, 2}}); merged != 1 {
		t.Fatalf("the first pair is made, not %d", merged)
	}
	// Both of these now resolve to #1, so there is nothing left to fold.
	if merged := world.merge(t, [][]int{{2, 1}}); merged != 0 {
		t.Fatalf("a pair that is already one row is not folded again: %d", merged)
	}
	if stated := world.stated(t); len(stated) != 2 {
		t.Fatalf("the page still states #1 and #3: %v", stated)
	}
}
