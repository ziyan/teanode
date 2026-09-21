package mailbox

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
)

// DraftStorage persists the MIME bytes before the draft can commit.
type DraftStorage interface {
	Put(context.Context, string, []string, []byte) error
}

// SaveDraftRequest identifies the owned mailbox and the save being replaced.
type SaveDraftRequest struct {
	MailboxID      string
	PreviousItemID string
}

// DraftPreparer validates and composes content on the command transaction.
// It must not commit, send or write through a separate database connection.
type DraftPreparer func(context.Context, db.Transaction, *models.Mailbox) (*models.Mail, error)

// SaveDraft atomically composes, stores, indexes and replaces a person's draft.
// Unreferenced stored bytes left by rollback follow normal spool retention.
func (self *Commands) SaveDraft(ctx context.Context, principal *access.Principal, request SaveDraftRequest, spool DraftStorage, prepare DraftPreparer) (*models.MailboxItem, error) {
	if principal == nil || principal.User == nil || principal.Permissions == nil || !principal.Permissions.Has(models.PermissionMailWrite) {
		return nil, db.ErrNotFound
	}
	var saved *models.MailboxItem
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		mailbox, err := transaction.GetMailbox(request.MailboxID)
		if err != nil {
			return err
		}
		if mailbox == nil || mailbox.UserID != principal.User.ID {
			return db.ErrNotFound
		}
		if prepare == nil || spool == nil {
			return db.ErrInvalidArguments
		}
		prepared, err := prepare(ctx, transaction, mailbox)
		if err != nil {
			return err
		}
		if prepared == nil || prepared.ID != "" || prepared.Kind != models.MailKindDraft {
			return fmt.Errorf("%w: preparation must return a new draft", db.ErrInvalidArguments)
		}
		drafts, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil {
			return err
		}
		if drafts == nil {
			return db.ErrNotFound
		}
		created, err := transaction.CreateMail(prepared, nil)
		if err != nil {
			return err
		}
		if err := spool.Put(ctx, created.ID, prepared.Headers, prepared.Body); err != nil {
			return err
		}
		item, err := transaction.AddItem(drafts.ID, created.ID, "", models.MailboxItemFlags{Draft: new(true), Seen: new(true)})
		if err != nil {
			return err
		}
		if err := transaction.SetMailSearch(created.ID, mx.SearchDocument(created), mx.AttachmentCount(created)); err != nil {
			return err
		}
		if request.PreviousItemID != "" {
			if err := RemoveDraft(ctx, transaction, mailbox.ID, request.PreviousItemID); err != nil {
				return err
			}
		}
		saved = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}
