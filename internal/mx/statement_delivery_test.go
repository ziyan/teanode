package mx

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// statementHook knows one token, and records what it was told.
type statementHook struct {
	token              string
	statementItemIds   []string
	mailboxDeliveryIds []string
}

func (self *statementHook) OnMailboxDelivery(_ db.Transaction, _ *models.Mailbox, item *models.MailboxItem, _ *models.Mail) {
	self.mailboxDeliveryIds = append(self.mailboxDeliveryIds, item.ID)
}

func (self *statementHook) IsStatementImportToken(_ db.Transaction, _ *models.Mailbox, token string) bool {
	return token == self.token
}

func (self *statementHook) OnStatementDelivery(_ db.Transaction, _ *models.Mailbox, item *models.MailboxItem, _ *models.Mail) {
	self.statementItemIds = append(self.statementItemIds, item.ID)
}

// A message to the person's address with "+statements-" and their token is
// filed in the Archive, read, and handed to the import; the same address
// with a wrong token is no address at all; a message the spam filter
// failed is not imported; and the plain address is delivered as ever.
func TestStatementImportAddress(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	hook := &statementHook{token: "invented0token01"}
	exchange := &exchange{database: database}
	exchange.SetAgentHook(hook)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "reader"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		domain := &models.Domain{ID: "domain-one", Domain: "example.com", Aliases: []*models.Alias{
			{ID: "alias-reader", Pattern: models.PatternForLocalPart("reader"), Kind: models.AliasKindMailbox, MailboxID: mailbox.ID},
		}}
		deliver := func(localPart string, mail *models.Mail) []*models.Delivery {
			t.Helper()
			stored, err := tx.CreateMail(mail, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			deliveries, err := exchange.matchAliases(tx, domain, localPart, stored)
			if err != nil {
				t.Fatalf("matchAliases: %s", err)
			}
			return deliveries
		}
		message := func() *models.Mail {
			return &models.Mail{From: "reader@example.com", ReceivedAt: time.Now(), Headers: []string{"From: reader@example.com"}}
		}

		deliveries := deliver("reader+statements-INVENTED0TOKEN01", message())
		if len(deliveries) != 1 || deliveries[0].Kind != models.DeliveryKindMailbox || deliveries[0].Recipient != "reader+statements-INVENTED0TOKEN01@example.com" {
			t.Fatalf("deliveries %+v", deliveries)
		}
		if len(hook.statementItemIds) != 1 || len(hook.mailboxDeliveryIds) != 0 {
			t.Fatalf("told %v about statements and %v about mail", hook.statementItemIds, hook.mailboxDeliveryIds)
		}
		item, err := tx.GetItem(hook.statementItemIds[0])
		if err != nil || item == nil {
			t.Fatalf("GetItem: %v %v", item, err)
		}
		archive, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindArchive)
		if err != nil || archive == nil {
			t.Fatalf("no Archive: %v", err)
		}
		if item.FolderID != archive.ID || !item.Seen {
			t.Errorf("filed in %s, seen %v; want the Archive, seen", item.FolderID, item.Seen)
		}

		if deliveries := deliver("reader+statements-wrong0token0000", message()); len(deliveries) != 0 {
			t.Errorf("a wrong token delivered: %+v", deliveries)
		}
		if len(hook.statementItemIds) != 1 {
			t.Errorf("a wrong token was imported: %v", hook.statementItemIds)
		}

		spam := message()
		spam.AuthenticationResults.SpamFilter = &models.SpamFilterResult{Result: "fail"}
		deliver("reader+statements-invented0token01", spam)
		if len(hook.statementItemIds) != 1 {
			t.Errorf("a message the spam filter failed was imported: %v", hook.statementItemIds)
		}

		if deliveries := deliver("reader", message()); len(deliveries) != 1 || len(hook.mailboxDeliveryIds) != 1 || len(hook.statementItemIds) != 1 {
			t.Errorf("the plain address: %d deliveries, told %v and %v", len(deliveries), hook.mailboxDeliveryIds, hook.statementItemIds)
		}
	})
}

// Without an agent to import, a statement address is matched as any
// address is.
func TestStatementImportAddressWithoutAnAgent(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	exchange := &exchange{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "reader"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		domain := &models.Domain{ID: "domain-one", Domain: "example.com", Aliases: []*models.Alias{
			{ID: "alias-reader", Pattern: models.PatternForLocalPart("reader"), Kind: models.AliasKindMailbox, MailboxID: mailbox.ID},
		}}
		stored, err := tx.CreateMail(&models.Mail{From: "reader@example.com", ReceivedAt: time.Now()}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		deliveries, err := exchange.matchAliases(tx, domain, "reader+statements-invented0token01", stored)
		if err != nil || len(deliveries) != 0 {
			t.Errorf("delivered %+v %v", deliveries, err)
		}
	})
}
