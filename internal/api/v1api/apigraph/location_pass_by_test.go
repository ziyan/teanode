package apigraph

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func movingPointInput(latitude, longitude, speedMetersPerSecond float64, measuredAt string) string {
	return fmt.Sprintf(`{latitudeDegrees: %v, longitudeDegrees: %v, accuracyMeters: 5, speedMetersPerSecond: %v, measuredAt: %q}`,
		latitude, longitude, speedMetersPerSecond, measuredAt)
}

func regionVisitInput(key, placeId string, latitude, longitude float64, arrivedAt, departedAt string) string {
	fields := fmt.Sprintf(`clientVisitKey: %q, placeId: %q, latitudeDegrees: %v, longitudeDegrees: %v, accuracyMeters: 0, visitSource: "region"`,
		key, placeId, latitude, longitude)
	if arrivedAt != "" {
		fields += fmt.Sprintf(`, arrivedAt: %q`, arrivedAt)
	}
	if departedAt != "" {
		fields += fmt.Sprintf(`, departedAt: %q`, departedAt)
	}
	return "{" + fields + "}"
}

// Driving through a place's circle is not a visit: the arrival is stored when
// it is reported, and the departure fifteen seconds later, with the person at
// 19 m/s between, takes it away again.
func TestDrivingThroughAPlaceIsNotAVisit(t *testing.T) {
	world := newLocationWorld(t)
	var school *models.Place
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if school, err = tx.CreatePlace(world.ada.ID, &models.Place{PlaceName: "School", LatitudeDegrees: 40, LongitudeDegrees: -75}); err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
	})
	entered := time.Now().Add(-5 * time.Minute).UTC().Truncate(time.Second)
	at := func(seconds int) string {
		return entered.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339)
	}
	visits := func() []any {
		data := world.call(t, world.ada, `query { ListLocationVisits { placeVisits { `+visitFieldsForTest+` } } }`)
		return data["ListLocationVisits"].(map[string]any)["placeVisits"].([]any)
	}

	world.report(t, world.ada, "", regionVisitInput("region-1", school.ID, 40, -75, at(0), ""))
	if found := visits(); len(found) != 1 {
		t.Fatalf("the arrival is a visit until the departure says otherwise: %v", found)
	}
	world.report(t, world.ada, movingPointInput(40, -75, 19, at(8)), regionVisitInput("region-1", school.ID, 40, -75, "", at(15)))
	if found := visits(); len(found) != 0 {
		t.Errorf("a pass-by is not a visit: %v", found)
	}

	// The same, stopping for two minutes to let somebody out, is a visit.
	world.report(t, world.ada, movingPointInput(40, -75, 0, at(60)), regionVisitInput("region-2", school.ID, 40, -75, at(30), at(150)))
	if found := visits(); len(found) != 1 {
		t.Errorf("a stop is a visit: %v", found)
	}
}

// iOS reports the departure of a visit with a position it made more exact,
// which the phone keys differently: it completes the visit that arrived in
// that second, and does not end it at once with a second visit.
func TestADepartureUnderAnotherKeyCompletesTheVisit(t *testing.T) {
	world := newLocationWorld(t)
	arrival := ago(40 * time.Minute)
	departure := ago(35 * time.Minute)
	world.report(t, world.ada, "", visitInput("visit:40.000:-75.000:a1", 40.0, -75.0, arrival, ""))
	world.report(t, world.ada, "", visitInput("visit:40.002:-75.000:a1", 40.002, -75.0, arrival, departure))

	data := world.call(t, world.ada, `query { ListLocationVisits { placeVisits { `+visitFieldsForTest+` } } }`)
	visits := data["ListLocationVisits"].(map[string]any)["placeVisits"].([]any)
	if len(visits) != 1 {
		t.Fatalf("one visit: %v", visits)
	}
	if visit := visits[0].(map[string]any); visit["durationSeconds"] != float64(5*60) {
		t.Errorf("the visit has its departure: %v", visit)
	}
}

// An arrival or a departure at a place's circle waits until the visit has
// lasted long enough not to be a pass-by; one iOS noticed is told at once.
func TestANoticeOfACircleWaitsUntilTheVisitCounts(t *testing.T) {
	arrivedAt := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	departedAt := arrivedAt.Add(time.Minute)
	region := &models.PlaceVisit{VisitSource: models.VisitSourceRegion, ArrivedAt: &arrivedAt, DepartedAt: &departedAt}
	noticed := &models.PlaceVisit{VisitSource: models.VisitSourceVisit, ArrivedAt: &arrivedAt}
	for name, test := range map[string]struct {
		notice   visitNotice
		expected time.Time
	}{
		"circle arrival":   {visitNotice{Visit: region, IsArrival: true, HappenedAt: arrivedAt}, arrivedAt.Add(db.PassByDurationMost)},
		"circle departure": {visitNotice{Visit: region, HappenedAt: departedAt}, arrivedAt.Add(db.PassByDurationMost + time.Second)},
		"noticed arrival":  {visitNotice{Visit: noticed, IsArrival: true, HappenedAt: arrivedAt}, time.Time{}},
	} {
		if dueAt := visitNoticeDueAt(test.notice); !dueAt.Equal(test.expected) {
			t.Errorf("%s: due at %s, expected %s", name, dueAt, test.expected)
		}
	}
}
