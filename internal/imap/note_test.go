package imap

import (
	"bytes"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// literal is a message handed over in an APPEND.
type literal struct {
	*bytes.Reader
}

func (self literal) Size() int64 { return self.Reader.Size() }

// iOSNote is a note as a phone's Notes app appends one: a single HTML part,
// the note's identity in its own header, and a Message-ID that is not it.
const iOSNote = "From: writer@example.com\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"Content-Transfer-Encoding: 7bit\r\n" +
	"Subject: First line\r\n" +
	"Mime-Version: 1.0\r\n" +
	"Date: Mon, 21 Sep 2026 10:00:00 +0000\r\n" +
	"X-Uniform-Type-Identifier: com.apple.mail-note\r\n" +
	"Message-Id: <5B1C2D3E-0000-4000-8000-00000000000A@example.com>\r\n" +
	"X-Universally-Unique-Identifier: 7A6B5C4D-1111-4222-8333-444455556666\r\n" +
	"X-Mail-Created-Date: Sun, 20 Sep 2026 09:00:00 +0000\r\n" +
	"\r\n" +
	"<html><head></head><body>First line<div><br></div><div>more</div></body></html>"

func TestAppendedNoteIsStoredAsANote(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "note-phone")
	var mailbox *models.Mailbox
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if mailbox, err = transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Phone"}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateFolder(&models.MailboxFolder{MailboxID: mailbox.ID, Name: "Notes"}); err != nil {
			test.Fatal(err)
		}
	})
	spool, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = spool.Close() })
	current := &session{settings: &Settings{Database: database, Storage: spool}, mailbox: mailbox, canWrite: true, checkedAt: time.Now()}

	appended, err := current.Append("Notes", literal{bytes.NewReader([]byte(iOSNote))}, &goimap.AppendOptions{Flags: []goimap.Flag{goimap.FlagSeen}})
	if err != nil || appended == nil {
		test.Fatalf("append=%+v, %v", appended, err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		items, err := transaction.ListNoteItems(mailbox.ID, "7A6B5C4D-1111-4222-8333-444455556666")
		if err != nil || len(items) != 1 {
			test.Fatalf("note items=%+v, %v", items, err)
		}
		if stored := items[0].Mail; stored.Kind != models.MailKindNote || stored.Subject != "First line" {
			test.Fatalf("stored=%+v", stored)
		}
	})
}
