package db_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// All the places of these tests are near 10.5, 20.5 on the earth of the
// tests. A degree of latitude is about 111 km, so 0.001 of one is 111 m.
const (
	locationLatitude  = 10.5
	locationLongitude = 20.5
)

var locationDay = time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)

func locationUser(t *testing.T, tx db.Transaction, username string) string {
	t.Helper()
	user, err := tx.CreateUser(&models.User{Username: username})
	if err != nil {
		t.Fatalf("CreateUser(%s): %s", username, err)
	}
	return user.ID
}

func locationPlace(t *testing.T, tx db.Transaction, userId, name string, latitudeDegrees float64) *models.Place {
	t.Helper()
	made, err := tx.CreatePlace(userId, &models.Place{PlaceName: name, LatitudeDegrees: latitudeDegrees, LongitudeDegrees: locationLongitude})
	if err != nil {
		t.Fatalf("CreatePlace(%s): %s", name, err)
	}
	return made
}

func minutesAfterNine(minutes int) *time.Time {
	moment := locationDay.Add(time.Duration(minutes) * time.Minute)
	return &moment
}

func locationVisit(placeId, key string, arrivedMinute, departedMinute int) *models.PlaceVisit {
	visit := &models.PlaceVisit{
		PlaceID: placeId, ClientVisitKey: key, VisitSource: models.VisitSourceVisit,
		LatitudeDegrees: locationLatitude, LongitudeDegrees: locationLongitude, AccuracyMeters: 20,
	}
	if arrivedMinute >= 0 {
		visit.ArrivedAt = minutesAfterNine(arrivedMinute)
	}
	if departedMinute >= 0 {
		visit.DepartedAt = minutesAfterNine(departedMinute)
	}
	return visit
}

// A position reported twice is stored once, the newest is found by when it was
// measured and not by when it arrived, and a position that cannot be on the
// earth is refused with the rest of its batch.
func TestPositionsAreStoredOnceAndTheLatestIsFound(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-points")
		otherId := locationUser(t, tx, "location-points-other")

		if point, err := tx.GetLatestLocationPoint(userId); point != nil || err != nil {
			t.Fatalf("a person with no positions has %v, %v", point, err)
		}
		altitude := 12.5
		batch := []*models.LocationPoint{
			{LatitudeDegrees: 10.5, LongitudeDegrees: 20.5, AccuracyMeters: 5, MeasuredAt: locationDay, LocationSource: models.LocationSourcePhone, AltitudeMeters: &altitude},
			{LatitudeDegrees: 10.6, LongitudeDegrees: 20.6, AccuracyMeters: 8, MeasuredAt: locationDay.Add(time.Minute), LocationSource: models.LocationSourcePhone},
			// The same instant from the browser is another position.
			{LatitudeDegrees: 10.7, LongitudeDegrees: 20.7, AccuracyMeters: 30, MeasuredAt: locationDay.Add(time.Minute), LocationSource: models.LocationSourceBrowser},
			// The same position twice in one batch.
			{LatitudeDegrees: 10.5, LongitudeDegrees: 20.5, AccuracyMeters: 5, MeasuredAt: locationDay, LocationSource: models.LocationSourcePhone},
		}
		count, err := tx.InsertLocationPoints(userId, batch)
		if err != nil || count != 3 {
			t.Fatalf("a batch of four with a repeat adds %d, %v", count, err)
		}
		count, err = tx.InsertLocationPoints(userId, batch)
		if err != nil || count != 0 {
			t.Fatalf("sending it again adds %d, %v", count, err)
		}
		// Another person's position at the same instant is theirs.
		if count, err = tx.InsertLocationPoints(otherId, batch[:1]); err != nil || count != 1 {
			t.Fatalf("another person adds %d, %v", count, err)
		}

		latest, err := tx.GetLatestLocationPoint(userId)
		if err != nil || latest == nil || latest.MeasuredAt.Before(locationDay.Add(time.Minute)) || latest.UserID != userId || latest.ID == "" || latest.ReceivedAt.IsZero() {
			t.Fatalf("the latest is %+v, %v", latest, err)
		}
		earliest, _ := tx.InsertLocationPoints(userId, []*models.LocationPoint{{LatitudeDegrees: 1, LongitudeDegrees: 2, AccuracyMeters: 1, MeasuredAt: locationDay.Add(-24 * time.Hour), LocationSource: models.LocationSourcePhone}})
		if earliest != 1 {
			t.Fatal("an older position was not stored")
		}
		if again, _ := tx.GetLatestLocationPoint(userId); again.ID != latest.ID {
			t.Errorf("a position measured earlier but received later became the latest")
		}

		for name, bad := range map[string]*models.LocationPoint{
			"latitude":  {LatitudeDegrees: 91, LongitudeDegrees: 0, AccuracyMeters: 5, MeasuredAt: locationDay, LocationSource: models.LocationSourcePhone},
			"longitude": {LatitudeDegrees: 0, LongitudeDegrees: -181, AccuracyMeters: 5, MeasuredAt: locationDay, LocationSource: models.LocationSourcePhone},
			"accuracy":  {LatitudeDegrees: 0, LongitudeDegrees: 0, AccuracyMeters: -1, MeasuredAt: locationDay, LocationSource: models.LocationSourcePhone},
			"source":    {LatitudeDegrees: 0, LongitudeDegrees: 0, AccuracyMeters: 1, MeasuredAt: locationDay, LocationSource: "watch"},
			"time":      {LatitudeDegrees: 0, LongitudeDegrees: 0, AccuracyMeters: 1, LocationSource: models.LocationSourcePhone},
		} {
			if _, err := tx.InsertLocationPoints(userId, []*models.LocationPoint{bad}); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}

// A point is in the nearest of the person's places whose radius holds it, and
// in nobody else's.
func TestAPointIsInTheNearestPlaceThatHoldsIt(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-find")
		otherId := locationUser(t, tx, "location-find-other")

		wide, err := tx.CreatePlace(userId, &models.Place{PlaceName: "Wide Field", LatitudeDegrees: 10.5, LongitudeDegrees: 20.5, RadiusMeters: 500})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
		// 55 m north of the point: with the default radius of 75 m it holds the
		// point, and is nearer than the wide field's centre.
		near := locationPlace(t, tx, userId, "Near Cafe", 10.5005)
		// 200 m away, with the default radius it does not.
		locationPlace(t, tx, userId, "Far Cafe", 10.5018)
		locationPlace(t, tx, otherId, "Someone Else's Cafe", 10.5)

		found, err := tx.FindPlaceAt(userId, 10.5, 20.5)
		if err != nil || found == nil || found.ID != wide.ID {
			t.Fatalf("at the centre of the wide field: %+v, %v", found, err)
		}
		found, err = tx.FindPlaceAt(userId, 10.5004, 20.5)
		if err != nil || found == nil || found.ID != near.ID {
			t.Fatalf("beside the near cafe: %+v, %v", found, err)
		}
		// 600 m from the wide field's centre: outside every radius.
		if found, err = tx.FindPlaceAt(userId, 10.5054, 20.5); found != nil || err != nil {
			t.Fatalf("outside every place: %+v, %v", found, err)
		}
		// 450 m is inside the wide field and outside the cafes.
		if found, err = tx.FindPlaceAt(userId, 10.504, 20.5); err != nil || found == nil || found.ID != wide.ID {
			t.Fatalf("inside only the wide field: %+v, %v", found, err)
		}
		if found, err = tx.FindPlaceAt(otherId, 10.4, 20.4); found != nil || err != nil {
			t.Fatalf("a person with a place elsewhere: %+v, %v", found, err)
		}
		if found, err = tx.FindPlaceAt(userId, 95, 20.5); found != nil || err != nil {
			t.Fatalf("a point off the earth: %+v, %v", found, err)
		}

		// Across the date line a place at 179.9998 holds a point at -179.9998 (44 m away).
		across, err := tx.CreatePlace(userId, &models.Place{PlaceName: "Date Line", LatitudeDegrees: 0, LongitudeDegrees: 179.9998})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
		if found, err = tx.FindPlaceAt(userId, 0, -179.9998); err != nil || found == nil || found.ID != across.ID {
			t.Fatalf("across the date line: %+v, %v", found, err)
		}
	})
}

