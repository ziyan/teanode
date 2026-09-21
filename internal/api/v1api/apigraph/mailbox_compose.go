package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	mailboxcommands "github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

// Writing from a mailbox: a new message, a reply, a forward, and the draft
// any of them is while it is being written.
//
// A draft is a complete message stored the way every message is stored,
// with a row of kind draft and an item in Drafts. Saving again writes a new
// one and removes the old in the same transaction; sending removes the draft
// and the message goes the way any submission goes, with an item in Sent
// added by the exchange in the transaction that records it.

type MailboxComposeQuery interface {
	// Recover a previously accepted send, including after its message was deleted.
	GetMailboxSubmission(ctx context.Context, arguments GetMailboxSubmissionArguments) (*MailboxSubmission, error)

	// Read a draft back into the compose page
	GetMailboxDraft(ctx context.Context, arguments GetMailboxDraftArguments) (*MailboxDraft, error)

	// Find a draft by the key it keeps across saves, for anything holding
	// on to one between one save and the next. Needs mail:read.
	FindMailboxDraft(ctx context.Context, arguments FindMailboxDraftArguments) (*MailboxDraft, error)
}

type MailboxComposeMutation interface {
	// Prevent an uncertain send, or recover its existing acceptance.
	CancelMailboxSubmission(ctx context.Context, arguments GetMailboxSubmissionArguments) (*MailboxSubmission, error)

	// Send a message from a mailbox, as one of its addresses
	SendMailboxMessage(ctx context.Context, arguments SendMailboxMessageArguments) (*SendMailboxMessageReturnValue, error)

	// Store a message being written, replacing the previous save
	SaveMailboxDraft(ctx context.Context, arguments SaveMailboxDraftArguments) (*models.MailboxItem, error)
}

// MailboxMessageParameters is what the compose page holds.
type MailboxMessageParameters struct {
	// Address to send as; one of the mailbox's addresses
	From string `json:"from"`

	// Display name beside it, optional
	FromName string `json:"fromName" graphapi:"nullable"`

	// Recipients, each an address or "Name <address>"
	To  []string `json:"to" graphapi:"nullable"`
	Cc  []string `json:"cc" graphapi:"nullable"`
	Bcc []string `json:"bcc" graphapi:"nullable"`

	Subject string `json:"subject" graphapi:"nullable"`

	// The body, in either or both forms
	HTMLContent string `json:"htmlContent" graphapi:"nullable"`
	TextContent string `json:"textContent" graphapi:"nullable"`

	// Item of the message being replied to, if any: sets In-Reply-To and
	// References, and marks the item answered once sent
	ReplyToItemID string `json:"replyToItemId" graphapi:"nullable"`

	// Item of the message being forwarded, if any, and which of its
	// attachments to carry, by index
	ForwardItemID      string `json:"forwardItemId" graphapi:"nullable"`
	ForwardAttachments []int  `json:"forwardAttachments" graphapi:"nullable"`

	// Item of the draft this is written from, if any: its attachments may
	// be kept by index, and it is removed when this is saved or sent
	DraftItemID     string `json:"draftItemId" graphapi:"nullable"`
	KeepAttachments []int  `json:"keepAttachments" graphapi:"nullable"`

	// Files of the caller's own agent conversation to carry as pictures
	// the HTML refers to: each becomes an inline part whose Content-ID is
	// the file's name, so <img src="cid:chart.png"> finds it. This is how
	// an agent illustrates a message -- a chart it drew, a picture it was
	// given -- without the bytes passing through the model or through a
	// second upload.
	InlineImages []string `json:"inlineImages" graphapi:"nullable"`
}

// GetMailboxSubmissionArguments identifies a send within an owned mailbox.
type GetMailboxSubmissionArguments struct {
	MailboxID    string `json:"mailboxId"`
	SubmissionID string `json:"submissionId"`
}

// MailboxSubmission is the durable local acceptance, not remote delivery status.
type MailboxSubmission struct {
	SubmissionID string    `json:"submissionId"`
	MailID       string    `json:"mailId"`
	SentItemID   string    `json:"sentItemId"`
	AcceptedAt   time.Time `json:"acceptedAt"`
	IsReconciled bool      `json:"isReconciled"`
}

