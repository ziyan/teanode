package db_test

import (
	"slices"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Mail with a person is the mail whose sender or recipients include one of the
// addresses as a whole, in any case and with a name beside it, in every mailbox
// of the owner and nobody else's; the trash, junk, drafts and notes are left
// out, and the copies of one mail a sender made by writing to each address are
// one mail.
func TestListMailWithAddressesMatchesWholeAddressesInEveryMailbox(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	now := time.Now()
	var ownerId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		check := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		owner, err := tx.CreateUser(&models.User{Username: "card-owner"})
		check(err)
		stranger, err := tx.CreateUser(&models.User{Username: "card-stranger"})
		check(err)
		ownerId = owner.ID
		folderOf := func(userId, mailboxName string) func(kind models.MailboxFolderKind) *models.MailboxFolder {
			mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: userId, Name: mailboxName})
			check(err)
			return func(kind models.MailboxFolderKind) *models.MailboxFolder {
				folder, err := tx.GetFolderByKind(mailbox.ID, kind)
				if err != nil || folder == nil {
					t.Fatalf("no %s folder: %v %v", kind, folder, err)
				}
				return folder
			}
		}
		deliver := func(folder *models.MailboxFolder, mail *models.Mail) {
			t.Helper()
			stored, err := tx.CreateMail(mail, nil)
			check(err)
			_, err = tx.AddItem(folder.ID, stored.ID, "", models.MailboxItemFlags{})
			check(err)
		}
		personal, work, strangers := folderOf(owner.ID, "Personal"), folderOf(owner.ID, "Work"), folderOf(stranger.ID, "Personal")

		deliver(personal(models.MailboxFolderKindInbox), &models.Mail{From: "sam@example.org", Sender: "sam@example.org", Recipients: []string{"owner@example.net"}, Subject: "plain", Kind: models.MailKindIncoming, ReceivedAt: now.Add(-5 * time.Hour)})
		deliver(work(models.MailboxFolderKindInbox), &models.Mail{From: "SAM.ALT@EXAMPLE.ORG", Sender: "SAM.ALT@EXAMPLE.ORG", Recipients: []string{"owner@example.net"}, Subject: "upper", Kind: models.MailKindIncoming, ReceivedAt: now.Add(-4 * time.Hour)})
		deliver(work(models.MailboxFolderKindSent), &models.Mail{From: "owner@example.net", Sender: "owner@example.net", Recipients: []string{"other@example.org", "sam@example.org"}, Subject: "sent", Kind: models.MailKindOutgoing, ReceivedAt: now.Add(-3 * time.Hour)})
		deliver(personal(models.MailboxFolderKindInbox), &models.Mail{From: "Sam Example <sam@example.org>", Recipients: []string{"owner@example.net"}, Subject: "named", Kind: models.MailKindIncoming, ReceivedAt: now.Add(-2 * time.Hour)})
		// Two copies of one mail, one to each of the owner's addresses.
		deliver(personal(models.MailboxFolderKindInbox), &models.Mail{From: "sam@example.org", Recipients: []string{"owner@example.net"}, Subject: "twice", MessageID: "<a@example.org>", Kind: models.MailKindIncoming, ReceivedAt: now.Add(-90 * time.Minute)})
		deliver(work(models.MailboxFolderKindInbox), &models.Mail{From: "sam@example.org", Recipients: []string{"owner@example.com"}, Subject: "twice", MessageID: "<b@example.org>", Kind: models.MailKindIncoming, ReceivedAt: now.Add(-89 * time.Minute)})

		// Not mail with the address.
		deliver(personal(models.MailboxFolderKindInbox), &models.Mail{From: "xsam@example.org", Recipients: []string{"owner@example.net"}, Subject: "longer before", Kind: models.MailKindIncoming, ReceivedAt: now})
		deliver(personal(models.MailboxFolderKindInbox), &models.Mail{From: "sam@example.org.example.net", Recipients: []string{"owner@example.net"}, Subject: "longer after", Kind: models.MailKindIncoming, ReceivedAt: now})
		deliver(personal(models.MailboxFolderKindInbox), &models.Mail{From: "bob@example.org", Recipients: []string{"xsam@example.org"}, Subject: "recipient longer", Kind: models.MailKindIncoming, ReceivedAt: now})
		deliver(personal(models.MailboxFolderKindInbox), &models.Mail{From: "bob@example.org", Recipients: []string{"%@example.org"}, Subject: "a wildcard is no address", Kind: models.MailKindIncoming, ReceivedAt: now})
		deliver(personal(models.MailboxFolderKindTrash), &models.Mail{From: "sam@example.org", Subject: "trash", Kind: models.MailKindIncoming, ReceivedAt: now})
		deliver(personal(models.MailboxFolderKindJunk), &models.Mail{From: "sam@example.org", Subject: "junk", Kind: models.MailKindIncoming, ReceivedAt: now})
		deliver(personal(models.MailboxFolderKindDrafts), &models.Mail{From: "owner@example.net", Recipients: []string{"sam@example.org"}, Subject: "draft", Kind: models.MailKindDraft, ReceivedAt: now})
		deliver(personal(models.MailboxFolderKindInbox), &models.Mail{From: "sam@example.org", Subject: "note", Kind: models.MailKindNote, ReceivedAt: now})
		deliver(strangers(models.MailboxFolderKindInbox), &models.Mail{From: "sam@example.org", Subject: "somebody else's", Kind: models.MailKindIncoming, ReceivedAt: now})
	})

	subjectsOf := func(found []*db.PersonCardMail) []string {
		subjects := []string{}
		for _, each := range found {
			subjects = append(subjects, each.Item.Mail.Subject)
		}
		return subjects
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, hasMore, err := tx.ListMailWithAddresses(ownerId, nil, []string{"Sam@Example.org", " sam.alt@example.org ", "", "sam@example.org"}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"twice", "named", "sent", "upper", "plain"}; !slices.Equal(subjectsOf(found), want) || hasMore {
			t.Errorf("the mail with the addresses was %v (more: %v), want %v", subjectsOf(found), hasMore, want)
		}
		for _, each := range found {
			isFrom := each.Item.Mail.Subject != "sent"
			if each.IsFromAddress != isFrom || each.MailboxID == "" {
				t.Errorf("%q: from the address %v, in mailbox %q", each.Item.Mail.Subject, each.IsFromAddress, each.MailboxID)
			}
		}

		found, hasMore, err = tx.ListMailWithAddresses(ownerId, nil, []string{"sam@example.org"}, 2)
		if err != nil || !slices.Equal(subjectsOf(found), []string{"twice", "named"}) || !hasMore {
			t.Errorf("two of them and more: %v %v %v", subjectsOf(found), hasMore, err)
		}

		// Nobody, nothing and nobody's addresses find nothing.
		for name, arguments := range map[string]struct {
			userId    string
			addresses []string
		}{
			"no address":     {ownerId, nil},
			"a blank one":    {ownerId, []string{"  "}},
			"another person": {"nobody", []string{"sam@example.org"}},
			"a wildcard":     {ownerId, []string{"%"}},
			"an underscore":  {ownerId, []string{"sa_@example.org"}},
		} {
			found, hasMore, err := tx.ListMailWithAddresses(arguments.userId, nil, arguments.addresses, 10)
			if err != nil || len(found) != 0 || hasMore {
				t.Errorf("%s: %v %v %v", name, subjectsOf(found), hasMore, err)
			}
		}
	})
}
