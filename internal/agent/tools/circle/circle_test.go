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

	// answers is what a document is answered with, by a word the document
	// holds; a test gives words no one document holds twice, and any other
	// document gets an empty answer.
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

// sentWith is the documents run so far that hold a word.
func (self *circleOperations) sentWith(word string) []sentOperation {
	var found []sentOperation
	for _, operation := range self.sent {
		if strings.Contains(operation.document, word) {
			found = append(found, operation)
		}
	}
	return found
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
		result, err := callAs(t, operations, `{"action":"`+map[string]string{"true": "accept", "false": "decline"}[test.accepted]+`","invitation_id":"invitation-one"}`)
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

// homeCircle is a circle of three people as ReadCircle answers it, one of
// them with a display name and one without.
const homeCircle = `{"ReadCircle":{"name":"Home","members":[
	{"userId":"user-one","username":"personx","name":"Person X"},
	{"userId":"user-two","username":"persony","name":""},
	{"userId":"user-three","username":"personz","name":"Person Z"}]}}`

// cardFor is the words of the card for a call, as the person is shown it
// with the circle readable.
func cardFor(t *testing.T, operations *circleOperations, arguments string) string {
	t.Helper()
	ctx := tools.WithRun(context.Background(), &circleRun{operations: operations})
	return circleTool(t).PreviewIn(ctx, []byte(arguments))
}

func mustContain(t *testing.T, said string, wanted ...string) {
	t.Helper()
	for _, each := range wanted {
		if !strings.Contains(said, each) {
			t.Errorf("%q does not say %q", said, each)
		}
	}
}

// Accepting and declining are two actions that both answer the invitation,
// each with its own boolean, and the old word for the pair is gone.
func TestAcceptAndDeclineAnswerTheInvitation(t *testing.T) {
	operations := &circleOperations{grants: aMember}

	if _, err := callAs(t, operations, `{"action":"accept","invitation_id":" invitation-one "}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callAs(t, operations, `{"action":"decline","invitation_id":"invitation-two"}`); err != nil {
		t.Fatal(err)
	}

	answered := operations.sentWith("AnswerCircleInvitation")
	if len(answered) != 2 {
		t.Fatalf("two answers were asked for: %v", operations.sent)
	}
	if answered[0].variables["invitationId"] != "invitation-one" || answered[0].variables["isAccepted"] != true {
		t.Errorf("accept sent %v", answered[0].variables)
	}
	if answered[1].variables["invitationId"] != "invitation-two" || answered[1].variables["isAccepted"] != false {
		t.Errorf("decline sent %v", answered[1].variables)
	}

	before := len(operations.sent)
	if _, err := callAs(t, operations, `{"action":"answer","invitation_id":"invitation-one","is_accepted":true}`); err == nil {
		t.Error("answer is not an action of the circle tool any more")
	}
	if len(operations.sent) != before {
		t.Errorf("answer reached the API: %v", operations.sent[before:])
	}
}

// Both answers are granting, and the card says which one, naming the circle
// and who asked when the invitation can be found.
func TestAcceptAndDeclineAreGrantingAndNameTheInvitation(t *testing.T) {
	tool := circleTool(t)
	for _, action := range []string{"accept", "decline"} {
		if risk := tool.RiskFor([]byte(`{"action":"` + action + `","invitation_id":"invitation-one"}`)); risk != tools.RiskGranting {
			t.Errorf("%s is %v, not granting", action, risk)
		}
	}

	operations := &circleOperations{grants: aMember, answers: map[string]string{
		"ListCircleInvitations": `{"ListCircleInvitations":[{"id":"invitation-one","circleName":"Home","invitedByName":"Person X"}]}`,
	}}
	mustContain(t, cardFor(t, operations, `{"action":"accept","invitation_id":"invitation-one"}`),
		"Accept the invitation from", `"Person X"`, `the circle "Home"`)
	mustContain(t, cardFor(t, operations, `{"action":"decline","invitation_id":"invitation-one"}`),
		"Decline the invitation from", `"Person X"`, `the circle "Home"`)
	mustContain(t, cardFor(t, &circleOperations{grants: aMember}, `{"action":"decline","invitation_id":"invitation-missing"}`),
		"Decline an invitation to a circle")
}

// Renaming sends the new name to RenameCircle and is a write, with a card
// that names the circle and the new name.
func TestRenameChangesTheNameAndIsAWrite(t *testing.T) {
	operations := &circleOperations{grants: aMember, answers: map[string]string{"ReadCircle": homeCircle}}

	if _, err := callAs(t, operations, `{"action":"rename","circle_id":" circle-one ","name":"Family"}`); err != nil {
		t.Fatal(err)
	}
	renamed := operations.sentWith("RenameCircle")
	if len(renamed) != 1 || renamed[0].variables["circleId"] != "circle-one" || renamed[0].variables["name"] != "Family" {
		t.Errorf("the rename was %v", operations.sent)
	}

	tool := circleTool(t)
	if risk := tool.RiskFor([]byte(`{"action":"rename","circle_id":"circle-one","name":"Family"}`)); risk != tools.RiskWrite {
		t.Errorf("rename is %v, not a write", risk)
	}
	mustContain(t, cardFor(t, operations, `{"action":"rename","circle_id":"circle-one","name":"Family"}`),
		"Rename", `the circle "Home" (3 people)`, `"Family"`)
}

// Deleting a circle is destructive and the card says what it stops.
func TestDeleteIsDestructiveAndSaysWhatItStops(t *testing.T) {
	operations := &circleOperations{grants: aMember, answers: map[string]string{"ReadCircle": homeCircle}}

	if _, err := callAs(t, operations, `{"action":"delete","circle_id":"circle-one"}`); err != nil {
		t.Fatal(err)
	}
	deleted := operations.sentWith("DeleteCircle")
	if len(deleted) != 1 || deleted[0].variables["circleId"] != "circle-one" {
		t.Errorf("the deletion was %v", operations.sent)
	}

	if risk := circleTool(t).RiskFor([]byte(`{"action":"delete","circle_id":"circle-one"}`)); risk != tools.RiskDestructive {
		t.Errorf("delete is %v, not destructive", risk)
	}
	mustContain(t, cardFor(t, operations, `{"action":"delete","circle_id":"circle-one"}`),
		"Delete", `the circle "Home" (3 people)`, "stops everything shared with it")
	mustContain(t, cardFor(t, &circleOperations{grants: aMember}, `{"action":"delete","circle_id":"circle-one"}`),
		"Delete one of your circles")
}

// The schema takes an account id, the person speaks in usernames: remove
// finds the member in the circle, ignoring case, and sends their id.
func TestRemoveTakesAMemberOutByUsername(t *testing.T) {
	operations := &circleOperations{grants: aMember, answers: map[string]string{"ReadCircle": homeCircle}}

	if _, err := callAs(t, operations, `{"action":"remove","circle_id":"circle-one","username":" PersonY "}`); err != nil {
		t.Fatal(err)
	}
	removed := operations.sentWith("RemoveCircleMember")
	if len(removed) != 1 || removed[0].variables["circleId"] != "circle-one" || removed[0].variables["userId"] != "user-two" {
		t.Errorf("the removal was %v", operations.sent)
	}

	if risk := circleTool(t).RiskFor([]byte(`{"action":"remove","circle_id":"circle-one","username":"persony"}`)); risk != tools.RiskDestructive {
		t.Errorf("remove is %v, not destructive", risk)
	}
	mustContain(t, cardFor(t, operations, `{"action":"remove","circle_id":"circle-one","username":"personx"}`),
		"Remove", `"Person X" (personx)`, "from", `the circle "Home" (3 people)`)
	mustContain(t, cardFor(t, operations, `{"action":"remove","circle_id":"circle-one","username":"persony"}`),
		`Remove "persony" from`)
}

// Somebody who is not in the circle is refused before any mutation is sent.
func TestRemoveAndRoleRefuseSomebodyWhoIsNotInTheCircle(t *testing.T) {
	operations := &circleOperations{grants: aMember, answers: map[string]string{"ReadCircle": homeCircle}}

	for _, arguments := range []string{
		`{"action":"remove","circle_id":"circle-one","username":"personw"}`,
		`{"action":"role","circle_id":"circle-one","username":"personw","member_role":"admin"}`,
	} {
		if _, err := callAs(t, operations, arguments); err == nil || !strings.Contains(err.Error(), "personw") {
			t.Errorf("%s was not refused by name: %v", arguments, err)
		}
	}
	if len(operations.sentWith("mutation")) != 0 {
		t.Errorf("a mutation was sent: %v", operations.sent)
	}
}

// Making somebody an admin and making an admin a member are both granting,
// and the card says what the person can do afterwards.
func TestRoleChangesAMembersRoleAndIsGrantingBothWays(t *testing.T) {
	operations := &circleOperations{grants: aMember, answers: map[string]string{"ReadCircle": homeCircle}}

	if _, err := callAs(t, operations, `{"action":"role","circle_id":"circle-one","username":"personz","member_role":"admin"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callAs(t, operations, `{"action":"role","circle_id":"circle-one","username":"personx","member_role":"member"}`); err != nil {
		t.Fatal(err)
	}
	changed := operations.sentWith("SetCircleMemberRole")
	if len(changed) != 2 {
		t.Fatalf("two role changes were asked for: %v", operations.sent)
	}
	if changed[0].variables["userId"] != "user-three" || changed[0].variables["memberRole"] != "admin" {
		t.Errorf("the first change was %v", changed[0].variables)
	}
	if changed[1].variables["userId"] != "user-one" || changed[1].variables["memberRole"] != "member" {
		t.Errorf("the second change was %v", changed[1].variables)
	}

	if _, err := callAs(t, operations, `{"action":"role","circle_id":"circle-one","username":"personz","member_role":"owner"}`); err == nil {
		t.Error("a role other than admin or member was sent")
	}
	if len(operations.sentWith("SetCircleMemberRole")) != 2 {
		t.Errorf("a bad role reached the API: %v", operations.sent)
	}

	tool := circleTool(t)
	for _, memberRole := range []string{"admin", "member"} {
		if risk := tool.RiskFor([]byte(`{"action":"role","circle_id":"circle-one","username":"personz","member_role":"` + memberRole + `"}`)); risk != tools.RiskGranting {
			t.Errorf("making somebody %s is %v, not granting", memberRole, risk)
		}
	}
	mustContain(t, cardFor(t, operations, `{"action":"role","circle_id":"circle-one","username":"personz","member_role":"admin"}`),
		"Make", `"Person Z" (personz)`, "an admin of", `the circle "Home" (3 people)`, "manage who is in it")
	mustContain(t, cardFor(t, operations, `{"action":"role","circle_id":"circle-one","username":"personz","member_role":"member"}`),
		"an ordinary member of", "no longer manage")
}

// Taking back an invitation is a write, and the card names who was invited
// and to which circle.
func TestWithdrawTakesBackAnInvitationAndIsAWrite(t *testing.T) {
	operations := &circleOperations{grants: aMember, answers: map[string]string{
		"ListCircles": `{"ListCircles":[{"name":"Home","pendingInvitations":[{"id":"invitation-one","invitedUsername":"personw"}]}]}`,
	}}

	if _, err := callAs(t, operations, `{"action":"withdraw","invitation_id":" invitation-one "}`); err != nil {
		t.Fatal(err)
	}
	withdrawn := operations.sentWith("WithdrawCircleInvitation")
	if len(withdrawn) != 1 || withdrawn[0].variables["invitationId"] != "invitation-one" {
		t.Errorf("the withdrawal was %v", operations.sent)
	}

	if risk := circleTool(t).RiskFor([]byte(`{"action":"withdraw","invitation_id":"invitation-one"}`)); risk != tools.RiskWrite {
		t.Errorf("withdraw is %v, not a write", risk)
	}
	mustContain(t, cardFor(t, operations, `{"action":"withdraw","invitation_id":"invitation-one"}`),
		"Take back the invitation to", `"personw"`, `the circle "Home"`)
	mustContain(t, cardFor(t, operations, `{"action":"withdraw","invitation_id":"invitation-missing"}`),
		"Take back an invitation to a circle")
}

// Somebody without the right to use circles is refused every one of the new
// actions, and nothing is sent.
func TestTheCircleActionsAreRefusedWithoutThePermissionToUseCircles(t *testing.T) {
	operations := &circleOperations{grants: nil}

	for _, arguments := range []string{
		`{"action":"rename","circle_id":"c","name":"n"}`,
		`{"action":"delete","circle_id":"c"}`,
		`{"action":"remove","circle_id":"c","username":"personx"}`,
		`{"action":"role","circle_id":"c","username":"personx","member_role":"admin"}`,
		`{"action":"withdraw","invitation_id":"i"}`,
		`{"action":"accept","invitation_id":"i"}`,
		`{"action":"decline","invitation_id":"i"}`,
	} {
		if _, err := callAs(t, operations, arguments); err == nil || !strings.Contains(err.Error(), "not something they may do") {
			t.Errorf("%s should be refused: %v", arguments, err)
		}
	}
	if len(operations.sent) != 0 {
		t.Errorf("the API was asked: %v", operations.sent)
	}
}
