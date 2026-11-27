package rule

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

type applyOperations struct {
	asked []map[string]any
}

func (self *applyOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailboxManage}})
}

func (self *applyOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	if !strings.Contains(document, "ApplyMailboxRules") {
		return json.Unmarshal([]byte(`{"ListMailboxes":[{"mailbox":{"id":"mailbox-one","name":"Main","agent":{"granted":true}},"folders":[]}]}`), response)
	}
	copied := map[string]any{}
	for key, value := range variables {
		copied[key] = value
	}
	self.asked = append(self.asked, copied)
	// Three batches of 500: the first leaves 100 in the folder, the second
	// 50, and the third reads only 20.
	answers := []string{
		`{"ApplyMailboxRules":{"listed":500,"considered":500,"matched":400,"moved":400,"remaining":100}}`,
		`{"ApplyMailboxRules":{"listed":500,"considered":500,"matched":450,"moved":450,"remaining":50}}`,
		`{"ApplyMailboxRules":{"listed":20,"considered":20,"matched":0,"moved":0,"remaining":20}}`,
	}
	return json.Unmarshal([]byte(answers[len(self.asked)-1]), response)
}

type applyRun struct {
	tools.Run
	operations *applyOperations
}

func (self *applyRun) Operations() tools.Operations { return self.operations }

// With no limit the rules run over the whole folder, a batch after another,
// each batch skipping what the earlier ones left in the folder.
func TestApplyRunsOverTheWholeFolder(test *testing.T) {
	operations := &applyOperations{}
	ctx := tools.WithRun(context.Background(), &applyRun{operations: operations})
	result, err := runRuleApply(ctx, &tools.Call{ID: "call-one", Arguments: []byte(`{}`)})
	if err != nil {
		test.Fatal(err)
	}
	if len(operations.asked) != 3 {
		test.Fatalf("%d calls, wanted 3", len(operations.asked))
	}
	for index, wanted := range []any{nil, 100, 150} {
		if operations.asked[index]["offset"] != wanted {
			test.Errorf("call %d skipped %v, wanted %v", index, operations.asked[index]["offset"], wanted)
		}
	}
	if !strings.Contains(result.Content, `"considered":1020`) || !strings.Contains(result.Content, `"moved":850`) {
		test.Fatalf("the totals are wrong: %s", result.Content)
	}
}