// GetMailboxSubmission exposes only the caller's accepted send identities.
func (self *graph) GetMailboxSubmission(ctx context.Context, arguments GetMailboxSubmissionArguments) (*MailboxSubmission, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailSend, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	accepted, err := self.transaction(ctx).GetSubmission(mailbox.UserID, arguments.SubmissionID)
	if err != nil {
		return nil, translateError(err)
	}
	if accepted == nil || accepted.MailboxID != mailbox.ID {
		return nil, nil
	}
	return mailboxSubmission(accepted), nil
}

// CancelMailboxSubmission returns nil only when this identity cannot send later.
// A non-nil result means acceptance won the race; accepted mail is not recalled.
func (self *graph) CancelMailboxSubmission(ctx context.Context, arguments GetMailboxSubmissionArguments) (*MailboxSubmission, error) {
	principal, err := self.requirePermission(ctx, models.PermissionMailSend)
	if err != nil {
		return nil, err
	}
	accepted, err := mailer.NewSubmissionCoordinator(self.transaction(ctx), self.mailer).Cancel(ctx, principal, arguments.MailboxID, arguments.SubmissionID)
	if err != nil {
		return nil, translateError(err)
	}
	return mailboxSubmission(accepted), nil
}

func mailboxSubmission(accepted *models.Submission) *MailboxSubmission {
	if accepted == nil {
		return nil
	}
	return &MailboxSubmission{SubmissionID: accepted.SubmissionID, MailID: accepted.MailID, SentItemID: accepted.SentItemID, AcceptedAt: accepted.AcceptedAt, IsReconciled: accepted.ReconciledAt != nil}
}

type SendMailboxMessageArguments struct {
	MailboxID string `json:"mailboxId"`
	// SubmissionID is retained for retries of one send; changing its content is refused.
	SubmissionID string `json:"submissionId" graphapi:"nullable"`

	Message MailboxMessageParameters `json:"message"`
}

type SendMailboxMessageReturnValue struct {
	// The message as stored, once accepted
	Mail *models.Mail `json:"mail"`

	// Where the copy of it landed in this mailbox, when there is a Sent
	// folder to land in. The dashboard opens the message that was just sent,
	// and an item is what it takes to open one — a mail identifier names the
	// message, not the copy of it in front of this person.
	Item *models.MailboxItem `json:"item"`
}

type SaveMailboxDraftArguments struct {
	MailboxID string `json:"mailboxId"`

	Message MailboxMessageParameters `json:"message"`
}

type GetMailboxDraftArguments struct {
	// The item in Drafts
	ItemID string `json:"itemId"`
}

// MailboxDraft is a stored draft read back into fields.
type MailboxDraft struct {
	ItemID   string        `json:"itemId"`
	MailID   string        `json:"mailId"`
	From     string        `json:"from"`
	FromName string        `json:"fromName,omitempty"`
	To       []string      `json:"to"`
	Cc       []string      `json:"cc"`
	Bcc      []string      `json:"bcc"`
	Subject  string        `json:"subject"`
	HTML     string        `json:"html,omitempty"`
	Text     string        `json:"text,omitempty"`
	Language string        `json:"language,omitempty"`
	Parts    []*Attachment `json:"attachments"`

	// What the draft was a reply to or a forward of, when it was: the
	// compose page keeps the thread when the draft is sent.
	ReplyToItemID string `json:"replyToItemId,omitempty"`
	ForwardItemID string `json:"forwardItemId,omitempty"`

	// Key names the draft across saves; ItemID names this save of it.
	Key string `json:"key,omitempty"`
}

// Private headers a draft carries for the compose page's sake and no
// recipient ever sees: they are written into drafts only, and a draft is
// sent by building a new message from its fields.
const (
	draftHeaderBcc     = mx.DraftHeaderBcc
	draftHeaderReplyTo = mx.DraftHeaderReplyTo
	draftHeaderForward = mx.DraftHeaderForward
	draftHeaderKey     = mx.DraftHeaderKey
)

