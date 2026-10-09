package memory_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A page with more facts than a get shows says how many more, and the get
// that reads on from where it stopped reaches the last of them.
//
// A get stopped at sixty facts with nothing to say there were more, and a
// model read the sixty as all the page knew.
func TestAGetPastItsFactsSaysHowManyMoreAndHowToReadThem(t *testing.T) {
	run, database, closeDatabase := person(t, "paging")
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(run.agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		node, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.agent.ID, Path: "things/shed", Kind: models.NodeThing, Name: "Shed"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for number := 1; number <= 75; number++ {
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: run.agent.ID, NodeID: node.ID, Kind: models.FactPlain,
				Text: fmt.Sprintf("Shelf %d holds jar %d.", number, number), Confidence: 1,
			}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")
	call := func(arguments string) string {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return result.Content
	}

	first := call(`{"action":"get","path":"things/shed"}`)
	if !strings.Contains(first, "15 more facts on this page are not shown") || !strings.Contains(first, "get again with from: 1") {
		t.Fatalf("a get of a long page says how many more and how to list them: %q", first)
	}

	byNumber := call(`{"action":"get","path":"things/shed","from":1}`)
	if !strings.Contains(byNumber, "#1 Shelf 1 holds jar 1.") || strings.Contains(byNumber, "#61 ") {
		t.Fatalf("from 1 lists the first sixty by number: %q", byNumber)
	}
	if !strings.Contains(byNumber, "… 15 more facts after #60; get again with from: 61") {
		t.Fatalf("and says where to read on: %q", byNumber)
	}

	rest := call(`{"action":"get","path":"things/shed","from":61}`)
	if !strings.Contains(rest, "#75 Shelf 75 holds jar 75.") || strings.Contains(rest, "more facts") {
		t.Fatalf("the next part reaches the last fact and says nothing more: %q", rest)
	}
}

// An index that stops at its limit says how many more lines there are and
// the offset that reads on.
func TestAnIndexPastItsLimitSaysWhereToReadOn(t *testing.T) {
	run, database, closeDatabase := person(t, "indexing")
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(run.agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		for number := 1; number <= 5; number++ {
			if _, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: run.agent.ID, Path: fmt.Sprintf("things/box-%d", number), Kind: models.NodeThing,
			}); err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
		}
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")
	result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"index","path":"things","limit":2}`)})
	if err != nil {
		t.Fatalf("index: %s", err)
	}
	// The folder itself is the first line, and its five pages the rest.
	if !strings.Contains(result.Content, "… 4 more; index again with offset: 2") {
		t.Fatalf("the index says how many more and where to read on: %q", result.Content)
	}
	result, err = tool.Run(ctx, &tools.Call{ID: "c2", Arguments: []byte(`{"action":"index","path":"things","limit":2,"offset":5}`)})
	if err != nil {
		t.Fatalf("index: %s", err)
	}
	if !strings.Contains(result.Content, "things/box-5") || strings.Contains(result.Content, "more") {
		t.Fatalf("the last part has the last page and nothing more: %q", result.Content)
	}
}

// A page that many pages link to lists the first of its links, says how
// many more there are, and a get with link_offset reads on from there to
// the last of them.
//
// A get listed every link, and a page that a thousand pages pointed at
// came to more than fifty thousand characters, which a client cut short
// without saying where.
func TestAGetPastItsLinksSaysHowManyMoreAndHowToReadThem(t *testing.T) {
	run, database, closeDatabase := person(t, "linking")
	defer closeDatabase()
	const linkCount = 300
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(run.agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		harbor, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.agent.ID, Path: "places/harbor", Kind: models.NodePlace, Name: "Harbor"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for number := 1; number <= linkCount; number++ {
			boat, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: run.agent.ID, Path: fmt.Sprintf("things/boat-%03d", number), Kind: models.NodeThing,
			})
			if err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: run.agent.ID, FromID: boat.ID, ToID: harbor.ID, Relation: models.EdgeLocatedIn,
			}); err != nil {
				t.Fatalf("PutAgentEdge: %s", err)
			}
		}
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")
	call := func(arguments string) string {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return result.Content
	}

	first := call(`{"action":"get","path":"places/harbor"}`)
	if len(first) > 4000 {
		t.Errorf("a get of a page with %d links is %d characters", linkCount, len(first))
	}
	if !strings.Contains(first, "← things/boat-040 located_in this") || strings.Contains(first, "boat-041") {
		t.Fatalf("a get lists the first forty links: %q", first)
	}
	if !strings.Contains(first, "… 260 more links; get again with link_offset: 40") {
		t.Fatalf("and says how many more and how to read them: %q", first)
	}

	next := call(`{"action":"get","path":"places/harbor","link_offset":40}`)
	if !strings.Contains(next, "links 41 to 80 of 300:") || !strings.Contains(next, "boat-041 ") || strings.Contains(next, "boat-040 ") {
		t.Fatalf("link_offset reads on from where the get stopped: %q", next)
	}
	if !strings.Contains(next, "… 220 more links; get again with link_offset: 80") {
		t.Fatalf("and says where to read on again: %q", next)
	}

	last := call(`{"action":"get","path":"places/harbor","link_offset":280}`)
	if !strings.Contains(last, "boat-300 ") || strings.Contains(last, "more links") {
		t.Fatalf("the last part reaches the last link and says nothing more: %q", last)
	}
}
