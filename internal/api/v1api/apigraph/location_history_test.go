package apigraph

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// A report is safe to send twice, an arrival and its departure are one stay
// that keeps its arrival and its accuracy, and a stay is at a place that is
// named for now after its city.
func TestAReportIsOneStayWhenSentInPieces(t *testing.T) {
	world := newLocationWorld(t)
	arrival := ago(40 * time.Minute)
	departure := ago(10 * time.Minute)

	first := world.report(t, world.ada, pointInput(40.0005, -75.0003, ago(30*time.Minute)), visitInput("k1", 40.0005, -75.0003, arrival, ""))
	if first["newPointCount"] != float64(1) || first["newVisitCount"] != float64(1) {
		t.Fatalf("the first report: %v", first)
	}
	again := world.report(t, world.ada, pointInput(40.0005, -75.0003, ago(30*time.Minute)), visitInput("k1", 40.0005, -75.0003, arrival, ""))
	if again["newPointCount"] != float64(0) || again["newVisitCount"] != float64(0) {
		t.Errorf("sent again it adds nothing: %v", again)
	}

	// The departure carries no arrival, and an accuracy of zero.
	departureInput := fmt.Sprintf(`{clientVisitKey: "k1", latitudeDegrees: 40.0005, longitudeDegrees: -75.0003, accuracyMeters: 0, departedAt: %q, visitSource: "visit"}`, departure)
	if sent := world.report(t, world.ada, "", departureInput); sent["newVisitCount"] != float64(0) || sent["updatedVisitCount"] != float64(1) {
		t.Errorf("the departure completes the stay: %v", sent)
	}

	data := world.call(t, world.ada, `query { ListLocationVisits { nextOffset placeVisits { `+visitFieldsForTest+` } } }`)
	page := data["ListLocationVisits"].(map[string]any)
	visits := page["placeVisits"].([]any)
	if len(visits) != 1 || page["nextOffset"] != nil {
		t.Fatalf("one stay: %v", page)
	}
	visit := visits[0].(map[string]any)
	if visit["arrivedAt"] == nil || visit["departedAt"] == nil || visit["durationSeconds"] != float64(30*60) || visit["accuracyMeters"] != float64(20) {
		t.Errorf("both times, and the accuracy the arrival gave: %v", visit)
	}

	// Nobody is at the place any more.
	location := world.call(t, world.ada, `query { GetLocation { username locationShareLevel latestPoint { latitudeDegrees } openVisit { id } } }`)["GetLocation"].(map[string]any)
	if location["openVisit"] != nil || location["latestPoint"] == nil || location["locationShareLevel"] != "owner" {
		t.Errorf("where they are: %v", location)
	}
}

// A stay that is still going is reported while the latest position is at the
// place, and not once a later position is somewhere else.
func TestAStayNobodyEndedIsNotForEver(t *testing.T) {
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

// A second stay near the first is at the same place, and a place keeps the
// name and label the person gave it.
func TestStaysAtOnePlaceAndNamingIt(t *testing.T) {
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
		t.Errorf("a forgotten place takes its stays with it: %v", visits)
	}
}

