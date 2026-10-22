package apigraph

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// locationWorld is a server with four people who may use circles, calendars
// and Location: ada and ben in one circle, cora in none of ada's, and dave in a
// circle of his own.
type locationWorld struct {
	*calendarShareWorld
}

func newLocationWorld(t *testing.T) *locationWorld {
	t.Helper()
	world := &locationWorld{calendarShareWorld: newCalendarShareWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "location role", Permissions: []models.Permission{models.PermissionLocationUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		for _, user := range []*models.User{world.ada, world.ben, world.cora, world.dave} {
			if _, err := tx.CreateGroup(&models.Group{Name: user.Username + " location group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				t.Fatalf("CreateGroup: %s", err)
			}
		}
	})
	// No test reaches the public place provider: the ones about naming set up
	// a provider of their own.
	if err := world.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.ProviderKind = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return world
}

// A moment a little before now, written for a query.
func ago(duration time.Duration) string {
	return time.Now().Add(-duration).UTC().Format(time.RFC3339)
}

func (self *locationWorld) report(t *testing.T, user *models.User, points, visits string) map[string]any {
	t.Helper()
	data := self.call(t, user, fmt.Sprintf(`mutation { ReportLocation(points: [%s], visits: [%s]) { newPointCount newVisitCount updatedVisitCount } }`, points, visits))
	return data["ReportLocation"].(map[string]any)
}

func pointInput(latitude, longitude float64, measuredAt string) string {
	return fmt.Sprintf(`{latitudeDegrees: %v, longitudeDegrees: %v, accuracyMeters: 10, measuredAt: %q}`, latitude, longitude, measuredAt)
}

func visitInput(key string, latitude, longitude float64, arrivedAt, departedAt string) string {
	fields := fmt.Sprintf(`clientVisitKey: %q, latitudeDegrees: %v, longitudeDegrees: %v, accuracyMeters: 20, visitSource: "visit"`, key, latitude, longitude)
	if arrivedAt != "" {
		fields += fmt.Sprintf(`, arrivedAt: %q`, arrivedAt)
	}
	if departedAt != "" {
		fields += fmt.Sprintf(`, departedAt: %q`, departedAt)
	}
	return "{" + fields + "}"
}

const visitFieldsForTest = `id arrivedAt departedAt durationSeconds accuracyMeters visitSource place { id placeName placeLabel isNamedByPerson }`

// A report is safe to send twice, an arrival and its departure are one visit
// that keeps its arrival and its accuracy, and a visit is at a place that is
// named for now after its city.
func TestAReportIsOneVisitWhenSentInPieces(t *testing.T) {
	world := newLocationWorld(t)
	arrival := ago(40 * time.Minute)
	departure := ago(10 * time.Minute)
	// One time for the point of both reports: a second call to ago can land in
	// the next second, and the resent position would be a different one.
	measuredAt := ago(30 * time.Minute)

	first := world.report(t, world.ada, pointInput(40.0005, -75.0003, measuredAt), visitInput("k1", 40.0005, -75.0003, arrival, ""))
	if first["newPointCount"] != float64(1) || first["newVisitCount"] != float64(1) {
		t.Fatalf("the first report: %v", first)
	}
	again := world.report(t, world.ada, pointInput(40.0005, -75.0003, measuredAt), visitInput("k1", 40.0005, -75.0003, arrival, ""))
	if again["newPointCount"] != float64(0) || again["newVisitCount"] != float64(0) || again["updatedVisitCount"] != float64(0) {
		t.Errorf("sent again it adds nothing: %v", again)
	}

	// The departure carries no arrival, and an accuracy of zero.
	departureInput := fmt.Sprintf(`{clientVisitKey: "k1", latitudeDegrees: 40.0005, longitudeDegrees: -75.0003, accuracyMeters: 0, departedAt: %q, visitSource: "visit"}`, departure)
	if sent := world.report(t, world.ada, "", departureInput); sent["newVisitCount"] != float64(0) || sent["updatedVisitCount"] != float64(1) {
		t.Errorf("the departure completes the visit: %v", sent)
	}

	data := world.call(t, world.ada, `query { ListLocationVisits { nextOffset placeVisits { `+visitFieldsForTest+` } } }`)
	page := data["ListLocationVisits"].(map[string]any)
	visits := page["placeVisits"].([]any)
	if len(visits) != 1 || page["nextOffset"] != nil {
		t.Fatalf("one visit: %v", page)
	}
	visit := visits[0].(map[string]any)
	if visit["arrivedAt"] == nil || visit["departedAt"] == nil || visit["durationSeconds"] != float64(30*60) || visit["accuracyMeters"] != float64(20) {
		t.Errorf("both times, and the accuracy the arrival gave: %v", visit)
	}

	// Nobody is at the place any more.
	location := world.call(t, world.ada, `query { GetLocation { username shareLevel latestPoint { latitudeDegrees } openVisit { id } } }`)["GetLocation"].(map[string]any)
	if location["openVisit"] != nil || location["latestPoint"] == nil || location["shareLevel"] != "owner" {
		t.Errorf("where they are: %v", location)
	}
}

// A visit that is still going is reported while the latest position is at the
// place, and not once a later position is somewhere else.
func TestAVisitNobodyEndedIsNotForEver(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40.0, -75.0, ago(50*time.Minute)), visitInput("k1", 40.0, -75.0, ago(60*time.Minute), ""))
	stillThere := world.call(t, world.ada, `query { GetLocation { openVisit { id place { placeName } } } }`)["GetLocation"].(map[string]any)
	if stillThere["openVisit"] == nil {
		t.Fatalf("the latest position is at the place: %v", stillThere)
	}
	world.report(t, world.ada, pointInput(40.5, -75.5, ago(5*time.Minute)), "")
	gone := world.call(t, world.ada, `query { GetLocation { openVisit { id } } }`)["GetLocation"].(map[string]any)
	if gone["openVisit"] != nil {
		t.Errorf("a later position elsewhere ends it: %v", gone)
	}
}