// SendMailboxMessage sends from a mailbox as one of its addresses. The
// permission is mail:send; the address is the mailbox's own, so a person
// sends as who they are and not as whoever they name.
func (self *graph) SendMailboxMessage(ctx context.Context, arguments SendMailboxMessageArguments) (*SendMailboxMessageReturnValue, error) {
	principal, err := self.requirePermission(ctx, models.PermissionMailSend)
	if err != nil {
		return nil, err
	}
	parameters := &arguments.Message
	requestContent, err := json.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	request := mailer.SubmissionRequest{
		SubmissionID: arguments.SubmissionID, MailboxID: arguments.MailboxID, RequestContent: requestContent,
		DraftItemID: parameters.DraftItemID, ReplyItemID: parameters.ReplyToItemID, ForwardItemID: parameters.ForwardItemID,
	}
	var response *SendMailboxMessageReturnValue
	err = self.transaction(ctx).TransactionContext(ctx, func(command db.Transaction) error {
		outcome, err := mailer.NewSubmissionCoordinator(command, self.mailer).Submit(ctx, principal, request, func(ctx context.Context, preparation db.Transaction, mailbox *models.Mailbox) (*mailparse.Envelope, *mailer.Message, error) {
			ctx = api.ContextWithTransaction(ctx, preparation)
			message, _, err := self.buildMailboxMessage(ctx, preparation, mailbox, parameters, nil)
			if err != nil {
				return nil, nil, err
			}
			if len(message.To)+len(message.Cc)+len(message.Bcc) == 0 {
				return nil, nil, fmt.Errorf("%w: a message needs a recipient", api.ErrInvalidArguments)
			}
			if strings.TrimSpace(message.Text) == "" && strings.TrimSpace(message.HTML) == "" && len(message.Attachments) == 0 {
				return nil, nil, fmt.Errorf("%w: a message needs a body or an attachment", api.ErrInvalidArguments)
			}
			_, threading, err := self.threadingHeaders(ctx, mailbox, parameters.ReplyToItemID)
			if err != nil {
				return nil, nil, err
			}
			message.Headers = append(message.Headers, threading...)
			envelope := self.envelopeFromRequest(ctx)
			envelope.MailboxID = mailbox.ID
			return envelope, message, nil
		})
		if err != nil {
			return err
		}
		accepted := outcome.Submission
		stored, err := command.GetMail(accepted.MailID, &db.Options{Columns: mailColumns})
		if err != nil {
			return err
		}
		item, err := command.GetItem(accepted.SentItemID)
		if err != nil {
			return err
		}
		if item != nil {
			folder, err := command.GetFolder(item.FolderID)
			if err != nil {
				return err
			}
			if folder == nil || folder.MailboxID != accepted.MailboxID || item.MailID != accepted.MailID {
				item = nil
			}
		}
		// Mail and its Sent item can have been deleted since acceptance. A replay
		// still succeeds with the existing nullable response fields, without resending.
		response = &SendMailboxMessageReturnValue{Mail: stored, Item: item}
		if err := mailer.NewSubmissionReconciler(command).Reconcile(ctx, accepted.OwnerID, accepted.SubmissionID); err != nil {
			// The failed savepoint keeps acceptance and pending recovery intact. The
			// background worker completes it after this caller commits.
			log.Warningf("cannot reconcile accepted submission %q immediately: %s", accepted.SubmissionID, err)
		}
		return nil
	})
	if errors.Is(err, mailer.ErrSubmissionConflict) {
		return nil, fmt.Errorf("%w: submission identifier already used for different content", api.ErrInvalidArguments)
	}
	if errors.Is(err, mailer.ErrSubmissionCancelled) {
		return nil, fmt.Errorf("%w: submission was cancelled", api.ErrInvalidArguments)
	}
	if err != nil {
		return nil, translateError(err)
	}
	return response, nil
}

