package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The reminders list is a calendar in the agent's sources, so the view says
// what each holds and a client can pick the one with events.
func TestTheAgentsCollectionsSayWhatEachCalendarHolds(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "source-owner")
	resolver := &graph{database: database}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for _, calendar := range []*models.Calendar{
			{UserID: userId, Name: "Calendar", CalendarKind: models.CalendarEvents},
			{UserID: userId, Name: "Reminders", CalendarKind: models.CalendarReminders},
		} {
			if _, err := transaction.CreateCalendar(calendar); err != nil {
				test.Fatal(err)
			}
		}
		if _, err := transaction.CreateAddressBook(&models.AddressBook{UserID: userId, Name: "Contacts"}); err != nil {
			test.Fatal(err)
		}
		collections, err := resolver.collectionsOf(transaction, userId)
		if err != nil {
			test.Fatal(err)
		}
		kinds := map[string]string{}
		for _, collection := range collections {
			kinds[collection.Name] = collection.Kind + "/" + collection.CalendarKind
		}
		want := map[string]string{"Calendar": "calendar/events", "Reminders": "calendar/reminders", "Contacts": "addressBook/"}
		for name, kind := range want {
			if kinds[name] != kind {
				test.Errorf("%s is %q, want %q", name, kinds[name], kind)
			}
		}
	})
}
