package memory_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// meaningRun knows what a search means, as a run with an embedding model
// does: the pages and facts it was given, nearest first, and nothing else,
// as a similarity floor leaves the weak matches out.
type meaningRun struct {
	*fakeRun
	nearNodes []*models.AgentNode
	nearFacts []*models.AgentFact
}

func (self *meaningRun) SearchGraphByMeaning(ctx context.Context, words string, limit int) ([]*models.AgentNode, []*models.AgentFact) {
	return self.nearNodes[:min(limit, len(self.nearNodes))], self.nearFacts[:min(limit, len(self.nearFacts))]
}

// A search that found more than it shows says how many more pages and
// facts there are and the call that reads on, and paging through it shows
// every page and fact once, in the order one long search ranks them.
//
// The search put what the meaning found in front of what the words found,
// and answered a partial result with a larger limit to ask for: there was
// no way to read the next twenty without reading the first twenty again.
func TestPagingThroughASearchShowsEveryPageAndFactOnce(t *testing.T) {
	base, database, closeDatabase := person(t, "searching")
	defer closeDatabase()
	run := &meaningRun{fakeRun: base}
	const nodeCount, factCount = 40, 75
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(base.agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		var shed *models.AgentNode
		for number := 1; number <= nodeCount; number++ {
			node, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: base.agent.ID, Path: fmt.Sprintf("things/jar-%d", number), Kind: models.NodeThing,
				Name: fmt.Sprintf("Jar %d", number),
			})
			if err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
			if number%5 == 0 {
				// Last first, so the meaning's order is not the words'.
				run.nearNodes = append([]*models.AgentNode{node}, run.nearNodes...)
			}
			shed = node
		}
		for number := 1; number <= factCount; number++ {
			fact, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: base.agent.ID, NodeID: shed.ID, Kind: models.FactPlain,
				Text: fmt.Sprintf("Shelf %d holds a jar of plum %d.", number, number), Confidence: 1,
			})
			if err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			if number%3 == 0 {
				run.nearFacts = append([]*models.AgentFact{fact}, run.nearFacts...)
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
	nodeLine := regexp.MustCompile(`(?m)^things/jar-\d+`)
	factLine := regexp.MustCompile(`(?m)^things/jar-\d+#\d+`)
	nextCall := regexp.MustCompile(`search again with offset: (\d+) and limit: 7$`)
	rows := func(content string) ([]string, []string) {
		facts := factLine.FindAllString(content, -1)
		isFact := map[string]bool{}
		for _, fact := range facts {
			isFact[strings.SplitN(fact, "#", 2)[0]] = true
		}
		var nodes []string
		for _, line := range strings.Split(content, "\n") {
			if node := nodeLine.FindString(line); node != "" && !strings.Contains(line, "#") {
				nodes = append(nodes, node)
			}
		}
		return nodes, facts
	}

	first := call(`{"action":"search","query":"jar","limit":7}`)
	if !strings.HasSuffix(first, "… 33 more pages and at least 68 more facts match; search again with offset: 7 and limit: 7") {
		t.Fatalf("the first page says how many more and the call that reads them: %q", first)
	}

	var pagedNodes, pagedFacts []string
	content := first
	for page := 0; page < 20; page++ {
		nodes, facts := rows(content)
		pagedNodes = append(pagedNodes, nodes...)
		pagedFacts = append(pagedFacts, facts...)
		next := nextCall.FindStringSubmatch(content)
		if next == nil {
			if strings.Contains(content, "more facts") {
				t.Fatalf("a page with more says how to read it: %q", content)
			}
			break
		}
		content = call(`{"action":"search","query":"jar","limit":7,"offset":` + next[1] + `}`)
	}

	whole := call(`{"action":"search","query":"jar","limit":200}`)
	if strings.Contains(whole, "more") {
		t.Fatalf("one search of everything says nothing more: %q", whole)
	}
	wholeNodes, wholeFacts := rows(whole)
	if len(wholeNodes) != nodeCount || len(wholeFacts) != factCount {
		t.Fatalf("one long search finds %d pages and %d facts, not %d and %d", len(wholeNodes), len(wholeFacts), nodeCount, factCount)
	}
	if strings.Join(pagedNodes, " ") != strings.Join(wholeNodes, " ") {
		t.Errorf("the pages show the pages once each in the long search's order:\n%v\n%v", pagedNodes, wholeNodes)
	}
	if strings.Join(pagedFacts, " ") != strings.Join(wholeFacts, " ") {
		t.Errorf("the pages show the facts once each in the long search's order:\n%v\n%v", pagedFacts, wholeFacts)
	}
	// What the meaning found comes first.
	if len(wholeNodes) > 0 && wholeNodes[0] != "things/jar-40" {
		t.Errorf("the nearest page by meaning is first, and %s is", wholeNodes[0])
	}

	past := call(`{"action":"search","query":"jar","offset":500}`)
	if !strings.Contains(past, "nothing past the first 500") {
		t.Errorf("a page past the end says so: %q", past)
	}
}
