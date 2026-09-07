package apigraph

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/aggregate"
	"github.com/ziyan/teanode/internal/util/mailparse"
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
	// Read a draft back into the compose page
	GetMailboxDraft(ctx context.Context, arguments GetMailboxDraftArguments) (*MailboxDraft, error)
}

type MailboxComposeMutation interface {
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

	// Files to attach, sent with this call
	Attachments []*AttachmentParameters `json:"attachments" graphapi:"nullable"`

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
}

type SendMailboxMessageArguments struct {
	MailboxID string `json:"mailboxId"`

	Message MailboxMessageParameters `json:"message"`
}

type SendMailboxMessageReturnValue struct {
	// The message as stored, once accepted
	Mail *models.Mail `json:"mail"`
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
}

// Private headers a draft carries for the compose page's sake and no
// recipient ever sees: they are written into drafts only, and a draft is
// sent by building a new message from its fields.
const (
	draftHeaderBcc     = "X-TeaNode-Draft-Bcc"
	draftHeaderReplyTo = "X-TeaNode-Draft-Reply-To-Item"
	draftHeaderForward = "X-TeaNode-Draft-Forward-Item"
)

// SendMailboxMessage sends from a mailbox as one of its addresses. The
// permission is mail:send; the address is the mailbox's own, so a person
// sends as who they are and not as whoever they name.
func (self *graph) SendMailboxMessage(ctx context.Context, arguments SendMailboxMessageArguments) (*SendMailboxMessageReturnValue, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailSend, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	parameters := &arguments.Message
	message, domain, err := self.buildMailboxMessage(ctx, tx, mailbox, parameters, false)
	if err != nil {
		return nil, err
	}
	if len(message.To)+len(message.Cc)+len(message.Bcc) == 0 {
		return nil, fmt.Errorf("%w: a message needs a recipient", api.ErrInvalidArguments)
	}
	if strings.TrimSpace(message.Text) == "" && strings.TrimSpace(message.HTML) == "" && len(message.Attachments) == 0 {
		return nil, fmt.Errorf("%w: a message needs a body or an attachment", api.ErrInvalidArguments)
	}

	// Threading. In-Reply-To names the message answered; References carries
	// its own references plus itself, which is how a thread stays one.
	var replied *models.MailboxItem
	if parameters.ReplyToItemID != "" {
		item, original, err := self.requireOwnItem(ctx, mailbox, parameters.ReplyToItemID)
		if err != nil {
			return nil, err
		}
		replied = item
		if original != nil && original.MessageID != "" {
			references := original.MessageID
			if headers, _, err := self.storage.Get(ctx, original.ID); err == nil {
				if existing := strings.TrimSpace(mailparse.FindHeaderValue(headers, "References")); existing != "" {
					references = existing + " " + original.MessageID
				}
			}
			message.Headers = append(message.Headers,
				mailparse.UnsplitHeader("In-Reply-To", original.MessageID),
				mailparse.UnsplitHeader("References", references),
			)
		}
	}
	var forwarded *models.MailboxItem
	if parameters.ForwardItemID != "" {
		item, _, err := self.requireOwnItem(ctx, mailbox, parameters.ForwardItemID)
		if err != nil {
			return nil, err
		}
		forwarded = item
	}

	envelope := self.envelopeFromRequest(ctx)
	envelope.MailboxID = mailbox.ID
	if err := self.mailer.Send(ctx, envelope, message); err != nil {
		return nil, err
	}

	// Sent. What remains is bookkeeping in the caller's transaction: the
	// answered and forwarded flags, and the draft this was written from.
	yes := true
	if replied != nil {
		if _, err := tx.SetItemFlags([]string{replied.ID}, models.MailboxItemFlags{Answered: &yes}); err != nil {
			return nil, err
		}
	}
	if forwarded != nil {
		if _, err := tx.SetItemFlags([]string{forwarded.ID}, models.MailboxItemFlags{Forwarded: &yes}); err != nil {
			return nil, err
		}
	}
	if parameters.DraftItemID != "" {
		if err := self.removeDraft(ctx, tx, mailbox, parameters.DraftItemID); err != nil {
			return nil, err
		}
	}

	mails, err := tx.ListMails(domain.ID, &db.Options{
		Limit:   1,
		Columns: mailColumns,
		Aggregations: aggregate.Pipeline{{Match: &aggregate.Filter{
			Operation: aggregate.OperationEqual,
			Field:     "envelopeId",
			Value:     &envelope.ID,
		}}},
	})
	if err != nil {
		return nil, err
	}
	if len(mails) == 0 {
		log.Warningf("sent envelope %q but found no stored mail for it", envelope.ID)
		return &SendMailboxMessageReturnValue{}, nil
	}
	return &SendMailboxMessageReturnValue{Mail: mails[0]}, nil
}