// A second visit near the first is at the same place, and a place keeps the
// name and label the person gave it.
func TestVisitsAtOnePlaceAndNamingIt(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, "", visitInput("k1", 40.0, -75.0, ago(300*time.Minute), ago(280*time.Minute)))
	world.report(t, world.ada, "", visitInput("k2", 40.0002, -75.0002, ago(100*time.Minute), ago(80*time.Minute)))
	places := world.call(t, world.ada, `query { ListPlaces { places { id placeName visitCount } } }`)["ListPlaces"].(map[string]any)["places"].([]any)
	if len(places) != 1 || places[0].(map[string]any)["visitCount"] != float64(2) {
		t.Fatalf("one place, two visits: %v", places)
	}
	placeId := places[0].(map[string]any)["id"].(string)

	saved := world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, placeName: "Grandma's", placeLabel: "family") { placeName placeLabel isNamedByPerson visitCount } }`, placeId))["SavePlace"].(map[string]any)
	if saved["placeName"] != "Grandma's" || saved["placeLabel"] != "family" || saved["isNamedByPerson"] != true || saved["visitCount"] != float64(2) {
		t.Errorf("named and labeled: %v", saved)
	}
	reset := world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, placeName: "", placeLabel: "") { placeName placeLabel isNamedByPerson } }`, placeId))["SavePlace"].(map[string]any)
	if reset["isNamedByPerson"] != false || reset["placeLabel"] != "" {
		t.Errorf("back to the map's name, no label: %v", reset)
	}
	if message := world.refused(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, placeLabel: "castle") { id } }`, placeId)); !strings.Contains(message, "home, work") && !strings.Contains(message, "label") {
		t.Errorf("a label there is not, in words: %s", message)
	}
	made := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 41.0, longitudeDegrees: -74.0, placeName: "Piano teacher", placeLabel: "") { id placeName isNamedByPerson } }`)["SavePlace"].(map[string]any)
	if made["placeName"] != "Piano teacher" || made["isNamedByPerson"] != true {
		t.Errorf("a new place: %v", made)
	}

	// Somebody else's place is not found, and neither is deleting it.
	world.refused(t, world.cora, fmt.Sprintf(`mutation { SavePlace(placeId: %q, placeName: "Mine") { id } }`, placeId))
	world.refused(t, world.cora, fmt.Sprintf(`mutation { ForgetPlace(placeId: %q) }`, placeId))
	world.call(t, world.ada, fmt.Sprintf(`mutation { ForgetPlace(placeId: %q) }`, placeId))
	if visits := world.call(t, world.ada, `query { ListLocationVisits { placeVisits { id } } }`)["ListLocationVisits"].(map[string]any)["placeVisits"].([]any); len(visits) != 0 {
		t.Errorf("a forgotten place takes its visits with it: %v", visits)
	}
}

// A check-in with coordinates alone uses the person's own place whose radius
// holds the point, and otherwise makes a place called after its city that is
// not yet named, so that it is named from OpenStreetMap after the commit.
func TestCheckInWithCoordinatesOnly(t *testing.T) {
	world := newLocationWorld(t)
	placeAt := func(placeId string) *models.Place {
		var found *models.Place
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			var err error
			if found, err = tx.GetPlace(world.ada.ID, placeId); err != nil {
				t.Fatalf("GetPlace: %s", err)
			}
		})
		return found
	}
	checkIn := func(latitude, longitude float64) string {
		answer := world.call(t, world.ada, fmt.Sprintf(`mutation { CheckIn(latitudeDegrees: %v, longitudeDegrees: %v) { `+visitFieldsForTest+` } }`, latitude, longitude))["CheckIn"].(map[string]any)
		return answer["place"].(map[string]any)["id"].(string)
	}

	made := checkIn(40.0, -75.0)
	if placeAt(made).NamedAt != nil {
		t.Errorf("a place made from coordinates alone is left for OpenStreetMap to name")
	}
	// A few meters away is inside the same place, whether or not it is named.
	if again := checkIn(40.00005, -75.00005); again != made {
		t.Errorf("the person's own place that holds the point is used: %s, then %s", made, again)
	}
	// Far away is a place of its own.
	if far := checkIn(41.0, -75.0); far == made {
		t.Errorf("a point outside every place makes a new one")
	}
	// A found place, with its name, is still named at once.
	found := world.call(t, world.ada, `mutation { CheckIn(latitudeDegrees: 42.0, longitudeDegrees: -75.0, placeName: "Corner Cafe", osmType: "node", osmId: 7) { place { id } } }`)["CheckIn"].(map[string]any)
	if placeAt(found["place"].(map[string]any)["id"].(string)).NamedAt == nil {
		t.Errorf("a found place arrives named")
	}
}

// A check-in ends the visit in progress and starts one, at an existing place or
// a found one.
func TestCheckInEndsTheVisitInProgress(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40.0, -75.0, ago(20*time.Minute)), visitInput("k1", 40.0, -75.0, ago(60*time.Minute), ""))
	checked := world.call(t, world.ada, `mutation { CheckIn(latitudeDegrees: 40.0004, longitudeDegrees: -75.0004, placeName: "Corner Cafe", osmType: "node", osmId: 42, osmCategory: "amenity=cafe") { `+visitFieldsForTest+` } }`)["CheckIn"].(map[string]any)
	if checked["visitSource"] != "check_in" || checked["place"].(map[string]any)["placeName"] != "Corner Cafe" {
		t.Fatalf("checked in: %v", checked)
	}
	visits := world.call(t, world.ada, `query { ListLocationVisits { placeVisits { departedAt place { placeName } } } }`)["ListLocationVisits"].(map[string]any)["placeVisits"].([]any)
	if len(visits) != 2 {
		t.Fatalf("two visits: %v", visits)
	}
	// Newest first: the check-in is in progress; the earlier visit ended.
	if visits[0].(map[string]any)["departedAt"] != nil || visits[1].(map[string]any)["departedAt"] == nil {
		t.Errorf("the first visit ended and the check-in is going: %v", visits)
	}
	// The same object again is the same place.
	again := world.call(t, world.ada, `mutation { CheckIn(latitudeDegrees: 40.0004, longitudeDegrees: -75.0004, placeName: "Corner Cafe", osmType: "node", osmId: 42) { place { id } } }`)["CheckIn"].(map[string]any)
	first := checked["place"].(map[string]any)["id"]
	if again["place"].(map[string]any)["id"] != first {
		t.Errorf("one place for one object on the map")
	}
	world.refused(t, world.ada, `mutation { CheckIn { id } }`)
	world.refused(t, world.ada, `mutation { CheckIn(placeId: "nope") { id } }`)
}

// A batch that is wrong anywhere is refused whole, in words.
func TestAReportThatIsWrongIsRefusedWhole(t *testing.T) {
	world := newLocationWorld(t)
	cases := map[string][2]string{
		"not on the earth":   {pointInput(95, 0, ago(time.Minute)), ""},
		"accuracy":           {`{latitudeDegrees: 1, longitudeDegrees: 1, accuracyMeters: -3, measuredAt: "` + ago(time.Minute) + `"}`, ""},
		"in the future":      {pointInput(1, 1, time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339)), ""},
		"neither an arrival": {"", `{clientVisitKey: "k", latitudeDegrees: 1, longitudeDegrees: 1, accuracyMeters: 1, visitSource: "visit"}`},
		"clientVisitKey":     {"", `{clientVisitKey: "", latitudeDegrees: 1, longitudeDegrees: 1, accuracyMeters: 1, arrivedAt: "` + ago(time.Hour) + `", visitSource: "visit"}`},
		"source":             {"", `{clientVisitKey: "k", latitudeDegrees: 1, longitudeDegrees: 1, accuracyMeters: 1, arrivedAt: "` + ago(time.Hour) + `", visitSource: "check_in"}`},
		"departs before":     {"", `{clientVisitKey: "k", latitudeDegrees: 1, longitudeDegrees: 1, accuracyMeters: 1, arrivedAt: "` + ago(time.Hour) + `", departedAt: "` + ago(2*time.Hour) + `", visitSource: "visit"}`},
	}
	for word, parts := range cases {
		message := world.refused(t, world.ada, fmt.Sprintf(`mutation { ReportLocation(points: [%s], visits: [%s]) { newPointCount } }`, parts[0], parts[1]))
		if !strings.Contains(message, word) {
			t.Errorf("%s: the refusal should say so: %s", word, message)
		}
	}
	// One good position beside a bad one stores nothing.
	world.refused(t, world.ada, fmt.Sprintf(`mutation { ReportLocation(points: [%s, %s], visits: []) { newPointCount } }`, pointInput(1, 1, ago(time.Minute)), pointInput(95, 0, ago(time.Minute))))
	if latest := world.call(t, world.ada, `query { GetLocation { latestPoint { latitudeDegrees } } }`)["GetLocation"].(map[string]any)["latestPoint"]; latest != nil {
		t.Errorf("nothing was stored: %v", latest)
	}
	// Too many in a batch.
	many := strings.TrimSuffix(strings.Repeat(pointInput(1, 1, ago(time.Minute))+",", 501), ",")
	if message := world.refused(t, world.ada, fmt.Sprintf(`mutation { ReportLocation(points: [%s], visits: []) { newPointCount } }`, many)); !strings.Contains(message, "500") {
		t.Errorf("too many: %s", message)
	}
	// A phone that is not theirs.
	world.refused(t, world.ada, `mutation { ReportLocation(agentPhoneId: "someone-elses", points: [], visits: []) { newPointCount } }`)
}

