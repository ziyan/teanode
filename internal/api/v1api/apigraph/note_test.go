package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/notes"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// noteFixture is a person with one mailbox at an address of a configured
// domain, a real mailer to compose with and storage in a temporary directory.
func noteFixture(test *testing.T) (db.Database, *graph, *api.Principal, *models.Mailbox, storage.Storage) {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "note-writer")
	principal := &api.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{
		{Permission: models.PermissionMailRead}, {Permission: models.PermissionMailWrite},
	})}
	var mailbox *models.Mailbox
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		created, err := transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Notes fixture"})
		if err != nil {
			test.Fatal(err)
		}
		domain, err := transaction.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "^writer$", Kind: models.AliasKindMailbox, MailboxID: created.ID}); err != nil {
			test.Fatal(err)
		}
		mailbox = created
	})
	spool, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = spool.Close() })
	resolver := &graph{database: database, config: config.NewMemoryStore(config.Default()), storage: spool}
	if resolver.mailer, err = mailer.New(database, resolver.config, nil, nil); err != nil {
		test.Fatal(err)
	}
	return database, resolver, principal, mailbox, spool
}

// asPerson runs one resolver call in a transaction of its own, as a request.
func asPerson(test *testing.T, database db.Database, principal *api.Principal, run func(ctx context.Context)) {
	test.Helper()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		run(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction))
	})
}

func TestSaveNoteReplacesThePreviousVersion(test *testing.T) {
	database, resolver, principal, mailbox, spool := noteFixture(test)

	var first *NoteView
	asPerson(test, database, principal, func(ctx context.Context) {
		var err error
		first, err = resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, Text: new("Packing list\n- socks\n- towel")})
		if err != nil || first == nil {
			test.Fatalf("save=%+v, %v", first, err)
		}
	})
	if first.Title != "Packing list" || first.ID != strings.ToUpper(first.ID) || first.Text != "Packing list\n- socks\n- towel" {
		test.Fatalf("first=%+v", first)
	}

	var second *NoteView
	asPerson(test, database, principal, func(ctx context.Context) {
		var err error
		second, err = resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, NoteID: first.ID, Text: new("Packing list\n- socks\n- towel\n- hat")})
		if err != nil {
			test.Fatal(err)
		}
	})
	if second.ID != first.ID || !second.CreatedAt.Equal(first.CreatedAt) || second.FolderID != first.FolderID {
		test.Fatalf("second=%+v, first=%+v", second, first)
	}

	var items []*models.MailboxItem
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if items, err = transaction.ListNoteItems(mailbox.ID, first.ID); err != nil {
			test.Fatal(err)
		}
		folder, err := transaction.GetFolder(first.FolderID)
		if err != nil || folder == nil || folder.Name != "Notes" {
			test.Fatalf("the notes folder=%+v, %v", folder, err)
		}
	})
	if len(items) != 1 || !items[0].Seen {
		test.Fatalf("items of the note=%+v", items)
	}
	headers, body, err := spool.Get(test.Context(), items[0].MailID)
	if err != nil {
		test.Fatal(err)
	}
	if notes.Identifier(headers) != first.ID || !notes.CreatedAt(headers).Equal(first.CreatedAt) {
		test.Fatalf("note headers=%q", headers)
	}
	if subject := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, "Subject")); subject != "Packing list" {
		test.Fatalf("subject=%q", subject)
	}
	if mailparse.CountHeaders(headers, "To") != 0 || !strings.Contains(mailparse.FindHeaderValue(headers, "Content-Type"), "text/html") {
		test.Fatalf("headers=%q", headers)
	}
	if len(body) == 0 {
		test.Fatal("the note was stored empty")
	}

	asPerson(test, database, principal, func(ctx context.Context) {
		listed, err := resolver.ListNotes(ctx, ListNotesArguments{MailboxID: mailbox.ID})
		if err != nil || len(listed) != 1 || listed[0].Preview != "- socks - towel - hat" || listed[0].Text != "" {
			test.Fatalf("list=%+v, %v", listed, err)
		}
		read, err := resolver.GetNote(ctx, GetNoteArguments{MailboxID: mailbox.ID, NoteID: first.ID})
		if err != nil || read.Text != "Packing list\n- socks\n- towel\n- hat" {
			test.Fatalf("read=%+v, %v", read, err)
		}
	})
}

