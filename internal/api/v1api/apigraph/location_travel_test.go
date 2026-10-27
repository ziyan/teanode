package apigraph

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

func activityInput(activityKind string, startedAt string) string {
	return fmt.Sprintf(`{startedAt: %q, activityKind: %q, activityConfidence: "high"}`, startedAt, activityKind)
}

const tripFieldsForTest = `id startedAt endedAt durationSeconds distanceMeters legs { id distanceMeters travelMode detectedTravelMode isTravelModeCorrected detectionSource }`

// travelDay reports a morning: home until minute 60, a walk of about 600
// meters, a drive to the office, which is reached at minute 90.
func travelDay(t *testing.T, world *locationWorld, at func(minutes float64) string) {
	t.Helper()
	var points []string
	for minute := 60; minute <= 90; minute += 2 {
		// A walk of about 600 meters to minute 68, then a drive.
		latitude := 40.0054 + (40.05-40.0054)*float64(minute-68)/22
		if minute <= 68 {
			latitude = 40 + 0.0054*float64(minute-60)/8
		}
		points = append(points, pointInput(latitude, -75, at(float64(minute))))
	}
	visits := visitInput("home", 40, -75, at(0), at(60)) + "," + visitInput("office", 40.05, -75, at(90), "")
	activities := strings.Join([]string{
		activityInput("stationary", at(0)), activityInput("walking", at(60)), activityInput("automotive", at(68)), activityInput("stationary", at(90)),
	}, ",")
	world.call(t, world.ada, fmt.Sprintf(`mutation { ReportLocation(points: [%s], visits: [%s], activities: [%s]) { newActivityCount } }`,
		strings.Join(points, ","), visits, activities))
}

func (self *locationWorld) tripsOf(t *testing.T, caller string, userId string, from, until string) []map[string]any {
	t.Helper()
	user := self.ada
	if caller == "ben" {
		user = self.ben
	}
	argument := ""
	if userId != "" {
		argument = fmt.Sprintf(`userId: %q, `, userId)
	}
	page := self.call(t, user, fmt.Sprintf(`query { ListTrips(%sfrom: %q, until: %q) { trips { %s } nextOffset } }`, argument, from, until, tripFieldsForTest))["ListTrips"].(map[string]any)
	trips := []map[string]any{}
	for _, each := range page["trips"].([]any) {
		trips = append(trips, each.(map[string]any))
	}
	return trips
}

func legsOf(trip map[string]any) []map[string]any {
	legs := []map[string]any{}
	for _, each := range trip["legs"].([]any) {
		legs = append(legs, each.(map[string]any))
	}
	return legs
}