// SaveMailboxDraft stores what is being written as a message in Drafts,
// and removes the previous save of it.
func (self *graph) SaveMailboxDraft(ctx context.Context, arguments SaveMailboxDraftArguments) (*models.MailboxItem, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	return self.saveDraft(ctx, self.transaction(ctx), mailbox, &arguments.Message, nil)
}

// threadingHeaders is In-Reply-To and References for a reply: the message
// being answered, and the chain it belongs to plus itself. Empty for a
// message that answers nothing.
//
// Returns the item replied to as well, which the send path marks answered.
func (self *graph) threadingHeaders(ctx context.Context, mailbox *models.Mailbox, replyToItemId string) (*models.MailboxItem, []string, error) {
	if replyToItemId == "" {
		return nil, nil, nil
	}
	item, original, err := self.requireOwnItem(ctx, mailbox, replyToItemId)
	if err != nil {
		return nil, nil, err
	}
	if original == nil || original.MessageID == "" {
		return item, nil, nil
	}
	references := original.MessageID
	if headers, _, err := self.storage.Get(ctx, original.ID); err == nil {
		if existing := strings.TrimSpace(mailparse.FindHeaderValue(headers, "References")); existing != "" {
			references = existing + " " + original.MessageID
		}
	}
	return item, []string{
		mailparse.UnsplitHeader("In-Reply-To", original.MessageID),
		mailparse.UnsplitHeader("References", references),
	}, nil
}

// saveDraft stores what is being written as a message in Drafts — the
// fields given, the parts kept from the draft being continued, and any
// files just uploaded — and removes the previous save of it.
func (self *graph) saveDraft(ctx context.Context, tx db.Transaction, mailbox *models.Mailbox, parameters *MailboxMessageParameters, uploads []*mailparse.Attachment) (*models.MailboxItem, error) {
	message, domain, err := self.buildMailboxMessage(ctx, tx, mailbox, parameters, uploads)
	if err != nil {
		return nil, err
	}
	// A draft may be empty of everything but a subject; the composer needs
	// something to put in the body for the message to be one at all.
	if strings.TrimSpace(message.Text) == "" && strings.TrimSpace(message.HTML) == "" && len(message.Attachments) == 0 {
		message.Text = " "
	}
	if len(message.Bcc) > 0 {
		message.Headers = append(message.Headers, mailparse.UnsplitHeader(draftHeaderBcc, strings.Join(message.Bcc, ", ")))
	}
	if parameters.ReplyToItemID != "" {
		// The same headers a sent reply carries, so that a draft reply
		// belongs to the conversation it answers and is shown in it. Without
		// them the draft has nothing to say what it answers, and a thread
		// somebody was midway through writing into looked empty of it.
		_, threading, err := self.threadingHeaders(ctx, mailbox, parameters.ReplyToItemID)
		if err != nil {
			return nil, err
		}
		message.Headers = append(message.Headers, threading...)
		message.Headers = append(message.Headers, mailparse.UnsplitHeader(draftHeaderReplyTo, parameters.ReplyToItemID))
	}
	if parameters.ForwardItemID != "" {
		if _, _, err := self.requireOwnItem(ctx, mailbox, parameters.ForwardItemID); err != nil {
			return nil, err
		}
		message.Headers = append(message.Headers, mailparse.UnsplitHeader(draftHeaderForward, parameters.ForwardItemID))
	}
	// The draft's own name, carried from the save being replaced so that it
	// is the same draft afterwards, and made here when there is none.
	key, err := self.draftKeyOf(ctx, mailbox, parameters.DraftItemID)
	if err != nil {
		return nil, err
	}
	message.Headers = append(message.Headers, mailparse.UnsplitHeader(draftHeaderKey, key))

	composed, err := self.mailer.Compose(ctx, message)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	drafts, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
	if err != nil {
		return nil, err
	}
	if drafts == nil {
		return nil, api.ErrNotFound
	}

	recipients := append(append(append([]string{}, message.To...), message.Cc...), message.Bcc...)
	now := time.Now()
	// A draft reply belongs to the conversation it answers, so that it shows
	// in it rather than as a message of its own with nothing around it.
	threadId, err := mx.ThreadIDFor(tx, composed.Headers)
	if err != nil {
		return nil, err
	}
	created, err := tx.CreateMail(&models.Mail{
		ThreadID:   threadId,
		DomainID:   domain.ID,
		EnvelopeID: composed.ID,
		Sender:     message.From,
		Recipients: recipients,
		MessageID:  mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(composed.Headers, "Message-ID")),
		From:       message.From,
		Subject:    message.Subject,
		Headers:    composed.Headers,
		Body:       composed.Body,
		Size:       uint64(len(composed.Body)),
		Status:     models.MailStatusAccepted,
		ReceivedAt: now,
		Kind:       models.MailKindDraft,
	}, nil)
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.storage.Put(ctx, created.ID, composed.Headers, composed.Body); err != nil {
		return nil, err
	}
	yes := true
	item, err := tx.AddItem(drafts.ID, created.ID, "", models.MailboxItemFlags{Draft: &yes, Seen: &yes})
	if err != nil {
		return nil, translateError(err)
	}
	if err := tx.SetMailSearch(created.ID, mx.SearchDocument(created), mx.AttachmentCount(created)); err != nil {
		return nil, err
	}
	if parameters.DraftItemID != "" {
		if err := self.removeDraft(ctx, tx, mailbox, parameters.DraftItemID); err != nil {
			return nil, err
		}
	}
	return item, nil
}