func TestDeleteNoteRemovesEveryVersion(test *testing.T) {
	database, resolver, principal, mailbox, _ := noteFixture(test)
	var saved *NoteView
	asPerson(test, database, principal, func(ctx context.Context) {
		var err error
		if saved, err = resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, Text: new("Recipe\nflour")}); err != nil {
			test.Fatal(err)
		}
	})
	// An older version the phone flagged and did not expunge.
	addFlaggedVersion(test, database, saved.FolderID, saved.ID)
	asPerson(test, database, principal, func(ctx context.Context) {
		if isDeleted, err := resolver.DeleteNote(ctx, DeleteNoteArguments{MailboxID: mailbox.ID, NoteID: saved.ID}); err != nil || !isDeleted {
			test.Fatalf("delete=%v, %v", isDeleted, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		items, err := transaction.ListNoteItems(mailbox.ID, saved.ID)
		if err != nil || len(items) != 0 {
			test.Fatalf("left after delete=%+v, %v", items, err)
		}
	})
}

func TestTheDashboardDoesNotCountWhatIsFlaggedDeleted(test *testing.T) {
	database, resolver, principal, mailbox, _ := noteFixture(test)
	var saved *NoteView
	asPerson(test, database, principal, func(ctx context.Context) {
		var err error
		if saved, err = resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, Text: new("Shopping")}); err != nil {
			test.Fatal(err)
		}
	})
	addFlaggedVersion(test, database, saved.FolderID, saved.ID)

	asPerson(test, database, principal, func(ctx context.Context) {
		views, err := resolver.ListMailboxes(ctx)
		if err != nil || len(views) != 1 {
			test.Fatalf("mailboxes=%+v, %v", views, err)
		}
		for _, folder := range views[0].Folders {
			if folder.ID == saved.FolderID && (folder.Total != 1 || folder.NoteCount != 1) {
				test.Fatalf("the dashboard counts %d in the notes folder, %d of them notes", folder.Total, folder.NoteCount)
			}
		}
		page, err := resolver.ListMailboxItems(ctx, ListMailboxItemsArguments{FolderID: saved.FolderID})
		if err != nil || len(page.Items) != 1 || page.Total != 1 || page.Items[0].Deleted {
			test.Fatalf("items=%+v, %v", page, err)
		}
		threads, err := resolver.ListMailboxThreads(ctx, ListMailboxThreadsArguments{FolderID: saved.FolderID})
		if err != nil || len(threads.Threads) != 1 || threads.Total != 1 {
			test.Fatalf("threads=%+v, %v", threads, err)
		}
		// A search of the whole mailbox is a search of its mail.
		everywhere, err := resolver.ListMailboxItems(ctx, ListMailboxItemsArguments{MailboxID: &mailbox.ID})
		if err != nil || len(everywhere.Items) != 0 {
			test.Fatalf("mailbox-wide items=%+v, %v", everywhere, err)
		}
	})
	// IMAP counts both until a client expunges.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		folders, err := transaction.ListFolders(mailbox.ID, nil)
		if err != nil {
			test.Fatal(err)
		}
		for _, folder := range folders {
			if folder.ID == saved.FolderID && folder.Total != 2 {
				test.Fatalf("IMAP counts %d in the notes folder", folder.Total)
			}
			// Counting notes is the dashboard's question, and costs a join
			// IMAP does not pay.
			if folder.NoteCount != 0 {
				test.Fatalf("IMAP counted %d notes in %s", folder.NoteCount, folder.Name)
			}
		}
	})
}

// addFlaggedVersion files an older version of a note, flagged deleted, as a
// phone leaves one after an edit.
func addFlaggedVersion(test *testing.T, database db.Database, folderId, noteIdentifier string) {
	test.Helper()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		older, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindNote, NoteIdentifier: noteIdentifier, Subject: "An older version"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.AddItem(folderId, older.ID, "", models.MailboxItemFlags{Seen: new(true), Deleted: new(true)}); err != nil {
			test.Fatal(err)
		}
	})
}

