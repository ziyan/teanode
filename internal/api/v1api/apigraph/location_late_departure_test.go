package apigraph

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// lateDepartureDay is a morning at home, then stops elsewhere that iOS
// reported, while the phone missed leaving home's circle and said so only on
// coming near it again, hours later. Each report is one of the phone's
// batches; the orders are the ones the batches can reach the server in.
func lateDepartureDay(placeId string, at func(minutes float64) string) map[string][]string {
	home := func(arrivedAt, departedAt string) string {
		return regionVisitInput("region-home", placeId, 40, -75, arrivedAt, departedAt)
	}
	far := visitInput("stop-far", 40.16, -75, at(48), at(136))
	second := visitInput("stop-second", 40.02, -75, at(167), at(186))
	third := visitInput("stop-third", 40.0201, -75.001, at(200), at(208))
	open := visitInput("stop-open", 40.0202, -75.003, at(217), "")
	departure := home("", at(217))
	return map[string][]string{
		"each as it happened":       {home(at(0), ""), far, second, third, open + "," + departure},
		"departure before the last": {home(at(0), ""), far, second, departure, third, open},
		"departure alone, last":     {home(at(0), ""), far, second, third, open, departure},
		"stops late, after it":      {home(at(0), ""), departure, far + "," + second + "," + third + "," + open},
	}
}

// A departure the phone sent hours late does not carry the visit over the
// stops made since: whatever order the batches come in, home ends where the
// first stop elsewhere began.
func TestALateDepartureEndsTheVisitAtTheFirstStopElsewhere(t *testing.T) {
	base := time.Now().Add(-8 * time.Hour).UTC().Truncate(time.Second)
	at := func(minutes float64) string {
		return base.Add(time.Duration(minutes * float64(time.Minute))).Format(time.RFC3339)
	}
	for name := range lateDepartureDay("", at) {
		t.Run(name, func(t *testing.T) {
			world := newLocationWorld(t)
			made := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 40, longitudeDegrees: -75, placeName: "Home", placeLabel: "home") { id } }`)["SavePlace"].(map[string]any)
			for _, visits := range lateDepartureDay(made["id"].(string), at)[name] {
				world.report(t, world.ada, "", visits)
			}
			for _, visit := range world.visitsOf(t, world.ada) {
				if visit["place"].(map[string]any)["placeName"] != "Home" {
					continue
				}
				departedAt, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(visit["departedAt"]))
				if want := base.Add(48 * time.Minute); !departedAt.Equal(want) {
					t.Errorf("home ends at %s, not where the first stop began, %s", departedAt.Sub(base), want.Sub(base))
				}
			}
		})
	}
}

// The departure of a visit the server ended sooner is told by when it ended,
// so that a phone that noticed leaving home hours late does not say so now.
func TestALateDepartureIsToldByWhenTheVisitEnded(t *testing.T) {
	world := newLocationWorld(t)
	base := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	made := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 40, longitudeDegrees: -75, placeName: "Home", placeLabel: "home") { id } }`)["SavePlace"].(map[string]any)
	placeId := made["id"].(string)
	world.report(t, world.ada, "", regionVisitInput("region-home", placeId, 40, -75, base.Format(time.RFC3339), ""))
	world.report(t, world.ada, "", visitInput("stop-far", 40.16, -75, base.Add(48*time.Minute).Format(time.RFC3339), base.Add(136*time.Minute).Format(time.RFC3339)))
	lateDeparture := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	world.report(t, world.ada, "", regionVisitInput("region-home", placeId, 40, -75, "", lateDeparture.Format(time.RFC3339)))

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		home, err := tx.GetPlaceVisitByClientKey(world.ada.ID, "region-home")
		if err != nil || home == nil {
			t.Fatalf("GetPlaceVisitByClientKey: %v %s", home, err)
		}
		notices := []visitNotice{{Visit: home, IsArrival: false, HappenedAt: lateDeparture}}
		if err := tellDeparturesWhenTheyEnded(tx, world.ada.ID, notices); err != nil {
			t.Fatalf("tellDeparturesWhenTheyEnded: %s", err)
		}
		if want := base.Add(48 * time.Minute); !notices[0].HappenedAt.Equal(want) {
			t.Errorf("the departure is told at %s, not when the visit ended, %s", notices[0].HappenedAt, want)
		}
	})
}
