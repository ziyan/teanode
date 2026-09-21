package apigraph

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestContactResolverJoinsCallerTransaction(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "contact-owner")
	principal := &api.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionContactsUse}})}
	var book *models.AddressBook
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		book, err = transaction.CreateAddressBook(&models.AddressBook{UserID: userId, Name: "Fixture contacts"})
		if err != nil {
			test.Fatal(err)
		}
	})
	resolver := &graph{database: database}
	interrupted := errors.New("caller rolled back")
	if err := database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		created, err := resolver.SaveContact(ctx, SaveContactArguments{AddressBookID: book.ID, Name: new("Fixture contact"), Note: new("Kept note")})
		if err != nil {
			return err
		}
		updated, err := resolver.SaveContact(ctx, SaveContactArguments{ID: created.ID, Name: new("Updated contact")})
		if err != nil {
			return err
		}
		if updated.ID != created.ID || updated.Name != "Updated contact" {
			test.Fatalf("updated contact=%+v", updated)
		}
		return interrupted
	}); !errors.Is(err, interrupted) {
		test.Fatal(err)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM contact`); count != "0" {
		test.Fatalf("resolver save escaped rollback: %s", count)
	}
}