func TestAPlaceTakesItsDefaultsAndRefusesWhatCannotBe(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-create")

		made, err := tx.CreatePlace(userId, &models.Place{
			PlaceName: " Example Park ", LatitudeDegrees: 10.5, LongitudeDegrees: 20.5,
			OSMType: models.OSMTypeWay, OSMID: 4242, OSMName: "Example Park", OSMCategory: "leisure=park", AddressText: "Example Park, Sample Street",
		})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
		if made.ID == "" || made.UserID != userId || made.PlaceName != "Example Park" || made.RadiusMeters != models.PlaceRadiusDefaultMeters ||
			made.IsNamedByPerson || made.PlaceLabel != "" || made.OSMID != 4242 || made.OSMType != "way" || made.NamedAt != nil || made.CreatedAt.IsZero() {
			t.Errorf("a new place is %+v", made)
		}
		unlocated, err := tx.CreatePlace(userId, &models.Place{LatitudeDegrees: 10.5, LongitudeDegrees: 20.5})
		if err != nil || unlocated.OSMID != 0 || unlocated.OSMType != "" {
			t.Errorf("a place with no OpenStreetMap object: %+v, %v", unlocated, err)
		}

		for name, bad := range map[string]*models.Place{
			"off the earth": {LatitudeDegrees: 100, LongitudeDegrees: 20},
			"small radius":  {LatitudeDegrees: 10, LongitudeDegrees: 20, RadiusMeters: 24},
			"large radius":  {LatitudeDegrees: 10, LongitudeDegrees: 20, RadiusMeters: 2001},
			"label":         {LatitudeDegrees: 10, LongitudeDegrees: 20, PlaceLabel: "office"},
			"osm type":      {LatitudeDegrees: 10, LongitudeDegrees: 20, OSMType: "area"},
			"long name":     {LatitudeDegrees: 10, LongitudeDegrees: 20, PlaceName: strings.Repeat("x", 257)},
		} {
			if _, err := tx.CreatePlace(userId, bad); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}

// Naming a place makes the name the person's, an empty name goes back to the
// map's, a label can be put on and taken off, and nobody can change a place
// that is not theirs.
func TestAPersonNamesLabelsAndMovesTheirPlaces(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-save")
		otherId := locationUser(t, tx, "location-save-other")
		made, err := tx.CreatePlace(userId, &models.Place{PlaceName: "Example Park", OSMName: "Example Park", LatitudeDegrees: 10.5, LongitudeDegrees: 20.5})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}

		name, label := "Sample Playground", models.PlaceLabelFamily
		saved, err := tx.SavePlace(userId, made.ID, db.PlaceChanges{PlaceName: &name, PlaceLabel: &label})
		if err != nil || saved.PlaceName != "Sample Playground" || !saved.IsNamedByPerson || saved.PlaceLabel != models.PlaceLabelFamily || saved.OSMName != "Example Park" {
			t.Fatalf("named and labeled: %+v, %v", saved, err)
		}

		// A label can change on its own, and the name stays.
		label = models.PlaceLabelGym
		if saved, err = tx.SavePlace(userId, made.ID, db.PlaceChanges{PlaceLabel: &label}); err != nil || saved.PlaceLabel != models.PlaceLabelGym || saved.PlaceName != "Sample Playground" || !saved.IsNamedByPerson {
			t.Fatalf("relabeled: %+v, %v", saved, err)
		}
		label = ""
		if saved, err = tx.SavePlace(userId, made.ID, db.PlaceChanges{PlaceLabel: &label}); err != nil || saved.PlaceLabel != "" {
			t.Fatalf("label taken off: %+v, %v", saved, err)
		}

		latitude, longitude, radius := 10.6, 20.6, 120.0
		if saved, err = tx.SavePlace(userId, made.ID, db.PlaceChanges{LatitudeDegrees: &latitude, LongitudeDegrees: &longitude, RadiusMeters: &radius}); err != nil ||
			saved.LatitudeDegrees != 10.6 || saved.LongitudeDegrees != 20.6 || saved.RadiusMeters != 120 {
			t.Fatalf("moved and widened: %+v, %v", saved, err)
		}

		// An empty name gives the map's back and clears the flag.
		empty := "  "
		if saved, err = tx.SavePlace(userId, made.ID, db.PlaceChanges{PlaceName: &empty}); err != nil || saved.PlaceName != "Example Park" || saved.IsNamedByPerson {
			t.Fatalf("name taken away: %+v, %v", saved, err)
		}
		// On a place the person never named it changes nothing.
		if saved, err = tx.SavePlace(userId, made.ID, db.PlaceChanges{PlaceName: &empty}); err != nil || saved.PlaceName != "Example Park" || saved.IsNamedByPerson {
			t.Fatalf("empty name on an unnamed place: %+v, %v", saved, err)
		}

		// A place the map never named has nothing to go back to: it has no
		// name, and waits to be named.
		bare := locationPlace(t, tx, userId, "", 11)
		if err = tx.SetPlaceNamed(bare.ID, nil, "", locationDay); err != nil {
			t.Fatalf("SetPlaceNamed: %s", err)
		}
		own := "My Spot"
		if saved, err = tx.SavePlace(userId, bare.ID, db.PlaceChanges{PlaceName: &own}); err != nil || saved.PlaceName != "My Spot" {
			t.Fatalf("named: %+v, %v", saved, err)
		}
		if saved, err = tx.SavePlace(userId, bare.ID, db.PlaceChanges{PlaceName: &empty}); err != nil || saved.PlaceName != "" || saved.IsNamedByPerson || saved.NamedAt != nil {
			t.Fatalf("name taken away from a place the map never named: %+v, %v", saved, err)
		}

		bad := models.PlaceLabel("office")
		tooSmall, tooLarge := 24.0, 2001.0
		for name, changes := range map[string]db.PlaceChanges{
			"label":        {PlaceLabel: &bad},
			"small radius": {RadiusMeters: &tooSmall},
			"large radius": {RadiusMeters: &tooLarge},
			"one of two":   {LatitudeDegrees: &latitude},
			"off earth":    {LatitudeDegrees: &tooLarge, LongitudeDegrees: &longitude},
		} {
			if _, err := tx.SavePlace(userId, made.ID, changes); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("%s: %v", name, err)
			}
		}

		if _, err = tx.SavePlace(otherId, made.ID, db.PlaceChanges{PlaceName: &own}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("another person's change: %v", err)
		}
		if _, err = tx.SavePlace(userId, "missing", db.PlaceChanges{PlaceName: &own}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a place that is not there: %v", err)
		}
		if _, err = tx.GetPlace(otherId, made.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("another person's place: %v", err)
		}
		if err = tx.ForgetPlace(otherId, made.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("another person forgetting it: %v", err)
		}
		if kept, err := tx.GetPlace(userId, made.ID); err != nil || kept.ID != made.ID {
			t.Errorf("the place is gone: %v", err)
		}
	})
}