// folderOfKind is the mailbox's folder of a kind, made when the mailbox was.
func folderOfKind(test *testing.T, database db.Database, mailboxId string, kind models.MailboxFolderKind) *models.MailboxFolder {
	test.Helper()
	var folder *models.MailboxFolder
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if folder, err = transaction.GetFolderByKind(mailboxId, kind); err != nil || folder == nil {
			test.Fatalf("the %s folder=%+v, %v", kind, folder, err)
		}
	})
	return folder
}

// addStoredVersion files a version of a note that is not flagged, with its
// message in storage, and returns its item. Headers left out are those of
// a single HTML part.
func addStoredVersion(test *testing.T, database db.Database, spool storage.Storage, folderId, noteIdentifier string, headers []string, body string) *models.MailboxItem {
	test.Helper()
	var item *models.MailboxItem
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindNote, NoteIdentifier: noteIdentifier, Subject: "A version"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		if len(headers) == 0 {
			headers = []string{"Content-Type: text/html; charset=utf-8\r\n"}
		}
		headers = append(append([]string{}, headers...), notes.Headers(noteIdentifier, time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC))...)
		if err := spool.Put(test.Context(), stored.ID, headers, []byte(body)); err != nil {
			test.Fatal(err)
		}
		if item, err = transaction.AddItem(folderId, stored.ID, "", models.MailboxItemFlags{Seen: new(true)}); err != nil {
			test.Fatal(err)
		}
	})
	return item
}

// A copy of a note in the trash or junk is not a note of the person's: it is
// not listed, a new note never goes there, and saving the note leaves it be.
// Nor is a copy in another folder of the owner's removed by a save.
func TestNotesLeaveOutTheTrashAndKeepOtherCopies(test *testing.T) {
	database, resolver, principal, mailbox, spool := noteFixture(test)
	trash := folderOfKind(test, database, mailbox.ID, models.MailboxFolderKindTrash)
	junk := folderOfKind(test, database, mailbox.ID, models.MailboxFolderKindJunk)
	const discarded = "0A1B2C3D-0000-4000-8000-00000000000A"
	addStoredVersion(test, database, spool, trash.ID, discarded, nil, "<div>Thrown away</div>")

	var saved *NoteView
	asPerson(test, database, principal, func(ctx context.Context) {
		var err error
		// Asked to go in the junk, it goes where notes go instead.
		if saved, err = resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, Text: new("Garden"), FolderID: junk.ID}); err != nil {
			test.Fatal(err)
		}
		listed, err := resolver.ListNotes(ctx, ListNotesArguments{MailboxID: mailbox.ID})
		if err != nil || len(listed) != 1 || listed[0].ID != saved.ID {
			test.Fatalf("list=%+v, %v", listed, err)
		}
		if _, err := resolver.GetNote(ctx, GetNoteArguments{MailboxID: mailbox.ID, NoteID: discarded}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("a note in the trash was read: %v", err)
		}
	})
	if saved.FolderID == trash.ID || saved.FolderID == junk.ID {
		test.Fatalf("a new note went in %s", saved.FolderID)
	}

	// A copy the owner filed in a folder of their own, before this save.
	var kept *models.MailboxFolder
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if kept, err = transaction.CreateFolder(&models.MailboxFolder{MailboxID: mailbox.ID, Name: "Kept"}); err != nil {
			test.Fatal(err)
		}
	})
	copied := addStoredVersion(test, database, spool, kept.ID, saved.ID, nil, "<div>Garden</div>")
	dbtest.Exec(test, database, fmt.Sprintf(`UPDATE mailbox_item SET added_at = added_at - interval '1 hour' WHERE id = '%s'`, copied.ID))
	addStoredVersion(test, database, spool, trash.ID, saved.ID, nil, "<div>Garden, binned</div>")

	asPerson(test, database, principal, func(ctx context.Context) {
		if _, err := resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, NoteID: saved.ID, Text: new("Garden\nroses")}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		items, err := transaction.ListNoteItems(mailbox.ID, saved.ID)
		if err != nil {
			test.Fatal(err)
		}
		inFolder := map[string]int{}
		for _, item := range items {
			inFolder[item.FolderID]++
		}
		if len(items) != 2 || inFolder[saved.FolderID] != 1 || inFolder[kept.ID] != 1 {
			test.Fatalf("after the save, by folder=%v", inFolder)
		}
		inTrash, err := transaction.ListItems(trash.ID, nil)
		if err != nil || len(inTrash) != 2 {
			test.Fatalf("the trash holds %d, %v", len(inTrash), err)
		}
	})
}

