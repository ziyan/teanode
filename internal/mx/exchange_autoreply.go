package mx

import (
	"bytes"
	"context"
	"fmt"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/dkim"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

// The out-of-office reply is the one thing a mailbox sends without a person
// pressing send, so most of this is about when not to send it: RFC 3834 and
// the lessons of every autoresponder loop since.

const (
	// autoReplyQuiet is how long a sender is left alone after one reply.
	autoReplyQuiet = 7 * 24 * time.Hour

	// autoReplyHourlyLimit is the most replies a mailbox sends in an hour, a
	// last defence against whatever the other rules did not catch.
	autoReplyHourlyLimit = 50
)

// maybeAutoReply answers a message that reached the Inbox, when the mailbox
// is away and every protection allows it. Errors are logged, not returned:
// the message has been delivered, and a reply that could not be sent is not
// a reason to refuse it.
func (self *exchange) maybeAutoReply(tx db.Transaction, mailbox *models.Mailbox, alias *models.Alias, recipient string, item *models.MailboxItem, mail *models.Mail) {
	setting := mailbox.AutoReply
	if setting == nil || !setting.Enabled {
		return
	}
	now := time.Now()
	if setting.From != nil && now.Before(*setting.From) {
		return
	}
	if setting.Until != nil && now.After(*setting.Until) {
		return
	}
	reason, err := self.autoReplyRefusal(tx, mailbox, recipient, item, mail, now)
	if err != nil {
		log.Warningf("cannot decide the out-of-office reply for mailbox %q: %s", mailbox.ID, err)
		return
	}
	if reason != "" {
		log.Debugf("no out-of-office reply from mailbox %q to %q: %s", mailbox.ID, mail.Sender, reason)
		return
	}
	if err := self.sendAutoReply(tx, mailbox, alias, recipient, mail, setting, now); err != nil {
		log.Warningf("failed to send the out-of-office reply from mailbox %q to %q: %s", mailbox.ID, mail.Sender, err)
	}
}

// autoReplyRefusal is why a reply is not sent, or empty when it is.
func (self *exchange) autoReplyRefusal(tx db.Transaction, mailbox *models.Mailbox, recipient string, item *models.MailboxItem, mail *models.Mail, now time.Time) (string, error) {
	// Still in the Inbox: not filed elsewhere or deleted by a rule, not in
	// Junk, not classified as spam.
	current, err := tx.GetItem(item.ID)
	if err != nil {
		return "", err
	}
	if current == nil {
		return "the message was deleted by a rule", nil
	}
	inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
	if err != nil {
		return "", err
	}
	if inbox == nil || current.FolderID != inbox.ID {
		return "the message is not in the Inbox", nil
	}
	if isSuspicious(mail) {
		return "the message looks like spam", nil
	}

	// An empty envelope sender is a bounce or another machine's automatic
	// reply, and answering it is how loops start.
	sender := strings.ToLower(strings.TrimSpace(mail.Sender))
	if sender == "" {
		return "the envelope sender is empty", nil
	}

	// Mailing lists and notification senders are never answered.
	if value := strings.ToLower(strings.TrimSpace(mailparse.FindHeaderValue(mail.Headers, "Auto-Submitted"))); value != "" && value != "no" {
		return "the message was sent automatically", nil
	}
	switch strings.ToLower(strings.TrimSpace(mailparse.FindHeaderValue(mail.Headers, "Precedence"))) {
	case "bulk", "list", "junk":
		return "the message is bulk or list mail", nil
	}
	for _, header := range []string{"List-Id", "List-Post", "List-Unsubscribe"} {
		if mailparse.FindHeaderValue(mail.Headers, header) != "" {
			return "the message came through a mailing list", nil
		}
	}

	// Written to this person on purpose: one of the mailbox's addresses in
	// To or Cc, not reached through Bcc, a wildcard or a forward.
	mine := map[string]bool{}
	for _, address := range mailbox.Addresses {
		mine[strings.ToLower(address.Address)] = true
	}
	addressed := false
	for _, header := range []string{"To", "Cc"} {
		list, err := netmail.ParseAddressList(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, header)))
		if err != nil {
			continue
		}
		for _, address := range list {
			if mine[strings.ToLower(address.Address)] {
				addressed = true
			}
		}
	}
	if !addressed {
		return "none of the mailbox's addresses is in To or Cc", nil
	}

	// Not to ourselves, and not to another mailbox here that is also away:
	// the two-colleagues-on-holiday loop, refused before it starts.
	if mine[sender] {
		return "the sender is this mailbox", nil
	}
	if other, err := self.mailboxAt(tx, sender); err != nil {
		return "", err
	} else if other != nil && other.AutoReply != nil && other.AutoReply.Enabled {
		return "the sender is another mailbox here with an out-of-office reply on", nil
	}

	// Once a week per sender, fifty an hour per mailbox.
	contact, err := tx.GetContact(mailbox.ID, sender)
	if err != nil {
		return "", err
	}
	if contact != nil && contact.AutoRepliedAt != nil && now.Sub(*contact.AutoRepliedAt) < autoReplyQuiet {
		return "the sender was replied to recently", nil
	}
	count, err := tx.CountAutoRepliesSince(mailbox.ID, now.Add(-time.Hour))
	if err != nil {
		return "", err
	}
	if count >= autoReplyHourlyLimit {
		return "the mailbox has sent enough automatic replies this hour", nil
	}
	return "", nil
}