// A morning reported with motion activity is one trip of a walk then a car;
// a correction to bus holds when the day is reported again and the trip is
// built again; the week's totals count it as bus; erasing the day takes the
// trip away.
func TestATripIsBuiltFromAReportAndKeepsItsCorrection(t *testing.T) {
	world := newLocationWorld(t)
	base := time.Now().Add(-6 * time.Hour).UTC().Truncate(time.Second)
	at := func(minutes float64) string {
		return base.Add(time.Duration(minutes * float64(time.Minute))).Format(time.RFC3339)
	}
	travelDay(t, world, at)

	trips := world.tripsOf(t, "ada", "", at(-60), at(180))
	if len(trips) != 1 {
		t.Fatalf("one trip, not %d: %v", len(trips), trips)
	}
	legs := legsOf(trips[0])
	if len(legs) != 2 || legs[0]["travelMode"] != "walk" || legs[1]["travelMode"] != "car" || legs[1]["detectionSource"] != "motion" {
		t.Fatalf("a walk then a car: %v", legs)
	}
	if distance := trips[0]["distanceMeters"].(float64); distance < 5000 || distance > 6000 {
		t.Errorf("the trip is about 5.5 km, not %v", distance)
	}

	corrected := world.call(t, world.ada, fmt.Sprintf(`mutation { SetTripLegTravelMode(tripLegId: %q, travelMode: "bus") { travelMode detectedTravelMode isTravelModeCorrected } }`, legs[1]["id"]))["SetTripLegTravelMode"].(map[string]any)
	if corrected["travelMode"] != "bus" || corrected["detectedTravelMode"] != "car" || corrected["isTravelModeCorrected"] != true {
		t.Fatalf("the correction: %v", corrected)
	}

	// The same day again, with one more position: the trip is built again.
	world.report(t, world.ada, pointInput(40.03, -75, at(81)), "")
	legs = legsOf(world.tripsOf(t, "ada", "", at(-60), at(180))[0])
	if legs[1]["travelMode"] != "bus" || legs[1]["isTravelModeCorrected"] != true {
		t.Errorf("the correction is kept when the trip is built again: %v", legs)
	}

	totals := world.call(t, world.ada, fmt.Sprintf(`query { ListTravelTotals(period: "week", from: %q, until: %q) { periodStartDay travelMode distanceMeters legCount } }`, at(-60), at(180)))["ListTravelTotals"].([]any)
	modes := map[string]float64{}
	for _, each := range totals {
		total := each.(map[string]any)
		modes[total["travelMode"].(string)] += total["distanceMeters"].(float64)
	}
	if modes["bus"] < 4000 || modes["walk"] < 300 || modes["car"] != 0 {
		t.Errorf("the week's totals count the bus: %v", modes)
	}

	// Taking the correction back goes to the detected way.
	reset := world.call(t, world.ada, fmt.Sprintf(`mutation { SetTripLegTravelMode(tripLegId: %q) { travelMode isTravelModeCorrected } }`, legs[1]["id"]))["SetTripLegTravelMode"].(map[string]any)
	if reset["travelMode"] != "car" || reset["isTravelModeCorrected"] != false {
		t.Errorf("as detected again: %v", reset)
	}

	world.call(t, world.ada, fmt.Sprintf(`mutation { EraseLocationHistory(from: %q, until: %q) { pointCount } }`, at(-60), at(180)))
	if trips := world.tripsOf(t, "ada", "", at(-60), at(180)); len(trips) != 0 {
		t.Errorf("erasing the day erases its trips: %v", trips)
	}
}

