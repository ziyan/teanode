package idea_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/idea"
	"github.com/ziyan/teanode/internal/models"
)

// ideaOperations answers ListAgentIdeas as the API does, over ideaCount
// open ideas, each as long as a real one: a page of limit from offset,
// the fields the document asked for, and how many there are in all.
type ideaOperations struct {
	ideaCount int
	sent      []map[string]any
}

func (self *ideaOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})
}

func (self *ideaOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	self.sent = append(self.sent, variables)
	isWhole := strings.Contains(document, "openingRequest")
	ideas := make([]map[string]any, 0, self.ideaCount)
	for number := 1; number <= self.ideaCount; number++ {
		idea := map[string]any{
			"id": fmt.Sprintf("idea-%02d", number), "ideaKind": "personal", "ideaCategory": "home", "emoji": "🏠",
			"headline":         fmt.Sprintf("I can sort the receipts in drawer %d by month.", number),
			"neededToolNames":  []string{"mail_search"},
			"suggestionReason": "They keep paper receipts.",
			"ideaStatus":       "open", "expiredReason": "", "startedConversationId": "",
			"createdAt": "2030-01-02T03:04:05Z", "startedAt": nil, "closedAt": nil, "expiresAt": nil,
		}
		if isWhole {
			idea["body"] = strings.Repeat("Each receipt goes in the month it was paid, and I ask before throwing any away. ", 4)
			idea["openingRequest"] = "Please sort the receipts in that drawer by month and tell me what is missing."
			idea["evidence"] = []map[string]any{{"evidenceKind": "page", "evidenceId": "things/drawer", "evidenceSummary": "the drawer of receipts"}}
		}
		ideas = append(ideas, idea)
	}
	if wanted, isNarrowed := variables["ideaIds"].([]string); isNarrowed {
		narrowed := ideas[:0]
		for _, idea := range ideas {
			for _, ideaId := range wanted {
				if idea["id"] == ideaId {
					narrowed = append(narrowed, idea)
				}
			}
		}
		ideas = narrowed
	}
	totalCount := len(ideas)
	offset, _ := variables["offset"].(int)
	limit, _ := variables["limit"].(int)
	ideas = ideas[min(offset, len(ideas)):]
	nextOffset := 0
	if limit > 0 && len(ideas) > limit {
		ideas = ideas[:limit]
		nextOffset = offset + limit
	}
	encoded, err := json.Marshal(map[string]any{"ListAgentIdeas": map[string]any{"ideas": ideas, "totalCount": totalCount, "nextOffset": nextOffset}})
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, response)
}

// ideaRun is a run with the operations above and nothing else.
type ideaRun struct {
	tools.Run
	operations *ideaOperations
}

func (self *ideaRun) Operations() tools.Operations { return self.operations }

func ideaTool(t *testing.T) *tools.Tool {
	t.Helper()
	for _, each := range tools.Build().All() {
		if each.Name == "idea" {
			return each
		}
	}
	t.Fatal("idea is not registered")
	return nil
}

// A list of many ideas is a page of their headlines, open ones by default,
// that says how many more there are and the offset that reads them, and
// the next page carries on where it stopped; get gives one idea whole.
//
// A list of every idea, each with its body, opening request and evidence,
// came to more than twenty thousand characters, which a client cut short
// without saying where.
func TestAListOfManyIdeasIsAPageOfHeadlinesThatSaysHowToReadOn(t *testing.T) {
	operations := &ideaOperations{ideaCount: 35}
	ctx := tools.WithRun(context.Background(), &ideaRun{operations: operations})
	tool := ideaTool(t)
	call := func(arguments string) string {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return result.Content
	}

	first := call(`{"action":"list"}`)
	if statuses, _ := operations.sent[0]["ideaStatuses"].([]string); len(statuses) != 1 || statuses[0] != "open" {
		t.Errorf("a list asks for the open ideas: %v", operations.sent[0])
	}
	t.Logf("a first page of 35 ideas: %d characters", len(first))
	if len(first) > 6000 {
		t.Errorf("a first page of 35 ideas is %d characters", len(first))
	}
	if !strings.Contains(first, `"idea-15"`) || strings.Contains(first, `"idea-16"`) {
		t.Fatalf("a first page holds fifteen ideas: %s", first)
	}
	for _, leftOut := range []string{"body", "openingRequest", "evidence", "startedAt", "expiredReason", "ideaCategories"} {
		if strings.Contains(first, `"`+leftOut+`"`) {
			t.Errorf("a list leaves out %s: %s", leftOut, first)
		}
	}
	if !strings.Contains(first, `"totalCount":35`) || !strings.Contains(first, "20 more, and list again with offset: 15 reads them") {
		t.Fatalf("and says how many more and how to read them: %s", first)
	}

	next := call(`{"action":"list","offset":15}`)
	if !strings.Contains(next, `"idea-16"`) || strings.Contains(next, `"idea-15"`) || !strings.Contains(next, "list again with offset: 30") {
		t.Fatalf("the next page carries on where the first stopped: %s", next)
	}
	last := call(`{"action":"list","offset":30}`)
	if !strings.Contains(last, `"idea-35"`) || strings.Contains(last, "more") {
		t.Fatalf("the last page reaches the last idea and says nothing more: %s", last)
	}

	whole := call(`{"action":"get","idea_id":"idea-07"}`)
	if !strings.Contains(whole, `"openingRequest"`) || !strings.Contains(whole, `"evidence"`) || !strings.Contains(whole, "drawer 7 ") {
		t.Fatalf("get gives one idea whole: %s", whole)
	}
	if strings.Contains(whole, `"closedAt"`) {
		t.Errorf("without the times that have not come: %s", whole)
	}
}
