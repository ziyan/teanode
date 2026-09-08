package mx

import (
	"context"
	"io"
	"net/textproto"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Mail that lives here.
//
// A message reaching a mailbox is not copied: an item in the mailbox's Inbox
// references the one stored message, with the folder's next UID, and a
// delivery of kind mailbox records that it got there. Three addresses in
// three mailboxes is three items, three deliveries, one Mail.

// searchDocumentLimit bounds how much of a message's text goes into the
// search document: enough to find a message by what it says, not the whole
// of a newsletter.
const searchDocumentLimit = 64 * 1024

// searchDocumentNames bounds how many attachment names join it.
const searchDocumentNames = 200

// deliverToMailbox places a message in a mailbox's Inbox and records the
// delivery, already delivered: there is nothing to queue.
func (self *exchange) deliverToMailbox(tx db.Transaction, mailbox *models.Mailbox, alias *models.Alias, recipient string, mail *models.Mail) (*models.Delivery, error) {
	inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
	if err != nil {
		return nil, err
	}
	if inbox == nil {
		// A mailbox with no Inbox is one that is being deleted under us.
		return nil, nil
	}
	// Where it lands: the Inbox, unless the filter called it spam or the
	// sender's own DMARC policy asked for suspicion, in which case Junk.
	// A message under a quarantine policy was accepted rather than refused
	// because that is what quarantine asks for; this is the quarantine.
	target := inbox
	if isSuspicious(mail) {
		junk, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindJunk)
		if err != nil {
			return nil, err
		}
		if junk != nil {
			target = junk
		}
	}
	// One copy per mailbox, however many aliases point at it. A domain with a
	// catch-all into a mailbox and a named address into the same mailbox
	// matches both for that address, and delivering twice puts the message in
	// the Inbox twice — which is what a person sees, and which no
	// configuration of aliases should be able to cause.
	//
	// One existence query, because this is the SMTP path: every message
	// delivered anywhere asks it.
	already, err := tx.MailIsInMailbox(mail.ID, mailbox.ID)
	if err != nil {
		return nil, err
	}
	if already {
		log.Noticef("message %q is already in mailbox %q, not delivering it again", mail.ID, mailbox.ID)
		return nil, nil
	}

	item, err := tx.AddItem(target.ID, mail.ID, models.MailboxItemFlags{})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	delivery := &models.Delivery{
		MailID:        mail.ID,
		Mail:          mail,
		AliasID:       alias.ID,
		Alias:         alias,
		Recipient:     recipient,
		Kind:          models.DeliveryKindMailbox,
		Status:        models.DeliveryStatusDelivered,
		Size:          mail.Size,
		DeliveredAt:   &now,
		MailboxID:     mailbox.ID,
		MailboxItemID: item.ID,
		Method:        "mailbox",
		Destination:   mailbox.Name,
	}
	// The sender becomes a contact of the mailbox, for completion and for
	// the "sender is known" rule.
	if address, name := senderOf(mail); address != "" {
		if err := tx.TouchContact(mailbox.ID, address, name, mail.ReceivedAt); err != nil {
			return nil, err
		}
	}
	// Then the mailbox's rules, against the new item.
	if err := self.runRules(tx, mailbox, target, item, mail); err != nil {
		log.Warningf("the rules of mailbox %q failed on message %q: %s", mailbox.ID, mail.ID, err)
	}
	// And the out-of-office reply, decided after the rules have had their
	// say about where the message ended up.
	self.maybeAutoReply(tx, mailbox, alias, recipient, item, mail)
	return delivery, nil
}

// FindSentCopy is the item a Sent folder already holds for a message, by
// Message-ID, or nil.
//
// A message a person sends reaches their Sent folder twice: this server
// files it there when it accepts the submission, and the mail program then
// uploads its own copy over IMAP, because a program cannot know that the
// server has filed it. The two arrive tens of milliseconds apart, in either
// order, and are the same message under one Message-ID. Whichever comes
// second finds the first here and leaves it at that. Only Sent: a program
// uploads a draft again on purpose, and there the newer copy is the one
// wanted.
func FindSentCopy(tx db.Transaction, folder *models.MailboxFolder, messageId string) (*models.MailboxItem, error) {
	if folder == nil || folder.Kind != models.MailboxFolderKindSent || strings.TrimSpace(messageId) == "" {
		return nil, nil
	}
	return tx.FindItemByMessageID(folder.ID, strings.TrimSpace(messageId))
}

