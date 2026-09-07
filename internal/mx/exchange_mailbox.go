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
	name := strings.TrimSpace(strings.TrimSuffix(from, "<"+address+">"))
	name = strings.Trim(name, "\" ")
	return strings.ToLower(address), name
}

// threadIDFor is the conversation a message belongs to: the thread of
// whatever it answers, by In-Reply-To or References, or a new one of its own.
func threadIDFor(tx db.Transaction, headers []string) (string, error) {
	var candidates []string
	for _, name := range []string{"In-Reply-To", "References"} {
		value := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, name))
		for _, field := range strings.Fields(value) {
			field = strings.TrimSpace(field)
			if field != "" {
				candidates = append(candidates, field)
			}
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	return tx.FindThreadID(candidates)
}

// searchDocument is what full text search runs over: subject, sender,
// recipients, and the message's text, bounded.
func searchDocument(mail *models.Mail) string {
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
	return builder.String()
}

// indexMail records what search and threading need once a message is stored:
// its search document, and the retention clock when nobody holds it.
func (self *exchange) indexMail(tx db.Transaction, mail *models.Mail, held bool) error {
	if err := tx.SetMailSearch(mail.ID, searchDocument(mail)); err != nil {
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
// mailboxes, so today's behaviour is the degenerate case.
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