// GetMailboxDraft reads a stored draft back into the fields it was written
// from, with the parser that reads any message.
func (self *graph) GetMailboxDraft(ctx context.Context, arguments GetMailboxDraftArguments) (*MailboxDraft, error) {
	mailbox, err := self.requireDraftOwner(ctx, models.PermissionMailRead, arguments.ItemID)
	if err != nil {
		return nil, err
	}
	return self.readDraft(ctx, mailbox, arguments.ItemID)
}

// readDraft reads a stored draft back into the fields it was written from,
// with the parser that reads any message.
func (self *graph) readDraft(ctx context.Context, mailbox *models.Mailbox, itemId string) (*MailboxDraft, error) {
	item, stored, err := self.requireOwnItem(ctx, mailbox, itemId)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, api.ErrNotFound
	}
	headers, body, err := self.storage.Get(ctx, stored.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, api.ErrNotFound
		}
		return nil, err
	}
	content, err := renderContent(stored.ID, headers, body)
	if err != nil {
		return nil, err
	}
	draft := &MailboxDraft{
		ItemID:        item.ID,
		MailID:        stored.ID,
		Subject:       mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, "Subject")),
		HTML:          content.HTML,
		Text:          content.Text,
		Language:      strings.TrimSpace(mailparse.FindHeaderValue(headers, "Content-Language")),
		Parts:         content.Attachments,
		To:            addressesOf(headers, "To"),
		Cc:            addressesOf(headers, "Cc"),
		Bcc:           addressesOf(headers, draftHeaderBcc),
		ReplyToItemID: strings.TrimSpace(mailparse.FindHeaderValue(headers, draftHeaderReplyTo)),
		ForwardItemID: strings.TrimSpace(mailparse.FindHeaderValue(headers, draftHeaderForward)),
		Key:           strings.TrimSpace(mailparse.FindHeaderValue(headers, draftHeaderKey)),
	}
	if from, err := mail.ParseAddress(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, "From"))); err == nil {
		draft.From = from.Address
		draft.FromName = from.Name
	}
	// A draft with no body was stored with a single space to be a message
	// at all; that space is not what was written.
	if strings.TrimSpace(draft.Text) == "" {
		draft.Text = ""
	}
	return draft, nil
}