// SaveMailboxDraft stores what is being written as a message in Drafts,
// and removes the previous save of it.
func (self *graph) SaveMailboxDraft(ctx context.Context, arguments SaveMailboxDraftArguments) (*models.MailboxItem, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	parameters := &arguments.Message
	message, domain, err := self.buildMailboxMessage(ctx, tx, mailbox, parameters, true)
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
		if _, _, err := self.requireOwnItem(ctx, mailbox, parameters.ReplyToItemID); err != nil {
			return nil, err
		}
		message.Headers = append(message.Headers, mailparse.UnsplitHeader(draftHeaderReplyTo, parameters.ReplyToItemID))
	}
	if parameters.ForwardItemID != "" {
		if _, _, err := self.requireOwnItem(ctx, mailbox, parameters.ForwardItemID); err != nil {
			return nil, err
		}
		message.Headers = append(message.Headers, mailparse.UnsplitHeader(draftHeaderForward, parameters.ForwardItemID))
	}

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
	created, err := tx.CreateMail(&models.Mail{
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
	item, err := tx.AddItem(drafts.ID, created.ID, models.MailboxItemFlags{Draft: &yes, Seen: &yes})
	if err != nil {
		return nil, translateError(err)
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
	mailbox, err := self.requireDraftOwner(ctx, arguments.ItemID)
	if err != nil {
		return nil, err
	}
	item, stored, err := self.requireOwnItem(ctx, mailbox, arguments.ItemID)
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

// buildMailboxMessage turns the compose page's fields into a message: the
// sender checked against the mailbox's addresses, the attachments gathered
// from the upload, the draft being continued and the message being
// forwarded. The signature is the mailbox's, added once, by the page: the
// server does not append one, so what is saved is what was written.
func (self *graph) buildMailboxMessage(ctx context.Context, tx db.Transaction, mailbox *models.Mailbox, parameters *MailboxMessageParameters, draft bool) (*mailer.Message, *models.Domain, error) {
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
	attachments := make([]*mailparse.Attachment, 0, len(parameters.Attachments))
	add := func(attachment *mailparse.Attachment) error {
		total += uint64(len(attachment.Content))
		if limit > 0 && total > limit {
			return fmt.Errorf("%w: the attachments come to more than the %d bytes a message may be", api.ErrInvalidArguments, limit)
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
	for _, attachment := range parameters.Attachments {
		if attachment == nil {
			continue
		}
		if strings.TrimSpace(attachment.Filename) == "" {
			return nil, nil, fmt.Errorf("%w: an attachment needs a filename", api.ErrInvalidArguments)
		}
		if err := add(&mailparse.Attachment{
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			Content:     attachment.Content,
		}); err != nil {
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
		parts = append(parts, &mailparse.Attachment{
			Filename:    part.Filename,
			ContentType: part.ContentType,
			Content:     part.Content,
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

// requireDraftOwner is the mailbox holding a draft item, for the caller.
func (self *graph) requireDraftOwner(ctx context.Context, itemId string) (*models.Mailbox, error) {
	principal, err := self.requirePermission(ctx, models.PermissionMailRead)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := tx.GetItem(itemId)
	if err != nil {
		return nil, err
	}
	if item == nil {
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

// removeDraft removes a superseded draft: its item, its row and its bytes.
// A draft nobody wants back gets no retention grace.
func (self *graph) removeDraft(ctx context.Context, tx db.Transaction, mailbox *models.Mailbox, itemId string) error {
	item, stored, err := self.requireOwnItem(ctx, mailbox, itemId)
	if err != nil {
		if errors.Is(err, api.ErrNotFound) {
			// Already gone — saved from two tabs, or sent twice.
			return nil
		}
		return err
	}
	if !item.Draft {
		return fmt.Errorf("%w: %q is not a draft", api.ErrInvalidArguments, itemId)
	}
	if _, err := tx.DeleteItems([]string{item.ID}); err != nil {
		return err
	}
	if stored != nil {
		if err := tx.DeleteMail(stored.ID, nil); err != nil {
			return err
		}
		if err := self.storage.Delete(ctx, stored.ID); err != nil && !errors.Is(err, storage.ErrNotFound) {
			log.Warningf("failed to remove the bytes of draft %q: %s", stored.ID, err)
		}
	}
	return nil
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
