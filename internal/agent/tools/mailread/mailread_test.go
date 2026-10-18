package mailread

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

type invitationOperations struct {
	invitation string
	sent       []string
}

func (self *invitationOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})
}

func (self *invitationOperations) Execute(_ context.Context, document string, _ map[string]any, response any) error {
	self.sent = append(self.sent, document)
	switch {
	case strings.Contains(document, "ListMailboxes"):
		return json.Unmarshal([]byte(`{"ListMailboxes":[{"mailbox":{"id":"mailbox-one","agent":{"granted":true}},"folders":[{"id":"folder-inbox","mailboxId":"mailbox-one","name":"Inbox","kind":"inbox"}]}]}`), response)
	case strings.Contains(document, "GetMailboxThread"):
		return json.Unmarshal([]byte(`{"GetMailboxThread":{"threadId":"thread-one","subject":"Planning","items":[{"folderId":"folder-inbox","item":{"id":"item-one"}}]}}`), response)
	case strings.Contains(document, "GetMailInvitation"):
		return json.Unmarshal([]byte(self.invitation), response)
	}
	return json.Unmarshal([]byte(`{}`), response)
}

type invitationRun struct {
	tools.Run
	operations *invitationOperations
}

func (self *invitationRun) Operations() tools.Operations { return self.operations }

func readWith(test *testing.T, operations *invitationOperations, arguments string) (*tools.Result, error) {
	test.Helper()
	ctx := tools.WithRun(context.Background(), &invitationRun{operations: operations})
	return runMailRead(ctx, &tools.Call{ID: "call-one", Arguments: []byte(arguments)})
}

// Asking for the invitation reads the card the dashboard draws above the
// message, not the message, and what it says is data from the organizer.
func TestReadInvitationReturnsTheCardAsUntrustedText(test *testing.T) {
	operations := &invitationOperations{invitation: `{"GetMailInvitation":{"method":"REQUEST","summary":"Planning","location":"Room 4","startsAt":"2031-03-04T10:00:00Z","organizer":"organizer@example.com","participation":"NEEDS-ACTION","attendees":[{"address":"you@example.com","participation":"NEEDS-ACTION"}]}}`}

	result, err := readWith(test, operations, `{"item_id":"item-one","invitation":true}`)
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"Planning", "Room 4", "organizer@example.com", "NEEDS-ACTION"} {
		if !strings.Contains(result.Content, wanted) {
			test.Errorf("the result %s does not say %q", result.Content, wanted)
		}
	}
	if !result.Untrusted {
		test.Error("an organizer's words were not marked untrusted")
	}
	for _, document := range operations.sent {
		if strings.Contains(document, "GetMailContent") {
			test.Error("the message body was read as well")
		}
	}
}

// A message that carried no invitation says so, rather than failing or
// showing an empty card.
func TestReadInvitationSaysWhenThereIsNone(test *testing.T) {
	operations := &invitationOperations{invitation: `{"GetMailInvitation":null}`}

	result, err := readWith(test, operations, `{"item_id":"item-one","invitation":true}`)
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(result.Content, "no calendar invitation") {
		test.Errorf("the result %s does not say there was none", result.Content)
	}
}
