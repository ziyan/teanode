package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A muted list's mail is filed in the Archive as it arrives, and still waits
// for the rules that read the agent's insight, so a rule the person keeps
// for it files it; mail of a list that is not muted, and anything in another
// folder, has been filed already.
func TestAMutedListsMailInTheArchiveStillWaitsForTheRules(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "reader"})
		if err != nil {
			test.Fatal(err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			test.Fatal(err)
		}
		inbox, _ := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		archive, _ := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindArchive)
		junk, _ := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindJunk)
		if err := tx.SetSubscriptionMuted(mailbox.ID, "deals@example.com", true); err != nil {
			test.Fatal(err)
		}
		muted := &models.Mail{ListKey: "deals@example.com"}
		listed := &models.Mail{ListKey: "news@example.com"}
		personal := &models.Mail{}
		for _, testCase := range []struct {
			folder    *models.MailboxFolder
			mail      *models.Mail
			isWaiting bool
		}{
			{inbox, personal, true},
			{inbox, listed, true},
			{archive, muted, true},
			{archive, listed, false},
			{archive, personal, false},
			{junk, muted, false},
		} {
			isWaiting, err := isWaitingForInsightRules(tx, mailbox.ID, testCase.folder, testCase.mail)
			if err != nil {
				test.Fatal(err)
			}
			if isWaiting != testCase.isWaiting {
				test.Errorf("%s, list %q: waiting %v, want %v", testCase.folder.Kind, testCase.mail.ListKey, isWaiting, testCase.isWaiting)
			}
		}
		if isWaiting, _ := isWaitingForInsightRules(tx, "another-mailbox", inbox, personal); isWaiting {
			test.Error("an item of another mailbox waits for this one's rules")
		}
	})
}
