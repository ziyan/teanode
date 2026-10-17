package circle_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/circle"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// sentOperation is one document the tool ran as the person.
type sentOperation struct {
	document  string
	variables map[string]any
}

// circleOperations records what the tool runs and answers every document
// with an empty answer, as the person holding the grants it was made with.
type circleOperations struct {
	grants []models.Grant
	sent   []sentOperation

	// answers is what a document is answered with instead of the empty
	// answer, by a word the document contains.
	answers map[string]string
}

func (self *circleOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions(self.grants)
}

func (self *circleOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	self.sent = append(self.sent, sentOperation{document: document, variables: variables})
	for word, answer := range self.answers {
		if strings.Contains(document, word) {
			return json.Unmarshal([]byte(answer), response)
		}
	}
	return json.Unmarshal([]byte(`{}`), response)
}

func (self *circleOperations) added() []sentOperation {
	var added []sentOperation
	for _, operation := range self.sent {
		if strings.Contains(operation.document, "AddCircleMember") {
			added = append(added, operation)
		}
	}
	return added
}

type circleRun struct {
	tools.Run
	operations *circleOperations
}

func (self *circleRun) Operations() tools.Operations         { return self.operations }
func (self *circleRun) Configuration() *config.Configuration { return config.Default() }

func circleTool(t *testing.T) *tools.Tool {
	t.Helper()
	for _, each := range tools.Build().All() {
		if each.Name == "circle" {
			return each
		}
	}
	t.Fatal("circle is not registered")
	return nil
}

func callAs(t *testing.T, operations *circleOperations, arguments string) (*tools.Result, error) {
	t.Helper()
	ctx := tools.WithRun(context.Background(), &circleRun{operations: operations})
	return circleTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
}

var (
	anOperator = []models.Grant{{Permission: models.PermissionUserManage}, {Permission: models.PermissionCircleUse}}
	aMember    = []models.Grant{{Permission: models.PermissionCircleUse}}
)

// For somebody who manages accounts, add puts the account in the circle with
// AddCircleMember, as a member unless told otherwise, with no invitation.
func TestAddPutsAnAccountInACircleWithoutAnInvitation(t *testing.T) {
	operations := &circleOperations{grants: anOperator}

	if _, err := callAs(t, operations, `{"action":"add","circle_id":" circle-one ","username":" personx "}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callAs(t, operations, `{"action":"add","circle_id":"circle-two","username":"persony","member_role":"admin"}`); err != nil {
		t.Fatal(err)
	}

	added := operations.added()
	if len(added) != 2 {
		t.Fatalf("two additions were asked for: %v", operations.sent)
	}
	if added[0].variables["circleId"] != "circle-one" || added[0].variables["username"] != "personx" || added[0].variables["memberRole"] != "member" {
		t.Errorf("the first addition was %v", added[0].variables)
	}
	if added[1].variables["circleId"] != "circle-two" || added[1].variables["username"] != "persony" || added[1].variables["memberRole"] != "admin" {
		t.Errorf("the second addition was %v", added[1].variables)
	}
	for _, operation := range operations.sent {
		if strings.Contains(operation.document, "InviteToCircle") {
			t.Errorf("an invitation was sent: %s", operation.document)
		}
	}
}

// A person who can use circles but not manage accounts is refused, and
// nothing is sent to the API.
func TestAddIsRefusedToSomebodyWhoCannotManageAccounts(t *testing.T) {
	operations := &circleOperations{grants: aMember}

	_, err := callAs(t, operations, `{"action":"add","circle_id":"circle-one","username":"personx"}`)
	if err == nil || !strings.Contains(err.Error(), "not something they may do") {
		t.Fatalf("add should be refused: %v", err)
	}
	if len(operations.sent) != 0 {
		t.Errorf("the API was asked: %v", operations.sent)
	}
}

// Adding is granting, so the person is asked first, and the card says there
// is no invitation to accept.
func TestAddIsGrantingAndSaysSoOnItsCard(t *testing.T) {
	tool := circleTool(t)

	if risk := tool.RiskFor([]byte(`{"action":"add","circle_id":"circle-one","username":"personx"}`)); risk != tools.RiskGranting {
		t.Errorf("add is %v, not granting", risk)
	}
	said := tool.Preview([]byte(`{"action":"add","circle_id":"circle-one","username":"personx","member_role":"admin"}`))
	for _, wanted := range []string{`"personx"`, "no invitation", "admin"} {
		if !strings.Contains(said, wanted) {
			t.Errorf("the card %q does not say %q", said, wanted)
		}
	}
}

// Joining a family circle, the answer says the person can share their
// calendar and where they are with it, and with which tool, for what they may
// use. A circle of colleagues is only the calendar, friends nothing, and
// declining is only declined.
func TestAnsweringAnInvitationSaysWhatCanBeSharedWithTheCircle(t *testing.T) {
	familyGrants := append([]models.Grant{{Permission: models.PermissionCalendarUse}, {Permission: models.PermissionLocationUse}}, aMember...)
	for _, test := range []struct {
		name     string
		grants   []models.Grant
		kind     string
		accepted string
		contains []string
		omits    []string
	}{
		{"family with both", familyGrants, "family", "true",
			[]string{"joined the circle", "their calendar, with the calendar tool's share action", "where they are, with the location tool's share action", "circle-one", "share only if they say yes"}, nil},
		{"family without location", append([]models.Grant{{Permission: models.PermissionCalendarUse}}, aMember...), "family", "true",
			[]string{"their calendar"}, []string{"location tool"}},
		{"family with neither", aMember, "family", "true", []string{"joined the circle"}, []string{"share"}},
		{"colleagues", familyGrants, "colleagues", "true",
			[]string{"their calendar"}, []string{"location tool"}},
		{"friends", familyGrants, "friends", "true", []string{"joined the circle"}, []string{"share"}},
		{"declined", familyGrants, "family", "false", []string{"declined the invitation"}, []string{"share", "joined"}},
	} {
		operations := &circleOperations{grants: test.grants, answers: map[string]string{
			"AnswerCircleInvitation": `{"AnswerCircleInvitation": {"id": "circle-one", "name": "Example", "circleKind": "` + test.kind + `"}}`,
		}}
		result, err := callAs(t, operations, `{"action":"answer","invitation_id":"invitation-one","is_accepted":`+test.accepted+`}`)
		if err != nil {
			t.Fatalf("%s: %s", test.name, err)
		}
		for _, wanted := range test.contains {
			if !strings.Contains(result.Content, wanted) {
				t.Errorf("%s: the answer %q does not say %q", test.name, result.Content, wanted)
			}
		}
		for _, unwanted := range test.omits {
			if strings.Contains(result.Content, unwanted) {
				t.Errorf("%s: the answer %q says %q", test.name, result.Content, unwanted)
			}
		}
	}
}