// Places list labeled first, then the ones the person named, then the rest,
// each by last visit, with the number of visits.
func TestPlacesListInTheOrderAPersonCaresAbout(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-list")
		otherId := locationUser(t, tx, "location-list-other")

		label := func(placeId string, value models.PlaceLabel) {
			if _, err := tx.SavePlace(userId, placeId, db.PlaceChanges{PlaceLabel: &value}); err != nil {
				t.Fatalf("label: %s", err)
			}
		}
		name := func(placeId, value string) {
			if _, err := tx.SavePlace(userId, placeId, db.PlaceChanges{PlaceName: &value}); err != nil {
				t.Fatalf("name: %s", err)
			}
		}
		visit := func(place *models.Place, key string, arrived int) {
			if _, err := tx.PutPlaceVisit(userId, locationVisit(place.ID, key, arrived, arrived+30)); err != nil {
				t.Fatalf("visit: %s", err)
			}
		}
		oldHome := locationPlace(t, tx, userId, "Maple Street", 10.1)
		label(oldHome.ID, models.PlaceLabelHome)
		visit(oldHome, "home-1", 0)
		recentWork := locationPlace(t, tx, userId, "Example Works", 10.2)
		label(recentWork.ID, models.PlaceLabelWork)
		visit(recentWork, "work-1", 600)
		visit(recentWork, "work-2", 1500)
		named := locationPlace(t, tx, userId, "Cedar Lane", 10.3)
		name(named.ID, "Grandma's")
		visit(named, "named-1", 300)
		stranger := locationPlace(t, tx, userId, "Birch Cafe", 10.4)
		visit(stranger, "stranger-1", 900)
		neverVisited := locationPlace(t, tx, userId, "Elm Park", 10.6)
		locationPlace(t, tx, otherId, "Someone Else's", 10.5)

		places, next, err := tx.ListPlaces(userId, db.PlaceFilter{})
		if err != nil || next != 0 {
			t.Fatalf("ListPlaces: %v, next %d", err, next)
		}
		names := []string{}
		for _, place := range places {
			names = append(names, place.PlaceName)
		}
		// Labeled (the work place was visited last), then named, then the
		// rest with the visited before the never visited.
		if strings.Join(names, ",") != "Example Works,Maple Street,Grandma's,Birch Cafe,Elm Park" {
			t.Fatalf("the order is %v", names)
		}
		if places[0].VisitCount != 2 || places[0].LastVisitAt == nil || !places[0].LastVisitAt.Equal(locationDay.Add(1530*time.Minute)) {
			t.Errorf("the work place has %d visits, last %v", places[0].VisitCount, places[0].LastVisitAt)
		}
		if places[4].ID != neverVisited.ID || places[4].VisitCount != 0 || places[4].LastVisitAt != nil {
			t.Errorf("the place never visited: %+v", places[4])
		}

		// Pages.
		first, next, err := tx.ListPlaces(userId, db.PlaceFilter{LimitCount: 2})
		if err != nil || len(first) != 2 || next != 2 {
			t.Fatalf("first page: %d places, next %d, %v", len(first), next, err)
		}
		last, next, err := tx.ListPlaces(userId, db.PlaceFilter{LimitCount: 2, Offset: 4})
		if err != nil || len(last) != 1 || next != 0 || last[0].ID != neverVisited.ID {
			t.Fatalf("last page: %d places, next %d, %v", len(last), next, err)
		}
		exact, next, err := tx.ListPlaces(userId, db.PlaceFilter{LimitCount: 5})
		if err != nil || len(exact) != 5 || next != 0 {
			t.Fatalf("a page that holds everything: %d places, next %d, %v", len(exact), next, err)
		}

		// Filters.
		homes, _, err := tx.ListPlaces(userId, db.PlaceFilter{PlaceLabel: models.PlaceLabelHome})
		if err != nil || len(homes) != 1 || homes[0].ID != oldHome.ID {
			t.Errorf("homes: %v, %v", homes, err)
		}
		matches, _, err := tx.ListPlaces(userId, db.PlaceFilter{Query: "  GRANDMA"})
		if err != nil || len(matches) != 1 || matches[0].ID != named.ID {
			t.Errorf("a search by name: %v, %v", matches, err)
		}
		if matches, _, err = tx.ListPlaces(userId, db.PlaceFilter{Query: "100%_"}); err != nil || len(matches) != 0 {
			t.Errorf("a search for wildcards: %v, %v", matches, err)
		}
		if _, _, err = tx.ListPlaces(userId, db.PlaceFilter{PlaceLabel: "office"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an unknown label: %v", err)
		}
		if _, _, err = tx.ListPlaces(userId, db.PlaceFilter{Offset: -1}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a negative offset: %v", err)
		}
	})
}

