package mailact

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

type sentOperation struct {
	document  string
	variables map[string]any
}

// invitationOperations answers the reads a tool makes to find the mailbox of
// a message, records the rest, and refuses an answer the way the API does for
// a person who may not send.
type invitationOperations struct {
	isSendRefused bool
	// isWithoutAnswerPermissions leaves the person with mail:write alone.
	isWithoutAnswerPermissions bool
	invitationJSON             string
	sent                       []sentOperation
}

func (self *invitationOperations) Permissions() *models.EffectivePermissions {
	grants := []models.Grant{{Permission: models.PermissionMailWrite}}
	if !self.isWithoutAnswerPermissions {
		grants = append(grants, models.Grant{Permission: models.PermissionMailSend}, models.Grant{Permission: models.PermissionCalendarUse})
	}
	return models.NewEffectivePermissions(grants)
}

func (self *invitationOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	self.sent = append(self.sent, sentOperation{document: document, variables: variables})
	switch {
	case strings.Contains(document, "ListMailboxes"):
		return json.Unmarshal([]byte(`{"ListMailboxes":[{"mailbox":{"id":"mailbox-one","agent":{"granted":true}},"folders":[{"id":"folder-inbox","mailboxId":"mailbox-one","name":"Inbox","kind":"inbox"}]}]}`), response)
	case strings.Contains(document, "GetMailboxThread"):
		return json.Unmarshal([]byte(`{"GetMailboxThread":{"threadId":"thread-one","subject":"Planning","items":[{"folderId":"folder-inbox","item":{"id":"item-one"}}]}}`), response)
	case strings.Contains(document, "GetMailInvitation") && !strings.Contains(document, "AnswerMailInvitation"):
		return json.Unmarshal([]byte(`{"GetMailInvitation":`+self.invitationJSON+`}`), response)
	case strings.Contains(document, "AnswerMailInvitation"):
		if self.isSendRefused {
			return context.DeadlineExceeded
		}
		return json.Unmarshal([]byte(`{"AnswerMailInvitation":{"method":"REQUEST","summary":"Planning","participation":"TENTATIVE"}}`), response)
	}
	return json.Unmarshal([]byte(`{}`), response)
}

func (self *invitationOperations) answers() []sentOperation {
	var answers []sentOperation
	for _, operation := range self.sent {
		if strings.Contains(operation.document, "AnswerMailInvitation") {
			answers = append(answers, operation)
		}
	}
	return answers
}

type invitationRun struct {
	tools.Run
	operations *invitationOperations
}

func (self *invitationRun) Operations() tools.Operations { return self.operations }

func mailActTool(test *testing.T) *tools.Tool {
	test.Helper()
	for _, each := range tools.Build().All() {
		if each.Name == "mail_act" {
			return each
		}
	}
	test.Fatal("mail_act is not registered")
	return nil
}

func answerWith(test *testing.T, operations *invitationOperations, arguments string, isConfirmed bool) (*tools.Result, error) {
	test.Helper()
	ctx := tools.WithRun(context.Background(), &invitationRun{operations: operations})
	return mailActTool(test).Run(ctx, &tools.Call{ID: "call-one", Arguments: []byte(arguments), Confirmed: isConfirmed})
}

// Answering mails the organizer in the person's name, so it is held to the
// standard of sending: outward, with a card that names the answer, while
// marking a message read stays an ordinary write.
func TestAnswerInvitationIsOutwardAndSaysSoOnItsCard(test *testing.T) {
	tool := mailActTool(test)

	if risk := tool.RiskFor([]byte(`{"action":"answer_invitation","item_ids":["item-one"],"answer":"accepted"}`)); risk != tools.RiskOutward {
		test.Errorf("answer_invitation is %v, not outward", risk)
	}
	if risk := tool.RiskFor([]byte(`{"action":"mark_read","item_ids":["item-one"]}`)); risk != tools.RiskWrite {
		test.Errorf("mark_read is %v, not write", risk)
	}
	said := tool.Preview([]byte(`{"action":"answer_invitation","item_ids":["item-one"],"answer":"declined"}`))
	for _, wanted := range []string{"invitation", "declined"} {
		if !strings.Contains(said, wanted) {
			test.Errorf("the card %q does not say %q", said, wanted)
		}
	}
}