// A check-in ends the stay in progress and starts one, at an existing place or
// a found one.
func TestCheckInEndsTheStayInProgress(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40.0, -75.0, ago(20*time.Minute)), visitInput("k1", 40.0, -75.0, ago(60*time.Minute), ""))
	checked := world.call(t, world.ada, `mutation { CheckIn(latitudeDegrees: 40.0004, longitudeDegrees: -75.0004, placeName: "Corner Cafe", osmType: "node", osmId: 42, osmCategory: "amenity=cafe") { `+visitFieldsForTest+` } }`)["CheckIn"].(map[string]any)
	if checked["visitSource"] != "check_in" || checked["place"].(map[string]any)["placeName"] != "Corner Cafe" {
		t.Fatalf("checked in: %v", checked)
	}
	visits := world.call(t, world.ada, `query { ListLocationVisits { placeVisits { departedAt place { placeName } } } }`)["ListLocationVisits"].(map[string]any)["placeVisits"].([]any)
	if len(visits) != 2 {
		t.Fatalf("two stays: %v", visits)
	}
	// Newest first: the check-in is in progress; the earlier stay ended.
	if visits[0].(map[string]any)["departedAt"] != nil || visits[1].(map[string]any)["departedAt"] == nil {
		t.Errorf("the first stay ended and the check-in is going: %v", visits)
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
	erased := world.call(t, world.ada, `mutation { ForgetLocationHistory { pointCount visitCount placeCount } }`)["ForgetLocationHistory"].(map[string]any)
	if erased["pointCount"] != float64(1) || erased["visitCount"] != float64(1) || erased["placeCount"] != float64(1) {
		t.Errorf("what was deleted: %v", erased)
	}
	world.refused(t, world.ada, fmt.Sprintf(`mutation { ForgetLocationHistory(from: %q, until: %q) { pointCount } }`, ago(0), ago(time.Hour)))
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
	seen := world.call(t, world.ben, fmt.Sprintf(`query { GetLocation(%s) { username locationShareLevel latestPoint { latitudeDegrees } } }`, asAda))["GetLocation"].(map[string]any)
	if seen["username"] != world.ada.Username || seen["locationShareLevel"] != "current" || seen["latestPoint"] == nil {
		t.Errorf("at current, where she is: %v", seen)
	}
	world.refused(t, world.ben, fmt.Sprintf(`query { ListLocationVisits(%s) { nextOffset } }`, asAda))
	world.refused(t, world.ben, fmt.Sprintf(`query { ListLocationPoints(%s, from: %q, until: %q) { latitudeDegrees } }`, asAda, ago(time.Hour), ago(0)))
	world.refused(t, world.cora, fmt.Sprintf(`query { GetLocation(%s) { username } }`, asAda))

	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "history") { id } }`, world.ben.ID))
	if visits := world.call(t, world.ben, fmt.Sprintf(`query { ListLocationVisits(%s) { placeVisits { id } } }`, asAda))["ListLocationVisits"].(map[string]any)["placeVisits"].([]any); len(visits) != 1 {
		t.Errorf("at history, the visits: %v", visits)
	}
	people := world.call(t, world.ben, `query { ListSharedLocations { username locationShareLevel } }`)["ListSharedLocations"].([]any)
	if len(people) != 1 || people[0].(map[string]any)["locationShareLevel"] != "history" {
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

// A person without location:use is refused everything, as not found.
func TestLocationNeedsItsPermission(t *testing.T) {
	world := newLocationWorld(t)
	var nobody *models.User
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if nobody, err = tx.CreateUser(&models.User{Username: "location-nobody"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
	})
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
	message := world.refused(t, world.ada, `query { ListNearbyPlaces(latitudeDegrees: 40, longitudeDegrees: -75) { placeName } }`)
	if !strings.Contains(message, "operator") || !strings.Contains(message, "OpenStreetMap") {
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

	world.call(t, world.ada, `mutation { SavePlace(latitudeDegrees: 40.0001, longitudeDegrees: -75.0001, placeName: "Home") { id } }`)
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

// A viewer is told that a person moved only while that person shares with
// them: the level is read again for each event.
func TestAViewerFollowsOnlyThoseSharingWithThem(t *testing.T) {
	world := newLocationWorld(t)
	ctx := t.Context()
	if !world.graph.canViewerSeeLocationEvent(ctx, world.ada.ID, world.ada.ID) {
		t.Error("everyone follows themselves")
	}
	if world.graph.canViewerSeeLocationEvent(ctx, world.ben.ID, world.ada.ID) {
		t.Error("nothing is shared yet")
	}
	share := world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))["ShareLocation"].(map[string]any)
	if !world.graph.canViewerSeeLocationEvent(ctx, world.ben.ID, world.ada.ID) {
		t.Error("shared at current")
	}
	if world.graph.canViewerSeeLocationEvent(ctx, world.cora.ID, world.ada.ID) {
		t.Error("cora shares no circle with her")
	}
	world.call(t, world.ada, fmt.Sprintf(`mutation { UnshareLocation(shareId: %q) }`, share["id"]))
	if world.graph.canViewerSeeLocationEvent(ctx, world.ben.ID, world.ada.ID) {
		t.Error("no longer shared, no longer followed")
	}
}
