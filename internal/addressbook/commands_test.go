package addressbook

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func contactCommandFixture(test *testing.T) (db.Database, *access.Principal, *models.AddressBook) {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "contact-owner")
	principal := &access.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionContactsUse}})}
	var book *models.AddressBook
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		book, err = transaction.CreateAddressBook(&models.AddressBook{UserID: userId, Name: "Fixture contacts"})
		if err != nil {
			test.Fatal(err)
		}
	})
	return database, principal, book
}

func TestContactCommandsRespectCallerRollbackAndPermissions(test *testing.T) {
	database, principal, book := contactCommandFixture(test)
	request := SaveRequest{AddressBookID: book.ID, Fields: contacts.Fields{Name: new("Fixture contact")}}
	interrupted := errors.New("caller rolled back")
	if err := database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
		if _, err := New(transaction).Save(test.Context(), principal, request); err != nil {
			return err
		}
		return interrupted
	}); !errors.Is(err, interrupted) {
		test.Fatal(err)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM contact`); count != "0" {
		test.Fatalf("save escaped parent rollback: %s", count)
	}
	created, err := New(database).Save(test.Context(), principal, request)
	if err != nil {
		test.Fatal(err)
	}
	for _, denied := range []*access.Principal{nil, {User: principal.User, Permissions: models.NewEffectivePermissions(nil)}, {User: &models.User{ID: "another-owner"}, Permissions: principal.Permissions}, {Console: true, Permissions: principal.Permissions}} {
		if _, err := New(database).Save(test.Context(), denied, request); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("create permission=%v", err)
		}
		if _, err := New(database).Save(test.Context(), denied, SaveRequest{ID: created.ID, Fields: contacts.Fields{Name: new("Changed")}}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("edit permission=%v", err)
		}
		if err := New(database).Delete(test.Context(), denied, created.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("delete permission=%v", err)
		}
	}
	if err := database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
		if err := New(transaction).Delete(test.Context(), principal, created.ID); err != nil {
			return err
		}
		return interrupted
	}); !errors.Is(err, interrupted) {
		test.Fatal(err)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM contact`); count != "1" {
		test.Fatalf("delete escaped parent rollback: %s", count)
	}
}

func TestContactCommandFailureDoesNotPoisonCaller(test *testing.T) {
	database, principal, book := contactCommandFixture(test)
	dbtest.Exec(test, database, `ALTER TABLE contact ADD CONSTRAINT fixture_name CHECK (name <> 'Refused')`)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		commands := New(transaction)
		if _, err := commands.Save(test.Context(), principal, SaveRequest{AddressBookID: book.ID, Fields: contacts.Fields{Name: new("Refused")}}); err == nil {
			test.Fatal("expected final SQL failure")
		}
		if _, err := commands.Save(test.Context(), principal, SaveRequest{AddressBookID: book.ID, Fields: contacts.Fields{Name: new("Kept")}}); err != nil {
			test.Fatal(err)
		}
	})
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM contact`); count != "1" {
		test.Fatalf("contacts=%s", count)
	}
}

func TestContactFormMergeWaitsForLatestLockedCard(test *testing.T) {
	database, principal, book := contactCommandFixture(test)
	created, err := New(database).Save(test.Context(), principal, SaveRequest{AddressBookID: book.ID, Fields: contacts.Fields{Name: new("Original"), Note: new("Original note")}})
	if err != nil {
		test.Fatal(err)
	}
	isLocked := make(chan struct{})
	canCommit := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
			_, err := New(transaction).Save(test.Context(), principal, SaveRequest{ID: created.ID, Fields: contacts.Fields{Name: new("Concurrent name")}})
			close(isLocked)
			<-canCommit
			return err
		})
	}()
	<-isLocked
	ctx, cancel := context.WithTimeout(test.Context(), 5*time.Second)
	defer cancel()
	secondDone := make(chan error, 1)
	go func() {
		_, err := New(database).Save(ctx, principal, SaveRequest{ID: created.ID, Fields: contacts.Fields{Note: new("Concurrent note")}})
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		close(canCommit)
		<-firstDone
		test.Fatalf("merge completed while card was locked: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(canCommit)
	if err := <-firstDone; err != nil {
		test.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		kept, err := transaction.GetContact(book.ID, created.ID)
		if err != nil {
			test.Fatal(err)
		}
		if kept.Name != "Concurrent name" || !strings.Contains(kept.Card, "Concurrent note") {
			test.Fatalf("merged card=%s", kept.Card)
		}
	})
}

func TestContactUIDUpdateAtCapacityDoesNotBecomeANewContact(test *testing.T) {
	database, principal, book := contactCommandFixture(test)
	created, err := New(database).Save(test.Context(), principal, SaveRequest{AddressBookID: book.ID, Fields: contacts.Fields{Name: new("Original")}})
	if err != nil {
		test.Fatal(err)
	}
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO contact (id, addressbook_id, created_at, modified_at, uid, etag, card, name, organization, emails, phones)
 SELECT 'fixture-' || entry, '%s', now(), now(), 'fixture-' || entry, 'fixture', 'fixture', 'Fixture', '', '', '' FROM generate_series(1, %d) AS entry`, book.ID, db.ContactsPerBook-1))
	parsed, err := contacts.Build([]byte(created.Card), &contacts.Fields{Name: new("Updated")})
	if err != nil {
		test.Fatal(err)
	}
	updated, err := New(database).Save(test.Context(), principal, SaveRequest{AddressBookID: book.ID, Card: string(parsed.Card)})
	if err != nil || updated == nil || updated.ID != created.ID || updated.Name != "Updated" {
		test.Fatalf("UID update=%+v, %v", updated, err)
	}
	if _, err := New(database).Save(test.Context(), principal, SaveRequest{AddressBookID: book.ID, Fields: contacts.Fields{Name: new("New")}}); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("new contact at capacity=%v", err)
	}
}

