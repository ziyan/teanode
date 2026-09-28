package server

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/notes"
	"github.com/ziyan/teanode/internal/storage"
)

// A note a phone appended before this server knew what a note was is stored
// as a copy of sent mail. The pass finds it among the copies in folders the
// owner made, marks it, and leaves the rest as they were, once.
func TestBackfillNotesMarksANoteStoredBefore(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	spool, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = spool.Close() })
	userId := dbtest.CreateUser(test, database, "note-backfill")
	const identifier = "3C2B1A00-5555-4666-8777-888899990000"
	var noteMailId, otherMailId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Older"})
		if err != nil {
			test.Fatal(err)
		}
		folder, err := transaction.CreateFolder(&models.MailboxFolder{MailboxID: mailbox.ID, Name: "Notes"})
		if err != nil {
			test.Fatal(err)
		}
		for _, stored := range []struct {
			mailId  *string
			headers []string
		}{
			{&noteMailId, append([]string{"Subject: Ideas\r\n"}, notes.Headers(identifier, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))...)},
			{&otherMailId, []string{"Subject: A copy of something sent\r\n"}},
		} {
			created, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindOutgoing}, nil)
			if err != nil {
				test.Fatal(err)
			}
			if err := spool.Put(context.Background(), created.ID, stored.headers, []byte("<div>body</div>")); err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.AddItem(folder.ID, created.ID, "", models.MailboxItemFlags{}); err != nil {
				test.Fatal(err)
			}
			*stored.mailId = created.ID
		}
	})
	// As they were before the column existed.
	dbtest.Exec(test, database, `UPDATE mail SET note_identifier = NULL`)

	examined, lastMailId, err := backfillNotes(context.Background(), database, spool, "")
	if err != nil || examined != 2 || lastMailId != max(noteMailId, otherMailId) {
		test.Fatalf("examined=%d through %q, %v", examined, lastMailId, err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.GetMails([]string{noteMailId, otherMailId}, nil)
		if err != nil {
			test.Fatal(err)
		}
		if stored[0].Kind != models.MailKindNote || stored[0].NoteIdentifier != identifier {
			test.Fatalf("the note=%+v", stored[0])
		}
		if stored[1].Kind != models.MailKindOutgoing || stored[1].NoteIdentifier != "" {
			test.Fatalf("the other=%+v", stored[1])
		}
	})
	if examined, _, err := backfillNotes(context.Background(), database, spool, ""); err != nil || examined != 0 {
		test.Fatalf("a second pass examined %d, %v", examined, err)
	}
}
