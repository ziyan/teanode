package mailbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

type draftOperations struct{}

func (self *draftOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})
}

func (self *draftOperations) Execute(_ context.Context, document string, _ map[string]any, response any) error {
	if strings.Contains(document, "FindMailboxDraft") {
		return json.Unmarshal([]byte(`{"FindMailboxDraft":null}`), response)
	}
	return json.Unmarshal([]byte(`{"GetMailboxDraft":{"mailboxId":"second-mailbox","itemId":"draft-item","key":"draft-key"}}`), response)
}

func TestDraftItemLookupRejectsTheWrongMailbox(test *testing.T) {
	operations := &draftOperations{}
	for _, mailboxId := range []string{"first-mailbox", "second-mailbox"} {
		view := &MailboxView{Mailbox: &models.Mailbox{ID: mailboxId}}
		draft, err := FindDraft(test.Context(), operations, view, "draft-item")
		if mailboxId == "first-mailbox" {
			if err == nil || draft != nil {
				test.Fatal("draft associated with the wrong granted mailbox")
			}
		} else if err != nil || draft == nil || draft.Key != "draft-key" {
			test.Fatalf("correct mailbox draft=%+v, %v", draft, err)
		}
	}
}