// The version that is the note is the one filed last, whatever date the
// client that wrote it put on the message.
func TestTheCurrentVersionIsTheOneFiledLast(test *testing.T) {
	database, resolver, principal, mailbox, spool := noteFixture(test)
	var saved *NoteView
	asPerson(test, database, principal, func(ctx context.Context) {
		var err error
		if saved, err = resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, Text: new("Recipes\nsoup")}); err != nil {
			test.Fatal(err)
		}
	})
	// A phone whose clock is a year behind appends an edit.
	later := addStoredVersion(test, database, spool, saved.FolderID, saved.ID, nil, "<div>Recipes</div><div>soup and bread</div>")
	dbtest.Exec(test, database, fmt.Sprintf(`UPDATE mail SET received_at = received_at - interval '1 year' WHERE id = '%s'`, later.MailID))
	asPerson(test, database, principal, func(ctx context.Context) {
		read, err := resolver.GetNote(ctx, GetNoteArguments{MailboxID: mailbox.ID, NoteID: saved.ID})
		if err != nil || read.Text != "Recipes\nsoup and bread" || read.ModifiedAt.Sub(later.AddedAt).Abs() >= time.Second {
			test.Fatalf("read=%+v, %v", read, err)
		}
	})
}

// A save names the version it read; when the note changed since, it is
// refused rather than written over the change.
func TestSaveNoteRefusesAChangeMadeSinceItWasRead(test *testing.T) {
	database, resolver, principal, mailbox, spool := noteFixture(test)
	var saved *NoteView
	asPerson(test, database, principal, func(ctx context.Context) {
		var err error
		if saved, err = resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, Text: new("Books\none")}); err != nil {
			test.Fatal(err)
		}
	})
	read := saved.ModifiedAt.Format(time.RFC3339Nano)

	// Saved against what it read, it is written.
	var second *NoteView
	asPerson(test, database, principal, func(ctx context.Context) {
		var err error
		if second, err = resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, NoteID: saved.ID, Text: new("Books\none\ntwo"), ExpectedModifiedAt: read}); err != nil {
			test.Fatal(err)
		}
	})

	// The phone changes it a minute later.
	edited := addStoredVersion(test, database, spool, saved.FolderID, saved.ID, nil, "<div>Books</div><div>one, from the phone</div>")
	dbtest.Exec(test, database, fmt.Sprintf(`UPDATE mailbox_item SET added_at = added_at + interval '1 minute' WHERE id = '%s'`, edited.ID))
	asPerson(test, database, principal, func(ctx context.Context) {
		_, err := resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, NoteID: saved.ID, Text: new("Books\nthree"), ExpectedModifiedAt: second.ModifiedAt.Format(time.RFC3339Nano)})
		if !errors.Is(err, api.ErrConflict) || !strings.Contains(err.Error(), "changed elsewhere") {
			test.Fatalf("a save over a newer version=%v", err)
		}
		read, err := resolver.GetNote(ctx, GetNoteArguments{MailboxID: mailbox.ID, NoteID: saved.ID})
		if err != nil || read.Text != "Books\none, from the phone" {
			test.Fatalf("the phone's change=%+v, %v", read, err)
		}
		// Without a version named, the last writer wins.
		if _, err := resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, NoteID: saved.ID, Text: new("Books\nfour")}); err != nil {
			test.Fatal(err)
		}
		if _, err := resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, NoteID: saved.ID, Text: new("Books"), ExpectedModifiedAt: "yesterday"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("an unreadable time=%v", err)
		}
	})
}