func TestBookMetadataCommandPreservesConcurrentGrantAndAuditActor(test *testing.T) {
	database, principal, book := contactCommandFixture(test)
	isLocked := make(chan struct{})
	canCommit := make(chan struct{})
	grantDone := make(chan error, 1)
	go func() {
		grantDone <- database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
			locked, err := transaction.LockAddressBook(book.ID)
			if err == nil {
				locked.AgentGranted = true
				_, err = transaction.UpdateAddressBook(locked)
			}
			close(isLocked)
			<-canCommit
			return err
		})
	}()
	<-isLocked
	ctx, cancel := context.WithTimeout(test.Context(), 5*time.Second)
	defer cancel()
	ctx = db.ContextWithAuditPrincipal(ctx, db.AuditPrincipal{ActorKind: models.AuditActorUser, UserID: principal.User.ID})
	updateDone := make(chan error, 1)
	go func() {
		_, err := New(database).UpdateBook(ctx, principal, UpdateBookRequest{ID: book.ID, Name: "Renamed", Description: "Updated"})
		updateDone <- err
	}()
	select {
	case err := <-updateDone:
		close(canCommit)
		<-grantDone
		test.Fatalf("metadata edit did not wait for grant: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(canCommit)
	if err := <-grantDone; err != nil {
		test.Fatal(err)
	}
	if err := <-updateDone; err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		kept, err := transaction.GetAddressBook(book.ID)
		if err != nil || kept == nil || !kept.AgentGranted || kept.Name != "Renamed" {
			test.Fatalf("metadata=%+v, %v", kept, err)
		}
		events, err := transaction.ListAuditEvents(&db.AuditOptions{ResourceID: book.ID, ActorUserID: principal.User.ID})
		if err != nil || len(events) != 1 || events[0].ActorKind != models.AuditActorUser {
			test.Fatalf("audit=%+v, %v", events, err)
		}
	})
	for _, denied := range []*access.Principal{nil, {User: principal.User, Permissions: models.NewEffectivePermissions(nil)}, {User: &models.User{ID: "different-owner"}, Permissions: principal.Permissions}} {
		if _, err := New(database).UpdateBook(test.Context(), denied, UpdateBookRequest{ID: book.ID, Name: "Refused"}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("metadata permission=%v", err)
		}
	}
}
