package apigraph

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// An arrival at a place of a circle waits to see whether it is a pass-by, and
// is stored for that time rather than kept in the memory of one instance: it
// survives a restart, is told by tellDueVisitNotices once due, and is gone
// once told.
func TestAnArrivalThatWaitsIsStoredAndToldWhenDue(test *testing.T) {
	world := newLocationWorld(test)
	configuration := config.Default()
	configuration.Server.Secret = "a secret for sealing in this test"
	world.graph.settings = &api.Settings{Chat: chat.New(&chat.Settings{
		Database: world.database, Configuration: func() *config.Configuration { return configuration }, Instance: "visit-test",
		Phones: agent.NewPhoneNotifier(&agent.PhoneNotifierSettings{
			Database: world.database, Configuration: func() *config.Configuration { return configuration },
			Context: context.Background(), WaitGroup: &sync.WaitGroup{}, Sender: &chatRelay{},
		}),
	})}

	made := world.call(test, world.ada, `mutation { SavePlace(latitudeDegrees: 40, longitudeDegrees: -75, placeName: "Home", placeLabel: "home") { id } }`)["SavePlace"].(map[string]any)
	arrivedAt := time.Now().UTC().Truncate(time.Second)
	world.report(test, world.ada, "", regionVisitInput("region-home", made["id"].(string), 40, -75, arrivedAt.Format(time.RFC3339), ""))

	waiting := func() string {
		return dbtest.QueryString(test, world.database, `SELECT count(*) FROM location_visit_notice WHERE user_id = '`+world.ada.ID+`' AND client_visit_key = 'region-home' AND is_arrival`)
	}
	if got := waiting(); got != "1" {
		test.Fatalf("%s arrival notices are waiting, want 1", got)
	}
	// Not yet due: nothing is claimed and the notice stays.
	if err := world.graph.tellDueVisitNotices(context.Background(), arrivedAt.Add(db.PassByDurationMost-time.Second)); err != nil {
		test.Fatal(err)
	}
	if got := waiting(); got != "1" {
		test.Fatalf("%s arrival notices are waiting before the time, want 1", got)
	}
	// Due: told, and forgotten.
	if err := world.graph.tellDueVisitNotices(context.Background(), arrivedAt.Add(db.PassByDurationMost+time.Second)); err != nil {
		test.Fatal(err)
	}
	if got := waiting(); got != "0" {
		test.Fatalf("%s arrival notices are waiting after it was told, want 0", got)
	}
}
