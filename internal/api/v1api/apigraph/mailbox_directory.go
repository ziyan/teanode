package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Who has a mailbox: for whoever points an address at one. A domain manager
// choosing where an alias delivers needs the list of mailboxes on this
// server, with whose each is — and nothing that is in them.

type MailboxDirectoryQuery interface {
	// Every mailbox on the server with its owner, for pointing an alias at one
	ListAllMailboxes(ctx context.Context) ([]*MailboxSummary, error)
}

// MailboxSummary is a mailbox as a domain manager sees it: whose it is and
// what it is called.
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
	if !principal.Permissions.HasAnywhere(models.PermissionDomainManage) && !principal.Permissions.Has(models.PermissionUserManage) {
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