// Without the person's word nothing is sent to the API at all, not even the
// read that finds the mailbox.
func TestAnswerInvitationWaitsForConfirmation(test *testing.T) {
	operations := &invitationOperations{}

	_, err := answerWith(test, operations, `{"action":"answer_invitation","item_ids":["item-one"],"answer":"accepted"}`, false)
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		test.Fatalf("an unconfirmed answer gave %v", err)
	}
	if len(operations.sent) != 0 {
		test.Errorf("the API was asked: %v", operations.sent)
	}
}

// A confirmed answer reaches AnswerMailInvitation with the answer in the
// form the API takes, for the one message named.
func TestAnswerInvitationSendsTheAnswerForOneMessage(test *testing.T) {
	operations := &invitationOperations{}

	result, err := answerWith(test, operations, `{"action":"answer_invitation","item_ids":["item-one"],"answer":"tentative"}`, true)
	if err != nil {
		test.Fatal(err)
	}
	answers := operations.answers()
	if len(answers) != 1 {
		test.Fatalf("one answer was expected: %v", operations.sent)
	}
	if answers[0].variables["itemId"] != "item-one" || answers[0].variables["answer"] != "TENTATIVE" {
		test.Errorf("the answer was %v", answers[0].variables)
	}
	if !result.Untrusted || !strings.Contains(result.Content, "Planning") {
		test.Errorf("the result %+v does not carry the invitation as untrusted text", result)
	}
}

// What cannot be an answer, or answers more than one invitation, is refused
// before the API is asked.
func TestAnswerInvitationRefusesWhatIsNotAnAnswer(test *testing.T) {
	for name, arguments := range map[string]string{
		"an answer that is not one": `{"action":"answer_invitation","item_ids":["item-one"],"answer":"maybe"}`,
		"no answer":                 `{"action":"answer_invitation","item_ids":["item-one"]}`,
		"two messages":              `{"action":"answer_invitation","item_ids":["item-one","item-two"],"answer":"accepted"}`,
		"a whole conversation":      `{"action":"answer_invitation","thread_item_id":"item-one","answer":"accepted"}`,
		"no message":                `{"action":"answer_invitation","answer":"accepted"}`,
	} {
		operations := &invitationOperations{}
		if _, err := answerWith(test, operations, arguments, true); err == nil {
			test.Errorf("%s was accepted", name)
		}
		if len(operations.answers()) != 0 {
			test.Errorf("%s reached the API", name)
		}
	}
}

// A refusal from the API, such as a person without the right to send, is
// what comes back, so the model says why and does not claim to have answered.
func TestAnswerInvitationPassesOnTheAPIsRefusal(test *testing.T) {
	operations := &invitationOperations{isSendRefused: true}

	if _, err := answerWith(test, operations, `{"action":"answer_invitation","item_ids":["item-one"],"answer":"accepted"}`, true); err == nil {
		test.Fatal("a refused answer reported success")
	}
}

func answerWithCall(test *testing.T, operations *invitationOperations, callId, arguments string) {
	test.Helper()
	ctx := tools.WithRun(context.Background(), &invitationRun{operations: operations})
	if _, err := mailActTool(test).Run(ctx, &tools.Call{ID: callId, Arguments: []byte(arguments), Confirmed: true}); err != nil {
		test.Fatal(err)
	}
}

// The server answers a repeated request identifier once, so a retried
// confirmation of one call has to send the identifier it sent before, and
// another call, message or answer a different one.
func TestAnswerInvitationSendsOneRequestIdentifierPerCall(test *testing.T) {
	operations := &invitationOperations{}
	accept := `{"action":"answer_invitation","item_ids":["item-one"],"answer":"accepted"}`
	answerWithCall(test, operations, "call-one", accept)
	answerWithCall(test, operations, "call-one", accept)
	answerWithCall(test, operations, "call-two", accept)
	answerWithCall(test, operations, "call-one", `{"action":"answer_invitation","item_ids":["item-one"],"answer":"declined"}`)

	identifiers := []string{}
	for _, answer := range operations.answers() {
		identifier, _ := answer.variables["requestId"].(string)
		if identifier == "" || len(identifier) > 128 {
			test.Fatalf("the request identifier %q is missing or too long for the server", identifier)
		}
		identifiers = append(identifiers, identifier)
	}
	if len(identifiers) != 4 || identifiers[0] != identifiers[1] {
		test.Errorf("a retry of one call sent %v", identifiers)
	}
	if identifiers[2] == identifiers[0] || identifiers[3] == identifiers[0] {
		test.Errorf("a different call or answer reused an identifier: %v", identifiers)
	}
}

