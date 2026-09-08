package apigraph

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/safefetch"
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

type UnsubscribeMailboxSubscriptionArguments struct {
	// MailboxID of the mailbox the list writes to
	MailboxID string `json:"mailboxId"`

	// Key of the subscription, as ListMailboxSubscriptions gives it
	Key string `json:"key"`
}

// UnsubscribeMailboxSubscription asks a sender to stop, the way that sender
// said to ask.
//
// Three ways, in the order they are worth trying. A sender that promised
// RFC 8058 gets one POST, made here rather than by the browser: a request from
// the browser hands the sender the reader's address and the fact that this
// message was open at this moment, which is what the image proxy exists to
// prevent. A sender that named an address gets a message from this mailbox. A
// sender that offers only a page is handed back for a person to open, because
// a page that wants a human cannot be pressed by a server.
//
// Nothing here happens on its own. An unsubscribe request tells the sender
// that a person reads this address, which is a thing to hand over on purpose.
func (self *graph) UnsubscribeMailboxSubscription(ctx context.Context,
	arguments UnsubscribeMailboxSubscriptionArguments) (*models.MailboxSubscription, error) {
	// Sending is what this does, whichever of the three it turns out to be —
	// a POST to a stranger is no less outward than an email.
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailSend, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	subscription, err := tx.GetSubscription(mailbox.ID, arguments.Key)
	if err != nil {
		return nil, err
	}
	if subscription == nil {
		return nil, api.ErrNotFound
	}

	info := mailparse.ListInfo{Unsubscribe: subscription.Unsubscribe, OneClick: subscription.OneClick}
	method, failure := self.leaveList(ctx, mailbox, subscription, info)
	reason := ""
	if failure != nil {
		reason = failure.Error()
	}
	if err := tx.RecordUnsubscribe(mailbox.ID, subscription.Key, method, failure != nil, reason); err != nil {
		return nil, err
	}
	log.Noticef("%s asked to leave the list %q by %s", operatorName(ctx), subscription.Key, method)

	return tx.GetSubscription(mailbox.ID, subscription.Key)
}

// leaveList does the asking and says how it was asked and what went wrong.
// A failure is recorded rather than returned as an error: "we tried and the
// sender answered 500" is something the row should say, and something a
// second attempt should be offered for.
func (self *graph) leaveList(ctx context.Context, mailbox *models.Mailbox,
	subscription *models.MailboxSubscription, info mailparse.ListInfo) (string, error) {
	if address := info.HTTPSUnsubscribe(); address != "" && info.OneClick {
		return models.UnsubscribeOneClick, postOneClick(ctx, address)
	}
	if address := info.MailUnsubscribe(); address != "" {
		return models.UnsubscribeMail, self.mailUnsubscribe(ctx, mailbox, address)
	}
	if info.WebUnsubscribe() != "" {
		// Recorded, not done: the page is opened by the person who asked.
		return models.UnsubscribeLink, nil
	}
	return models.UnsubscribeLink, fmt.Errorf("%w: this list named no way to leave it", api.ErrInvalidArguments)
}

// postOneClick is the request RFC 8058 describes: a POST carrying exactly
// List-Unsubscribe=One-Click, with no credentials and nothing else to it.
func postOneClick(ctx context.Context, address string) error {
	target, err := safefetch.ParseTarget(address)
	if err != nil {
		return err
	}
	if target.Scheme != "https" {
		return errors.New("one-click unsubscribe is only followed over https")
	}
	timed, cancel := context.WithTimeout(ctx, safefetch.Timeout)
	defer cancel()

	body := strings.NewReader("List-Unsubscribe=One-Click")
	request, err := http.NewRequestWithContext(timed, http.MethodPost, target.String(), body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := safefetch.Client().Do(request)
	if err != nil {
		return err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Debugf("failed to close the unsubscribe response: %s", err)
		}
	}()
	// Read a little and discard it: the answer is the status, and a sender
	// that replies with a gigabyte should not be able to spend this server's
	// memory saying nothing.
	_, _ = io.CopyN(io.Discard, response.Body, 64<<10)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("the sender answered %s", response.Status)
	}
	return nil
}

// mailUnsubscribe writes to the address the sender named, from this mailbox,
// with the subject and body the address asked for.
func (self *graph) mailUnsubscribe(ctx context.Context, mailbox *models.Mailbox, address string) error {
	target, err := url.Parse(address)
	if err != nil {
		return err
	}
	to := strings.TrimSpace(target.Opaque)
	if to == "" {
		to = strings.TrimSpace(target.Path)
	}
	if to == "" {
		return errors.New("the address to leave by is empty")
	}
	query := target.Query()
	subject := strings.TrimSpace(query.Get("subject"))
	if subject == "" {
		// What senders that read these look for, and harmless to one that
		// only reads the address.
		subject = "unsubscribe"
	}
	text := strings.TrimSpace(query.Get("body"))
	if text == "" {
		text = "unsubscribe"
	}
	from := ""
	if len(mailbox.Addresses) > 0 {
		from = mailbox.Addresses[0].Address
	}
	if from == "" {
		return errors.New("this mailbox has no address to write from")
	}

	_, err = self.SendMailboxMessage(ctx, SendMailboxMessageArguments{
		MailboxID: mailbox.ID,
		Message: MailboxMessageParameters{
			From:        from,
			To:          []string{to},
			Subject:     subject,
			TextContent: text,
		},
	})
	return err
}