// The search reads the OpenStreetMap name and the address too.
func TestPlacesAreFoundByTheirMapNameAndAddress(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-search")
		made, err := tx.CreatePlace(userId, &models.Place{PlaceName: "Grandma's", IsNamedByPerson: true, OSMName: "Willow Cottage", AddressText: "4, Orchard Road, Exampleton", LatitudeDegrees: 10, LongitudeDegrees: 20})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
		for _, query := range []string{"willow", "orchard road", "grandma"} {
			found, _, err := tx.ListPlaces(userId, db.PlaceFilter{Query: query})
			if err != nil || len(found) != 1 || found[0].ID != made.ID {
				t.Errorf("%q finds %v, %v", query, found, err)
			}
		}
	})
}

// Reporting a stay again completes it, and stays a few minutes apart at one
// place are one stay.
func TestStaysAreCompletedByALaterReportAndJoinedWhenCloseTogether(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-visit")
		otherId := locationUser(t, tx, "location-visit-other")
		cafe := locationPlace(t, tx, userId, "Birch Cafe", 10.5)
		library := locationPlace(t, tx, userId, "Elm Library", 10.6)

		// An arrival, then the same stay's departure.
		arrival := locationVisit(cafe.ID, "visit-1", 0, -1)
		isNew, err := tx.PutPlaceVisit(userId, arrival)
		if err != nil || !isNew || arrival.ID == "" || arrival.DepartedAt != nil {
			t.Fatalf("the arrival: new %v, %+v, %v", isNew, arrival, err)
		}
		open, err := tx.GetOpenPlaceVisit(userId)
		if err != nil || open == nil || open.ID != arrival.ID || open.Place == nil || open.Place.ID != cafe.ID {
			t.Fatalf("the stay in progress: %+v, %v", open, err)
		}
		departure := locationVisit(cafe.ID, "visit-1", -1, 40)
		isNew, err = tx.PutPlaceVisit(userId, departure)
		if err != nil || isNew || departure.ID != arrival.ID || departure.ArrivedAt == nil || !departure.ArrivedAt.Equal(*arrival.ArrivedAt) || departure.DepartedAt == nil || !departure.DepartedAt.Equal(*minutesAfterNine(40)) {
			t.Fatalf("the departure: new %v, %+v, %v", isNew, departure, err)
		}
		if open, err = tx.GetOpenPlaceVisit(userId); open != nil || err != nil {
			t.Fatalf("nothing is in progress: %+v, %v", open, err)
		}
		// Sent again, it changes nothing.
		again := locationVisit(cafe.ID, "visit-1", 0, 40)
		if isNew, err = tx.PutPlaceVisit(userId, again); err != nil || isNew || again.ID != arrival.ID {
			t.Fatalf("the same stay twice: new %v, %+v, %v", isNew, again, err)
		}

		// A departure iOS reported without an arrival takes the arrival of a
		// later report.
		late := locationVisit(library.ID, "visit-late", -1, 200)
		if isNew, err = tx.PutPlaceVisit(userId, late); err != nil || !isNew || late.ArrivedAt != nil {
			t.Fatalf("a departure alone: new %v, %+v, %v", isNew, late, err)
		}
		completed := locationVisit(library.ID, "visit-late", 150, -1)
		if _, err = tx.PutPlaceVisit(userId, completed); err != nil || completed.ArrivedAt == nil || !completed.ArrivedAt.Equal(*minutesAfterNine(150)) {
			t.Fatalf("the arrival that was missing: %+v, %v", completed, err)
		}

		// Five minutes after leaving the cafe, a stay at the cafe starts: one stay.
		joinedAfter := locationVisit(cafe.ID, "visit-2", 45, 70)
		if isNew, err = tx.PutPlaceVisit(userId, joinedAfter); err != nil || !isNew {
			t.Fatalf("the second stay: new %v, %v", isNew, err)
		}
		if joinedAfter.ID != arrival.ID || !joinedAfter.ArrivedAt.Equal(*minutesAfterNine(0)) || !joinedAfter.DepartedAt.Equal(*minutesAfterNine(70)) {
			t.Fatalf("the two stays are not one: %+v", joinedAfter)
		}
		visits, _, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{PlaceID: cafe.ID})
		if err != nil || len(visits) != 1 {
			t.Fatalf("the cafe has %d stays, %v", len(visits), err)
		}

		// Eleven minutes after leaving it is another stay.
		if _, err = tx.PutPlaceVisit(userId, locationVisit(cafe.ID, "visit-3", 81, 90)); err != nil {
			t.Fatalf("the third stay: %s", err)
		}
		if visits, _, err = tx.ListPlaceVisits(userId, db.PlaceVisitFilter{PlaceID: cafe.ID}); err != nil || len(visits) != 2 {
			t.Fatalf("the cafe has %d stays, %v", len(visits), err)
		}

		// A stay reported late, that sits between two, joins all three.
		if _, err = tx.PutPlaceVisit(userId, locationVisit(cafe.ID, "visit-4", 74, 78)); err != nil {
			t.Fatalf("the fourth stay: %s", err)
		}
		if visits, _, err = tx.ListPlaceVisits(userId, db.PlaceVisitFilter{PlaceID: cafe.ID}); err != nil || len(visits) != 1 ||
			!visits[0].ArrivedAt.Equal(*minutesAfterNine(0)) || !visits[0].DepartedAt.Equal(*minutesAfterNine(90)) {
			t.Fatalf("the stays between join them all: %+v, %v", visits, err)
		}

		// A different place is not joined, and nor is another person's stay at
		// a place of their own with the same times.
		if visits, _, err = tx.ListPlaceVisits(userId, db.PlaceVisitFilter{PlaceID: library.ID}); err != nil || len(visits) != 1 {
			t.Fatalf("the library has %d stays, %v", len(visits), err)
		}
		theirs := locationPlace(t, tx, otherId, "Their Cafe", 10.5)
		if _, err = tx.PutPlaceVisit(otherId, locationVisit(theirs.ID, "visit-1", 0, 40)); err != nil {
			t.Errorf("the same key for another person: %s", err)
		}

		// A stay with no key is given one.
		keyless := locationVisit(library.ID, "", 600, 610)
		keyless.VisitSource = models.VisitSourceCheckIn
		if isNew, err = tx.PutPlaceVisit(userId, keyless); err != nil || !isNew || keyless.ClientVisitKey == "" {
			t.Fatalf("a check in: new %v, %+v, %v", isNew, keyless, err)
		}
		another := locationVisit(library.ID, "", 800, 810)
		another.VisitSource = models.VisitSourceCheckIn
		if _, err = tx.PutPlaceVisit(userId, another); err != nil || another.ClientVisitKey == keyless.ClientVisitKey {
			t.Fatalf("a second check in shares the key: %+v, %v", another, err)
		}

		// What cannot be stored.
		if _, err = tx.PutPlaceVisit(userId, locationVisit(theirs.ID, "stolen", 0, 10)); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a stay at another person's place: %v", err)
		}
		if _, err = tx.PutPlaceVisit(userId, locationVisit(cafe.ID, "backwards", 20, 10)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a stay that ends before it starts: %v", err)
		}
		if _, err = tx.PutPlaceVisit(userId, locationVisit(cafe.ID, "timeless", -1, -1)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a stay with no time: %v", err)
		}
		unsourced := locationVisit(cafe.ID, "unsourced", 0, 10)
		unsourced.VisitSource = "guess"
		if _, err = tx.PutPlaceVisit(userId, unsourced); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a stay from nowhere: %v", err)
		}
	})
}