// Trips are seen at the history level and corrected only by the person.
func TestTripsAreSeenWithHistoryAndCorrectedOnlyByThePerson(t *testing.T) {
	world := newLocationWorld(t)
	base := time.Now().Add(-6 * time.Hour).UTC().Truncate(time.Second)
	at := func(minutes float64) string {
		return base.Add(time.Duration(minutes * float64(time.Minute))).Format(time.RFC3339)
	}
	travelDay(t, world, at)

	query := fmt.Sprintf(`query { ListTrips(userId: %q, from: %q, until: %q) { trips { id } } }`, world.ada.ID, at(-60), at(180))
	world.refused(t, world.ben, query)
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	world.refused(t, world.ben, query)
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "history") { id } }`, world.ben.ID))
	trips := world.tripsOf(t, "ben", world.ada.ID, at(-60), at(180))
	if len(trips) != 1 {
		t.Fatalf("history sees the trip: %v", trips)
	}
	world.refused(t, world.ben, fmt.Sprintf(`mutation { SetTripLegTravelMode(tripLegId: %q, travelMode: "train") { id } }`, legsOf(trips[0])[0]["id"]))
}

// A report's motion activity must be one of the kinds and confidences, and a
// way of travel one of the eight.
func TestTravelRefusesUnknownKindsAndWays(t *testing.T) {
	world := newLocationWorld(t)
	refusal := world.refused(t, world.ada, fmt.Sprintf(`mutation { ReportLocation(points: [], visits: [], activities: [%s]) { newActivityCount } }`, activityInput("flying", ago(time.Minute))))
	if !strings.Contains(refusal, "kind") {
		t.Errorf("the refusal says why: %q", refusal)
	}
	base := time.Now().Add(-6 * time.Hour).UTC().Truncate(time.Second)
	at := func(minutes float64) string {
		return base.Add(time.Duration(minutes * float64(time.Minute))).Format(time.RFC3339)
	}
	travelDay(t, world, at)
	leg := legsOf(world.tripsOf(t, "ada", "", at(-60), at(180))[0])[0]
	refusal = world.refused(t, world.ada, fmt.Sprintf(`mutation { SetTripLegTravelMode(tripLegId: %q, travelMode: "rocket") { id } }`, leg["id"]))
	if !strings.Contains(refusal, "walk, run, bike") {
		t.Errorf("the refusal names the ways: %q", refusal)
	}
	world.refused(t, world.ada, fmt.Sprintf(`query { ListTravelTotals(period: "fortnight", from: %q, until: %q) { travelMode } }`, at(-60), at(180)))
}

// threeVisitDay reports home, a stop and an office along one road, 2 and 3
// kilometers apart: two trips.
func threeVisitDay(t *testing.T, world *locationWorld, at func(minutes float64) string) {
	t.Helper()
	visits := strings.Join([]string{
		visitInput("home", 40, -75, at(0), at(60)),
		visitInput("stop", 40.02, -75, at(90), at(120)),
		visitInput("office", 40.05, -75, at(150), ""),
	}, ",")
	world.report(t, world.ada, pointInput(40.01, -75, at(75))+","+pointInput(40.02, -75, at(105))+","+pointInput(40.035, -75, at(135)), visits)
}

func tripsWithPlaces(t *testing.T, world *locationWorld, from, until string) []map[string]any {
	t.Helper()
	page := world.call(t, world.ada, fmt.Sprintf(`query { ListTrips(from: %q, until: %q) { trips { id startedAt fromPlace { id } toPlace { id } } } }`, from, until))["ListTrips"].(map[string]any)
	trips := []map[string]any{}
	for _, each := range page["trips"].([]any) {
		trips = append(trips, each.(map[string]any))
	}
	return trips
}

// A person the trips are shared with sees the places at the ends as a list of
// visits shows them, without the owner's own records of them.
func TestTripPlacesAreReducedForSomeoneTheyAreSharedWith(t *testing.T) {
	world := newLocationWorld(t)
	base := time.Now().Add(-6 * time.Hour).UTC().Truncate(time.Second)
	at := func(minutes float64) string {
		return base.Add(time.Duration(minutes * float64(time.Minute))).Format(time.RFC3339)
	}
	travelDay(t, world, at)
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "history") { id } }`, world.ben.ID))
	query := `fromPlace { placeName radiusMeters createdAt } toPlace { placeName radiusMeters createdAt }`
	read := func(user *models.User, argument string) map[string]any {
		page := world.call(t, user, fmt.Sprintf(`query { ListTrips(%sfrom: %q, until: %q) { trips { %s } } }`, argument, at(-60), at(180), query))["ListTrips"].(map[string]any)
		trips := page["trips"].([]any)
		if len(trips) != 1 {
			t.Fatalf("one trip: %v", trips)
		}
		return trips[0].(map[string]any)["fromPlace"].(map[string]any)
	}
	own := read(world.ada, "")
	if own["radiusMeters"].(float64) <= 0 {
		t.Fatalf("the owner sees the radius: %v", own)
	}
	viewed := read(world.ben, fmt.Sprintf(`userId: %q, `, world.ada.ID))
	if viewed["placeName"] == "" || viewed["radiusMeters"].(float64) != 0 {
		t.Errorf("a viewer sees the name and not the radius: %v", viewed)
	}
}

// A leg id the person was shown still names the leg after another report
// builds the day again.
func TestALegIdSurvivesAnotherReportOfTheSameDay(t *testing.T) {
	world := newLocationWorld(t)
	base := time.Now().Add(-6 * time.Hour).UTC().Truncate(time.Second)
	at := func(minutes float64) string {
		return base.Add(time.Duration(minutes * float64(time.Minute))).Format(time.RFC3339)
	}
	travelDay(t, world, at)
	trip := world.tripsOf(t, "ada", "", at(-60), at(180))[0]
	shown := legsOf(trip)[1]["id"]

	world.report(t, world.ada, pointInput(40.03, -75, at(81)), "")
	trips := world.tripsOf(t, "ada", "", at(-60), at(180))
	if len(trips) != 1 || trips[0]["id"] != trip["id"] || legsOf(trips[0])[1]["id"] != shown {
		t.Fatalf("the trip and its legs keep their ids: %v", trips)
	}
	corrected := world.call(t, world.ada, fmt.Sprintf(`mutation { SetTripLegTravelMode(tripLegId: %q, travelMode: "bus") { travelMode } }`, shown))["SetTripLegTravelMode"].(map[string]any)
	if corrected["travelMode"] != "bus" {
		t.Errorf("the leg the person was shown is corrected: %v", corrected)
	}
}

