package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A day's notes are the ones written or changed that day, each with all its
// versions so that the caller can tell which is the note as it stands now.
func TestNoteItemsBetweenFindsNotesWrittenOrChangedInTheStretch(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	zone := time.FixedZone("test", -7*3600)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	var mailboxId string
	itemIds := map[string]string{}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner := newCircleTestUser(t, tx, "notes-ada")
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal"})
		if err != nil {
			t.Fatal(err)
		}
		mailboxId = mailbox.ID
		folder, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || folder == nil {
			t.Fatalf("an inbox: %v %v", folder, err)
		}
		trash, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindTrash)
		if err != nil || trash == nil {
			t.Fatalf("a trash: %v %v", trash, err)
		}
		put := func(name, identifier, subject string, folderId string, receivedAt time.Time) {
			mail, err := tx.CreateMail(&models.Mail{Subject: subject, Kind: models.MailKindNote, NoteIdentifier: identifier, ReceivedAt: receivedAt}, nil)
			if err != nil {
				t.Fatal(err)
			}
			item, err := tx.AddItem(folderId, mail.ID, "", models.MailboxItemFlags{})
			if err != nil {
				t.Fatal(err)
			}
			itemIds[name] = item.ID
		}
		// Written on the 22nd and not touched again.
		put("packing", "note-packing", "Packing list", folder.ID, at(22, 9, 0))
		// Written on the 20th, changed on the 22nd: two versions.
		put("ideas-first", "note-ideas", "Ideas", folder.ID, at(20, 9, 0))
		put("ideas-second", "note-ideas", "Ideas for the trip", folder.ID, at(22, 15, 0))
		// Written and changed on other days.
		put("old", "note-old", "Old note", folder.ID, at(10, 9, 0))
		put("later", "note-later", "Later note", folder.ID, at(25, 9, 0))
		// Written on the 22nd and thrown away.
		put("thrown", "note-thrown", "Thrown away", trash.ID, at(22, 10, 0))
		// A message that is not a note.
		mail, err := tx.CreateMail(&models.Mail{Subject: "Not a note", Kind: models.MailKindIncoming, ReceivedAt: at(22, 11, 0)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatal(err)
		}
	})
	for name, addedAt := range map[string]time.Time{
		"packing": at(22, 9, 0), "ideas-first": at(20, 9, 0), "ideas-second": at(22, 15, 0),
		"old": at(10, 9, 0), "later": at(25, 9, 0), "thrown": at(22, 10, 0),
	} {
		dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "mailbox_item" SET "added_at" = '%s' WHERE "id" = '%s'`, addedAt.UTC().Format(time.RFC3339), itemIds[name]))
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		items, err := tx.ListNoteItemsBetween(mailboxId, at(22, 0, 0), at(23, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		subjects := []string{}
		for _, item := range items {
			subjects = append(subjects, item.Mail.Subject)
		}
		// Newest item first: the packing list, the two versions of the ideas.
		want := []string{"Ideas for the trip", "Packing list", "Ideas"}
		if fmt.Sprint(subjects) != fmt.Sprint(want) {
			t.Fatalf("versions of the notes written or changed that day: %v, want %v", subjects, want)
		}
		quiet, err := tx.ListNoteItemsBetween(mailboxId, at(15, 0, 0), at(16, 0, 0))
		if err != nil || len(quiet) != 0 {
			t.Fatalf("a quiet day has no notes: %v %v", quiet, err)
		}
	})
}
