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

	// Left asks for the lists already left instead of the ones still
	// subscribed to. One or the other, not both: a list somebody has left is
	// not one they are subscribed to, and a page called Subscriptions that
	// shows both is answering two questions at once. Default false.
	//
	// A request that failed is not a list left — nothing was accepted and the
	// mail keeps coming — so those are with the subscribed.
	Left *bool `json:"left"`
}

type MailboxSubscriptionPage struct {
	Subscriptions []*models.MailboxSubscription `json:"subscriptions"`
	Total         int64                         `json:"total"`

	// How many there are on each side, whichever side is being shown, so the
	// switch between them can say what it would show.
	Subscribed int64 `json:"subscribed"`
	Left       int64 `json:"left"`
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
	side := db.SubscribedTo
	if arguments.Left != nil && *arguments.Left {
		side = db.Left
	}
	tx := self.transaction(ctx)
	subscriptions, err := tx.ListSubscriptions(mailbox.ID, limit, offset, side)
	if err != nil {
		return nil, err
	}
	// Both counts, whichever side is being read: the switch says how many are
	// on the other side as well, and a number nobody can see is how somebody
	// comes to wonder where a list they remember has gone.
	subscribed, err := tx.CountSubscriptions(mailbox.ID, db.SubscribedTo)
	if err != nil {
		return nil, err
	}
	left, err := tx.CountSubscriptions(mailbox.ID, db.Left)
	if err != nil {
		return nil, err
	}
	total := subscribed
	if side == db.Left {
		total = left
	}
	return &MailboxSubscriptionPage{
		Subscriptions: subscriptions,
		Total:         total,
		Subscribed:    subscribed,
		Left:          left,
	}, nil
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
	// The same mail the listing counted. Trash and Junk are left out there —
	// what you threw away is not a subscription you have, and what a filter
	// caught is not one you agreed to — and a reader that showed them anyway
	// disagreed with the count beside the list's own name.
	deleted := false
	items, err := tx.ListItems("", &db.ItemOptions{
		MailboxID:    mailbox.ID,
		ListKey:      arguments.Key,
		ByReceived:   true,
		Limit:        threadLimit,
		Deleted:      &deleted,
		ExcludeKinds: []models.MailboxFolderKind{models.MailboxFolderKindTrash, models.MailboxFolderKindJunk},
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

// archiveLimit bounds what muting clears out of the Inbox in one go. A list
// somebody is only now muting can have a great deal of mail there, and this
// runs inside the request's transaction.
const archiveLimit = 500

type MuteMailboxSubscriptionArguments struct {
	// MailboxID of the mailbox the list writes to
	MailboxID string `json:"mailboxId"`

	// Key of the subscription, as ListMailboxSubscriptions gives it
	Key string `json:"key"`

	// Muted: true to keep it out of the Inbox, false to let it back
	Muted bool `json:"muted"`
}

// MuteMailboxSubscription keeps a list arriving and stops it being in the way.
//
// The other answer to a newsletter, and often the better one. Leaving tells
// the sender that a person reads this address and cannot be taken back; some
// lists offer no way out at all; and a reader may want the mail without
// wanting it first thing. A muted list is filed in the Archive, unread, and is
// still there to search and to read as a group — the unread count on the list
// is how somebody comes back to what they have not read yet.
//
// Muting also clears what the Inbox is holding from that list, because a
// reader muting a list while its mail sits in front of them means both.
func (self *graph) MuteMailboxSubscription(ctx context.Context,
	arguments MuteMailboxSubscriptionArguments) (*models.MailboxSubscription, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
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

	if err := tx.SetSubscriptionMuted(mailbox.ID, subscription.Key, arguments.Muted); err != nil {
		return nil, err
	}

	moved := 0
	if arguments.Muted {
		if moved, err = self.archiveInboxMail(tx, mailbox, subscription.Key); err != nil {
			return nil, err
		}
	}

	if arguments.Muted {
		log.Noticef("%s muted the list %q, and archived %d of its messages from the Inbox",
			operatorName(ctx), subscription.Key, moved)
	} else {
		log.Noticef("%s unmuted the list %q", operatorName(ctx), subscription.Key)
	}
	return tx.GetSubscription(mailbox.ID, subscription.Key)
}

// archiveInboxMail files what the Inbox holds from one list into the Archive,
// read. It reports how many, and does nothing when the mailbox has no Archive.
func (self *graph) archiveInboxMail(tx db.Transaction, mailbox *models.Mailbox, key string) (int, error) {
	inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
	if err != nil || inbox == nil {
		return 0, err
	}
	archive, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindArchive)
	if err != nil || archive == nil {
		return 0, err
	}

	items, err := tx.ListItems(inbox.ID, &db.ItemOptions{ListKey: key, Limit: archiveLimit})
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	itemIds := make([]string, 0, len(items))
	for _, item := range items {
		itemIds = append(itemIds, item.ID)
	}
	// Moved, and left as they were found. Muting a list is a reader saying
	// where its mail belongs, not that they have read it; marking it read on
	// the way past is answering a question nobody asked.
	if _, err := tx.MoveItems(itemIds, archive.ID); err != nil {
		return 0, err
	}
	return len(itemIds), nil
}

type ShowMailboxSubscriptionImagesArguments struct {
	// MailboxID of the mailbox the list writes to
	MailboxID string `json:"mailboxId"`

	// Key of the subscription, as ListMailboxSubscriptions gives it
	Key string `json:"key"`

	// Show: true to load this list's pictures without asking, false to go
	// back to asking
	Show bool `json:"show"`
}

// ShowMailboxSubscriptionImages says the pictures in this list's mail may be
// loaded without asking, every time.
//
// The cost is the same as loading them once, repeated: the sender learns the
// message was opened. A reader who has decided that for a newsletter they
// read every week should be able to say so once.
func (self *graph) ShowMailboxSubscriptionImages(ctx context.Context,
	arguments ShowMailboxSubscriptionImagesArguments) (*models.MailboxSubscription, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
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
	if err := tx.SetSubscriptionImages(mailbox.ID, subscription.Key, arguments.Show); err != nil {
		return nil, err
	}
	log.Noticef("%s set pictures in %q to %s", operatorName(ctx), subscription.Key,
		map[bool]string{true: "always load", false: "ask"}[arguments.Show])
	return tx.GetSubscription(mailbox.ID, subscription.Key)
}

type GetMailboxSubscriptionArguments struct {
	// MailboxID of the mailbox to read
	MailboxID string `json:"mailboxId"`

	// ID of the subscription, as a link to one names it. Either this or the
	// key; this is what the dashboard sends.
	ID *string `json:"id"`

	// Key of the subscription, as ListMailboxSubscriptions gives it. Still
	// answered, so that a link made when the key was the identity keeps
	// working.
	Key *string `json:"key"`
}

// GetMailboxSubscription is one list: the same row the list gives, for a page
// showing only that one.
func (self *graph) GetMailboxSubscription(ctx context.Context,
	arguments GetMailboxSubscriptionArguments) (*models.MailboxSubscription, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	identity, key := "", ""
	if arguments.ID != nil {
		identity = strings.TrimSpace(*arguments.ID)
	}
	if arguments.Key != nil {
		key = strings.TrimSpace(*arguments.Key)
	}
	var subscription *models.MailboxSubscription
	if identity != "" {
		subscription, err = tx.GetSubscriptionByID(mailbox.ID, identity)
	} else {
		subscription, err = tx.GetSubscription(mailbox.ID, key)
	}
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
