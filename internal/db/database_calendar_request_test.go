package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

func TestCalendarRequestMigrationPreservesEventAndMail(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "calendar-owner")
	var calendarId, objectId, mailId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		storedCalendar, err := transaction.CreateCalendar(&models.Calendar{UserID: userId})
		if err != nil {
			test.Fatal(err)
		}
		calendarId = storedCalendar.ID
		object, err := transaction.PutCalendarObject(&models.CalendarObject{CalendarID: calendarId, UID: "weekly", ETag: "e1", Data: oneEvent, Summary: "Weekly sync", StartsAt: at(14, 10), EndsAt: at(14, 11)}, nil)
		if err != nil {
			test.Fatal(err)
		}
		objectId = object.ID
		mail, err := transaction.CreateMail(&models.Mail{Subject: "Retained fixture"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		mailId = mail.ID
		receipt := &models.CalendarRequestReceipt{UserID: userId, RequestID: "fixture", Operation: "save", CalendarID: calendarId, ObjectID: objectId, RequestDigest: strings.Repeat("a", 64), CompletedAt: time.Now()}
		if err := transaction.CreateCalendarRequest(receipt); err != nil {
			test.Fatal(err)
		}
	})
	for _, migration := range migrations.Migrations() {
		if migration.ID != "0095_calendar_request" {
			continue
		}
		dbtest.Exec(test, database, migration.ReverseSQL)
		dbtest.Exec(test, database, migration.SQL)
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			mail, err := transaction.GetMail(mailId, nil)
			if err != nil || mail == nil {
				test.Fatalf("mail after reverse=%+v, %v", mail, err)
			}
			object, err := transaction.GetCalendarObject(calendarId, objectId)
			if err != nil || object == nil || object.Data != oneEvent {
				test.Fatalf("event after reverse=%+v, %v", object, err)
			}
			receipt, err := transaction.GetCalendarRequest(userId, "fixture")
			if err != nil || receipt != nil {
				test.Fatalf("receipt after reverse=%+v, %v", receipt, err)
			}
		})
		return
	}
	test.Fatal("calendar request migration is missing")
}

func TestCalendarRequestPermissionMigrationReversesWithoutLosingIdentity(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.CreateCalendarRequest(&models.CalendarRequestReceipt{UserID: "fixture-owner", RequestID: "fixture", Operation: "save", CalendarID: "fixture-calendar", ObjectID: "fixture-event", RequestDigest: strings.Repeat("a", 64), CompletedAt: time.Now(), IsMailSendRequired: true}); err != nil {
			test.Fatal(err)
		}
	})
	for _, migration := range migrations.Migrations() {
		if migration.ID != "0096_calendar_request_permission" {
			continue
		}
		dbtest.Exec(test, database, migration.ReverseSQL)
		if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_request`); count != "1" {
			test.Fatalf("retained identities=%s", count)
		}
		dbtest.Exec(test, database, migration.SQL)
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			receipt, err := transaction.GetCalendarRequest("fixture-owner", "fixture")
			if err != nil || receipt == nil || receipt.ObjectID != "fixture-event" || receipt.IsMailSendRequired {
				test.Fatalf("reapplied permission=%+v, %v", receipt, err)
			}
		})
		return
	}
	test.Fatal("calendar request permission migration is missing")
}