// A new arrival at a place the person is still recorded at is the same stay,
// and a departure ends the stay in progress, never before it started.
func TestTheStayInProgressIsEndedOnce(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-end")
		cafe := locationPlace(t, tx, userId, "Birch Cafe", 10.5)
		library := locationPlace(t, tx, userId, "Elm Library", 10.6)

		if ended, err := tx.EndOpenPlaceVisit(userId, locationDay); ended != nil || err != nil {
			t.Fatalf("ending nothing: %+v, %v", ended, err)
		}
		if _, err := tx.PutPlaceVisit(userId, locationVisit(cafe.ID, "old", 0, 30)); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
		if _, err := tx.PutPlaceVisit(userId, locationVisit(library.ID, "current", 100, -1)); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
		ended, err := tx.EndOpenPlaceVisit(userId, *minutesAfterNine(130))
		if err != nil || ended == nil || ended.Place == nil || ended.Place.ID != library.ID || ended.DepartedAt == nil || !ended.DepartedAt.Equal(*minutesAfterNine(130)) {
			t.Fatalf("ending the stay: %+v, %v", ended, err)
		}
		if open, _ := tx.GetOpenPlaceVisit(userId); open != nil {
			t.Fatalf("a stay is still in progress: %+v", open)
		}

		// An end before the arrival is the arrival.
		if _, err = tx.PutPlaceVisit(userId, locationVisit(cafe.ID, "next", 300, -1)); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
		if ended, err = tx.EndOpenPlaceVisit(userId, *minutesAfterNine(200)); err != nil || !ended.DepartedAt.Equal(*minutesAfterNine(300)) {
			t.Fatalf("an end before the start: %+v, %v", ended, err)
		}
	})
}

