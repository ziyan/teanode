package imap

import (
	"errors"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func TestDisabledMailboxesRefuseNewAndExistingSessions(test *testing.T) {
	isMailboxEnabled := true
	current := &session{settings: &Settings{IsMailboxEnabled: func() bool { return isMailboxEnabled }}, mailbox: &models.Mailbox{ID: "test-mailbox"}, checkedAt: time.Now()}
	if err := current.requireMailbox(); err != nil {
		test.Fatal(err)
	}
	isMailboxEnabled = false
	for _, operation := range []func() error{
		func() error { return current.Login("test-user", "test-password") },
		func() error { return current.stillAllowed(nil) },
		func() error {
			return current.transaction(func(db.Transaction) error { test.Fatal("disabled session accessed mail"); return nil })
		},
		func() error { return current.Poll(nil, true) },
		func() error { _, err := current.requireSelected(); return err },
	} {
		var protocolError *goimap.Error
		if err := operation(); !errors.As(err, &protocolError) || protocolError.Type != goimap.StatusResponseTypeBye {
			test.Fatalf("disabled session was not ended: %v", err)
		}
	}
	isMailboxEnabled = true
	if err := current.requireMailbox(); err != nil {
		test.Fatal("re-enabled mailboxes still refused access")
	}
}
