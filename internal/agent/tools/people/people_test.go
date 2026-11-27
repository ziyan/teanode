package people_test

import (
	"context"
	"encoding/json"
	"github.com/ziyan/teanode/internal/models"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/people"
)

// Another account's notification address is where its mailed notices go, so
// a new one asks first like the groups and the sign-in switch do; a new name
// stays an ordinary write.
func TestUserUpdateRisk(t *testing.T) {
	var user *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "user" {
			user = each
		}
	}
	if user == nil {
		t.Fatal("user is not registered")
	}
	for arguments, wanted := range map[string]tools.Risk{
		`{"action":"update","user_id":"user-two","email":"someone@example.org"}`: tools.RiskOutward,
		`{"action":"update","user_id":"user-two","name":"Robin"}`:                tools.RiskWrite,
		`{"action":"update","user_id":"user-two","group_ids":["group-one"]}`:     tools.RiskGranting,
		`{"action":"update","user_id":"user-two","disabled":true}`:               tools.RiskDestructive,
	} {
		if risk := user.RiskFor([]byte(arguments)); risk != wanted {
			t.Errorf("%s is judged %q, wanted %q", arguments, risk, wanted)
		}
	}
}

type auditOperations struct{ variables map[string]any }

func (self *auditOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAuditRead}})
}

func (self *auditOperations) Execute(_ context.Context, _ string, variables map[string]any, response any) error {
	self.variables = variables
	return json.Unmarshal([]byte(`{"ListAuditEvents":{"total":7,"events":[{"id":"event-one"},{"id":"event-two"}]}}`), response)
}

type auditRun struct {
	tools.Run
	operations *auditOperations
}

func (self *auditRun) Operations() tools.Operations { return self.operations }

// A page of the audit log says where the next one starts and asks the API
// from the offset it was given.
func TestAuditLogPages(t *testing.T) {
	var audit *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "audit_log" {
			audit = each
		}
	}
	if audit == nil {
		t.Fatal("audit_log is not registered")
	}
	operations := &auditOperations{}
	ctx := tools.WithRun(context.Background(), &auditRun{operations: operations})
	result, err := audit.Run(ctx, &tools.Call{ID: "call-one", Arguments: []byte(`{"limit":2,"offset":4}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, `"next_offset":6`) || operations.variables["offset"] != 4 {
		t.Fatalf("offset %v, result %s", operations.variables["offset"], result.Content)
	}
}