// A note holding a picture or an attachment is read, not written: a version
// written from its HTML would lose them.
func TestANoteWithPicturesIsNotEditable(test *testing.T) {
	database, resolver, principal, mailbox, spool := noteFixture(test)
	notesFolder := ""
	asPerson(test, database, principal, func(ctx context.Context) {
		saved, err := resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, Text: new("Plain")})
		if err != nil || !saved.IsEditable {
			test.Fatalf("a plain note=%+v, %v", saved, err)
		}
		notesFolder = saved.FolderID
	})
	const pictured, attached = "0A1B2C3D-0000-4000-8000-00000000000B", "0A1B2C3D-0000-4000-8000-00000000000C"
	addStoredVersion(test, database, spool, notesFolder, pictured, nil,
		`<div>Holiday</div><div><img src="cid:example-picture"></div>`)
	addStoredVersion(test, database, spool, notesFolder, attached, []string{"Content-Type: multipart/mixed; boundary=\"example\"\r\n"},
		"--example\r\nContent-Type: text/html\r\n\r\n<div>Tickets</div>\r\n--example\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"example.pdf\"\r\n\r\nexample\r\n--example--\r\n")
	asPerson(test, database, principal, func(ctx context.Context) {
		for _, noteId := range []string{pictured, attached} {
			read, err := resolver.GetNote(ctx, GetNoteArguments{MailboxID: mailbox.ID, NoteID: noteId})
			if err != nil || read.IsEditable {
				test.Fatalf("%s=%+v, %v", noteId, read, err)
			}
			_, err = resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID, NoteID: noteId, Text: new("Changed")})
			if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), notes.UneditableReason) {
				test.Fatalf("saving %s=%v", noteId, err)
			}
		}
	})
}

// A note's HTML goes into the dashboard's own page, so it comes back without
// what would reach outside the editor.
func TestGetNoteHTMLIsFitForTheDashboard(test *testing.T) {
	database, resolver, principal, mailbox, _ := noteFixture(test)
	asPerson(test, database, principal, func(ctx context.Context) {
		saved, err := resolver.SaveNote(ctx, SaveNoteArguments{MailboxID: mailbox.ID,
			HTML: new(`<style>body { display: none }</style><div class="compose" id="sign-in" onclick="steal()">Errands</div>`)})
		if err != nil {
			test.Fatal(err)
		}
		read, err := resolver.GetNote(ctx, GetNoteArguments{MailboxID: mailbox.ID, NoteID: saved.ID})
		if err != nil || !strings.Contains(read.HTML, "Errands") {
			test.Fatalf("read=%+v, %v", read, err)
		}
		for _, unwanted := range []string{"<style", "class=", "id=", "onclick"} {
			if strings.Contains(read.HTML, unwanted) {
				test.Fatalf("%q survived: %s", unwanted, read.HTML)
			}
		}
	})
}

// A conversation read in the dashboard leaves out what is flagged deleted,
// as its lists and counts do.
func TestGetMailboxThreadLeavesOutWhatIsFlaggedDeleted(test *testing.T) {
	database, resolver, principal, mailbox, _ := noteFixture(test)
	inbox := folderOfKind(test, database, mailbox.ID, models.MailboxFolderKindInbox)
	const threadId = "examplethread0001"
	var liveItemId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for index, isDeleted := range []bool{false, true} {
			stored, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindIncoming, Subject: fmt.Sprintf("Plans %d", index), ThreadID: threadId}, nil)
			if err != nil {
				test.Fatal(err)
			}
			item, err := transaction.AddItem(inbox.ID, stored.ID, "", models.MailboxItemFlags{Deleted: new(isDeleted)})
			if err != nil {
				test.Fatal(err)
			}
			if !isDeleted {
				liveItemId = item.ID
			}
		}
	})
	asPerson(test, database, principal, func(ctx context.Context) {
		thread, err := resolver.GetMailboxThread(ctx, GetMailboxThreadArguments{ItemID: liveItemId})
		if err != nil || len(thread.Items) != 1 || thread.Items[0].Item.ID != liveItemId {
			test.Fatalf("thread=%+v, %v", thread, err)
		}
	})
}