// Positions of a path: at most two days and the person's own.
func TestThePathOfADayHasLimits(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(1, 1, ago(3*time.Hour))+","+pointInput(1.001, 1, ago(2*time.Hour)), "")
	points := world.call(t, world.ada, fmt.Sprintf(`query { ListLocationPoints(from: %q, until: %q) { latitudeDegrees locationSource } }`, ago(24*time.Hour), ago(0)))["ListLocationPoints"].([]any)
	if len(points) != 2 || points[0].(map[string]any)["latitudeDegrees"] != 1.0 || points[0].(map[string]any)["locationSource"] != "phone" {
		t.Errorf("oldest first: %v", points)
	}
	world.refused(t, world.ada, fmt.Sprintf(`query { ListLocationPoints(from: %q, until: %q) { latitudeDegrees } }`, ago(72*time.Hour), ago(0)))
	world.refused(t, world.ada, fmt.Sprintf(`query { ListLocationPoints(from: %q, until: %q) { latitudeDegrees } }`, ago(0), ago(time.Hour)))
}

// Erasing history deletes a range, and the places that leaves unused.
func TestErasingHistory(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(1, 1, ago(5*time.Hour)), visitInput("k1", 1, 1, ago(5*time.Hour), ago(4*time.Hour)))
	erased := world.call(t, world.ada, `mutation { EraseLocationHistory { pointCount visitCount placeCount } }`)["EraseLocationHistory"].(map[string]any)
	if erased["pointCount"] != float64(1) || erased["visitCount"] != float64(1) || erased["placeCount"] != float64(1) {
		t.Errorf("what was deleted: %v", erased)
	}
	world.refused(t, world.ada, fmt.Sprintf(`mutation { EraseLocationHistory(from: %q, until: %q) { pointCount } }`, ago(0), ago(time.Hour)))
}

