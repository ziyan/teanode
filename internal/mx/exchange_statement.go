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
// out-of-office reply sees it.
//
// An address with the statements- detail whose local part is a person's
// own mailbox address is never matched as ordinary mail: a wrong token, a
// switched off source or no agent at all refuses it, and a message the
// spam filter failed goes to that person's Junk unimported. Falling
// through instead would hand the statement, and the token in its address,
// to whoever owns a catch-all. An address with the detail whose local
// part names nobody's mailbox is matched like any other address.

// statementDetailMarker separates a statement import address's local part
// from its token.
const statementDetailMarker = "+statements-"

// splitStatementAddress cuts a local part at its last "+statements-", in
// any case, into the person's own local part and the token. The last,
// because a local part may itself hold a plus.
func splitStatementAddress(recipientAlias string) (string, string, bool) {
	for index := len(recipientAlias) - len(statementDetailMarker); index > 0; index-- {
		if !strings.EqualFold(recipientAlias[index:index+len(statementDetailMarker)], statementDetailMarker) {
			continue
		}
		token := strings.ToLower(recipientAlias[index+len(statementDetailMarker):])
		if token == "" {
			return "", "", false
		}
		return recipientAlias[:index], token, true
	}
	return "", "", false
}

// matchStatementImport delivers a message addressed to a statement import
// address, and says whether the recipient was one. Answered true with no
// delivery is a refusal.
func (self *exchange) matchStatementImport(tx db.Transaction, domain *models.Domain, recipientAlias string, mail *models.Mail) ([]*models.Delivery, bool, error) {
	localPart, token, isStatementAddress := splitStatementAddress(recipientAlias)
	if !isStatementAddress {
		return nil, false, nil
	}
	type owner struct {
		alias   *models.Alias
		mailbox *models.Mailbox
	}
	var owners []owner
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
		if mailbox != nil {
			owners = append(owners, owner{alias: alias, mailbox: mailbox})
		}
	}
	if len(owners) == 0 {
		return nil, false, nil
	}
	recipient := mailparse.UnsplitAddress(recipientAlias, domain.Domain)
	if hook, isStatementHook := self.currentAgentHook().(StatementHook); isStatementHook {
		for _, candidate := range owners {
			if !hook.IsStatementImportToken(tx, candidate.mailbox, token) {
				continue
			}
			self.trackAliasUsageAfterCommit(tx, mail.ReceivedAt, candidate.alias.ID, aliasUsage{
				bytesReceived: mail.Size,
				mailsAccepted: 1,
			})
			var delivery *models.Delivery
			var err error
			if isSuspicious(mail) {
				delivery, err = self.fileStatementInJunk(tx, candidate.mailbox, candidate.alias, recipient, mail)
			} else {
				delivery, err = self.deliverStatement(tx, hook, candidate.mailbox, candidate.alias, recipient, mail)
			}
			if err != nil || delivery == nil {
				return nil, true, err
			}
			return []*models.Delivery{delivery}, true, nil
		}
	}
	log.Noticef("refusing message %q to %q: it names a person's statement import address, and no open one has that token", mail.ID, recipient)
	return nil, true, nil
}

// deliverStatement files a message that reached a statement import address
// in the mailbox's Archive, read, records the delivery, and tells the hook.
//
// The mailbox may hold the message already: the person sent it, so it is
// in their Sent folder, or it was also addressed to their plain address
// and is in their Inbox. Then no second copy is filed, and the hook is
// still told, since the import is queued once per message whichever copy
// came first.
func (self *exchange) deliverStatement(tx db.Transaction, hook StatementHook, mailbox *models.Mailbox, alias *models.Alias, recipient string, mail *models.Mail) (*models.Delivery, error) {
	item, err := existingCopy(tx, mailbox, mail)
	if err != nil {
		return nil, err
	}
	if item == nil {
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
		isSeen := true
		if item, err = tx.AddItem(target.ID, mail.ID, "", models.MailboxItemFlags{Seen: &isSeen}); err != nil {
			return nil, err
		}
	}
	hook.OnStatementDelivery(tx, mailbox, item, mail)
	return statementDelivery(mailbox, alias, recipient, mail, item), nil
}

// fileStatementInJunk puts a message the spam filter failed, addressed to
// a statement import address, in the person's Junk, unread and not
// imported. Junk rather than a refusal, because that is where every other
// message the filter fails goes, and a statement wrongly failed can be
// imported from there by pointing the agent at it.
func (self *exchange) fileStatementInJunk(tx db.Transaction, mailbox *models.Mailbox, alias *models.Alias, recipient string, mail *models.Mail) (*models.Delivery, error) {
	item, err := existingCopy(tx, mailbox, mail)
	if err != nil {
		return nil, err
	}
	if item == nil {
		junk, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindJunk)
		if err != nil {
			return nil, err
		}
		if junk == nil {
			return nil, nil
		}
		if item, err = tx.AddItem(junk.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			return nil, err
		}
	}
	return statementDelivery(mailbox, alias, recipient, mail, item), nil
}

// existingCopy is the item a mailbox already holds for a message, or nil:
// any folder but Drafts, Sent included. A copy outside Sent is preferred,
// as the one the person would look for.
func existingCopy(tx db.Transaction, mailbox *models.Mailbox, mail *models.Mail) (*models.MailboxItem, error) {
	items, err := tx.ListItemsByMail(mail.ID)
	if err != nil {
		return nil, err
	}
	var sentCopy *models.MailboxItem
	for _, item := range items {
		folder, err := tx.GetFolder(item.FolderID)
		if err != nil {
			return nil, err
		}
		if folder == nil || folder.MailboxID != mailbox.ID || folder.Kind == models.MailboxFolderKindDrafts {
			continue
		}
		if folder.Kind != models.MailboxFolderKindSent {
			return item, nil
		}
		if sentCopy == nil {
			sentCopy = item
		}
	}
	return sentCopy, nil
}

// statementDelivery is the delivery row for a message filed at a statement
// import address.
func statementDelivery(mailbox *models.Mailbox, alias *models.Alias, recipient string, mail *models.Mail, item *models.MailboxItem) *models.Delivery {
	now := time.Now()
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
	}
}