func TestStaysListNewestFirstWithTheirPlaces(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-visits")
		otherId := locationUser(t, tx, "location-visits-other")
		cafe := locationPlace(t, tx, userId, "Birch Cafe", 10.5)
		office := locationPlace(t, tx, userId, "Example Works", 10.6)
		work := models.PlaceLabelWork
		if _, err := tx.SavePlace(userId, office.ID, db.PlaceChanges{PlaceLabel: &work}); err != nil {
			t.Fatalf("SavePlace: %s", err)
		}
		for _, visit := range []*models.PlaceVisit{
			locationVisit(cafe.ID, "a", 0, 30),
			locationVisit(office.ID, "b", 120, 480),
			locationVisit(cafe.ID, "c", 600, 630),
			locationVisit(office.ID, "d", 1560, -1),
		} {
			if _, err := tx.PutPlaceVisit(userId, visit); err != nil {
				t.Fatalf("PutPlaceVisit: %s", err)
			}
		}
		theirs := locationPlace(t, tx, otherId, "Their Cafe", 10.5)
		if _, err := tx.PutPlaceVisit(otherId, locationVisit(theirs.ID, "a", 0, 30)); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}

		keys := func(filter db.PlaceVisitFilter) string {
			visits, _, err := tx.ListPlaceVisits(userId, filter)
			if err != nil {
				t.Fatalf("ListPlaceVisits: %s", err)
			}
			keys := []string{}
			for _, visit := range visits {
				if visit.Place == nil || visit.Place.ID != visit.PlaceID {
					t.Errorf("a stay without its place: %+v", visit)
				}
				keys = append(keys, visit.ClientVisitKey)
			}
			return strings.Join(keys, "")
		}
		if got := keys(db.PlaceVisitFilter{}); got != "dcba" {
			t.Errorf("newest first: %s", got)
		}
		if got := keys(db.PlaceVisitFilter{PlaceID: cafe.ID}); got != "ca" {
			t.Errorf("one place: %s", got)
		}
		if got := keys(db.PlaceVisitFilter{PlaceLabel: models.PlaceLabelWork}); got != "db" {
			t.Errorf("one label: %s", got)
		}
		// 09:00 plus 100 minutes to plus 500 minutes: the office stay
		// overlaps the range at both ends of it, the first and last do not.
		if got := keys(db.PlaceVisitFilter{From: minutesAfterNine(100), Until: minutesAfterNine(500)}); got != "b" {
			t.Errorf("overlapping a range: %s", got)
		}
		// A stay that ended exactly at From still overlaps it; one that began
		// exactly at Until does not.
		if got := keys(db.PlaceVisitFilter{From: minutesAfterNine(30), Until: minutesAfterNine(120)}); got != "a" {
			t.Errorf("touching a range: %s", got)
		}
		// The stay in progress reaches any later range.
		if got := keys(db.PlaceVisitFilter{From: minutesAfterNine(5000)}); got != "d" {
			t.Errorf("the stay in progress: %s", got)
		}

		first, next, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{LimitCount: 3})
		if err != nil || len(first) != 3 || next != 3 {
			t.Fatalf("first page: %d stays, next %d, %v", len(first), next, err)
		}
		last, next, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{LimitCount: 3, Offset: 3})
		if err != nil || len(last) != 1 || next != 0 || last[0].ClientVisitKey != "a" {
			t.Fatalf("last page: %d stays, next %d, %v", len(last), next, err)
		}

		// Forgetting a place takes its stays with it.
		if err := tx.ForgetPlace(userId, cafe.ID); err != nil {
			t.Fatalf("ForgetPlace: %s", err)
		}
		if got := keys(db.PlaceVisitFilter{}); got != "db" {
			t.Errorf("after forgetting the cafe: %s", got)
		}
		if got := keys(db.PlaceVisitFilter{PlaceID: cafe.ID}); got != "" {
			t.Errorf("the forgotten place still has stays: %s", got)
		}
	})
}