// Another person's location is reached only as far as it is shared, and only
// while the two share a circle.
func TestWhoReachesWhoseLocation(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40, -75, ago(5*time.Minute)), visitInput("k1", 40, -75, ago(60*time.Minute), ago(30*time.Minute)))
	asAda := fmt.Sprintf(`userId: %q`, world.ada.ID)

	for _, query := range []string{
		fmt.Sprintf(`query { GetLocation(%s) { username } }`, asAda),
		fmt.Sprintf(`query { ListLocationVisits(%s) { nextOffset } }`, asAda),
		fmt.Sprintf(`query { ListPlaces(%s) { nextOffset } }`, asAda),
	} {
		world.refused(t, world.ben, query)
		world.refused(t, world.cora, query)
	}
	if people := world.call(t, world.ben, `query { ListSharedLocations { username } }`)["ListSharedLocations"].([]any); len(people) != 0 {
		t.Errorf("nobody shares yet: %v", people)
	}

	share := world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id shareLevel granteeUsername } }`, world.ben.ID))["ShareLocation"].(map[string]any)
	if share["shareLevel"] != "current" || share["granteeUsername"] != world.ben.Username {
		t.Fatalf("shared: %v", share)
	}
	seen := world.call(t, world.ben, fmt.Sprintf(`query { GetLocation(%s) { username shareLevel latestPoint { latitudeDegrees } } }`, asAda))["GetLocation"].(map[string]any)
	if seen["username"] != world.ada.Username || seen["shareLevel"] != "current" || seen["latestPoint"] == nil {
		t.Errorf("at current, where she is: %v", seen)
	}
	world.refused(t, world.ben, fmt.Sprintf(`query { ListLocationVisits(%s) { nextOffset } }`, asAda))
	world.refused(t, world.ben, fmt.Sprintf(`query { ListLocationPoints(%s, from: %q, until: %q) { latitudeDegrees } }`, asAda, ago(time.Hour), ago(0)))
	world.refused(t, world.cora, fmt.Sprintf(`query { GetLocation(%s) { username } }`, asAda))

	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "history") { id } }`, world.ben.ID))
	if visits := world.call(t, world.ben, fmt.Sprintf(`query { ListLocationVisits(%s) { placeVisits { id } } }`, asAda))["ListLocationVisits"].(map[string]any)["placeVisits"].([]any); len(visits) != 1 {
		t.Errorf("at history, the visits: %v", visits)
	}
	people := world.call(t, world.ben, `query { ListSharedLocations { username shareLevel } }`)["ListSharedLocations"].([]any)
	if len(people) != 1 || people[0].(map[string]any)["shareLevel"] != "history" {
		t.Errorf("who shares with ben: %v", people)
	}

	// Only the owner takes a share back, and someone with no circle in common
	// is refused.
	shares := world.call(t, world.ada, `query { ListLocationShares { id granteeUsername shareLevel } }`)["ListLocationShares"].([]any)
	if len(shares) != 1 {
		t.Fatalf("one share, changed in place: %v", shares)
	}
	shareId := shares[0].(map[string]any)["id"].(string)
	world.refused(t, world.ben, fmt.Sprintf(`mutation { UnshareLocation(shareId: %q) }`, shareId))
	world.refused(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.cora.ID))
	world.refused(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "everything") { id } }`, world.ben.ID))
	world.call(t, world.ada, fmt.Sprintf(`mutation { UnshareLocation(shareId: %q) }`, shareId))
	world.refused(t, world.ben, fmt.Sprintf(`query { GetLocation(%s) { username } }`, asAda))
}

// makeNobody is an account that may use nothing.
func (self *locationWorld) makeNobody(t *testing.T) *models.User {
	t.Helper()
	var nobody *models.User
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if nobody, err = tx.CreateUser(&models.User{Username: "location-nobody"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
	})
	return nobody
}

// A person without location:use is refused everything, as not found.
func TestLocationNeedsItsPermission(t *testing.T) {
	world := newLocationWorld(t)
	nobody := world.makeNobody(t)
	for _, document := range []string{
		`query { GetLocation { username } }`,
		`mutation { ReportLocation(points: [], visits: []) { newPointCount } }`,
		`query { ListSharedLocations { username } }`,
	} {
		if message := world.refused(t, nobody, document); !strings.Contains(message, "not found") {
			t.Errorf("%s: %s", document, message)
		}
	}
}

// With the provider off, finding places says whose switch it is; with one,
// the nearby list holds the map's answer and the person's own places.
func TestPlacesFromTheProvider(t *testing.T) {
	world := newLocationWorld(t)
	if err := world.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.ProviderKind = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A check-in list still offers the person's own places while the provider
	// is off, and only a search is refused.
	world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 40.0001, longitudeDegrees: -75.0001, placeName: "Home") { id } }`)
	own := world.call(t, world.ada, `query { ListNearbyPlaces(latitudeDegrees: 40, longitudeDegrees: -75) { placeName placeId placeLabel isNamedByPerson } }`)["ListNearbyPlaces"].([]any)
	if len(own) != 1 || own[0].(map[string]any)["placeName"] != "Home" || own[0].(map[string]any)["placeId"] == nil {
		t.Errorf("their own places with the provider off: %v", own)
	}
	if row := own[0].(map[string]any); row["isNamedByPerson"] != true || row["placeLabel"] != "" {
		t.Errorf("a place they named says so: %v", row)
	}
	world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 41.5, longitudeDegrees: -75.0, placeLabel: "gym") { id } }`)
	labeled := world.call(t, world.ada, `query { ListNearbyPlaces(latitudeDegrees: 41.5, longitudeDegrees: -75.0) { placeName placeLabel isNamedByPerson } }`)["ListNearbyPlaces"].([]any)
	if row := labeled[0].(map[string]any); row["placeLabel"] != "gym" || row["isNamedByPerson"] != false {
		t.Errorf("a place they only labeled carries its label and the map's name: %v", labeled)
	}
	if message := world.refused(t, world.ada, `query { SearchPlaces(query: "playground", latitudeDegrees: 40, longitudeDegrees: -75) { foundPlaces { placeName } } }`); !strings.Contains(message, "operator") || !strings.Contains(message, "OpenStreetMap") {
		t.Errorf("whose switch it is: %s", message)
	}
	world.refused(t, world.ada, `query { SearchPlaces(query: "playground", latitudeDegrees: 40, longitudeDegrees: -75) { foundPlaces { placeName } } }`)

	var requests []string
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.Path+"?"+request.URL.RawQuery)
		if got := request.Header.Get("User-Agent"); !strings.HasPrefix(got, "TeaNode/") {
			t.Errorf("the provider is told who asks: %q", got)
		}
		response.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(request.URL.Path, "/reverse") {
			_, _ = response.Write([]byte(`{"osm_type":"node","osm_id":42,"lat":"40.0006","lon":"-75.0004","category":"amenity","type":"cafe","name":"Corner Cafe","display_name":"Corner Cafe, 1 Elm Street"}`))
			return
		}
		_, _ = response.Write([]byte(`[{"osm_type":"way","osm_id":7,"lat":"40.01","lon":"-75.0","category":"leisure","type":"playground","name":"Elm Playground","display_name":"Elm Playground, Elm"}]`))
	}))
	defer provider.Close()
	if err := world.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.ProviderKind = config.PlacesProviderNominatim
		configuration.Places.ProviderURL = provider.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rows := world.call(t, world.ada, `query { ListNearbyPlaces(latitudeDegrees: 40.0005, longitudeDegrees: -75.0003) { placeName osmId distanceMeters placeId } }`)["ListNearbyPlaces"].([]any)
	if len(rows) != 2 {
		t.Fatalf("the map's answer and the person's place: %v", rows)
	}
	names := []string{rows[0].(map[string]any)["placeName"].(string), rows[1].(map[string]any)["placeName"].(string)}
	if (names[0] != "Home" && names[1] != "Home") || (names[0] != "Corner Cafe" && names[1] != "Corner Cafe") {
		t.Errorf("both: %v", names)
	}
	for _, row := range rows {
		if row.(map[string]any)["placeName"] == "Home" && row.(map[string]any)["placeId"] == nil {
			t.Errorf("their own place says so: %v", row)
		}
	}

	found := world.call(t, world.ada, `query { SearchPlaces(query: "playground", latitudeDegrees: 40.0, longitudeDegrees: -75.0, radiusMeters: 5000) { nextOffset foundPlaces { placeName osmType osmId distanceMeters } } }`)["SearchPlaces"].(map[string]any)
	places := found["foundPlaces"].([]any)
	if len(places) != 1 || places[0].(map[string]any)["placeName"] != "Elm Playground" || places[0].(map[string]any)["osmType"] != "way" {
		t.Errorf("the search: %v", found)
	}
	// Searching with nowhere to search around says so.
	if message := world.refused(t, world.dave, `query { SearchPlaces(query: "park") { foundPlaces { placeName } } }`); !strings.Contains(message, "position") {
		t.Errorf("nowhere to search: %s", message)
	}
	world.refused(t, world.ada, `query { ListNearbyPlaces(latitudeDegrees: 40, longitudeDegrees: -75, radiusMeters: 5000) { placeName } }`)
	world.refused(t, world.ada, `query { SearchPlaces(query: "  ", latitudeDegrees: 40, longitudeDegrees: -75) { foundPlaces { placeName } } }`)
}

// A place a report made is named at once, whether or not the agent runs, and
// not before the report is answered; a person's own name is never replaced.
func TestANewPlaceIsNamedRightAfterTheReport(t *testing.T) {
	world := newLocationWorld(t)
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"osm_type":"node","osm_id":42,"lat":"40.0001","lon":"-75.0001","category":"amenity","type":"cafe","name":"Corner Cafe","display_name":"Corner Cafe, 1 Elm Street"}`))
	}))
	defer provider.Close()
	if err := world.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.ProviderKind = config.PlacesProviderNominatim
		configuration.Places.ProviderURL = provider.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	world.report(t, world.ada, "", visitInput("k1", 40.0, -75.0, ago(30*time.Minute), ""))
	deadline := time.Now().Add(15 * time.Second)
	for {
		places := world.call(t, world.ada, `query { ListPlaces { places { placeName osmId } } }`)["ListPlaces"].(map[string]any)["places"].([]any)
		if len(places) == 1 && places[0].(map[string]any)["placeName"] == "Corner Cafe" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the place was not named: %v", places)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// listedVisits is the visits of a person as "place arrived to departed" lines,
// oldest first, in minutes after the base time.
func (self *locationWorld) listedVisits(t *testing.T, user *models.User, base time.Time) []string {
	t.Helper()
	data := self.call(t, user, `query { ListLocationVisits(limitCount: 200) { placeVisits { arrivedAt departedAt place { id } } } }`)
	visits := data["ListLocationVisits"].(map[string]any)["placeVisits"].([]any)
	minutes := func(value any) string {
		if value == nil {
			return "open"
		}
		moment, err := time.Parse(time.RFC3339, value.(string))
		if err != nil {
			t.Fatalf("time %v: %s", value, err)
		}
		return fmt.Sprint(int(moment.Sub(base).Minutes()))
	}
	lines := make([]string, 0, len(visits))
	for index := len(visits) - 1; index >= 0; index-- {
		visit := visits[index].(map[string]any)
		lines = append(lines, fmt.Sprintf("%s %s to %s", visit["place"].(map[string]any)["id"], minutes(visit["arrivedAt"]), minutes(visit["departedAt"])))
	}
	return lines
}

// One day's arrivals and departures delivered in every order end up as the
// visits they are in order: no visit inside another, none left open when the
// person has gone on, and a visit that was joined before the one between its
// parts arrived is split again.
func TestVisitsDeliveredInAnyOrderEndUpAsInOrder(t *testing.T) {
	world := newLocationWorld(t)
	base := time.Now().Add(-12 * time.Hour).Truncate(time.Minute)
	at := func(minutes int) string {
		return base.Add(time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339)
	}
	const (
		homeLatitude, workLatitude, gymLatitude = 40.0, 41.0, 42.0
		longitude                               = -75.0
	)

	// Home, work, and home again.
	deliverInEveryOrder(t, world, base, []string{
		visitInput("home-1", homeLatitude, longitude, at(0), ""),
		visitInput("home-1", homeLatitude, longitude, "", at(60)),
		visitInput("work-1", workLatitude, longitude, at(60), ""),
		visitInput("work-1", workLatitude, longitude, "", at(300)),
		visitInput("home-2", homeLatitude, longitude, at(330), ""),
		visitInput("home-2", homeLatitude, longitude, "", at(500)),
	}, []string{"0 to 60", "60 to 300", "330 to 500"})

	// Home, the gym, and home again, the last still going.
	deliverInEveryOrder(t, world, base, []string{
		visitInput("home-1", homeLatitude, longitude, at(0), ""),
		visitInput("home-1", homeLatitude, longitude, "", at(100)),
		visitInput("gym-1", gymLatitude, longitude, at(100), ""),
		visitInput("gym-1", gymLatitude, longitude, "", at(160)),
		visitInput("home-2", homeLatitude, longitude, at(170), ""),
		visitInput("work-1", workLatitude, longitude, at(400), ""),
	}, []string{"0 to 100", "100 to 160", "170 to 400", "400 to open"})
}

// deliverInEveryOrder sends the events, one report each, in every order of
// them (by Heap's algorithm), and checks that the person's visits end up the
// ones the first order, which is the order they happened in, makes.
func deliverInEveryOrder(t *testing.T, world *locationWorld, base time.Time, events []string, wantSuffixes []string) {
	t.Helper()
	erase := func() {
		world.call(t, world.ada, `mutation { EraseLocationHistory { visitCount } }`)
		// Each order starts with the allowance for new places full.
		world.graph.locationLimits()
		world.graph.locationLimitsValue = newLocationRateLimits()
	}
	erase()
	for _, event := range events {
		world.report(t, world.ada, "", event)
	}
	inOrder := world.listedVisits(t, world.ada, base)
	if len(inOrder) != len(wantSuffixes) {
		t.Fatalf("in order: %v, want %v", inOrder, wantSuffixes)
	}
	for index, want := range wantSuffixes {
		if !strings.HasSuffix(inOrder[index], " "+want) {
			t.Fatalf("in order: %v, want %v", inOrder, wantSuffixes)
		}
	}

	order := make([]int, len(events))
	for index := range order {
		order[index] = index
	}
	orderCount, lastOrder := 0, ""
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the last order started was %s", lastOrder)
		}
	})
	var permute func(size int)
	permute = func(size int) {
		if size == 1 {
			orderCount++
			lastOrder = fmt.Sprint(order)
			erase()
			for _, index := range order {
				world.report(t, world.ada, "", events[index])
			}
			// Place ids differ after an erase: compare the times, and which
			// visits are at the same place.
			got := world.listedVisits(t, world.ada, base)
			if len(got) != len(inOrder) {
				t.Errorf("order %v: %v, in order %v", order, got, inOrder)
				return
			}
			for first := range got {
				if got[first][strings.Index(got[first], " "):] != inOrder[first][strings.Index(inOrder[first], " "):] {
					t.Errorf("order %v: %v, in order %v", order, got, inOrder)
					return
				}
				for second := range got {
					if (strings.Fields(got[first])[0] == strings.Fields(got[second])[0]) != (strings.Fields(inOrder[first])[0] == strings.Fields(inOrder[second])[0]) {
						t.Errorf("order %v: places %v, in order %v", order, got, inOrder)
						return
					}
				}
			}
			return
		}
		for index := 0; index < size; index++ {
			permute(size - 1)
			if size%2 == 1 {
				order[0], order[size-1] = order[size-1], order[0]
			} else {
				order[index], order[size-1] = order[size-1], order[index]
			}
		}
	}
	permute(len(order))
	factorial := 1
	for count := 2; count <= len(events); count++ {
		factorial *= count
	}
	if orderCount != factorial {
		t.Fatalf("%d orders, want %d", orderCount, factorial)
	}
}

