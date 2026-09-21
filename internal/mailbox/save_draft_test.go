package mailbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/models"
)

func TestSaveDraftAuthorizesBeforePreparingContent(test *testing.T) {
	database, principal, owned := folderFixture(test)
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailWrite}})
	prepare := func(context.Context, db.Transaction, *models.Mailbox) (*models.Mail, error) {
		test.Fatal("unauthorized draft reached composition")
		return nil, nil
	}
	for _, denied := range []*access.Principal{
		nil,
		{User: principal.User, Permissions: models.NewEffectivePermissions(nil)},
		{User: &models.User{ID: "another-owner"}, Permissions: principal.Permissions},
		{Console: true, Permissions: principal.Permissions},
	} {
		if _, err := mailbox.New(database).SaveDraft(test.Context(), denied, mailbox.SaveDraftRequest{MailboxID: owned.ID}, nil, prepare); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("draft authorization=%v", err)
		}
	}
	if _, err := mailbox.New(database).SaveDraft(test.Context(), principal, mailbox.SaveDraftRequest{MailboxID: "missing-mailbox"}, nil, prepare); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("missing mailbox=%v", err)
	}
}