// Erasing a range takes the positions and stays that began in it, then the
// places left with nothing, except the ones the person made their own.
func TestErasingLocationHistoryKeepsWhatThePersonNamed(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-erase")
		otherId := locationUser(t, tx, "location-erase-other")
		cafe := locationPlace(t, tx, userId, "Birch Cafe", 10.5)
		park := locationPlace(t, tx, userId, "Example Park", 10.6)
		home := locationPlace(t, tx, userId, "Maple Street", 10.7)
		grandma := locationPlace(t, tx, userId, "Cedar Lane", 10.8)
		homeLabel, grandmaName := models.PlaceLabelHome, "Grandma's"
		if _, err := tx.SavePlace(userId, home.ID, db.PlaceChanges{PlaceLabel: &homeLabel}); err != nil {
			t.Fatalf("SavePlace: %s", err)
		}
		if _, err := tx.SavePlace(userId, grandma.ID, db.PlaceChanges{PlaceName: &grandmaName}); err != nil {
			t.Fatalf("SavePlace: %s", err)
		}
		day := 24 * 60
		for key, visit := range map[string]*models.PlaceVisit{
			"cafe-1":    locationVisit(cafe.ID, "cafe-1", 0, 30),
			"park-1":    locationVisit(park.ID, "park-1", day, day+30),
			"home-1":    locationVisit(home.ID, "home-1", 2*day, 2*day+30),
			"grandma-1": locationVisit(grandma.ID, "grandma-1", 2*day+100, 2*day+130),
		} {
			if _, err := tx.PutPlaceVisit(userId, visit); err != nil {
				t.Fatalf("%s: %s", key, err)
			}
		}
		theirs := locationPlace(t, tx, otherId, "Their Cafe", 10.5)
		if _, err := tx.PutPlaceVisit(otherId, locationVisit(theirs.ID, "theirs", 0, 30)); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
		points := []*models.LocationPoint{}
		for index := 0; index < 6; index++ {
			points = append(points, &models.LocationPoint{LatitudeDegrees: 10.5, LongitudeDegrees: 20.5, AccuracyMeters: 5, MeasuredAt: locationDay.Add(time.Duration(index) * 12 * time.Hour), LocationSource: models.LocationSourcePhone})
		}
		if _, err := tx.InsertLocationPoints(userId, points); err != nil {
			t.Fatalf("InsertLocationPoints: %s", err)
		}
		if _, err := tx.InsertLocationPoints(otherId, points[:1]); err != nil {
			t.Fatalf("InsertLocationPoints: %s", err)
		}

		// The first day: three of six positions (midnight is not in it, nine
		// o'clock is), the cafe stay, and the cafe, which nothing else holds.
		from, until := locationDay.Add(-time.Hour), locationDay.Add(23*time.Hour)
		pointCount, visitCount, placeCount, err := tx.ForgetLocationHistory(userId, &from, &until)
		if err != nil || pointCount != 2 || visitCount != 1 || placeCount != 1 {
			t.Fatalf("the first day: %d points, %d stays, %d places, %v", pointCount, visitCount, placeCount, err)
		}
		// The park has a stay left; the labeled and the named place are kept
		// whatever happens to their stays.
		pointCount, visitCount, placeCount, err = tx.ForgetLocationHistory(userId, nil, nil)
		if err != nil || pointCount != 4 || visitCount != 3 || placeCount != 1 {
			t.Fatalf("everything: %d points, %d stays, %d places, %v", pointCount, visitCount, placeCount, err)
		}
		places, _, _ := tx.ListPlaces(userId, db.PlaceFilter{})
		names := []string{}
		for _, place := range places {
			names = append(names, place.PlaceName)
		}
		if strings.Join(names, ",") != "Maple Street,Grandma's" {
			t.Errorf("the places left are %v", names)
		}
		if latest, _ := tx.GetLatestLocationPoint(userId); latest != nil {
			t.Errorf("a position is left: %+v", latest)
		}
		// Nothing of the other person's was touched.
		if latest, _ := tx.GetLatestLocationPoint(otherId); latest == nil {
			t.Error("another person's position went")
		}
		if visits, _, _ := tx.ListPlaceVisits(otherId, db.PlaceVisitFilter{}); len(visits) != 1 {
			t.Errorf("another person has %d stays", len(visits))
		}
		if _, _, _, err = tx.ForgetLocationHistory(userId, &until, &from); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a range that ends before it starts: %v", err)
		}
	})
}