// mailboxAt is the mailbox an address on this server delivers into, or nil.
func (self *exchange) mailboxAt(tx db.Transaction, address string) (*models.Mailbox, error) {
	localPart, domainName := mailparse.SplitAddress(address)
	domain, err := tx.GetDomainByName(domainName)
	if err != nil || domain == nil {
		return nil, err
	}
	for _, alias := range self.matchingAliases(domain, localPart) {
		if alias.Kind == models.AliasKindMailbox && alias.MailboxID != "" {
			return tx.GetMailbox(alias.MailboxID)
		}
	}
	return nil, nil
}

// sendAutoReply builds the reply so that it cannot start a loop even where
// the other side has none of the protections above: from the address that
// was written to, to the envelope sender, marked auto-replied, and with an
// empty envelope sender of its own so it can neither bounce back nor be
// answered by a responder that follows the rules. It goes through the
// ordinary queue, signed like anything else, and gets no item in Sent.
func (self *exchange) sendAutoReply(tx db.Transaction, mailbox *models.Mailbox, alias *models.Alias, recipient string, original *models.Mail, setting *models.MailboxAutoReply, now time.Time) error {
	domain, err := tx.GetDomain(alias.DomainID)
	if err != nil {
		return err
	}
	if domain == nil {
		return fmt.Errorf("the domain of %q is gone", recipient)
	}
	to := strings.TrimSpace(original.Sender)

	subject := strings.TrimSpace(setting.Subject)
	if subject == "" {
		subject = "Auto: " + original.Subject
	}
	var body bytes.Buffer
	bodyHeaders, err := mailparse.Compose(&body, []byte(setting.Text), []byte(setting.HTML), nil)
	if err != nil {
		return err
	}
	id := security.NewULID()
	messageId := fmt.Sprintf("<%s@%s>", id, domain.Domain)
	from := &netmail.Address{Name: mailbox.Name, Address: recipient}
	headers := []string{
		mailparse.UnsplitHeader("Message-ID", messageId),
		mailparse.UnsplitHeader("Date", now.Format(time.RFC1123Z)),
		mailparse.UnsplitHeader("From", from.String()),
		mailparse.UnsplitHeader("To", to),
		mailparse.UnsplitHeader("Subject", mailparse.EncodeHeaderValue(subject)),
		mailparse.UnsplitHeader("Auto-Submitted", "auto-replied"),
		mailparse.UnsplitHeader("X-Auto-Response-Suppress", "All"),
		mailparse.UnsplitHeader("Precedence", "auto_reply"),
	}
	if original.MessageID != "" {
		references := original.MessageID
		if existing := strings.TrimSpace(mailparse.FindHeaderValue(original.Headers, "References")); existing != "" {
			references = existing + " " + original.MessageID
		}
		headers = append(headers,
			mailparse.UnsplitHeader("In-Reply-To", original.MessageID),
			mailparse.UnsplitHeader("References", references),
		)
	}
	headers = mailparse.MergeHeaders(headers, bodyHeaders)

	// Signed with the domain's key, as anything this server sends is.
	if signer, selector, ok := self.signerFor(domain); ok {
		signed, err := dkim.Sign(headers, body.Bytes(), &dkim.SignOptions{Domain: domain.Domain, Selector: selector, Signer: signer})
		if err != nil {
			return err
		}
		headers = mailparse.MergeHeaders(signed, headers)
	}

	reply, err := tx.CreateMail(&models.Mail{
		DomainID:   domain.ID,
		EnvelopeID: id,
		Sender:     "", // MAIL FROM:<>
		Recipients: []string{to},
		MessageID:  messageId,
		From:       recipient,
		Subject:    subject,
		Headers:    headers,
		Body:       body.Bytes(),
		Size:       uint64(body.Len()),
		Status:     models.MailStatusAccepted,
		ReceivedAt: now,
		Kind:       models.MailKindOutgoing,
	}, nil)
	if err != nil {
		return err
	}
	if err := self.storage.Put(context.Background(), reply.ID, headers, body.Bytes()); err != nil {
		return err
	}
	// To a person here, by reference into their mailbox, the way any local
	// recipient is reached; to anyone else, through the queue.
	localPart, domainName := mailparse.SplitAddress(to)
	if local, err := tx.GetDomainByName(domainName); err != nil {
		return err
	} else if local != nil {
		deliveries, err := self.matchAliases(tx, local, localPart, reply)
		if err != nil {
			return err
		}
		if _, err := tx.CreateDeliveries(deliveries, nil); err != nil {
			return err
		}
	} else if _, err := tx.CreateDeliveries([]*models.Delivery{{
		MailID:    reply.ID,
		Mail:      reply,
		Recipient: to,
		Kind:      models.DeliveryKindExternal,
	}}, nil); err != nil {
		return err
	}
	if err := tx.MarkContactAutoReplied(mailbox.ID, strings.ToLower(to), now); err != nil {
		return err
	}
	log.Noticef("mailbox %q sent its out-of-office reply to %q", mailbox.ID, to)
	return nil
}
