package db_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Changing a delivery reads it, lets the caller change it and saves the whole
// row. Two instances doing that at once take turns, so neither change is
// written over by the other.
func TestChangingOneDeliveryAtOnceLosesNoChange(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	var deliveryId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mail, err := transaction.CreateMail(&models.Mail{Subject: "Fixture"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		delivery, err := transaction.CreateDelivery(&models.Delivery{MailID: mail.ID, Recipient: "recipient@example.com", Kind: models.DeliveryKindExternal}, nil)
		if err != nil {
			test.Fatal(err)
		}
		deliveryId = delivery.ID
	})

	const changerCount = 6
	var group sync.WaitGroup
	for range changerCount {
		group.Add(1)
		go func() {
			defer group.Done()
			err := database.Transaction(func(transaction db.Transaction) error {
				_, err := transaction.ModifyDelivery(deliveryId, func(stored *models.Delivery) error {
					attempts := stored.Attempts
					// Long enough for every other changer to have read the
					// row by now, were it not locked.
					time.Sleep(50 * time.Millisecond)
					stored.Attempts = attempts + 1
					return nil
				}, nil)
				return err
			})
			if err != nil {
				test.Errorf("ModifyDelivery: %s", err)
			}
		}()
	}
	group.Wait()

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.GetDelivery(deliveryId, nil)
		if err != nil || stored == nil || stored.Attempts != changerCount {
			test.Fatalf("attempts after %d changes: %+v %v", changerCount, stored, err)
		}
	})
}

// The recurring events running out are locked by the instance extending them,
// and another instance asking meanwhile is not given those, nor made to wait
// for them.
func TestRecurringEventsBeingExtendedAreNotGivenToAnotherInstance(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	var objectId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		owner, err := transaction.CreateUser(&models.User{Username: "calendar-owner"})
		if err != nil {
			test.Fatal(err)
		}
		calendar, err := transaction.CreateCalendar(&models.Calendar{UserID: owner.ID})
		if err != nil {
			test.Fatal(err)
		}
		start := time.Now().Add(time.Hour)
		object, err := transaction.PutCalendarObject(&models.CalendarObject{
			CalendarID: calendar.ID, UID: "weekly", ETag: "e1", Data: "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n",
			Summary: "Weekly", StartsAt: start, EndsAt: start.Add(time.Hour), Recurring: true,
		}, nil)
		if err != nil {
			test.Fatal(err)
		}
		objectId = object.ID
	})

	listRunningOut := func(transaction db.Transaction) []*models.CalendarObject {
		found, err := transaction.ListCalendarObjectsRunningOut(time.Now().AddDate(10, 0, 0), time.Now(), 20)
		if err != nil {
			test.Fatal(err)
		}
		return found
	}

	hasListed := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- database.Transaction(func(transaction db.Transaction) error {
			if found := listRunningOut(transaction); len(found) != 1 || found[0].ID != objectId {
				test.Errorf("the first instance is given %d events", len(found))
			}
			close(hasListed)
			<-release
			return nil
		})
	}()
	<-hasListed

	done := make(chan int, 1)
	go func() {
		count := 0
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			count = len(listRunningOut(transaction))
		})
		done <- count
	}()
	select {
	case count := <-done:
		if count != 0 {
			test.Errorf("the second instance is given %d events the first holds", count)
		}
	case <-time.After(5 * time.Second):
		test.Error("the second instance waited for the events the first holds")
	}
	close(release)
	if err := <-finished; err != nil {
		test.Fatal(err)
	}
}
