package mx

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
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
		spamDeliveries := deliver("reader+statements-invented0token01", spam)
		if len(hook.statementItemIds) != 1 {
			t.Errorf("a message the spam filter failed was imported: %v", hook.statementItemIds)
		}
		junk, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindJunk)
		if err != nil || junk == nil {
			t.Fatalf("no Junk: %v", err)
		}
		if len(spamDeliveries) != 1 {
			t.Fatalf("a message the spam filter failed: %+v, want one delivery to Junk", spamDeliveries)
		}
		if junkItem, err := tx.GetItem(spamDeliveries[0].MailboxItemID); err != nil || junkItem == nil || junkItem.FolderID != junk.ID {
			t.Errorf("a message the spam filter failed was filed as %+v, %v; want Junk", junkItem, err)
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

// folderItemCount is how many items a mailbox's folder of one kind holds.
func folderItemCount(t *testing.T, tx db.Transaction, mailboxId string, kind models.MailboxFolderKind) int {
	t.Helper()
	folder, err := tx.GetFolderByKind(mailboxId, kind)
	if err != nil || folder == nil {
		t.Fatalf("no %s folder: %v", kind, err)
	}
	items, err := tx.ListItems(folder.ID, nil)
	if err != nil {
		t.Fatalf("ListItems: %s", err)
	}
	return len(items)
}

// A message addressed to both the plain address and the statement import
// address, the plain one first, is in the Inbox once and still imported:
// the copy already there used to make the statement recipient a silent
// nothing, with no delivery and no import.
func TestStatementAlsoAddressedToThePlainAddressIsImported(t *testing.T) {
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
		stored, err := tx.CreateMail(&models.Mail{From: "bank@example.net", ReceivedAt: time.Now(), Headers: []string{"From: bank@example.net"}}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		// In the order handleIncoming walks the envelope's recipients.
		var deliveries []*models.Delivery
		for _, localPart := range []string{"reader", "reader+statements-invented0token01"} {
			matched, err := exchange.matchAliases(tx, domain, localPart, stored)
			if err != nil {
				t.Fatalf("matchAliases %s: %s", localPart, err)
			}
			deliveries = append(deliveries, matched...)
		}
		if len(deliveries) != 2 {
			t.Fatalf("deliveries %+v, want one for each recipient", deliveries)
		}
		if len(hook.statementItemIds) != 1 || hook.statementItemIds[0] != deliveries[0].MailboxItemID {
			t.Errorf("told %v about statements; want the Inbox copy %s once", hook.statementItemIds, deliveries[0].MailboxItemID)
		}
		if inboxCount, archiveCount := folderItemCount(t, tx, mailbox.ID, models.MailboxFolderKindInbox), folderItemCount(t, tx, mailbox.ID, models.MailboxFolderKindArchive); inboxCount != 1 || archiveCount != 0 {
			t.Errorf("%d in the Inbox and %d in the Archive; want the one Inbox copy", inboxCount, archiveCount)
		}
	})
}

// A statement the person sends from their own mail program, through the
// submission port to their own statement import address, is imported and
// kept once, in Sent: the person sent it, and a second copy in the Archive
// would be the same message twice.
func TestStatementSentFromTheOwnMailboxIsImported(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	localStorage, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = localStorage.Close() })
	exchange, mailbox := submissionExchange(t, database, localStorage)
	hook := &statementHook{token: "invented0token01"}
	exchange.SetAgentHook(hook)
	var deliveries []*models.Delivery
	if err := database.Transaction(func(tx db.Transaction) error {
		var err error
		deliveries, err = exchange.handleOutgoing(context.Background(), tx, &mailparse.Envelope{
			ID: "statement-envelope", MailboxID: mailbox.ID, Sender: "sender@example.com",
			Recipients: []string{"sender+statements-invented0token01@example.com"}, IP: net.IPv4(127, 0, 0, 1), ReceivedAt: time.Now(),
			Headers: []string{"From: sender@example.com\r\n", "Message-ID: <statement-submission@example.com>\r\n", "Content-Type: text/plain\r\n"},
			Body:    []byte("Statement fixture\r\n"), Size: 128,
		})
		return err
	}); err != nil {
		t.Fatalf("handleOutgoing: %s", err)
	}
	if len(deliveries) != 1 || deliveries[0].Kind != models.DeliveryKindMailbox {
		t.Fatalf("deliveries %+v, want the one to the statement import address", deliveries)
	}
	if len(hook.statementItemIds) != 1 {
		t.Errorf("told %v about statements, want once", hook.statementItemIds)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if sentCount, archiveCount := folderItemCount(t, tx, mailbox.ID, models.MailboxFolderKindSent), folderItemCount(t, tx, mailbox.ID, models.MailboxFolderKindArchive); sentCount != 1 || archiveCount != 0 {
			t.Errorf("%d in Sent and %d in the Archive; want the one Sent copy", sentCount, archiveCount)
		}
	})
}