// isSuspicious is whether a message belongs in Junk rather than the Inbox:
// the spam filter failed it, or it failed DMARC under a quarantine policy.
func isSuspicious(mail *models.Mail) bool {
	results := mail.AuthenticationResults
	if results.SpamFilter != nil && results.SpamFilter.Result == "fail" {
		return true
	}
	if results.DMARC != nil && results.DMARC.Result == "fail" && results.DMARC.Policy == "quarantine" {
		return true
	}
	return false
}

// senderOf is the From address and display name of a message.
func senderOf(mail *models.Mail) (string, string) {
	from := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "From"))
	if from == "" {
		return strings.ToLower(mail.From), ""
	}
	address, err := mailparse.ParseAddress(from)
	if err != nil {
		return strings.ToLower(mail.From), ""
	}
	name := from
	if at := strings.LastIndex(name, "<"); at >= 0 {
		name = name[:at]
	} else if strings.EqualFold(strings.TrimSpace(name), address) {
		name = ""
	}
	name = strings.Trim(strings.TrimSpace(name), "\" ")
	return strings.ToLower(address), name
}

// ThreadIDFor is the conversation a message belongs to: the thread of
// whatever it answers, by In-Reply-To or References, or nothing when it
// answers nothing and so begins one of its own.
//
// Exported because the exchange is not the only thing that stores a message.
// A draft saved from the dashboard and a message a mail program appends over
// IMAP are stored too, and one stored without a conversation reads as a
// conversation of its own — which for a draft reply, or a program's own copy
// of what it sent, is wrong in the one place it shows.
func ThreadIDFor(tx db.Transaction, headers []string) (string, error) {
	candidates := threadCandidates(headers)
	if len(candidates) == 0 {
		return "", nil
	}
	return tx.FindThreadID(candidates)
}

// threadCandidates is the message ids a message says it answers: the one in
// In-Reply-To and every one in References, which carries the whole chain.
//
// Both headers are a list of angle-bracketed ids separated by whitespace, and
// a long References is folded across lines by the sending program, so the
// value is read as fields rather than as one string. Whichever of these the
// server already has decides the conversation.
func threadCandidates(headers []string) []string {
	var candidates []string
	for _, name := range []string{"In-Reply-To", "References"} {
		value := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, name))
		for _, field := range strings.Fields(value) {
			if field = strings.TrimSpace(field); field != "" {
				candidates = append(candidates, field)
			}
		}
	}
	return candidates
}

// SearchDocument is the text a folder search runs over: subject, sender,
// recipients, the readable body and the attachments' names.
func SearchDocument(mail *models.Mail) string {
	return searchDocument(mail, mailparse.AttachmentNames(mail.Headers, mail.Body))
}

// AttachmentCount is how many attachments a message carries, for the index.
func AttachmentCount(mail *models.Mail) int {
	return len(mailparse.AttachmentNames(mail.Headers, mail.Body))
}