// draftKeyOf is the name the draft already has, or a new one.
//
// A draft that is gone gets a new name -- that is an ordinary race, and the
// save is making a draft rather than continuing one. Anything else is
// returned: minting a new name because a read failed would look like it
// worked and quietly break whatever was holding the old one, which is the
// failure this key exists to prevent.
func (self *graph) draftKeyOf(ctx context.Context, mailbox *models.Mailbox, draftItemId string) (string, error) {
	if strings.TrimSpace(draftItemId) == "" {
		return security.NewULID(), nil
	}
	_, stored, err := self.requireOwnItem(ctx, mailbox, draftItemId)
	if errors.Is(err, api.ErrNotFound) || (err == nil && stored == nil) {
		return security.NewULID(), nil
	}
	if err != nil {
		return "", err
	}
	headers, _, err := self.storage.Get(ctx, stored.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return security.NewULID(), nil
	}
	if err != nil {
		return "", err
	}
	if key := strings.TrimSpace(mailparse.FindHeaderValue(headers, draftHeaderKey)); key != "" {
		return key, nil
	}
	return security.NewULID(), nil
}

// FindMailboxDraftArguments name a draft by the key it keeps across saves.
type FindMailboxDraftArguments struct {
	MailboxID string `json:"mailboxId"`
	Key       string `json:"key"`
}

// FindMailboxDraft is the draft with this key as it stands now.
//
// Saving a draft replaces the message that holds it, so an item id names one
// save. This answers "where is that draft now", which is what anything
// holding on to a draft between one save and the next has to ask.
func (self *graph) FindMailboxDraft(ctx context.Context, arguments FindMailboxDraftArguments) (*MailboxDraft, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(arguments.Key)
	if key == "" {
		return nil, fmt.Errorf("%w: which draft? give key", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	drafts, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
	if err != nil {
		return nil, err
	}
	if drafts == nil {
		return nil, api.ErrNotFound
	}
	items, err := tx.ListItems(drafts.ID, &db.ItemOptions{Limit: draftsSearched})
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		headers, _, err := self.storage.Get(ctx, item.MailID)
		if err != nil {
			continue
		}
		if strings.TrimSpace(mailparse.FindHeaderValue(headers, draftHeaderKey)) == key {
			return self.readDraft(ctx, mailbox, item.ID)
		}
	}
	return nil, api.ErrNotFound
}

// draftsSearched bounds the walk above. A person has a handful of drafts;
// somebody with more than this many has a Drafts folder they are not using
// as one, and the newest are the ones anything is waiting on.
const draftsSearched = 200

// buildMailboxMessage turns the compose page's fields into a message: the
// sender checked against the mailbox's addresses, the attachments gathered
// from the upload, the draft being continued and the message being
// forwarded. The signature is the mailbox's, added once, by the page: the
// server does not append one, so what is saved is what was written.
func (self *graph) buildMailboxMessage(ctx context.Context, tx db.Transaction, mailbox *models.Mailbox, parameters *MailboxMessageParameters, uploads []*mailparse.Attachment) (*mailer.Message, *models.Domain, error) {
	fromAddress, err := mail.ParseAddress(parameters.From)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %q is not an address", api.ErrInvalidArguments, parameters.From)
	}
	var address *models.MailboxAddress
	for _, candidate := range mailbox.Addresses {
		if strings.EqualFold(candidate.Address, fromAddress.Address) {
			address = candidate
			break
		}
	}
	if address == nil {
		return nil, nil, fmt.Errorf("%w: %q is not an address of this mailbox", api.ErrInvalidArguments, fromAddress.Address)
	}
	domain, err := tx.GetDomain(address.DomainID)
	if err != nil {
		return nil, nil, err
	}
	if domain == nil {
		return nil, nil, api.ErrNotFound
	}
	fromName := parameters.FromName
	if fromName == "" {
		fromName = fromAddress.Name
	}

	to, err := parseAddresses(parameters.To)
	if err != nil {
		return nil, nil, err
	}
	cc, err := parseAddresses(parameters.Cc)
	if err != nil {
		return nil, nil, err
	}
	bcc, err := parseAddresses(parameters.Bcc)
	if err != nil {
		return nil, nil, err
	}

	limit := self.config.Current().SMTP.MaxMessageSize.Bytes()
	var total uint64
	attachments := make([]*mailparse.Attachment, 0, len(uploads))
	add := func(attachment *mailparse.Attachment) error {
		total += uint64(len(attachment.Content))
		if limit > 0 && total > limit {
			return fmt.Errorf("%w: the attachments come to more than the %d bytes a message may be: %w", api.ErrInvalidArguments, limit, errTooLarge)
		}
		attachments = append(attachments, attachment)
		return nil
	}
	// Parts carried over from the draft being continued, and from the
	// message being forwarded: copied out of the stored message, so the
	// browser never uploads a file it did not just choose.
	if parameters.DraftItemID != "" && len(parameters.KeepAttachments) > 0 {
		kept, err := self.partsOf(ctx, mailbox, parameters.DraftItemID, parameters.KeepAttachments)
		if err != nil {
			return nil, nil, err
		}
		for _, part := range kept {
			if err := add(part); err != nil {
				return nil, nil, err
			}
		}
	}
	if parameters.ForwardItemID != "" && len(parameters.ForwardAttachments) > 0 {
		carried, err := self.partsOf(ctx, mailbox, parameters.ForwardItemID, parameters.ForwardAttachments)
		if err != nil {
			return nil, nil, err
		}
		for _, part := range carried {
			if err := add(part); err != nil {
				return nil, nil, err
			}
		}
	}
	// Pictures the body refers to by cid:, from the caller's own agent
	// conversation. Before the uploads, because they belong to the body.
	if len(parameters.InlineImages) > 0 {
		pictures, err := self.inlinePicturesOf(ctx, tx, parameters.InlineImages)
		if err != nil {
			return nil, nil, err
		}
		for _, picture := range pictures {
			if err := add(picture); err != nil {
				return nil, nil, err
			}
		}
	}
	// Files just uploaded, through the upload route, come last.
	for _, upload := range uploads {
		if upload == nil {
			continue
		}
		if strings.TrimSpace(upload.Filename) == "" {
			return nil, nil, fmt.Errorf("%w: an attachment needs a filename", api.ErrInvalidArguments)
		}
		if err := add(upload); err != nil {
			return nil, nil, err
		}
	}

	return &mailer.Message{
		From:        fromAddress.Address,
		FromName:    fromName,
		To:          to,
		Cc:          cc,
		Bcc:         bcc,
		Subject:     parameters.Subject,
		Text:        parameters.TextContent,
		HTML:        parameters.HTMLContent,
		Attachments: attachments,
	}, domain, nil
}

