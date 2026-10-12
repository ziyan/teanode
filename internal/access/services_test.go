package access_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestIdentityProvisioningObeysMailboxAvailability(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	for _, shouldProvisionMailbox := range []bool{false, true} {
		username := "assistant-account"
		if shouldProvisionMailbox {
			username = "mail-account"
		}
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			user, isCreated, err := access.SignInWithIdentity(transaction, "test-provider", &access.IdentityClaims{Subject: username, Username: username}, true, shouldProvisionMailbox)
			if err != nil || !isCreated {
				test.Fatalf("identity provisioning: %v", err)
			}
			mailboxes, err := transaction.ListMailboxes(user.ID)
			if err != nil {
				test.Fatal(err)
			}
			if (len(mailboxes) > 0) != shouldProvisionMailbox {
				test.Fatal("identity provisioned an unavailable mailbox")
			}
		})
	}
}
