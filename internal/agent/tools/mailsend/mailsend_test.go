package mailsend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

type sendRun struct {
	tools.Run
	operations *sendOperations
}

func (self *sendRun) Operations() tools.Operations { return self.operations }

type sendOperations struct {
	accepted                map[string]string
	sendCount               int
	draftReadCount          int
	hasLostResponse         bool
	hasLookupFailure        bool
	hasAcceptanceDuringFind bool
	hasDraftRemoved         bool
}

func (self *sendOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailSend}})
}

func (self *sendOperations) Execute(_ context.Context, document string, variables map[string]any, result any) error {
	var response any
	switch {
	case strings.Contains(document, "ListMailboxes"):
		response = map[string]any{"ListMailboxes": []any{map[string]any{"mailbox": map[string]any{"id": "mailbox-fixture", "agent": map[string]any{"granted": true}}}}}
	case strings.Contains(document, "GetMailboxSubmission"):
		if self.hasLookupFailure {
			return errors.New("lookup unavailable")
		}
		var accepted any
		if mailId := self.accepted[variables["submissionId"].(string)]; mailId != "" {
			accepted = map[string]any{"mailId": mailId}
		}
		response = map[string]any{"GetMailboxSubmission": accepted}
	case strings.Contains(document, "FindMailboxDraft"):
		self.draftReadCount++
		if self.hasAcceptanceDuringFind {
			self.accepted[draftSubmissionId(variables["mailboxId"].(string), variables["key"].(string))] = "accepted-during-read"
			self.hasDraftRemoved = true
		}
		if self.hasDraftRemoved {
			return errors.New("draft was reconciled")
		}
		if len(self.accepted) > 0 && self.hasLostResponse {
			return errors.New("draft already removed")
		}
		response = map[string]any{"FindMailboxDraft": map[string]any{"itemId": "draft-item", "key": variables["key"]}}
	case strings.Contains(document, "GetMailboxDraft"):
		self.draftReadCount++
		if self.hasDraftRemoved {
			return errors.New("draft was reconciled")
		}
		response = map[string]any{"GetMailboxDraft": map[string]any{"from": "sender@example.com", "to": []string{"recipient@example.net"}, "subject": "Fixture", "text": "Fixture content"}}
	case strings.Contains(document, "SendMailboxMessage"):
		self.sendCount++
		submissionId, ok := variables["submissionId"].(string)
		if !ok || submissionId == "" || !strings.Contains(document, "submissionId: $submissionId") {
			return errors.New("send omitted its identity")
		}
		mailId := fmt.Sprintf("accepted-mail-%d", self.sendCount)
		self.accepted[submissionId] = mailId
		if self.hasLostResponse {
			return errors.New("response was lost after acceptance")
		}
		response = map[string]any{"SendMailboxMessage": map[string]any{"mail": map[string]any{"id": mailId}}}
	default:
		return fmt.Errorf("unexpected operation: %s", document)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, result)
}

func TestMailSendRecoversLostResponseWithoutReadingRemovedDraft(test *testing.T) {
	operations := &sendOperations{accepted: map[string]string{}, hasLostResponse: true}
	ctx := tools.WithRun(context.Background(), &sendRun{operations: operations})
	call := &tools.Call{ID: "provider-call", Arguments: json.RawMessage(`{"draft_id":"draft-key"}`), Confirmed: true}
	if _, err := runMailSend(ctx, call); err == nil {
		test.Fatal("expected the lost response")
	}
	readsBeforeRetry := operations.draftReadCount
	recovered, err := runMailSend(ctx, call)
	if err != nil || recovered == nil || !strings.Contains(recovered.Content, `"is_replay":true`) || !strings.Contains(recovered.Content, "accepted-mail-1") {
		test.Fatalf("retry = %+v, %v", recovered, err)
	}
	if operations.sendCount != 1 || operations.draftReadCount != readsBeforeRetry {
		test.Fatal("retry reread a removed draft or sent another message")
	}
}

func TestMailSendDoesNotSendWhenAcceptanceLookupFails(test *testing.T) {
	operations := &sendOperations{accepted: map[string]string{}, hasLookupFailure: true}
	ctx := tools.WithRun(context.Background(), &sendRun{operations: operations})
	if _, err := runMailSend(ctx, &tools.Call{Arguments: json.RawMessage(`{"draft_id":"draft-key"}`), Confirmed: true}); err == nil {
		test.Fatal("lookup failure was ignored")
	}
	if operations.sendCount != 0 || operations.draftReadCount != 0 {
		test.Fatal("uncertain acceptance proceeded to send preparation")
	}
}

func TestMailSendScopesIdentityToDraftAndMailboxNotProviderCall(test *testing.T) {
	operations := &sendOperations{accepted: map[string]string{}}
	ctx := tools.WithRun(context.Background(), &sendRun{operations: operations})
	for _, draftId := range []string{"first-draft", "second-draft", "first-draft"} {
		arguments, err := json.Marshal(mailSendArguments{DraftID: draftId})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := runMailSend(ctx, &tools.Call{ID: "reused-provider-call", Arguments: arguments, Confirmed: true}); err != nil {
			test.Fatal(err)
		}
	}
	if operations.sendCount != 2 {
		test.Fatalf("accepted %d drafts, want two", operations.sendCount)
	}
	if draftSubmissionId("first-mailbox", "draft") == draftSubmissionId("second-mailbox", "draft") {
		test.Fatal("identity is not scoped to mailbox")
	}
	if len(draftSubmissionId("mailbox", strings.Repeat("draft", 100))) > 128 {
		test.Fatal("identity exceeds the server's bound")
	}
}

func TestMailSendStillRequiresConfirmationOnRetry(test *testing.T) {
	operations := &sendOperations{accepted: map[string]string{draftSubmissionId("mailbox-fixture", "draft-key"): "accepted-mail"}}
	ctx := tools.WithRun(context.Background(), &sendRun{operations: operations})
	if _, err := runMailSend(ctx, &tools.Call{Arguments: json.RawMessage(`{"draft_id":"draft-key"}`)}); err == nil {
		test.Fatal("unconfirmed call was accepted")
	}
}

func TestMailSendRecoversAcceptanceDuringDraftLookup(test *testing.T) {
	operations := &sendOperations{accepted: map[string]string{}, hasAcceptanceDuringFind: true}
	ctx := tools.WithRun(context.Background(), &sendRun{operations: operations})
	recovered, err := runMailSend(ctx, &tools.Call{Arguments: json.RawMessage(`{"draft_id":"draft-key"}`), Confirmed: true})
	if err != nil || recovered == nil || !strings.Contains(recovered.Content, "accepted-during-read") || operations.sendCount != 0 {
		test.Fatalf("concurrent acceptance = %+v, %v, sends=%d", recovered, err, operations.sendCount)
	}
}
