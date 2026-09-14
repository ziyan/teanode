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
	// autoReplyHourlyLimit is the most replies a mailbox sends in an hour, a
	// last defense against whatever the other rules did not catch.
	//
	// What stood beside it -- one reply per sender per week -- is gone, and
	// with it the ledger of every address that had ever written, which was
	// the only thing that made it possible. This is what actually stops a
	// loop: two away messages talking to each other are stopped by the
	// fiftieth, not by the calendar.
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
	claimed, err := tx.ClaimAutoReply(mailbox.ID, now, autoReplyHourlyLimit)
	if err != nil {
		log.Warningf("cannot claim the out-of-office reply for mailbox %q: %s", mailbox.ID, err)
		return
	}
	if !claimed {
		log.Debugf("no out-of-office reply from mailbox %q to %q: the mailbox has sent enough this hour", mailbox.ID, mail.Sender)
		return
	}
	if err := self.sendAutoReply(tx, mailbox, alias, recipient, mail, setting, now); err != nil {
		log.Warningf("failed to send the out-of-office reply from mailbox %q to %q: %s", mailbox.ID, mail.Sender, err)
	}
}

// AutoReplyRefusal is the ladder as the agent climbs it before answering
// on the person's behalf: the same refusals as the out-of-office reply,
// with the agent's own quiet period per sender.
func (self *exchange) AutoReplyRefusal(tx db.Transaction, mailbox *models.Mailbox, recipient string, item *models.MailboxItem, mail *models.Mail, now time.Time) (string, error) {
	return self.autoReplyRefusal(tx, mailbox, recipient, item, mail, now)
}

// unvouchedSender is why the envelope sender is not somewhere to write to,
// or empty when something stands behind it.
//
// Two things can: SPF passing for the domain in MAIL FROM, which is exactly
// the question "may this host send as that address"; or the envelope sender
// being the same address the From header carries, when DMARC passed -- then
// the domain that authorized the message is the domain being written to.
func unvouchedSender(mail *models.Mail, sender string) string {
	results := mail.AuthenticationResults
	if results.SPF != nil && results.SPF.Result == "pass" {
		return ""
	}
	if mail.DMARCPassed() {
		from := strings.ToLower(strings.TrimSpace(mail.From))
		if from != "" && strings.EqualFold(from, sender) {
			return ""
		}
	}
	return "nothing vouched for the address the reply would go to"
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

	// And an envelope sender nothing vouched for is not somewhere to send
	// this server's own signed mail.
	//
	// The reply goes to MAIL FROM, which is not what DMARC checks: DMARC
	// aligns the From header, and a message may pass it with an envelope
	// sender belonging to somebody else entirely. Accepting such a message
	// is right -- the From domain really did authorize it -- but answering
	// it is a reflector: an attacker sends from a domain of their own with
	// MAIL FROM naming their victim, and this server writes to the victim,
	// from the person's address, signed by their domain, quoting a subject
	// the attacker chose. Varying the local part walks the per-sender
	// limits. It is also how a stranger reads an away message that names
	// the person, their dates and their deputy.
	if reason := unvouchedSender(mail, sender); reason != "" {
		return reason, nil
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

	// Colleagues only, where the mailbox asked for that: the sender is at
	// one of the domains this mailbox's own addresses are at.
	if mailbox.AutoReply != nil && mailbox.AutoReply.SameDomainOnly {
		_, senderDomain := mailparse.SplitAddress(sender)
		found := false
		for _, address := range mailbox.Addresses {
			if _, domain := mailparse.SplitAddress(strings.ToLower(address.Address)); domain != "" && domain == senderDomain {
				found = true
				break
			}
		}
		if !found {
			return "the sender is not at one of this mailbox's own domains", nil
		}
	}

	// The hourly limit is counted where the reply is claimed, in one
	// statement, so that two instances cannot both be the fiftieth.
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
	log.Noticef("mailbox %q sent its out-of-office reply to %q", mailbox.ID, to)
	return nil
}
