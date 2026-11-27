package subscription

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

type listOperations struct {
	variables map[string]any
}

func (self *listOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})
}

func (self *listOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	if strings.Contains(document, "ListMailboxSubscriptions") {
		self.variables = variables
		return json.Unmarshal([]byte(`{"ListMailboxSubscriptions":{"subscriptions":[{"key":"a","name":"A"},{"key":"b","name":"B"}],"total":5}}`), response)
	}
	return json.Unmarshal([]byte(`{"ListMailboxes":[{"mailbox":{"id":"mailbox-one","name":"Main","agent":{"granted":true}},"folders":[]}]}`), response)
}

type listRun struct {
	tools.Run
	operations *listOperations
}

func (self *listRun) Operations() tools.Operations { return self.operations }

// A page of the list says the total and where the next page starts, and asks
// the API from the offset it was given.
func TestListSaysTheTotalAndWhereToReadOn(test *testing.T) {
	operations := &listOperations{}
	ctx := tools.WithRun(context.Background(), &listRun{operations: operations})
	result, err := runSubscription(ctx, &tools.Call{ID: "call-one", Arguments: []byte(`{"action":"list","limit":2,"offset":2}`)})
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(result.Content, `"total":5`) || !strings.Contains(result.Content, `"next_offset":4`) {
		test.Fatalf("the list does not say the total and the next offset: %s", result.Content)
	}
	if operations.variables["offset"] != 2 {
		test.Fatalf("the API was asked from %v", operations.variables["offset"])
	}
}
