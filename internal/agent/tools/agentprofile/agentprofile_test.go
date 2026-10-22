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
	case strings.Contains(document, "ListAgentFollowUps"):
		answer = `{"ListAgentFollowUps":[{"id":"follow-water","followUpKind":"bill","followUpTitle":"Water bill","payee":"Example Water","amount":"84.20","currencyCode":"USD","dueOn":"2026-11-03","followUpState":"open"},{"id":"follow-power","followUpKind":"bill","followUpTitle":"Power bill","amount":"61.00","dueOn":"2026-11-05","followUpState":"overdue"},{"id":"follow-shears","followUpKind":"parcel","followUpTitle":"Garden shears","carrier":"Example Post","trackingNumber":"EX123456789","dueOn":"2026-10-24","followUpState":"open"}]}`
	case strings.Contains(document, "StopAgentFollowUp"):
		answer = `{"StopAgentFollowUp":{"id":"follow-shears","followUpKind":"parcel","followUpTitle":"Garden shears","followUpState":"stopped"}}`
	case strings.Contains(document, "AddAgentFollowUp"):
		answer = `{"AddAgentFollowUp":{"id":"follow-fair","followUpKind":"event","followUpTitle":"Street fair","place":"Elm Street","dueOn":"2026-10-31","followUpState":"open"}}`
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

