package agentprofile_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/agentprofile"
	"github.com/ziyan/teanode/internal/models"
)

// alertOperations answers the alert documents as the API would, and keeps
// what it was sent.
type alertOperations struct {
	sent []sentDocument
}

type sentDocument struct {
	document  string
	variables map[string]any
}

func (self *alertOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})
}

func (self *alertOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	self.sent = append(self.sent, sentDocument{document: document, variables: variables})
	answer := `{}`
	switch {
	case strings.Contains(document, "ListAgentAlertMutes"):
		answer = `{"ListAgentAlertMutes":[{"id":"mute-1","muteScope":"domain","muteTarget":"shop.example.com"}]}`
	case strings.Contains(document, "ListAgentAlerts"):
		answer = `{"ListAgentAlerts":[{"id":"alert-latest"}]}`
	case strings.Contains(document, "MuteAgentAlert"):
		answer = `{"MuteAgentAlert":{"id":"mute-2","muteScope":"subjectKey","muteTarget":"photo app sign-in codes"}}`
	case strings.Contains(document, "UnmuteAgentAlert"):
		answer = `{"UnmuteAgentAlert":true}`
	case strings.Contains(document, "UpdateAgent"):
		answer = `{"UpdateAgent":{"agent":{"id":"agent-1"}}}`
	}
	return json.Unmarshal([]byte(answer), response)
}

// profileRun is a run with an agent and the operations above, and nothing
// else.
type profileRun struct {
	tools.Run
	operations *alertOperations
}

func (self *profileRun) Agent() *models.Agent         { return &models.Agent{ID: "agent-1"} }
func (self *profileRun) Owner() *models.User          { return &models.User{ID: "owner-1"} }
func (self *profileRun) Operations() tools.Operations { return self.operations }

// The person's word about alerts, said in the conversation, reaches the
// same operations as the settings page and the Mute button: off and on,
// "don't tell me about these" meaning the latest alert, a sender named,
// and a mute taken back by what it matches.
func TestAgentProfileAlertActions(t *testing.T) {
	var profile *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "agent_profile" {
			profile = each
		}
	}
	if profile == nil {
		t.Fatal("agent_profile is not registered")
	}
	call := func(arguments string) (*alertOperations, *tools.Result) {
		t.Helper()
		operations := &alertOperations{}
		result, err := profile.Run(tools.WithRun(context.Background(), &profileRun{operations: operations}), &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return operations, result
	}

	operations, _ := call(`{"action":"no_alerts"}`)
	if len(operations.sent) != 1 || !strings.Contains(operations.sent[0].document, "isAlertsEnabled: false") {
		t.Fatalf("no_alerts switches them off: %+v", operations.sent)
	}
	operations, _ = call(`{"action":"alerts_on"}`)
	if len(operations.sent) != 1 || !strings.Contains(operations.sent[0].document, "isAlertsEnabled: true") {
		t.Fatalf("alerts_on switches them on: %+v", operations.sent)
	}

	operations, result := call(`{"action":"mute_alert"}`)
	if len(operations.sent) != 2 || operations.sent[1].variables["alertId"] != "alert-latest" || operations.sent[1].variables["muteScope"] != nil {
		t.Fatalf("mute_alert mutes the latest alert by what it was about, the server's choice: %+v", operations.sent)
	}
	if !strings.Contains(result.Content, "photo app sign-in codes") {
		t.Fatalf("and says what it muted: %q", result.Content)
	}
	operations, _ = call(`{"action":"mute_alert","mute_scope":"sender","mute_target":"offers@shop.example.com"}`)
	if len(operations.sent) != 1 || operations.sent[0].variables["muteTarget"] != "offers@shop.example.com" || operations.sent[0].variables["alertId"] != nil {
		t.Fatalf("a target named is muted as named: %+v", operations.sent)
	}

	operations, _ = call(`{"action":"unmute_alert","mute_target":"Shop.example.com"}`)
	if len(operations.sent) != 2 || operations.sent[1].variables["muteId"] != "mute-1" {
		t.Fatalf("unmute_alert takes back the mute that matches: %+v", operations.sent)
	}
	operations = &alertOperations{}
	if _, err := profile.Run(tools.WithRun(context.Background(), &profileRun{operations: operations}), &tools.Call{ID: "c1", Arguments: []byte(`{"action":"unmute_alert","mute_target":"nothing.example.org"}`)}); err == nil || !strings.Contains(err.Error(), "shop.example.com") {
		t.Fatalf("a target nothing matches says what is muted: %v", err)
	}
}
