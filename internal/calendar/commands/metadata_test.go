package commands

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func calendarCommandFixture(test *testing.T) (db.Database, *access.Principal, *models.Calendar) {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "calendar-owner")
	principal := &access.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}})}
	var storedCalendar *models.Calendar
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		storedCalendar, err = transaction.CreateCalendar(&models.Calendar{UserID: userId, Name: "Original", WeekStart: models.WeekStartsMonday})
		if err != nil {
			test.Fatal(err)
		}
	})
	return database, principal, storedCalendar
}

func TestCalendarMetadataCommandPreservesConcurrentGrantAndAuditActor(test *testing.T) {
	database, principal, storedCalendar := calendarCommandFixture(test)
	isLocked := make(chan struct{})
	canCommit := make(chan struct{})
	grantDone := make(chan error, 1)
	go func() {
		grantDone <- database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
			locked, err := transaction.LockCalendar(storedCalendar.ID)
			if err == nil {
				locked.AgentGranted = true
				_, err = transaction.UpdateCalendar(locked)
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
		_, err := New(database).Update(ctx, principal, UpdateRequest{ID: storedCalendar.ID, Name: "Renamed", Description: "Updated"})
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
		kept, err := transaction.GetCalendar(storedCalendar.ID)
		if err != nil || kept == nil || !kept.AgentGranted || kept.Name != "Renamed" {
			test.Fatalf("metadata=%+v, %v", kept, err)
		}
		events, err := transaction.ListAuditEvents(&db.AuditOptions{ResourceID: storedCalendar.ID, ActorUserID: principal.User.ID})
		if err != nil || len(events) != 1 || events[0].ActorKind != models.AuditActorUser {
			test.Fatalf("audit=%+v, %v", events, err)
		}
	})
	for _, denied := range []*access.Principal{nil, {User: principal.User, Permissions: models.NewEffectivePermissions(nil)}, {User: &models.User{ID: "different-owner"}, Permissions: principal.Permissions}} {
		if _, err := New(database).Update(test.Context(), denied, UpdateRequest{ID: storedCalendar.ID, Name: "Refused"}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("metadata permission=%v", err)
		}
	}
}

func TestCalendarMetadataValidationAndEmptyFields(test *testing.T) {
	database, principal, storedCalendar := calendarCommandFixture(test)
	for _, request := range []UpdateRequest{
		{ID: storedCalendar.ID, Name: "Refused", Timezone: "Invalid/Fixture"},
		{ID: storedCalendar.ID, Name: "Refused", WeekStart: "tuesday"},
	} {
		if _, err := New(database).Update(test.Context(), principal, request); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("validation=%v", err)
		}
	}
	outcome, err := New(database).Update(test.Context(), principal, UpdateRequest{ID: storedCalendar.ID, Timezone: "UTC", Colour: "#123456", Description: "Fixture"})
	if err != nil {
		test.Fatal(err)
	}
	if outcome.Calendar.Name != "Original" || outcome.Calendar.WeekStart != models.WeekStartsMonday || outcome.Calendar.Timezone != "UTC" || outcome.EventCount != 0 {
		test.Fatalf("metadata=%+v", outcome)
	}
	outcome, err = New(database).Update(test.Context(), principal, UpdateRequest{ID: storedCalendar.ID})
	if err != nil {
		test.Fatal(err)
	}
	if outcome.Calendar.Name != "Original" || outcome.Calendar.WeekStart != models.WeekStartsMonday || outcome.Calendar.Timezone != "" || outcome.Calendar.Description != "" || outcome.Calendar.Colour != "" {
		test.Fatalf("empty fields=%+v", outcome.Calendar)
	}
}
