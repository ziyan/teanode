package mx

import (
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Statements and tax documents mailed in.
//
// A person's statement import address is their own mailbox address with a
// detail after a plus: name+statements-<token>@domain. Their tax document
// address is the same with taxes- (name+taxes-<token>@domain), whose PDFs
// and pictures the agent reads as paystubs and W-2s; everything below
// holds for both. Nothing new is
// needed in DNS or among the aliases, and the token, which only the person
// is shown, is what lets a message in. Such a message is filed in the
// mailbox's Archive, read, so it does not wait in the Inbox, and the agent
// is told to import its OFX files; no rule, no triage, no calendar and no
// out-of-office reply sees it.
//
// A tax document address differs in one thing: one whose token is not the
// person's is delivered as ordinary mail rather than refused, since a plus
// tag such as +taxes-2025 is something a person may well use on their own.
//
// An address with the statements- detail whose local part is a person's
// own mailbox address is never matched as ordinary mail: a wrong token, a
// switched off source or no agent at all refuses it, and a message the
// spam filter failed goes to that person's Junk unimported. Falling
// through instead would hand the statement, and the token in its address,
// to whoever owns a catch-all. An address with the detail whose local
// part names nobody's mailbox is matched like any other address.

// statementDetailMarker separates a statement import address's local part
// from its token, and taxDocumentDetailMarker a tax document address's.
const (
	statementDetailMarker   = "+statements-"
	taxDocumentDetailMarker = "+taxes-"
)

// splitStatementAddress cuts a local part at its last "+statements-", in
// any case, into the person's own local part and the token.
func splitStatementAddress(recipientAlias string) (string, string, bool) {
	return splitDetailAddress(recipientAlias, statementDetailMarker)
}

// splitDetailAddress cuts a local part at the last place marker appears,
// in any case, into the person's own local part and the token. The last,
// because a local part may itself hold a plus.
func splitDetailAddress(recipientAlias, marker string) (string, string, bool) {
	for index := len(recipientAlias) - len(marker); index > 0; index-- {
		if !strings.EqualFold(recipientAlias[index:index+len(marker)], marker) {
			continue
		}
		token := strings.ToLower(recipientAlias[index+len(marker):])
		if token == "" {
			return "", "", false
		}
		return recipientAlias[:index], token, true
	}
	return "", "", false
}

// importAddress is one kind of address a person mails files in at: the
// detail that marks it, what it is called in a log line, and the hook's
// question and notice for it, nil when the hook takes none.
type importAddress struct {
	marker      string
	addressName string
	// isRefusedUnlessOpen says a message to the address without an open
	// token is refused rather than matched as ordinary mail.
	isRefusedUnlessOpen bool
	isToken             func(tx db.Transaction, mailbox *models.Mailbox, token string) bool
	onDelivery          func(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail)
}

// importAddresses are the kinds of import address the current hook
// takes, statements first.
func (self *exchange) importAddresses() []importAddress {
	hook := self.currentAgentHook()
	statements := importAddress{marker: statementDetailMarker, addressName: "statement import address", isRefusedUnlessOpen: true}
	if statementHook, isStatementHook := hook.(StatementHook); isStatementHook {
		statements.isToken, statements.onDelivery = statementHook.IsStatementImportToken, statementHook.OnStatementDelivery
	}
	taxDocuments := importAddress{marker: taxDocumentDetailMarker, addressName: "tax document address"}
	if taxDocumentHook, isTaxDocumentHook := hook.(TaxDocumentHook); isTaxDocumentHook {
		taxDocuments.isToken, taxDocuments.onDelivery = taxDocumentHook.IsTaxDocumentToken, taxDocumentHook.OnTaxDocumentDelivery
	}
	return []importAddress{statements, taxDocuments}
}

// matchStatementImport delivers a message addressed to an import address,
// a statement import address or a tax document address, and says whether
// the recipient was one. Answered true with no delivery is a refusal.
func (self *exchange) matchStatementImport(tx db.Transaction, domain *models.Domain, recipientAlias string, mail *models.Mail) ([]*models.Delivery, bool, error) {
	for _, address := range self.importAddresses() {
		if deliveries, isImportAddress, err := self.matchImportAddress(tx, domain, recipientAlias, mail, address); err != nil || isImportAddress {
			return deliveries, isImportAddress, err
		}
	}
	return nil, false, nil
}

// matchImportAddress delivers a message addressed to one kind of import
// address, as matchStatementImport does.
func (self *exchange) matchImportAddress(tx db.Transaction, domain *models.Domain, recipientAlias string, mail *models.Mail, address importAddress) ([]*models.Delivery, bool, error) {
	localPart, token, isDetailAddress := splitDetailAddress(recipientAlias, address.marker)
	if !isDetailAddress {
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
		if mailbox == nil {
			continue
		}
		if isTaking, err := takesMail(tx, mailbox); err != nil {
			return nil, false, err
		} else if isTaking {
			owners = append(owners, owner{alias: alias, mailbox: mailbox})
		}
	}
	if len(owners) == 0 {
		return nil, false, nil
	}
	recipient := mailparse.UnsplitAddress(recipientAlias, domain.Domain)
	if address.isToken != nil {
		for _, candidate := range owners {
			if !address.isToken(tx, candidate.mailbox, token) {
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
				delivery, err = self.deliverStatement(tx, address.onDelivery, candidate.mailbox, candidate.alias, recipient, mail)
			}
			if err != nil || delivery == nil {
				return nil, true, err
			}
			return []*models.Delivery{delivery}, true, nil
		}
	}
	if !address.isRefusedUnlessOpen {
		return nil, false, nil
	}
	log.Noticef("refusing message %q to %q: it names a person's %s, and no open one has that token", mail.ID, recipient, address.addressName)
	return nil, true, nil
}

// deliverStatement files a message that reached an import address in the
// mailbox's Archive, read, records the delivery, and tells the hook.
//
// The mailbox may hold the message already: the person sent it, so it is
// in their Sent folder, or it was also addressed to their plain address
// and is in their Inbox. Then no second copy is filed, and the hook is
// still told, since the import is queued once per message whichever copy
// came first.
func (self *exchange) deliverStatement(tx db.Transaction, onDelivery func(db.Transaction, *models.Mailbox, *models.MailboxItem, *models.Mail), mailbox *models.Mailbox, alias *models.Alias, recipient string, mail *models.Mail) (*models.Delivery, error) {
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
	onDelivery(tx, mailbox, item, mail)
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