// The card names the event, when, and who is mailed, because the call holds
// only a message identifier.
func TestAnswerInvitationCardNamesTheEventAndTheOrganizer(test *testing.T) {
	tool := mailActTool(test)
	arguments := []byte(`{"action":"answer_invitation","item_ids":["item-one"],"answer":"accepted"}`)
	operations := &invitationOperations{invitationJSON: `{"method":"REQUEST","summary":"Planning","startsAt":"2031-03-04T10:00:00Z","organizer":"ann@example.org"}`}
	ctx := tools.WithRun(context.Background(), &invitationRun{operations: operations})

	said := tool.PreviewLine(ctx, arguments)
	for _, wanted := range []string{`"Planning"`, "2031-03-04T10:00:00Z", "ann@example.org", "accepted"} {
		if !strings.Contains(said, wanted) {
			test.Errorf("the card %q does not say %q", said, wanted)
		}
	}
	operations.invitationJSON = `{"method":"REQUEST","summary":"Planning"}`
	if said := tool.PreviewLine(ctx, arguments); !strings.Contains(said, "no organizer") {
		test.Errorf("the card %q does not say nobody is mailed", said)
	}
	if said := tool.PreviewLine(ctx, []byte(`{"action":"mark_read","item_ids":["item-one"]}`)); strings.Contains(said, "invitation") {
		test.Errorf("another action got the invitation card: %q", said)
	}
}

// A person who may not send mail or use the calendar is not offered the
// action, and a call that names it anyway is refused before the API.
func TestAnswerInvitationIsOfferedOnlyWithTheRightToSendAndUseTheCalendar(test *testing.T) {
	tool := mailActTool(test)
	holds := func(permissions ...models.Permission) *models.EffectivePermissions {
		grants := []models.Grant{}
		for _, permission := range permissions {
			grants = append(grants, models.Grant{Permission: permission})
		}
		return models.NewEffectivePermissions(grants)
	}
	for name, permissions := range map[string]*models.EffectivePermissions{
		"mail:write alone": holds(models.PermissionMailWrite),
		"no calendar":      holds(models.PermissionMailWrite, models.PermissionMailSend),
		"no right to send": holds(models.PermissionMailWrite, models.PermissionCalendarUse),
	} {
		if slices.Contains(tools.ActionsOf(tool.WithHeldActions(permissions)), "answer_invitation") {
			test.Errorf("%s was offered answer_invitation", name)
		}
		if !slices.Contains(tools.ActionsOf(tool.WithHeldActions(permissions)), "mark_read") {
			test.Errorf("%s lost mark_read", name)
		}
	}
	all := holds(models.PermissionMailWrite, models.PermissionMailSend, models.PermissionCalendarUse)
	if !slices.Contains(tools.ActionsOf(tool.WithHeldActions(all)), "answer_invitation") {
		test.Error("a person who may do both was not offered answer_invitation")
	}
	if !slices.Contains(tools.ActionsOf(tool), "answer_invitation") {
		test.Error("narrowing one person's offer changed the catalog's own tool")
	}

	operations := &invitationOperations{isWithoutAnswerPermissions: true}
	if _, err := answerWith(test, operations, `{"action":"answer_invitation","item_ids":["item-one"],"answer":"accepted"}`, true); err == nil {
		test.Error("a call without the permissions was run")
	}
	if len(operations.answers()) != 0 {
		test.Error("a call without the permissions reached the API")
	}
}

// A call made directly has no id of its own, so two answers to one message
// are two answers, not one repeated: the third of accept, decline, accept
// must not be taken for the first.
func TestADirectCallsAnswersAreNotTakenForRepeats(t *testing.T) {
	first, second := answerRequestId("direct", "item-1", "ACCEPTED"), answerRequestId("direct", "item-1", "ACCEPTED")
	if first == second {
		t.Fatalf("two direct answers shared the id %q", first)
	}
	if answerRequestId("call-7", "item-1", "ACCEPTED") != answerRequestId("call-7", "item-1", "ACCEPTED") {
		t.Fatal("a retried confirmation of one call must reuse its id")
	}
}