// Forgetting a place joins the visits beside it by a trip of their own.
func TestForgettingAPlaceBuildsTheTripBetweenItsNeighbors(t *testing.T) {
	world := newLocationWorld(t)
	base := time.Now().Add(-6 * time.Hour).UTC().Truncate(time.Second)
	at := func(minutes float64) string {
		return base.Add(time.Duration(minutes * float64(time.Minute))).Format(time.RFC3339)
	}
	threeVisitDay(t, world, at)
	trips := tripsWithPlaces(t, world, at(-60), at(300))
	if len(trips) != 2 {
		t.Fatalf("two trips: %v", trips)
	}
	// Newest first: the second trip ends at the office and begins at the stop.
	stopId := trips[0]["fromPlace"].(map[string]any)["id"]
	world.call(t, world.ada, fmt.Sprintf(`mutation { ForgetPlace(placeId: %q) }`, stopId))
	trips = tripsWithPlaces(t, world, at(-60), at(300))
	if len(trips) != 1 || trips[0]["fromPlace"] == nil || trips[0]["toPlace"] == nil {
		t.Fatalf("one trip, from home to the office, with both places: %v", trips)
	}
}

// Erasing the middle of a day takes the trips through it, and builds none
// across it: with its positions gone, nothing says how the person went.
func TestErasingTheMiddleOfADayGuessesNoTripAcrossIt(t *testing.T) {
	world := newLocationWorld(t)
	base := time.Now().Add(-6 * time.Hour).UTC().Truncate(time.Second)
	at := func(minutes float64) string {
		return base.Add(time.Duration(minutes * float64(time.Minute))).Format(time.RFC3339)
	}
	threeVisitDay(t, world, at)
	world.call(t, world.ada, fmt.Sprintf(`mutation { EraseLocationHistory(from: %q, until: %q) { visitCount } }`, at(80), at(130)))
	trips := tripsWithPlaces(t, world, at(-60), at(300))
	if len(trips) != 0 {
		t.Fatalf("no trip across the erased hour: %v", trips)
	}
}

// A time more than 400 days back is refused, so that one report cannot build
// the trips of years.
func TestAReportOfTimesLongAgoIsRefused(t *testing.T) {
	world := newLocationWorld(t)
	longAgo := time.Now().AddDate(-1, -2, 0).UTC().Format(time.RFC3339)
	for name, mutation := range map[string]string{
		"position": fmt.Sprintf(`mutation { ReportLocation(points: [%s], visits: []) { newPointCount } }`, pointInput(40, -75, "1970-01-02T00:00:00Z")),
		"visit":    fmt.Sprintf(`mutation { ReportLocation(points: [], visits: [%s]) { newPointCount } }`, visitInput("old", 40, -75, longAgo, "")),
		"activity": fmt.Sprintf(`mutation { ReportLocation(points: [], visits: [], activities: [%s]) { newPointCount } }`, activityInput("walking", longAgo)),
	} {
		if refusal := world.refused(t, world.ada, mutation); !strings.Contains(refusal, "400 days") {
			t.Errorf("%s: the refusal says why: %q", name, refusal)
		}
	}
}

// A report of two days a long way apart builds the trips of both.
func TestAReportOfTwoDistantDaysBuildsBothDaysTrips(t *testing.T) {
	world := newLocationWorld(t)
	now := time.Now().UTC().Truncate(time.Second)
	format := func(base time.Time, minutes float64) string {
		return base.Add(time.Duration(minutes * float64(time.Minute))).Format(time.RFC3339)
	}
	oldBase, newBase := now.Add(-100*24*time.Hour), now.Add(-6*time.Hour)
	visits := strings.Join([]string{
		visitInput("old-home", 40, -75, format(oldBase, 0), format(oldBase, 60)),
		visitInput("old-office", 40.05, -75, format(oldBase, 90), format(oldBase, 150)),
		visitInput("new-home", 40, -75, format(newBase, 0), format(newBase, 60)),
		visitInput("new-office", 40.05, -75, format(newBase, 90), ""),
	}, ",")
	world.report(t, world.ada, "", visits)
	if trips := world.tripsOf(t, "ada", "", format(oldBase, -60), format(oldBase, 300)); len(trips) != 1 {
		t.Errorf("the old day has its trip: %v", trips)
	}
	if trips := world.tripsOf(t, "ada", "", format(newBase, -60), format(newBase, 300)); len(trips) != 1 {
		t.Errorf("the new day has its trip: %v", trips)
	}
}