// searchDocument is what full text search runs over: subject, sender,
// recipients, and the message's text, bounded.
func searchDocument(mail *models.Mail, names []string) string {
	var builder strings.Builder
	builder.WriteString(mail.Subject)
	builder.WriteString("\n")
	builder.WriteString(mail.From)
	builder.WriteString("\n")
	builder.WriteString(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "From")))
	builder.WriteString("\n")
	builder.WriteString(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "To")))
	builder.WriteString("\n")
	builder.WriteString(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "Cc")))
	builder.WriteString("\n")
	for _, recipient := range mail.Recipients {
		builder.WriteString(recipient)
		builder.WriteString("\n")
	}
	remaining := searchDocumentLimit
	_ = mailparse.TraverseParts(mail.Headers, mail.Body, func(header textproto.MIMEHeader, reader io.Reader) error {
		if remaining <= 0 {
			return nil
		}
		contentType := strings.ToLower(header.Get("Content-Type"))
		if !strings.HasPrefix(contentType, "text/plain") {
			return nil
		}
		part, err := mailparse.DecodePart(header, reader, int64(remaining))
		if err != nil || part == nil {
			return nil
		}
		text := string(part.Content)
		if len(text) > remaining {
			text = text[:remaining]
		}
		remaining -= len(text)
		builder.WriteString(text)
		builder.WriteString("\n")
		return nil
	})
	// An attachment's name three ways, because the parser reads
	// "invoice-march.pdf" as one file token and a person types any part of
	// it: as given, without its extension, and with its punctuation as spaces.
	// Bounded like the text is: a message of thousands of named parts
	// would otherwise make a document PostgreSQL refuses to index.
	for index, name := range names {
		if index >= searchDocumentNames || remaining <= 0 {
			break
		}
		remaining -= len(name) * 3
		builder.WriteString("\n")
		builder.WriteString(name)
		if dot := strings.LastIndex(name, "."); dot > 0 {
			builder.WriteString("\n")
			builder.WriteString(name[:dot])
		}
		builder.WriteString("\n")
		builder.WriteString(strings.Map(func(character rune) rune {
			if character == '.' || character == '-' || character == '_' {
				return ' '
			}
			return character
		}, name))
	}
	return builder.String()
}

// indexMail records what search and threading need once a message is stored:
// its search document, and the retention clock when nobody holds it.
func (self *exchange) indexMail(tx db.Transaction, mail *models.Mail, held bool) error {
	names := mailparse.AttachmentNames(mail.Headers, mail.Body)
	if err := tx.SetMailSearch(mail.ID, searchDocument(mail, names), len(names)); err != nil {
		return err
	}
	if held {
		return nil
	}
	now := time.Now()
	_, err := tx.ModifyMail(mail.ID, func(mail *models.Mail) error {
		mail.UnreferencedAt = &now
		return nil
	}, nil)
	return err
}

// scavengeRetention is the grace a message gets once nothing holds it: the
// spool retention, which is what the age-based sweep used before there were
// mailboxes, so today's behavior is the degenerate case.
func (self *exchange) scavengeRetention() time.Duration {
	return self.config.Current().Storage.SpoolRetention.Duration()
}

// scavengeMailOnce prunes messages unreferenced for longer than the
// retention, and what is stored for them, on one instance at a time: the
// advisory lock is what stops two sweeps racing a new item.
func (self *exchange) scavengeMailOnce(ctx context.Context) error {
	retention := self.scavengeRetention()
	if retention <= 0 {
		return nil
	}
	var removed []string
	if err := self.database.Transaction(func(tx db.Transaction) error {
		locked, err := tx.TryAdvisoryLock(scavengeLockKey)
		if err != nil || !locked {
			return err
		}
		removed, err = tx.ScavengeMails(time.Now().Add(-retention), scavengeBatch)
		if err != nil {
			return err
		}
		_, err = tx.ScavengeExpunged(time.Now().Add(-expungeLogRetention))
		return err
	}); err != nil {
		return err
	}
	for _, mailId := range removed {
		if err := self.storage.Delete(ctx, mailId); err != nil {
			log.Warningf("failed to remove the stored message %q: %s", mailId, err)
		}
	}
	if len(removed) > 0 {
		log.Noticef("removed %d messages unreferenced for longer than %s", len(removed), retention)
	}
	return nil
}

const (
	// scavengeLockKey is the advisory lock the mail sweep takes.
	scavengeLockKey = 0x7ea0de01

	// scavengeBatch bounds one sweep, so a backlog is worked off a thousand
	// rows at a time rather than in one transaction that holds the lock for
	// an hour.
	scavengeBatch = 1000

	// expungeLogRetention is how long a folder remembers what left it, for
	// clients syncing with QRESYNC. A client older than this gets the full
	// UID list once.
	expungeLogRetention = 90 * 24 * time.Hour
)