// A person at the most places they may have loses only the visit that needs a
// new place: the positions and the visits at places they have are stored, and
// the answer says how many visits were left out.
func TestAReportAtTheMostPlacesLeavesOutOnlyTheVisitThatNeedsAPlace(t *testing.T) {
	world := newLocationWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for index := 0; index < db.PlaceCountMost; index++ {
			// A grid of places 1 km apart on the earth of the tests.
			latitude, longitude := -60+float64(index/100)*0.01, -100+float64(index%100)*0.02
			if _, err := tx.CreatePlace(world.ada.ID, &models.Place{PlaceName: fmt.Sprintf("Place %d", index), LatitudeDegrees: latitude, LongitudeDegrees: longitude}); err != nil {
				t.Fatalf("CreatePlace %d: %s", index, err)
			}
		}
	})
	data := world.call(t, world.ada, fmt.Sprintf(`mutation { ReportLocation(points: [%s], visits: [%s, %s]) { newPointCount newVisitCount updatedVisitCount refusedVisitCount } }`,
		pointInput(-60.0, -100.0, ago(30*time.Minute)),
		visitInput("at-a-known-place", -60.0, -100.0, ago(60*time.Minute), ""),
		visitInput("needs-a-new-place", 10.0, 10.0, ago(20*time.Minute), "")))
	view := data["ReportLocation"].(map[string]any)
	if view["newPointCount"] != float64(1) || view["newVisitCount"] != float64(1) || view["refusedVisitCount"] != float64(1) {
		t.Errorf("one position, one visit stored, one left out: %v", view)
	}
}

// The allowance for new places is taken once a report is committed, for the
// places it made: a full report of new places passes, and a report that made
// none takes nothing.
func TestTheAllowanceForNewPlacesIsTakenAfterTheReportForThePlacesItMade(t *testing.T) {
	world := newLocationWorld(t)
	available := func() int64 { return world.graph.locationLimits().newPlaces.For(world.ada.ID).Available() }
	const visitCountMost = 50
	visits := make([]string, 0, visitCountMost)
	for index := 0; index < visitCountMost; index++ {
		visits = append(visits, visitInput(fmt.Sprintf("burst-%d", index), 30+float64(index)*0.1, 50, ago(time.Duration(index+10)*time.Minute), ""))
	}
	report := world.report(t, world.ada, "", strings.Join(visits, ", "))
	if report["newVisitCount"] != float64(visitCountMost) {
		t.Fatalf("a day offline passes in one report: %v", report)
	}
	if left := available(); left > newPlacesPerMinute-visitCountMost+2 {
		t.Errorf("the places it made were taken: %d left", left)
	}
	before := available()
	world.report(t, world.ada, "", strings.Join(visits, ", "))
	if after := available(); after < before {
		t.Errorf("a report that made no place took %d", before-after)
	}
}

