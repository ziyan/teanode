package account_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/account"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// accountOperations records what the tool runs as the person and answers
// every document with an empty answer.
type accountOperations struct {
	documents []string
	variables []map[string]any
}

func (self *accountOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions(nil)
}

func (self *accountOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	self.documents = append(self.documents, document)
	self.variables = append(self.variables, variables)
	return json.Unmarshal([]byte(`{}`), response)
}

type accountRun struct {
	tools.Run
	operations *accountOperations
}

func (self *accountRun) Operations() tools.Operations         { return self.operations }
func (self *accountRun) Configuration() *config.Configuration { return config.Default() }
func (self *accountRun) Owner() *models.User                  { return &models.User{ID: "user-one", Username: "robin"} }

func accountTool(t *testing.T) *tools.Tool {
	t.Helper()
	for _, each := range tools.Build().All() {
		if each.Name == "account" {
			return each
		}
	}
	t.Fatal("account is not registered")
	return nil
}

func update(t *testing.T, operations *accountOperations, arguments string) error {
	t.Helper()
	ctx := tools.WithRun(context.Background(), &accountRun{operations: operations})
	_, err := accountTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
	return err
}

// The person says how they are described and the tool sends it to
// UpdateUser as the pronoun, for their own account; saying nothing sends
// nothing, and an empty one is sent so that it clears.
func TestUpdateSendsThePronounOfThePersonsOwnAccount(t *testing.T) {
	operations := &accountOperations{}
	if err := update(t, operations, `{"action":"update","pronoun":"she"}`); err != nil {
		t.Fatal(err)
	}
	if len(operations.documents) != 1 || !strings.Contains(operations.documents[0], "pronoun: $pronoun") {
		t.Fatalf("the update was %v", operations.documents)
	}
	if operations.variables[0]["userId"] != "user-one" || operations.variables[0]["pronoun"] != "she" {
		t.Errorf("the update sent %v", operations.variables[0])
	}

	if err := update(t, operations, `{"action":"update","pronoun":""}`); err != nil {
		t.Fatal(err)
	}
	if pronoun, isSent := operations.variables[1]["pronoun"]; !isSent || pronoun != "" {
		t.Errorf("an empty pronoun did not clear it: %v", operations.variables[1])
	}

	if err := update(t, operations, `{"action":"update","name":"Robin Park"}`); err != nil {
		t.Fatal(err)
	}
	if _, isSent := operations.variables[2]["pronoun"]; isSent {
		t.Errorf("a pronoun was sent that nobody gave: %v", operations.variables[2])
	}
}

// It is a write like the rest of the account's own details, and the card
// names the pronoun among what changes.
func TestUpdatingThePronounIsAWriteThatTheCardNames(t *testing.T) {
	tool := accountTool(t)
	if risk := tool.RiskFor([]byte(`{"action":"update","pronoun":"he"}`)); risk != tools.RiskWrite {
		t.Errorf("updating the pronoun is %v, not a write", risk)
	}
	if said := tool.Preview([]byte(`{"action":"update","pronoun":"he"}`)); !strings.Contains(said, "pronoun") {
		t.Errorf("the card %q does not name the pronoun", said)
	}
}

// The notification address is where the agent's mailed briefings and notices
// go, so pointing it somewhere else is judged like speaking for the person
// and raises a card, in a conversation and, unless they allowed it, in a run
// nobody is watching. The other fields stay ordinary writes.
func TestAccountUpdateEmailIsOutward(t *testing.T) {
	tool := accountTool(t)
	for arguments, wanted := range map[string]tools.Risk{
		`{"action":"update","email":"someone@example.org"}`:                tools.RiskOutward,
		`{"action":"update","name":"Robin","email":"someone@example.org"}`: tools.RiskOutward,
		`{"action":"update","name":"Robin"}`:                               tools.RiskWrite,
		`{"action":"update","locale":"ja","timezone":"Asia/Tokyo"}`:        tools.RiskWrite,
	} {
		if risk := tool.RiskFor([]byte(arguments)); risk != wanted {
			t.Errorf("%s is judged %q, wanted %q", arguments, risk, wanted)
		}
	}
	arguments := []byte(`{"action":"update","email":"someone@example.org"}`)
	if reasons := tools.ConfirmationReasons(tool, arguments, &config.AgentTools{}, &models.Agent{}); len(reasons) != 1 || reasons[0] != models.UnattendedRiskOutward {
		t.Fatalf("a new notification address asks first: %v", reasons)
	}
	arguments = []byte(`{"action":"update","name":"Robin"}`)
	if reasons := tools.ConfirmationReasons(tool, arguments, &config.AgentTools{}, &models.Agent{}); len(reasons) != 0 {
		t.Fatalf("a new name asks nothing: %v", reasons)
	}
}
