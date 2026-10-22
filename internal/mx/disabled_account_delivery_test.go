package mx

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A disabled account takes no mail: an address that reaches only its
// mailbox is refused, nothing is filed in it, its import address does not
// import, and a message to it and to an enabled account still reaches the
// enabled one.
func TestADisabledAccountTakesNoMail(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	hook := &statementHook{token: "invented0token02"}
	exchange := &exchange{database: database}
	exchange.SetAgentHook(hook)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		disabledAt := time.Now().Add(-time.Hour)
		gone, err := tx.CreateUser(&models.User{Username: "departed", DisabledAt: &disabledAt})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		here, err := tx.CreateUser(&models.User{Username: "present"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		goneMailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: gone.ID, Name: "Departed"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		hereMailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: here.ID, Name: "Present"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		domain := &models.Domain{ID: "domain-disabled", Domain: "example.com", Aliases: []*models.Alias{
			{ID: "alias-departed", Pattern: models.PatternForLocalPart("departed"), Kind: models.AliasKindMailbox, MailboxID: goneMailbox.ID},
			{ID: "alias-present", Pattern: models.PatternForLocalPart("present"), Kind: models.AliasKindMailbox, MailboxID: hereMailbox.ID},
		}}
		deliver := func(localPart string) []*models.Delivery {
			t.Helper()
			stored, err := tx.CreateMail(&models.Mail{From: "sender@example.net", ReceivedAt: time.Now(), Headers: []string{"From: sender@example.net"}}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			deliveries, err := exchange.matchAliases(tx, domain, localPart, stored)
			if err != nil {
				t.Fatalf("matchAliases: %s", err)
			}
			return deliveries
		}

		if isOnlyDisabled, err := exchange.reachesOnlyDisabledAccounts(tx, domain, "departed"); err != nil || !isOnlyDisabled {
			t.Fatalf("the disabled account's address is not refused: %v %v", isOnlyDisabled, err)
		}
		if isOnlyDisabled, err := exchange.reachesOnlyDisabledAccounts(tx, domain, "present"); err != nil || isOnlyDisabled {
			t.Fatalf("an enabled account's address is refused: %v %v", isOnlyDisabled, err)
		}
		if isOnlyDisabled, err := exchange.reachesOnlyDisabledAccounts(tx, domain, "nobody"); err != nil || isOnlyDisabled {
			t.Fatalf("an address that reaches nobody changed how it is handled: %v %v", isOnlyDisabled, err)
		}

		if deliveries := deliver("departed"); len(deliveries) != 0 {
			t.Fatalf("mail was filed for a disabled account: %+v", deliveries)
		}
		if deliveries := deliver("departed+statements-INVENTED0TOKEN02"); len(deliveries) != 0 || len(hook.statementItemIds) != 0 {
			t.Fatalf("a disabled account's import address imported: %+v %v", deliveries, hook.statementItemIds)
		}
		if deliveries := deliver("present"); len(deliveries) != 1 || deliveries[0].Kind != models.DeliveryKindMailbox {
			t.Fatalf("an enabled account did not get its mail: %+v", deliveries)
		}
		inbox, err := tx.GetFolderByKind(goneMailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			t.Fatalf("GetFolderByKind: %v %s", inbox, err)
		}
		if count, err := tx.CountItems(inbox.ID, nil); err != nil || count != 0 {
			t.Fatalf("%d items were filed in a disabled account's Inbox (%v)", count, err)
		}
	})
}