// A visit that is new and already has its departure tells the people the
// location is shared with that the person left, and a visit with only an
// arrival that they arrived.
func TestANewClosedVisitIsToldAsALeaving(t *testing.T) {
	world := newLocationWorld(t)
	reported := func(arrivedAt, departedAt *time.Time) *visitNotice {
		var notice *visitNotice
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			stored, err := world.graph.putReportedVisit(tx, world.ada.ID, ReportedVisit{
				ClientVisitKey: fmt.Sprintf("notice-%d", time.Now().UnixNano()), LatitudeDegrees: 40, LongitudeDegrees: -75, AccuracyMeters: 10,
				VisitSource: string(models.VisitSourceVisit), ArrivedAt: arrivedAt, DepartedAt: departedAt,
			}, nil)
			if err != nil {
				t.Fatalf("putReportedVisit: %s", err)
			}
			notice = stored.Notice
		})
		return notice
	}
	arrivedAt, departedAt := time.Now().Add(-12*time.Minute).Truncate(time.Second), time.Now().Add(-2*time.Minute).Truncate(time.Second)
	if notice := reported(&arrivedAt, &departedAt); notice == nil || notice.IsArrival || !notice.HappenedAt.Equal(departedAt) {
		t.Errorf("a closed visit is a leaving: %+v", notice)
	}
	// Another visit: one arriving in the same second would be the same visit.
	arrivedAt = arrivedAt.Add(time.Minute)
	if notice := reported(&arrivedAt, nil); notice == nil || !notice.IsArrival || !notice.HappenedAt.Equal(arrivedAt) {
		t.Errorf("an open visit is an arrival: %+v", notice)
	}
}

// Everybody who shares with the viewer is described from one read of the
// positions and one of the visits in progress.
func TestListSharedLocationsDescribesEverySharer(t *testing.T) {
	world := newLocationWorld(t)
	circleId := world.makeCircle(t, world.ada, "Neighbors")
	world.join(t, world.ada, circleId, world.ben)
	world.join(t, world.ada, circleId, world.cora)
	for _, sharer := range []*models.User{world.ada, world.cora} {
		world.call(t, sharer, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	}
	world.report(t, world.ada, pointInput(40.0, -75.0, ago(5*time.Minute)), visitInput("ada-here", 40.0, -75.0, ago(30*time.Minute), ""))
	world.report(t, world.cora, pointInput(41.0, -75.0, ago(5*time.Minute)), "")

	people := world.call(t, world.ben, `query { ListSharedLocations { username latestPoint { latitudeDegrees } openVisit { id } } }`)["ListSharedLocations"].([]any)
	if len(people) != 2 {
		t.Fatalf("two people share: %v", people)
	}
	for _, each := range people {
		person := each.(map[string]any)
		hasVisit := person["openVisit"] != nil
		if person["latestPoint"] == nil || hasVisit != (person["username"] == world.ada.Username) {
			t.Errorf("%v", person)
		}
	}
}

// providerRequests are the paths the address provider was asked for. The
// naming job asks from its own goroutine, so they are read under the mutex.
type providerRequests struct {
	mutex sync.Mutex
	paths []string
}

// asked returns the paths asked for so far.
func (self *providerRequests) asked() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string(nil), self.paths...)
}

// waitForNaming waits for the naming job's reverse lookup of a new place, and
// then forgets every path, so that what the naming asked for is not mistaken
// for what a later step asks for.
func (self *providerRequests) waitForNaming(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		self.mutex.Lock()
		isNamed := slices.ContainsFunc(self.paths, func(path string) bool { return strings.HasPrefix(path, "/reverse") })
		if isNamed {
			self.paths = nil
		}
		self.mutex.Unlock()
		if isNamed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the new place was never named: %v", self.asked())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// addressProvider answers an address search for invented addresses and records
// the paths it was asked for, so that a test can tell a search from a reverse
// lookup (which would be the naming job replacing a typed address).
func (self *locationWorld) addressProvider(t *testing.T) *providerRequests {
	t.Helper()
	requests := &providerRequests{}
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.mutex.Lock()
		requests.paths = append(requests.paths, request.URL.Path+"?"+request.URL.RawQuery)
		requests.mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path != "/search" {
			_, _ = response.Write([]byte(`{"osm_type":"node","osm_id":42,"lat":"40.5","lon":"-75.5","category":"amenity","type":"cafe","name":"Corner Cafe","display_name":"Corner Cafe, 1 Elm Street"}`))
			return
		}
		_, _ = response.Write([]byte(`[
			{"osm_type":"node","osm_id":71,"lat":"40.500000","lon":"-75.500000","category":"place","type":"house","name":"","display_name":"6, Rue Example, Exampleton","address":{"house_number":"6","road":"Rue Example","city":"Exampleton","country":"Testland"}},
			{"osm_type":"node","osm_id":72,"lat":"40.600000","lon":"-75.600000","category":"place","type":"house","name":"","display_name":"8, Rue Example, Exampleton","address":{"house_number":"8","road":"Rue Example","city":"Exampleton","country":"Testland"}}
		]`))
	}))
	t.Cleanup(provider.Close)
	if err := self.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.ProviderKind = config.PlacesProviderNominatim
		configuration.Places.ProviderURL = provider.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return requests
}