// An address with the statements- detail naming a person's mailbox, whose
// token is wrong or whose agent is missing, is refused, not handed to a
// catch-all with the token in it. One naming nobody's mailbox is matched
// as any address is, catch-all included.
func TestStatementImportAddressIsNotCaughtByACatchAll(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	hook := &statementHook{token: "invented0token01"}
	withAgent := &exchange{database: database}
	withAgent.SetAgentHook(hook)
	withoutAgent := &exchange{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "reader"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		catchAllMailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Everything else"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		domain := &models.Domain{ID: "domain-one", Domain: "example.com", Aliases: []*models.Alias{
			{ID: "alias-reader", Pattern: models.PatternForLocalPart("reader"), Kind: models.AliasKindMailbox, MailboxID: mailbox.ID},
			{ID: "alias-catch-all", Kind: models.AliasKindMailbox, MailboxID: catchAllMailbox.ID},
		}}
		deliver := func(matcher *exchange, localPart string) []*models.Delivery {
			t.Helper()
			stored, err := tx.CreateMail(&models.Mail{From: "bank@example.net", ReceivedAt: time.Now(), Headers: []string{"From: bank@example.net"}}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			deliveries, err := matcher.matchAliases(tx, domain, localPart, stored)
			if err != nil {
				t.Fatalf("matchAliases: %s", err)
			}
			return deliveries
		}
		if deliveries := deliver(withAgent, "reader+statements-wrong0token0000"); len(deliveries) != 0 {
			t.Errorf("a wrong token was delivered: %+v", deliveries)
		}
		if deliveries := deliver(withoutAgent, "reader+statements-invented0token01"); len(deliveries) != 0 {
			t.Errorf("without an agent, the address was delivered: %+v", deliveries)
		}
		if count := folderItemCount(t, tx, catchAllMailbox.ID, models.MailboxFolderKindInbox); count != 0 {
			t.Errorf("the catch-all took %d statements", count)
		}
		if deliveries := deliver(withAgent, "nobody+statements-invented0token01"); len(deliveries) != 1 || deliveries[0].MailboxID != catchAllMailbox.ID {
			t.Errorf("an address naming nobody: %+v, want the catch-all as before", deliveries)
		}
		if len(hook.statementItemIds) != 0 {
			t.Errorf("imported %v", hook.statementItemIds)
		}
	})
}

// A local part that holds a plus of its own still has a statement import
// address: the token is after the last "+statements-".
func TestStatementImportAddressWithAPlusInTheLocalPart(t *testing.T) {
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
			{ID: "alias-reader", Pattern: models.PatternForLocalPart("first+last"), Kind: models.AliasKindMailbox, MailboxID: mailbox.ID},
		}}
		stored, err := tx.CreateMail(&models.Mail{From: "bank@example.net", ReceivedAt: time.Now(), Headers: []string{"From: bank@example.net"}}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		deliveries, err := exchange.matchAliases(tx, domain, "first+last+Statements-INVENTED0TOKEN01", stored)
		if err != nil || len(deliveries) != 1 || len(hook.statementItemIds) != 1 {
			t.Errorf("deliveries %+v, told %v, %v", deliveries, hook.statementItemIds, err)
		}
	})
}

func TestSplitStatementAddress(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		recipientAlias     string
		expectedLocalPart  string
		expectedToken      string
		isStatementAddress bool
	}{
		{"reader+statements-abc", "reader", "abc", true},
		{"first+last+STATEMENTS-Abc", "first+last", "abc", true},
		{"reader+statements-a+statements-b", "reader+statements-a", "b", true},
		{"reader+statements-", "", "", false},
		{"+statements-abc", "", "", false},
		{"reader+other", "", "", false},
		{"reader", "", "", false},
	} {
		localPart, token, isStatementAddress := splitStatementAddress(test.recipientAlias)
		if localPart != test.expectedLocalPart || token != test.expectedToken || isStatementAddress != test.isStatementAddress {
			t.Errorf("%q: %q %q %v", test.recipientAlias, localPart, token, isStatementAddress)
		}
	}
}
