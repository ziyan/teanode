package mx

import (
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Statements mailed in.
//
// A person's statement import address is their own mailbox address with a
// detail after a plus: name+statements-<token>@domain. Nothing new is
// needed in DNS or among the aliases, and the token, which only the person
// is shown, is what lets a message in. Such a message is filed in the
// mailbox's Archive, read, so it does not wait in the Inbox, and the agent
// is told to import its OFX files; no rule, no triage, no calendar and no
// out-of-office reply sees it. A wrong token is no statement address at
// all: the recipient is matched like any other, which for an address
// nobody configured is a refusal.

// statementDetailPrefix begins the detail of a statement import address.
const statementDetailPrefix = "statements-"

// matchStatementImport delivers a message addressed to a statement import
// address, and says whether the recipient was one. A message the spam
// filter failed, or that failed DMARC under a quarantine policy, is not
// imported: it is answered false and goes the ordinary way, which for it
// is Junk or nothing.
func (self *exchange) matchStatementImport(tx db.Transaction, domain *models.Domain, recipientAlias string, mail *models.Mail) ([]*models.Delivery, bool, error) {
	localPart, detail, hasDetail := strings.Cut(recipientAlias, "+")
	if !hasDetail || localPart == "" {
		return nil, false, nil
	}
	token, isStatement := strings.CutPrefix(strings.ToLower(detail), statementDetailPrefix)
	if !isStatement || token == "" {
		return nil, false, nil
	}
	hook, isStatementHook := self.currentAgentHook().(StatementHook)
	if !isStatementHook || isSuspicious(mail) {
		return nil, false, nil
	}
	recipient := mailparse.UnsplitAddress(recipientAlias, domain.Domain)
	for _, alias := range self.matchingAliases(domain, localPart) {
		// The person's own address, never a catch-all: an address shown
		// to nobody must not become a way in.
		if alias.Kind != models.AliasKindMailbox || alias.IsCatchAll() {
			continue
		}
		mailbox, err := tx.GetMailbox(alias.MailboxID)
		if err != nil {
			return nil, false, err
		}
		if mailbox == nil || !hook.IsStatementImportToken(tx, mailbox, token) {
			continue
		}
		self.trackAliasUsageAfterCommit(tx, mail.ReceivedAt, alias.ID, aliasUsage{
			bytesReceived: mail.Size,
			mailsAccepted: 1,
		})
		delivery, err := self.deliverStatement(tx, hook, mailbox, alias, recipient, mail)
		if err != nil {
			return nil, false, err
		}
		if delivery == nil {
			return nil, true, nil
		}
		return []*models.Delivery{delivery}, true, nil
	}
	return nil, false, nil
}

// deliverStatement files a message that reached a statement import address
// in the mailbox's Archive, read, records the delivery, and tells the hook.
func (self *exchange) deliverStatement(tx db.Transaction, hook StatementHook, mailbox *models.Mailbox, alias *models.Alias, recipient string, mail *models.Mail) (*models.Delivery, error) {
	target, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindArchive)
	if err != nil {
		return nil, err
	}
	if target == nil {
		if target, err = tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox); err != nil {
			return nil, err
		}
	}
	if target == nil {
		// A mailbox with neither folder is one being deleted under us.
		return nil, nil
	}
	// The same message reaching the address twice, by two aliases into
	// one mailbox, is imported once.
	already, err := tx.MailIsInMailbox(mail.ID, mailbox.ID)
	if err != nil {
		return nil, err
	}
	if already {
		return nil, nil
	}
	isSeen := true
	item, err := tx.AddItem(target.ID, mail.ID, "", models.MailboxItemFlags{Seen: &isSeen})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	hook.OnStatementDelivery(tx, mailbox, item, mail)
	return &models.Delivery{
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
	}, nil
}
