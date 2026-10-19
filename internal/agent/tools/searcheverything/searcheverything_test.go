package searcheverything_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/searcheverything"
	"github.com/ziyan/teanode/internal/models"
)

// searchOperations answers SearchEverything with a mail group that has more
// and a contacts group, and records what it was sent.
type searchOperations struct {
	sent []map[string]any
}

func (self *searchOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})
}

func (self *searchOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	self.sent = append(self.sent, variables)
	encoded, err := json.Marshal(map[string]any{"SearchEverything": map[string]any{
		"words":       variables["words"],
		"failedAreas": []string{"photos"},
		"areas": []map[string]any{
			{"searchArea": "mail", "hasMore": true, "results": []map[string]any{
				{"searchArea": "mail", "targetId": "item-1", "titleText": "Harbor schedule", "detailText": "Ada Example", "happenedAt": "2030-03-01T09:30:00Z"},
			}},
			{"searchArea": "contacts", "hasMore": false, "results": []map[string]any{
				{"searchArea": "contacts", "targetId": "contact-1", "titleText": "Harbor Office", "detailText": "", "happenedAt": nil},
			}},
		},
	}})
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, response)
}

type searchRun struct {
	tools.Run
	operations *searchOperations
}

func (self *searchRun) Operations() tools.Operations { return self.operations }
func (self *searchRun) Owner() *models.User          { return &models.User{Timezone: "UTC"} }

func searchTool(test *testing.T) *tools.Tool {
	test.Helper()
	for _, each := range tools.Build().All() {
		if each.Name == "search" {
			return each
		}
	}
	test.Fatal("search is not registered")
	return nil
}

// The tool is a read, sends the arguments as the operation takes them, and
// answers a group per area with the tool that finds more of it, what did not
// answer, and the results marked as written by somebody else.
func TestSearchAnswersAGroupPerAreaAndNamesTheToolForMore(test *testing.T) {
	operations := &searchOperations{}
	ctx := tools.WithRun(context.Background(), &searchRun{operations: operations})
	tool := searchTool(test)
	if tool.Risk != tools.RiskRead {
		test.Errorf("the risk is %q, want read", tool.Risk)
	}

	result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"words":" harbor ","areas":["mail","contacts"],"per_area_limit":3}`)})
	if err != nil {
		test.Fatal(err)
	}
	sent := operations.sent[0]
	if sent["words"] != "harbor" || sent["perAreaLimit"] != 3 || len(sent["areas"].([]string)) != 2 {
		test.Errorf("the operation was sent %v", sent)
	}
	var answer struct {
		Words       string   `json:"words"`
		FailedAreas []string `json:"failed_areas"`
		Areas       []struct {
			Area    string `json:"area"`
			HasMore bool   `json:"has_more"`
			Tool    string `json:"tool"`
			Results []struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Detail   string `json:"detail"`
				Happened string `json:"happened"`
			} `json:"results"`
		} `json:"areas"`
	}
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		test.Fatalf("%s: %s", err, result.Content)
	}
	if len(answer.Areas) != 2 || answer.Areas[0].Area != "mail" || !answer.Areas[0].HasMore || answer.Areas[0].Tool != "mail_search" || answer.Areas[1].Tool != "contact_book" {
		test.Fatalf("the areas: %s", result.Content)
	}
	if first := answer.Areas[0].Results[0]; first.ID != "item-1" || first.Title != "Harbor schedule" || first.Detail != "Ada Example" || !strings.HasPrefix(first.Happened, "2030-03-01") {
		test.Errorf("the first result: %+v", first)
	}
	if len(answer.FailedAreas) != 1 || answer.FailedAreas[0] != "photos" {
		test.Errorf("failed areas: %v", answer.FailedAreas)
	}
	if !result.Untrusted {
		test.Error("what was found is the person's mail and messages, and was not marked as data")
	}
}

// Nothing to look for is said, not sent.
func TestSearchWithNoWordsIsRefused(test *testing.T) {
	operations := &searchOperations{}
	ctx := tools.WithRun(context.Background(), &searchRun{operations: operations})
	if _, err := searchTool(test).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"words":"  "}`)}); err == nil || len(operations.sent) != 0 {
		test.Errorf("blank words were sent: %v %v", err, operations.sent)
	}
}
