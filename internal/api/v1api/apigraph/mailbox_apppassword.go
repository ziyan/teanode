package apigraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// App passwords: how a mail program signs in to a mailbox. One per device,
// named for it, shown once when made, and revoked on its own. The account's
// password never goes into a mail program, so a phone that is lost gives up
// one app password and nothing else.

type MailboxAppPasswordQuery interface {
	// The app passwords of a mailbox: names and last use, never the secret
	ListMailboxAppPasswords(ctx context.Context, arguments ListMailboxAppPasswordsArguments) ([]*models.MailboxAppPassword, error)
}

type MailboxAppPasswordMutation interface {
	// Make an app password for a device; the password is in the reply and nowhere else, ever
	CreateMailboxAppPassword(ctx context.Context, arguments CreateMailboxAppPasswordArguments) (*CreatedAppPassword, error)

	// Revoke an app password; the device it was on cannot sign in again
	DeleteMailboxAppPassword(ctx context.Context, arguments DeleteMailboxAppPasswordArguments) error
}

type ListMailboxAppPasswordsArguments struct {
	MailboxID string `json:"mailboxId"`
}

func (self *graph) ListMailboxAppPasswords(ctx context.Context, arguments ListMailboxAppPasswordsArguments) ([]*models.MailboxAppPassword, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailboxManage, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	appPasswords, err := self.transaction(ctx).ListAppPasswords(mailbox.ID)
	if err != nil {
		return nil, err
	}
	if appPasswords == nil {
		appPasswords = []*models.MailboxAppPassword{}
	}
	return appPasswords, nil
}

type CreateMailboxAppPasswordArguments struct {
	MailboxID string `json:"mailboxId"`

	// What the device is called: "Phone", "Laptop Thunderbird"
	Name string `json:"name"`
}

// CreatedAppPassword is an app password at the one moment its secret is known.
type CreatedAppPassword struct {
	AppPassword *models.MailboxAppPassword `json:"appPassword"`

	// The password to type into the mail program. Not stored; only its hash is.
	Password string `json:"password"`

	// The login name to go with it: one of the mailbox's addresses.
	Username string `json:"username"`
}

// appPasswordAlphabet leaves out the characters that are misread when typed
// from a screen: 0 and O, 1 and l and I.
const appPasswordAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func (self *graph) CreateMailboxAppPassword(ctx context.Context, arguments CreateMailboxAppPasswordArguments) (*CreatedAppPassword, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailboxManage, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(arguments.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: an app password needs a name", api.ErrInvalidArguments)
	}
	if len(mailbox.Addresses) == 0 {
		return nil, fmt.Errorf("%w: the mailbox has no address to sign in with", api.ErrInvalidArguments)
	}
	// Twenty characters of a 32-letter alphabet is a hundred bits, and the
	// grouping makes it possible to type from a phone's screen.
	raw := security.GenerateRandomString(20, appPasswordAlphabet)
	password := strings.Join([]string{raw[0:5], raw[5:10], raw[10:15], raw[15:20]}, "-")
	hash, err := security.HashPassword(password)
	if err != nil {
		return nil, err
	}
	created, err := self.transaction(ctx).CreateAppPassword(&models.MailboxAppPassword{
		MailboxID:    mailbox.ID,
		Name:         name,
		PasswordHash: string(hash),
	})
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s added app password %q to mailbox %q", operatorName(ctx), name, mailbox.ID)
	return &CreatedAppPassword{AppPassword: created, Password: password, Username: mailbox.Addresses[0].Address}, nil
}

type DeleteMailboxAppPasswordArguments struct {
	AppPasswordID string `json:"appPasswordId"`
}

func (self *graph) DeleteMailboxAppPassword(ctx context.Context, arguments DeleteMailboxAppPasswordArguments) error {
	principal, err := self.requirePermission(ctx, models.PermissionMailboxManage)
	if err != nil {
		return err
	}
	tx := self.transaction(ctx)
	appPassword, err := tx.GetAppPassword(arguments.AppPasswordID)
	if err != nil {
		return err
	}
	if appPassword == nil {
		return api.ErrNotFound
	}
	mailbox, err := tx.GetMailbox(appPassword.MailboxID)
	if err != nil {
		return err
	}
	if mailbox == nil || principal.User == nil || mailbox.UserID != principal.User.ID {
		return api.ErrNotFound
	}
	if err := tx.DeleteAppPassword(appPassword.ID); err != nil {
		return translateError(err)
	}
	log.Noticef("%s removed app password %q from mailbox %q", operatorName(ctx), appPassword.Name, mailbox.ID)
	return nil
}
