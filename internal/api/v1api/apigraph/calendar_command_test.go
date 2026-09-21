package apigraph

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestCalendarMetadataPreservesAgentGrantAndCallerRollback(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "calendar-owner")
	principal := &api.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}})}
	var storedCalendar *models.Calendar
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		storedCalendar, err = transaction.CreateCalendar(&models.Calendar{UserID: userId, Name: "Original"})
		if err != nil {
			test.Fatal(err)
		}
		storedCalendar.AgentGranted = true
		if _, err := transaction.UpdateCalendar(storedCalendar); err != nil {
			test.Fatal(err)
		}
	})
	resolver := &graph{database: database}
	interrupted := errors.New("caller rolled back")
	if err := database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		updated, err := resolver.SaveCalendar(ctx, SaveCalendarArguments{ID: storedCalendar.ID, Name: "Renamed", Description: "Changed description"})
		if err != nil {
			return err
		}
		if !updated.AgentGranted || updated.Name != "Renamed" {
			test.Errorf("rename changed grant: %+v", updated)
		}
		return interrupted
	}); !errors.Is(err, interrupted) {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		kept, err := transaction.GetCalendar(storedCalendar.ID)
		if err != nil {
			test.Fatal(err)
		}
		if kept.Name != "Original" || !kept.AgentGranted {
			test.Fatalf("metadata escaped rollback: %+v", kept)
		}
	})
}
