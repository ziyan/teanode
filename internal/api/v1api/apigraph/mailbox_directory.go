package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Who has a mailbox: every account's mailboxes, with whose each is and
// nothing that is in them. It names every account on the server, so it is
// for the operator and for whoever manages accounts; a manager of one domain
// does not see the other accounts this way.

type MailboxDirectoryQuery interface {
	// Every mailbox on the server with its owner, for pointing an alias at one
	ListAllMailboxes(ctx context.Context) ([]*MailboxSummary, error)
}

// MailboxSummary is a mailbox as the operator sees it in this list: whose it
// is and what it is called.
type MailboxSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	UserID   string `json:"userId"`
	Username string `json:"username"`
	UserName string `json:"userName,omitempty"`
}

func (self *graph) ListAllMailboxes(ctx context.Context) ([]*MailboxSummary, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.Permissions.Has(models.PermissionServerManage) && !principal.Permissions.Has(models.PermissionUserManage) {
		return nil, api.ErrNotFound
	}
	tx := self.transaction(ctx)
	users, err := tx.ListUsers()
	if err != nil {
		return nil, err
	}
	summaries := []*MailboxSummary{}
	for _, user := range users {
		mailboxes, err := tx.ListMailboxes(user.ID)
		if err != nil {
			return nil, err
		}
		for _, mailbox := range mailboxes {
			summaries = append(summaries, &MailboxSummary{
				ID:       mailbox.ID,
				Name:     mailbox.Name,
				UserID:   user.ID,
				Username: user.Username,
				UserName: user.Name,
			})
		}
	}
	return summaries, nil
}