// An address search is refused while the provider is off, asks without a box
// around any position until there is one, tells which matches are where the
// caller has a place, and puts the matches nearest the place being given an
// address first.
func TestSearchAddressesFromTheProvider(t *testing.T) {
	world := newLocationWorld(t)
	const search = `query { SearchAddresses(query: "6 rue example") { placeName addressText latitudeDegrees longitudeDegrees distanceMeters placeId placeLabel isNamedByPerson } }`
	if message := world.refused(t, world.ada, search); !strings.Contains(message, "operator") || !strings.Contains(message, "OpenStreetMap") {
		t.Errorf("whose switch it is: %s", message)
	}

	requests := world.addressProvider(t)
	workId := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 40.6001, longitudeDegrees: -75.6001, placeLabel: "work") { id } }`)["SavePlace"].(map[string]any)["id"].(string)

	rows := world.call(t, world.ada, search)["SearchAddresses"].([]any)
	if len(rows) != 2 {
		t.Fatalf("the matches: %v", rows)
	}
	first, second := rows[0].(map[string]any), rows[1].(map[string]any)
	if first["placeName"] != "6 Rue Example" || first["addressText"] != "6 Rue Example, Exampleton, Testland" || first["distanceMeters"].(float64) != 0 || first["placeId"] != nil {
		t.Errorf("the first match with no position reported: %v", first)
	}
	if second["placeId"] == nil || second["placeLabel"] != "work" {
		t.Errorf("a match where the caller has a place says so: %v", second)
	}
	// The new place was named by a reverse lookup of its own; the search is
	// the one request that is not.
	searches := 0
	asked := requests.asked()
	for _, path := range asked {
		if strings.HasPrefix(path, "/search?") {
			searches++
			if strings.Contains(path, "viewbox") || strings.Contains(path, "bounded") {
				t.Errorf("the search was limited to a box: %v", path)
			}
		}
	}
	if searches != 1 {
		t.Errorf("the provider was asked %v", asked)
	}

	// With a reported position the distances are from it.
	world.report(t, world.ada, pointInput(40.5, -75.5, ago(time.Minute)), "")
	rows = world.call(t, world.ada, search)["SearchAddresses"].([]any)
	if rows[0].(map[string]any)["distanceMeters"].(float64) > 5 || rows[1].(map[string]any)["distanceMeters"].(float64) < 1000 {
		t.Errorf("distances from the latest position: %v", rows)
	}

	// Giving the work place an address, the match beside it comes first,
	// although the other is nearer the latest position; the provider is asked
	// to prefer a box around the place, not to keep to it.
	rows = world.call(t, world.ada, fmt.Sprintf(`query { SearchAddresses(query: "6 rue example", placeId: %q) { placeId distanceMeters } }`, workId))["SearchAddresses"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["placeId"] != workId || rows[0].(map[string]any)["distanceMeters"].(float64) > 50 ||
		rows[1].(map[string]any)["distanceMeters"].(float64) < 1000 {
		t.Errorf("nearest the place first: %v", rows)
	}
	asked = requests.asked()
	if last := asked[len(asked)-1]; !strings.HasPrefix(last, "/search?") || !strings.Contains(last, "viewbox") || !strings.Contains(last, "bounded=0") {
		t.Errorf("the search near the place asked %v", last)
	}
	// A position given is the same.
	rows = world.call(t, world.ada, `query { SearchAddresses(query: "6 rue example", latitudeDegrees: 40.6, longitudeDegrees: -75.6) { placeId } }`)["SearchAddresses"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["placeId"] != workId {
		t.Errorf("nearest the position given first: %v", rows)
	}
	world.refused(t, world.ada, `query { SearchAddresses(query: "elm", latitudeDegrees: 40.6) { placeName } }`)
	unknown := world.refused(t, world.ada, `query { SearchAddresses(query: "elm", placeId: "no-such-place") { placeName } }`)
	// Another person's place is not found either, the same way, and its
	// position is not searched around.
	before := len(requests.asked())
	if message := world.refused(t, world.ben, fmt.Sprintf(`query { SearchAddresses(query: "elm", placeId: %q) { placeName } }`, workId)); message != unknown {
		t.Errorf("another person's place: %q, an unknown one: %q", message, unknown)
	}
	if after := len(requests.asked()); after != before {
		t.Errorf("the provider was asked for another person's place: %v", requests.asked()[before:])
	}

	world.refused(t, world.ada, `query { SearchAddresses(query: "   ") { placeName } }`)
	world.refused(t, world.ada, fmt.Sprintf(`query { SearchAddresses(query: %q) { placeName } }`, strings.Repeat("a", 201)))
	world.refused(t, world.ada, `query { SearchAddresses(query: "elm", limitCount: 21) { placeName } }`)
	world.refused(t, world.ada, `query { SearchAddresses(query: "elm", limitCount: 0) { placeName } }`)
	if message := world.refused(t, world.makeNobody(t), search); !strings.Contains(message, "not found") {
		t.Errorf("a person without the permission: %s", message)
	}
}

// A labeled place moved to an address keeps its label, holds the address, is
// called by the first part of it, and is left alone by the naming job.
func TestSavePlaceMovedToATypedAddress(t *testing.T) {
	world := newLocationWorld(t)
	requests := world.addressProvider(t)

	made := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 41.0, longitudeDegrees: -74.0, placeLabel: "home") { id } }`)["SavePlace"].(map[string]any)
	placeId := made["id"].(string)
	requests.waitForNaming(t)

	if message := world.refused(t, world.ada, `mutation { SavePlace(placeLabel: "home", addressText: "6 Rue Example, Exampleton") { id } }`); !strings.Contains(message, "position of the address") {
		t.Errorf("an address without its position and without a place: %s", message)
	}

	saved := world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, latitudeDegrees: 40.5, longitudeDegrees: -75.5, addressText: "6 Rue Example, Exampleton, Testland") { id placeName placeLabel isNamedByPerson addressText osmId osmType osmName osmCategory latitudeDegrees longitudeDegrees } }`, placeId))["SavePlace"].(map[string]any)
	if saved["id"] != placeId || saved["placeLabel"] != "home" || saved["placeName"] != "6 Rue Example" || saved["isNamedByPerson"] != false ||
		saved["addressText"] != "6 Rue Example, Exampleton, Testland" || saved["latitudeDegrees"] != 40.5 || saved["longitudeDegrees"] != -75.5 {
		t.Errorf("the place moved to the address: %v", saved)
	}
	if saved["osmId"] != nil || saved["osmType"] != "" || saved["osmName"] != "" || saved["osmCategory"] != "" {
		t.Errorf("what was said about the old position stayed: %v", saved)
	}

	// The naming job has nothing to claim, and nothing was asked of the provider.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		claimed, err := tx.ClaimPlaceToName(placeId, time.Now())
		if err != nil || claimed != nil {
			t.Errorf("the naming job claimed the place: %v, %v", claimed, err)
		}
	})
	time.Sleep(300 * time.Millisecond)
	if asked := requests.asked(); len(asked) != 0 {
		t.Errorf("the provider was asked after the address was typed: %v", asked)
	}

	// A name the person gave stays theirs.
	world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, placeName: "Our flat") { id } }`, placeId))
	named := world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, latitudeDegrees: 40.6, longitudeDegrees: -75.6, addressText: "8 Rue Example, Exampleton, Testland") { placeName isNamedByPerson addressText } }`, placeId))["SavePlace"].(map[string]any)
	if named["placeName"] != "Our flat" || named["isNamedByPerson"] != true || named["addressText"] != "8 Rue Example, Exampleton, Testland" {
		t.Errorf("the person's name for it: %v", named)
	}
}

// An address the place provider does not know is kept as typed, on a place
// that stays where it is, keeps its label, and is not named again.
func TestSavePlaceKeepsATypedAddressWithoutMovingThePlace(t *testing.T) {
	world := newLocationWorld(t)
	requests := world.addressProvider(t)

	made := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 41.0, longitudeDegrees: -74.0, placeLabel: "home") { id } }`)["SavePlace"].(map[string]any)
	placeId := made["id"].(string)
	requests.waitForNaming(t)

	saved := world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, addressText: "  12 Lane Example, Exampleton ") { id placeName placeLabel isNamedByPerson addressText latitudeDegrees longitudeDegrees } }`, placeId))["SavePlace"].(map[string]any)
	if saved["id"] != placeId || saved["latitudeDegrees"] != 41.0 || saved["longitudeDegrees"] != -74.0 || saved["placeLabel"] != "home" ||
		saved["addressText"] != "12 Lane Example, Exampleton" || saved["placeName"] != "12 Lane Example" || saved["isNamedByPerson"] != false {
		t.Errorf("the address as typed, where the place was: %v", saved)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		claimed, err := tx.ClaimPlaceToName(placeId, time.Now())
		if err != nil || claimed != nil {
			t.Errorf("the naming job claimed the place: %v, %v", claimed, err)
		}
	})
	time.Sleep(300 * time.Millisecond)
	if asked := requests.asked(); len(asked) != 0 {
		t.Errorf("the provider was asked after the address was typed: %v", asked)
	}

	// Moving the place onto the house keeps the address as typed, and the
	// naming job leaves it alone.
	moved := world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, latitudeDegrees: 41.2, longitudeDegrees: -74.2) { addressText isAddressTyped placeName latitudeDegrees } }`, placeId))["SavePlace"].(map[string]any)
	if moved["addressText"] != "12 Lane Example, Exampleton" || moved["isAddressTyped"] != true || moved["placeName"] != "12 Lane Example" || moved["latitudeDegrees"] != 41.2 {
		t.Errorf("the move lost the typed address: %v", moved)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		claimed, err := tx.ClaimPlaceToName(placeId, time.Now())
		if err != nil || claimed != nil {
			t.Errorf("the naming job claimed a place with a typed address: %v, %v", claimed, err)
		}
	})

	// An empty address takes it away, and the place is named again.
	cleared := world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, addressText: "") { addressText isAddressTyped } }`, placeId))["SavePlace"].(map[string]any)
	if cleared["isAddressTyped"] != false {
		t.Errorf("the typed address was not taken away: %v", cleared)
	}
}

// A place made from coordinates and a typed address, with no placeId, says the
// address is typed, and moving it afterwards without an address keeps the
// address and the name and leaves the place to the naming job no more than
// before.
func TestSavePlaceMadeFromATypedAddressKeepsItWhenMoved(t *testing.T) {
	world := newLocationWorld(t)
	requests := world.addressProvider(t)

	made := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 40.5, longitudeDegrees: -75.5, addressText: "6 Rue Example, Exampleton, Testland") { id placeName addressText isAddressTyped } }`)["SavePlace"].(map[string]any)
	placeId := made["id"].(string)
	if made["isAddressTyped"] != true || made["addressText"] != "6 Rue Example, Exampleton, Testland" || made["placeName"] != "6 Rue Example" {
		t.Fatalf("a new place at a typed address: %v", made)
	}

	moved := world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, latitudeDegrees: 40.7, longitudeDegrees: -75.7) { addressText isAddressTyped placeName latitudeDegrees } }`, placeId))["SavePlace"].(map[string]any)
	if moved["addressText"] != "6 Rue Example, Exampleton, Testland" || moved["isAddressTyped"] != true || moved["placeName"] != "6 Rue Example" || moved["latitudeDegrees"] != 40.7 {
		t.Errorf("the move lost the typed address: %v", moved)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		claimed, err := tx.ClaimPlaceToName(placeId, time.Now())
		if err != nil || claimed != nil {
			t.Errorf("the naming job claimed a place with a typed address: %v, %v", claimed, err)
		}
	})
	time.Sleep(300 * time.Millisecond)
	for _, path := range requests.asked() {
		if strings.HasPrefix(path, "/reverse") {
			t.Errorf("the provider was asked to name the place: %v", requests.asked())
		}
	}
}

// A check-in keeps the accuracy the device reported on its visit.
func TestCheckInKeepsTheAccuracy(t *testing.T) {
	world := newLocationWorld(t)
	answer := world.call(t, world.ada, `mutation { CheckIn(latitudeDegrees: 40.0, longitudeDegrees: -75.0, accuracyMeters: 850) { accuracyMeters } }`)["CheckIn"].(map[string]any)
	if answer["accuracyMeters"] != 850.0 {
		t.Errorf("the accuracy was not kept on the visit: %v", answer)
	}
	world.refused(t, world.ada, `mutation { CheckIn(latitudeDegrees: 41.0, longitudeDegrees: -75.0, accuracyMeters: -1) { id } }`)
	plain := world.call(t, world.ada, `mutation { CheckIn(latitudeDegrees: 42.0, longitudeDegrees: -75.0) { accuracyMeters } }`)["CheckIn"].(map[string]any)
	if plain["accuracyMeters"] != 0.0 {
		t.Errorf("no accuracy was given: %v", plain)
	}
}

// Typing an address for home uses the place the caller already has there, and
// makes one that is not renamed when there is none.
func TestMakingHomeFromAnAddressReusesAnOwnPlaceThere(t *testing.T) {
	world := newLocationWorld(t)
	requests := world.addressProvider(t)

	existing := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 40.5, longitudeDegrees: -75.5, placeName: "Piano teacher") { id } }`)["SavePlace"].(map[string]any)["id"].(string)
	home := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 40.5001, longitudeDegrees: -75.5001, placeLabel: "home", addressText: "6 Rue Example, Exampleton, Testland") { id placeName placeLabel isNamedByPerson addressText } }`)["SavePlace"].(map[string]any)
	if home["id"] != existing || home["placeLabel"] != "home" || home["placeName"] != "Piano teacher" || home["addressText"] != "6 Rue Example, Exampleton, Testland" {
		t.Errorf("home was not the place that is there: %v", home)
	}
	listed := world.call(t, world.ada, `query { ListPlaces { places { id } } }`)["ListPlaces"].(map[string]any)["places"].([]any)
	if len(listed) != 1 {
		t.Errorf("a second place was made at the same address: %v", listed)
	}

	made := world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 41.5, longitudeDegrees: -76.5, placeLabel: "work", addressText: "8 Rue Example, Exampleton, Testland") { id placeName placeLabel isNamedByPerson addressText } }`)["SavePlace"].(map[string]any)
	if made["id"] == existing || made["placeLabel"] != "work" || made["placeName"] != "8 Rue Example" || made["isNamedByPerson"] != false || made["addressText"] != "8 Rue Example, Exampleton, Testland" {
		t.Errorf("a new place at the typed address: %v", made)
	}
	time.Sleep(300 * time.Millisecond)
	asked := requests.asked()
	for _, path := range asked {
		if strings.HasPrefix(path, "/reverse") {
			t.Errorf("the provider was asked to name a place with a typed address: %v", asked)
		}
	}
}