// inlinePicturesOf is the caller's own agent files, as parts the body can
// refer to by cid: under their own names.
//
// The caller's own: an attachment belongs to an agent, an agent belongs to a
// person, and the person asking to send the message must be that person.
// Anything else would make a file id -- which is quoted in transcripts the
// agent can read -- a way to put somebody else's picture into a message.
func (self *graph) inlinePicturesOf(ctx context.Context, tx db.Transaction, attachmentIds []string) ([]*mailparse.Attachment, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	pictures := make([]*mailparse.Attachment, 0, len(attachmentIds))
	for _, attachmentId := range attachmentIds {
		attachment, err := tx.GetAgentAttachment(strings.TrimSpace(attachmentId))
		if err != nil {
			return nil, err
		}
		if attachment == nil || attachment.AgentID != found.ID {
			return nil, fmt.Errorf("%w: there is no file %q", api.ErrNotFound, attachmentId)
		}
		if !tools.IsImage(attachment.ContentType) {
			return nil, fmt.Errorf("%w: %q is %s; only a picture goes in the body", api.ErrInvalidArguments, attachment.Name, attachment.ContentType)
		}
		content, err := self.storage.GetFile(ctx, attachment.ID)
		if err != nil {
			return nil, err
		}
		pictures = append(pictures, &mailparse.Attachment{
			Filename: attachment.Name, ContentType: attachment.ContentType,
			Content: content, ContentID: attachment.Name, Inline: true,
		})
	}
	return pictures, nil
}

