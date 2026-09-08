package apigraph

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/db"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Subscriptions: the mailing lists a mailbox receives, and leaving them.
//
// Most of what arrives in a mailbox is not a letter. A newsletter says so in
// its headers — List-Unsubscribe says how to leave, List-Id names the list —
// and those are read when a message is stored, into columns on the message.
// A subscription is therefore not a stored thing but a grouping of stored
// things: every message that named the same list. What is stored is the
// request to leave, which outlives the mail.

type ListMailboxSubscriptionsArguments struct {
	// MailboxID of the mailbox to read
	MailboxID string `json:"mailboxId"`

	// How many to return, and where to start
	First  *int `json:"first"`
	Offset *int `json:"offset"`
}

type MailboxSubscriptionPage struct {
	Subscriptions []*models.MailboxSubscription `json:"subscriptions"`
	Total         int64                         `json:"total"`
}

// ListMailboxSubscriptions is every mailing list this mailbox receives: who
// sends it, how much of it there is, and whether leaving it has been asked
// for. Mail in Trash and Junk is left out — what you threw away is not a
// subscription you have, and what a filter caught is not one you agreed to.
func (self *graph) ListMailboxSubscriptions(ctx context.Context,
	arguments ListMailboxSubscriptionsArguments) (*MailboxSubscriptionPage, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	limit, offset := 100, 0
	if arguments.First != nil && *arguments.First > 0 {
		limit = min(*arguments.First, 500)
	}
	if arguments.Offset != nil && *arguments.Offset > 0 {
		offset = *arguments.Offset
	}
	tx := self.transaction(ctx)
	subscriptions, err := tx.ListSubscriptions(mailbox.ID, limit, offset)
	if err != nil {
		return nil, err
	}
	total, err := tx.CountSubscriptions(mailbox.ID)
	if err != nil {
		return nil, err
	}
	return &MailboxSubscriptionPage{Subscriptions: subscriptions, Total: total}, nil
}

type ReadMailboxSubscriptionArguments struct {
	// MailboxID of the mailbox to read
	MailboxID string `json:"mailboxId"`

	// Key of the subscription, as ListMailboxSubscriptions gives it
	Key string `json:"key"`
}

// ReadMailboxSubscription is a mailing list's mail, newest first, in the shape
// a conversation is read in — so that "what has this newsletter sent me" is
// one page rather than a search. Capped like a conversation, and saying so
// when it is.
func (self *graph) ReadMailboxSubscription(ctx context.Context,
	arguments ReadMailboxSubscriptionArguments) (*MailboxThreadView, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.Key) == "" {
		return nil, api.ErrInvalidArguments
	}
	tx := self.transaction(ctx)
	subscription, err := tx.GetSubscription(mailbox.ID, arguments.Key)
	if err != nil {
		return nil, err
	}
	if subscription == nil {
		return nil, api.ErrNotFound
	}

	// By when each message was written rather than by when it was filed, for
	// the reason a conversation is: moving a message makes a new item with a
	// new added_at, and the list would put whatever was last archived first.
	items, err := tx.ListItems("", &db.ItemOptions{
		MailboxID:  mailbox.ID,
		ListKey:    arguments.Key,
		ByReceived: true,
		Limit:      threadLimit,
	})
	if err != nil {
		return nil, err
	}
	if err := self.attachMails(ctx, items); err != nil {
		return nil, err
	}
	view, err := self.threadViewOf(ctx, mailbox, items, "")
	if err != nil {
		return nil, err
	}
	view.ThreadID = arguments.Key
	view.Subject = subscription.Name
	return view, nil
}

type GetMailboxSubscriptionArguments struct {
	// MailboxID of the mailbox to read
	MailboxID string `json:"mailboxId"`

	// Key of the subscription, as ListMailboxSubscriptions gives it
	Key string `json:"key"`
}

// GetMailboxSubscription is one list: the same row the list gives, for a page
// showing only that one.
func (self *graph) GetMailboxSubscription(ctx context.Context,
	arguments GetMailboxSubscriptionArguments) (*models.MailboxSubscription, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	subscription, err := self.transaction(ctx).GetSubscription(mailbox.ID, arguments.Key)
	if err != nil {
		return nil, err
	}
	if subscription == nil {
		return nil, api.ErrNotFound
	}
	return subscription, nil
}
