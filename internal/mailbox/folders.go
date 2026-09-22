// Package mailbox implements authorized mailbox commands independently of their
// HTTP, agent or other transport adapters.
package mailbox

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// TransactionScope runs commands either in a database transaction or within an
// existing transaction's savepoint. Both implementations preserve audit context.
type TransactionScope interface {
	TransactionContext(context.Context, func(db.Transaction) error) error
}

// Commands owns mailbox command authorization and atomic writes.
type Commands struct {
	transactions TransactionScope
}

// New uses a database for standalone commands, or a transaction when a caller
// needs command rollback without committing its larger unit of work.
func New(transactions TransactionScope) *Commands {
	return &Commands{transactions: transactions}
}

// CreateFolderRequest describes a new custom folder.
type CreateFolderRequest struct {
	MailboxID string
	Name      string
	ParentID  string
}

// CreateFolder adds a folder only to a mailbox owned by the principal.
func (self *Commands) CreateFolder(ctx context.Context, principal *access.Principal, request CreateFolderRequest) (*models.MailboxFolder, error) {
	var created *models.MailboxFolder
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		if err := requireMailbox(transaction, principal, request.MailboxID); err != nil {
			return err
		}
		var err error
		created, err = transaction.CreateFolder(&models.MailboxFolder{
			MailboxID: request.MailboxID, Name: strings.TrimSpace(request.Name), ParentID: request.ParentID,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateFolderRequest retains fields that are absent from an update.
type UpdateFolderRequest struct {
	FolderID string
	Name     *string
	ParentID *string
}

// UpdateFolder renames or moves a custom folder without changing built-in names.
func (self *Commands) UpdateFolder(ctx context.Context, principal *access.Principal, request UpdateFolderRequest) (*models.MailboxFolder, error) {
	var updated *models.MailboxFolder
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		folder, err := requireFolder(transaction, principal, request.FolderID)
		if err != nil {
			return err
		}
		if folder.Kind != models.MailboxFolderKindCustom {
			return db.ErrInvalidArguments
		}
		updated, err = transaction.UpdateFolder(folder.ID, func(folder *models.MailboxFolder) error {
			if request.Name != nil {
				folder.Name = strings.TrimSpace(*request.Name)
			}
			if request.ParentID != nil {
				folder.ParentID = *request.ParentID
			}
			return nil
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// SetFolderPinned changes a folder's pin without allowing the Inbox to be unpinned.
func (self *Commands) SetFolderPinned(ctx context.Context, principal *access.Principal, folderId string, isPinned bool) (*models.MailboxFolder, error) {
	var updated *models.MailboxFolder
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		folder, err := requireFolder(transaction, principal, folderId)
		if err != nil {
			return err
		}
		if folder.Kind == models.MailboxFolderKindInbox {
			return db.ErrInvalidArguments
		}
		updated, err = transaction.UpdateFolder(folder.ID, func(folder *models.MailboxFolder) error {
			switch {
			case isPinned && folder.PinnedAt == nil:
				now := time.Now()
				folder.PinnedAt = &now
			case !isPinned:
				folder.PinnedAt = nil
			}
			return nil
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteFolder removes an owned custom folder and its contents as one command.
func (self *Commands) DeleteFolder(ctx context.Context, principal *access.Principal, folderId string) error {
	return self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		if _, err := requireFolder(transaction, principal, folderId); err != nil {
			return err
		}
		return transaction.DeleteFolder(folderId)
	})
}

func requireMailbox(transaction db.Transaction, principal *access.Principal, mailboxId string) error {
	if principal == nil || principal.User == nil || principal.Permissions == nil || !principal.Permissions.Has(models.PermissionMailboxManage) {
		return db.ErrNotFound
	}
	mailbox, err := transaction.GetMailbox(mailboxId)
	if err != nil {
		return err
	}
	if mailbox == nil || mailbox.UserID != principal.User.ID {
		return db.ErrNotFound
	}
	return nil
}

func requireFolder(transaction db.Transaction, principal *access.Principal, folderId string) (*models.MailboxFolder, error) {
	if principal == nil || principal.User == nil || principal.Permissions == nil || !principal.Permissions.Has(models.PermissionMailboxManage) {
		return nil, db.ErrNotFound
	}
	folder, err := transaction.GetFolder(folderId)
	if err != nil {
		return nil, err
	}
	if folder == nil {
		return nil, db.ErrNotFound
	}
	if err := requireMailbox(transaction, principal, folder.MailboxID); err != nil {
		return nil, err
	}
	return folder, nil
}