// partsOf is the attachments named by index from a message in one of the
// caller's folders.
func (self *graph) partsOf(ctx context.Context, mailbox *models.Mailbox, itemId string, indexes []int) ([]*mailparse.Attachment, error) {
	_, stored, err := self.requireOwnItem(ctx, mailbox, itemId)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, api.ErrNotFound
	}
	headers, body, err := self.storage.Get(ctx, stored.ID)
	if err != nil {
		return nil, err
	}
	parts := make([]*mailparse.Attachment, 0, len(indexes))
	for _, index := range indexes {
		part, err := mailparse.PartAt(headers, body, index)
		if err != nil {
			return nil, fmt.Errorf("%w: no attachment %d", api.ErrInvalidArguments, index)
		}
		// The Content-ID and the inline flag come too: a picture the body
		// refers to by cid: is still that picture after the draft is saved
		// and opened again, and dropping them turned an illustration into
		// an attachment and a broken image.
		parts = append(parts, &mailparse.Attachment{
			Filename:    part.Filename,
			ContentType: part.ContentType,
			Content:     part.Content,
			ContentID:   part.ContentID,
			Inline:      part.Inline,
		})
	}
	return parts, nil
}

// requireOwnItem is an item in one of the mailbox's folders, with its
// message, or not found.
func (self *graph) requireOwnItem(ctx context.Context, mailbox *models.Mailbox, itemId string) (*models.MailboxItem, *models.Mail, error) {
	tx := self.transaction(ctx)
	item, err := tx.GetItem(itemId)
	if err != nil {
		return nil, nil, err
	}
	if item == nil {
		return nil, nil, api.ErrNotFound
	}
	folder, err := tx.GetFolder(item.FolderID)
	if err != nil {
		return nil, nil, err
	}
	if folder == nil || folder.MailboxID != mailbox.ID {
		return nil, nil, api.ErrNotFound
	}
	stored, err := tx.GetMail(item.MailID, nil)
	if err != nil {
		return nil, nil, err
	}
	return item, stored, nil
}

// requireDraftOwner is the mailbox holding a draft item, for a caller with
// the permission: reading it to open it, writing it to rewrite it. An item
// that is not a draft is not found, whatever it is.
func (self *graph) requireDraftOwner(ctx context.Context, permission models.Permission, itemId string) (*models.Mailbox, error) {
	principal, err := self.requirePermission(ctx, permission)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := tx.GetItem(itemId)
	if err != nil {
		return nil, err
	}
	if item == nil || !item.Draft {
		return nil, api.ErrNotFound
	}
	folder, err := tx.GetFolder(item.FolderID)
	if err != nil {
		return nil, err
	}
	if folder == nil {
		return nil, api.ErrNotFound
	}
	mailbox, err := tx.GetMailbox(folder.MailboxID)
	if err != nil {
		return nil, err
	}
	if mailbox == nil || principal.User == nil || mailbox.UserID != principal.User.ID {
		return nil, api.ErrNotFound
	}
	return mailbox, nil
}

// removeDraft removes a superseded draft from its mailbox. Retention removes
// unreferenced mail and bytes after commit so rollback can restore a readable draft.
func (self *graph) removeDraft(ctx context.Context, tx db.Transaction, mailbox *models.Mailbox, itemId string) error {
	return translateError(mailboxcommands.RemoveDraft(ctx, tx, mailbox.ID, itemId))
}

// envelopeFromRequest is an envelope carrying where the request came from.
func (self *graph) envelopeFromRequest(ctx context.Context) *mailparse.Envelope {
	envelope := &mailparse.Envelope{}
	if request := api.ContextRequest(ctx); request != nil {
		host, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil {
			host = request.RemoteAddr
		}
		envelope.IP = net.ParseIP(host)
		envelope.Location = self.locator.Locate(envelope.IP)
		envelope.TLS = request.TLS
	}
	return envelope
}

// addressesOf is a header's addresses, as "Name <address>" strings.
func addressesOf(headers []string, name string) []string {
	value := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, name))
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parsed, err := mail.ParseAddressList(value)
	if err != nil {
		return []string{value}
	}
	addresses := make([]string, 0, len(parsed))
	for _, address := range parsed {
		addresses = append(addresses, address.String())
	}
	return addresses
}
