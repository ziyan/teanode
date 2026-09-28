package apigraph

import (
	"context"
	"strings"
	"testing"

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
			if folder.ID == saved.FolderID && folder.Total != 1 {
				test.Fatalf("the dashboard counts %d in the notes folder", folder.Total)
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