// One place is read by its id: the caller's own, another person's at history
// level; not below it, and an unknown id is not found.
func TestReadPlaceByID(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40, -75, ago(5*time.Minute)), visitInput("k1", 40, -75, ago(60*time.Minute), ago(30*time.Minute)))
	listed := world.call(t, world.ada, `query { ListPlaces { places { id } } }`)["ListPlaces"].(map[string]any)["places"].([]any)
	if len(listed) != 1 {
		t.Fatalf("one place wanted: %v", listed)
	}
	placeId := listed[0].(map[string]any)["id"].(string)
	asAda := fmt.Sprintf(`placeId: %q, userId: %q`, placeId, world.ada.ID)
	const fields = `{ id placeName visitCount }`

	own := world.call(t, world.ada, fmt.Sprintf(`query { ReadPlace(placeId: %q) %s }`, placeId, fields))["ReadPlace"].(map[string]any)
	if own["id"] != placeId || own["visitCount"] != float64(1) {
		t.Errorf("the caller's own place: %v", own)
	}
	world.refused(t, world.ben, fmt.Sprintf(`query { ReadPlace(%s) %s }`, asAda, fields))
	world.refused(t, world.ben, fmt.Sprintf(`query { ReadPlace(placeId: %q) %s }`, placeId, fields))

	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	world.refused(t, world.ben, fmt.Sprintf(`query { ReadPlace(%s) %s }`, asAda, fields))

	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "history") { id } }`, world.ben.ID))
	seen := world.call(t, world.ben, fmt.Sprintf(`query { ReadPlace(%s) %s }`, asAda, fields))["ReadPlace"].(map[string]any)
	if seen["id"] != placeId {
		t.Errorf("at history, the place: %v", seen)
	}
	world.refused(t, world.ada, `query { ReadPlace(placeId: "no-such-place") `+fields+` }`)
}