// Places wait for a name in turn, one instance at a time, and a provider's
// answer never replaces a name the person gave.
func TestPlacesAreClaimedForNamingOnceAndNamedByTheProvider(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	userId := dbtest.CreateUser(t, database, "location-claim")
	var firstId, secondId, thirdId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		first := locationPlace(t, tx, userId, "Exampleton", 10.5)
		time.Sleep(2 * time.Millisecond)
		second := locationPlace(t, tx, userId, "Exampleton", 10.6)
		time.Sleep(2 * time.Millisecond)
		third := locationPlace(t, tx, userId, "Exampleton", 10.7)
		firstId, secondId, thirdId = first.ID, second.ID, third.ID
	})

	now := time.Now()
	var claimed []*models.Place
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if claimed, err = tx.ClaimPlacesToName(2, now); err != nil {
			t.Fatalf("ClaimPlacesToName: %s", err)
		}
		if len(claimed) != 2 || claimed[0].ID != firstId || claimed[1].ID != secondId || claimed[0].NamingClaimedAt == nil {
			t.Fatalf("the first claim: %+v", claimed)
		}
	})

	// Five minutes later the two are still claimed; only the third is free.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		again, err := tx.ClaimPlacesToName(5, now.Add(5*time.Minute))
		if err != nil || len(again) != 1 || again[0].ID != thirdId {
			t.Fatalf("the second claim: %+v, %v", again, err)
		}
	})

	// Eleven minutes after the first claim it is old, and the first two come
	// back; the third (claimed five minutes in) is not.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		again, err := tx.ClaimPlacesToName(5, now.Add(11*time.Minute))
		if err != nil || len(again) != 2 || again[0].ID != firstId || again[1].ID != secondId {
			t.Fatalf("the third claim: %+v, %v", again, err)
		}
		if none, err := tx.ClaimPlacesToName(0, now); none != nil || err != nil {
			t.Fatalf("a claim of none: %+v, %v", none, err)
		}
	})

	// Two instances at once get different places.
	freshId := dbtest.CreateUser(t, database, "location-claim-fresh")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for index := 0; index < 4; index++ {
			locationPlace(t, tx, freshId, "Fresh", 11+float64(index)/10)
		}
	})
	dbtest.Exec(t, database, `UPDATE "place" SET "named_at" = now() WHERE "user_id" = '`+userId+`'`)
	firstHolder := make(chan []*models.Place)
	release2 := make(chan struct{})
	go func() {
		_ = database.Transaction(func(tx db.Transaction) error {
			places, err := tx.ClaimPlacesToName(2, time.Now())
			if err != nil {
				t.Errorf("first holder: %s", err)
			}
			firstHolder <- places
			<-release2
			return nil
		})
	}()
	firstPlaces := <-firstHolder
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		secondPlaces, err := tx.ClaimPlacesToName(2, time.Now())
		if err != nil || len(secondPlaces) != 2 {
			t.Fatalf("the second instance: %+v, %v", secondPlaces, err)
		}
		for _, place := range secondPlaces {
			for _, other := range firstPlaces {
				if place.ID == other.ID {
					t.Errorf("both instances took %s", place.ID)
				}
			}
		}
	})
	close(release2)
}

func TestWhatAProviderAnsweredIsRecordedWithoutReplacingAPersonsName(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-named")
		made := locationPlace(t, tx, userId, "Exampleton", 10.5)
		now := time.Now()

		// A failure is recorded and the place keeps waiting.
		if err := tx.SetPlaceNamed(made.ID, nil, "place: the provider answered 503", now); err != nil {
			t.Fatalf("SetPlaceNamed: %s", err)
		}
		kept, _ := tx.GetPlace(userId, made.ID)
		if kept.NamedAt != nil || kept.NamingErrorMessage != "place: the provider answered 503" || kept.PlaceName != "Exampleton" {
			t.Fatalf("after a failure: %+v", kept)
		}

		found := &db.NamedPlace{PlaceName: "Example Park", OSMType: models.OSMTypeWay, OSMID: 4242, OSMCategory: "leisure=park", AddressText: "Example Park, Sample Street, Exampleton"}
		if err := tx.SetPlaceNamed(made.ID, found, "", now); err != nil {
			t.Fatalf("SetPlaceNamed: %s", err)
		}
		kept, _ = tx.GetPlace(userId, made.ID)
		if kept.NamedAt == nil || kept.NamingErrorMessage != "" || kept.PlaceName != "Example Park" || kept.OSMName != "Example Park" ||
			kept.OSMType != "way" || kept.OSMID != 4242 || kept.OSMCategory != "leisure=park" || kept.AddressText != "Example Park, Sample Street, Exampleton" {
			t.Fatalf("after the answer: %+v", kept)
		}
		if claimed, _ := tx.ClaimPlacesToName(10, now.Add(time.Hour)); len(claimed) != 0 {
			t.Errorf("a named place is claimed again: %+v", claimed)
		}

		// The person names it; a later answer fills in the map's side only.
		own := "Sample Playground"
		if _, err := tx.SavePlace(userId, made.ID, db.PlaceChanges{PlaceName: &own}); err != nil {
			t.Fatalf("SavePlace: %s", err)
		}
		renamed := &db.NamedPlace{PlaceName: "Example Gardens", OSMType: models.OSMTypeNode, OSMID: 7}
		if err := tx.SetPlaceNamed(made.ID, renamed, "", now); err != nil {
			t.Fatalf("SetPlaceNamed: %s", err)
		}
		kept, _ = tx.GetPlace(userId, made.ID)
		if kept.PlaceName != "Sample Playground" || !kept.IsNamedByPerson || kept.OSMName != "Example Gardens" || kept.OSMID != 7 {
			t.Fatalf("after naming it again: %+v", kept)
		}

		// A provider that knows nothing still ends the wait, and leaves the
		// name as it was.
		quiet := locationPlace(t, tx, userId, "Elsewhere", 12)
		if err := tx.SetPlaceNamed(quiet.ID, nil, "", now); err != nil {
			t.Fatalf("SetPlaceNamed: %s", err)
		}
		if kept, _ = tx.GetPlace(userId, quiet.ID); kept.NamedAt == nil || kept.PlaceName != "Elsewhere" || kept.OSMType != "" {
			t.Fatalf("after an empty answer: %+v", kept)
		}

		if err := tx.SetPlaceNamed("missing", nil, "", now); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a place that is not there: %v", err)
		}
		if err := tx.SetPlaceNamed(made.ID, &db.NamedPlace{OSMType: "area"}, "", now); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a type nobody knows: %v", err)
		}
	})
}