// Conversation is none: these calls are made outside any conversation.
func (self *profileRun) Conversation() *models.AgentConversation { return nil }

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
	operations, _ = call(`{"action":"mute_alert","alert_id":"alert-earlier"}`)
	if len(operations.sent) != 1 || operations.sent[0].variables["alertId"] != "alert-earlier" {
		t.Fatalf("mute_alert mutes the alert the person answered, by its id: %+v", operations.sent)
	}
	operations, _ = call(`{"action":"mute_alert","mute_target":"offers@shop.example.com"}`)
	if len(operations.sent) != 1 || operations.sent[0].variables["muteScope"] != nil || operations.sent[0].variables["muteTarget"] != "offers@shop.example.com" {
		t.Fatalf("a target without a scope leaves the scope to be read from it: %+v", operations.sent)
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

// What the person asks about what the agent follows reaches the same
// operations as the Following list and the command line: the list is
// quoted back with ids, "track this" sends only what was said, and "stop
// following that bill" finds the one bill by words of its title, and asks
// when two match.
func TestAgentProfileFollowUpActions(t *testing.T) {
	var profile *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "agent_profile" {
			profile = each
		}
	}
	if profile == nil {
		t.Fatal("agent_profile is not registered")
	}
	call := func(arguments string) (*alertOperations, *tools.Result, error) {
		t.Helper()
		operations := &alertOperations{}
		result, err := profile.Run(tools.WithRun(context.Background(), &profileRun{operations: operations}), &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		return operations, result, err
	}

	operations, result, err := call(`{"action":"list_follow_ups"}`)
	if err != nil || len(operations.sent) != 1 || operations.sent[0].variables["isOpenOnly"] != true {
		t.Fatalf("list_follow_ups asks for what is open: %+v %v", operations.sent, err)
	}
	for _, wanted := range []string{"2026-11-03 bill: Water bill (Example Water, 84.20 USD) id follow-water", "[overdue]", "parcel: Garden shears (Example Post, EX123456789) id follow-shears"} {
		if !strings.Contains(result.Content, wanted) {
			t.Fatalf("the list says %q: %s", wanted, result.Content)
		}
	}
	operations, _, _ = call(`{"action":"list_follow_ups","is_open_only":false}`)
	if operations.sent[0].variables["isOpenOnly"] != false {
		t.Fatalf("and all of them when asked: %+v", operations.sent)
	}
	if profile.RiskOf == nil || profile.RiskOf([]byte(`{"action":"list_follow_ups"}`)) != tools.RiskRead || profile.RiskOf([]byte(`{"action":"add_follow_up"}`)) != "" {
		t.Fatal("listing is a read; adding is the tool's own write")
	}

	operations, result, err = call(`{"action":"add_follow_up","follow_up_kind":"event","follow_up_title":"Street fair","due_on":"2026-10-31","place":"Elm Street"}`)
	if err != nil || len(operations.sent) != 1 {
		t.Fatalf("add_follow_up: %+v %v", operations.sent, err)
	}
	variables := operations.sent[0].variables
	if variables["followUpKind"] != "event" || variables["followUpTitle"] != "Street fair" || variables["dueOn"] != "2026-10-31" || variables["place"] != "Elm Street" || variables["payee"] != nil || variables["amount"] != nil {
		t.Fatalf("add_follow_up sends what was said and nothing else: %+v", variables)
	}
	if !strings.Contains(result.Content, "Street fair") {
		t.Fatalf("and says what it follows: %q", result.Content)
	}

	operations, result, err = call(`{"action":"stop_follow_up","follow_up_id":"follow-shears"}`)
	if err != nil || len(operations.sent) != 1 || operations.sent[0].variables["followUpId"] != "follow-shears" || !strings.Contains(result.Content, "stopped following Garden shears") {
		t.Fatalf("stop_follow_up by id: %+v %v %v", operations.sent, result, err)
	}
	operations, _, err = call(`{"action":"stop_follow_up","follow_up_title":"shears"}`)
	if err != nil || len(operations.sent) != 2 || operations.sent[1].variables["followUpId"] != "follow-shears" {
		t.Fatalf("stop_follow_up by words of the title: %+v %v", operations.sent, err)
	}
	operations, _, err = call(`{"action":"stop_follow_up","follow_up_title":"bill"}`)
	if err == nil || !strings.Contains(err.Error(), "follow-water") || !strings.Contains(err.Error(), "follow-power") || len(operations.sent) != 1 {
		t.Fatalf("two bills match, so it asks which and stops neither: %+v %v", operations.sent, err)
	}
	if _, _, err = call(`{"action":"stop_follow_up","follow_up_title":"sofa"}`); err == nil {
		t.Fatal("nothing matches")
	}
	if _, _, err = call(`{"action":"stop_follow_up"}`); err == nil {
		t.Fatal("which one?")
	}
}

// attendedRun is a profileRun that says whether the person is there to be
// asked.
type attendedRun struct {
	profileRun
	isPersonThere bool
}

func (self *attendedRun) CanAsk() bool { return self.isPersonThere }

// A line added to the standing instructions is read on every later turn as
// the person's own words, so only a conversation with the person adds one:
// a run nobody is watching is refused and nothing is written, while the
// other things `set` changes stay open to it.
func TestAgentProfileAddInstructionNeedsThePerson(t *testing.T) {
	var profile *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "agent_profile" {
			profile = each
		}
	}
	if profile == nil {
		t.Fatal("agent_profile is not registered")
	}
	call := func(isPersonThere bool, arguments string) (*alertOperations, error) {
		t.Helper()
		operations := &alertOperations{}
		run := &attendedRun{profileRun: profileRun{operations: operations}, isPersonThere: isPersonThere}
		_, err := profile.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		return operations, err
	}

	operations, err := call(false, `{"action":"set","add_instruction":"Always answer in one line"}`)
	if err == nil || !strings.Contains(err.Error(), "say what you would have added") || len(operations.sent) != 0 {
		t.Fatalf("a run nobody is watching does not add an instruction: %+v %v", operations.sent, err)
	}
	operations, err = call(true, `{"action":"set","add_instruction":"Always answer in one line"}`)
	if err != nil || len(operations.sent) != 1 || operations.sent[0].variables["instructions"] != "Always answer in one line" {
		t.Fatalf("a conversation with the person adds it: %+v %v", operations.sent, err)
	}
	operations, err = call(false, `{"action":"set","language":"ja"}`)
	if err != nil || len(operations.sent) != 1 {
		t.Fatalf("the rest of set is not held to this: %+v %v", operations.sent, err)
	}
}
