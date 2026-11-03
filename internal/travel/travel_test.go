package travel

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/place"
)

var morning = time.Date(2030, 3, 4, 8, 0, 0, 0, time.UTC)

// metersPerDegree is the length of a degree of latitude, which the tests walk
// along a meridian to know how far they went.
const metersPerDegree = 111194.9

func at(minutes float64) time.Time {
	return morning.Add(time.Duration(minutes * float64(time.Minute)))
}

func pointer(moment time.Time) *time.Time { return &moment }

// northOf is a latitude the given meters north of the tests' starting point.
func northOf(meters float64) float64 { return 10 + meters/metersPerDegree }

// visits are a visit that ends when the trip starts and one that begins when
// it ends, the second the given meters north of the first.
func visits(departureMinutes, arrivalMinutes, meters float64) []Visit {
	return []Visit{
		{VisitID: "home", ArrivedAt: pointer(at(-600)), DepartedAt: pointer(at(departureMinutes)), LatitudeDegrees: northOf(0), LongitudeDegrees: 20},
		{VisitID: "office", ArrivedAt: pointer(at(arrivalMinutes)), LatitudeDegrees: northOf(meters), LongitudeDegrees: 20},
	}
}

// along is a position every step minutes on a way that goes the given meters
// from the start to the end of the interval, at an even speed.
func along(startMinutes, endMinutes, stepMinutes, startMeters, endMeters float64) []Position {
	var positions []Position
	for minutes := startMinutes; minutes <= endMinutes+1e-9; minutes += stepMinutes {
		share := (minutes - startMinutes) / (endMinutes - startMinutes)
		positions = append(positions, Position{
			MeasuredAt:       at(minutes),
			LatitudeDegrees:  northOf(startMeters + share*(endMeters-startMeters)),
			LongitudeDegrees: 20,
			AccuracyMeters:   10,
		})
	}
	return positions
}

func pathLength(positions []Position, visitList []Visit) float64 {
	total := 0.0
	latitude, longitude := visitList[0].LatitudeDegrees, visitList[0].LongitudeDegrees
	for _, position := range positions {
		total += place.DistanceMeters(latitude, longitude, position.LatitudeDegrees, position.LongitudeDegrees)
		latitude, longitude = position.LatitudeDegrees, position.LongitudeDegrees
	}
	return total + place.DistanceMeters(latitude, longitude, visitList[1].LatitudeDegrees, visitList[1].LongitudeDegrees)
}

func onlyTrip(t *testing.T, trips []Trip) Trip {
	t.Helper()
	if len(trips) != 1 {
		t.Fatalf("got %d trips, want 1: %+v", len(trips), trips)
	}
	return trips[0]
}

func walkThenCarPositions() []Position {
	return append(along(0, 15, 1, 0, 1000), along(16, 25, 1, 1000+10000.0/10, 11000)...)
}

func TestWalkThenCarWithActivity(t *testing.T) {
	visitList := visits(0, 25, 11000)
	positions := walkThenCarPositions()
	activities := []MotionActivity{
		{StartedAt: at(0), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(15), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(25), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
	}
	trip := onlyTrip(t, BuildTrips(visitList, positions, activities))
	if trip.FromVisitID != "home" || trip.ToVisitID != "office" {
		t.Fatalf("visits %q to %q", trip.FromVisitID, trip.ToVisitID)
	}
	if len(trip.Legs) != 2 {
		t.Fatalf("got %d legs, want 2: %+v", len(trip.Legs), trip.Legs)
	}
	walk, car := trip.Legs[0], trip.Legs[1]
	if walk.DetectedTravelMode != TravelModeWalk || car.DetectedTravelMode != TravelModeCar {
		t.Fatalf("modes %q then %q", walk.DetectedTravelMode, car.DetectedTravelMode)
	}
	if walk.DetectionSource != DetectionSourceMotion || car.DetectionSource != DetectionSourceMotion {
		t.Fatalf("sources %q and %q", walk.DetectionSource, car.DetectionSource)
	}
	if !walk.StartedAt.Equal(trip.StartedAt) || !walk.EndedAt.Equal(car.StartedAt) || !car.EndedAt.Equal(trip.EndedAt) {
		t.Fatalf("legs do not cover the trip: %+v", trip.Legs)
	}
	if math.Abs(walk.DistanceMeters+car.DistanceMeters-trip.DistanceMeters) > 1 {
		t.Fatalf("legs add up to %.2f, trip is %.2f", walk.DistanceMeters+car.DistanceMeters, trip.DistanceMeters)
	}
	if want := pathLength(positions, visitList); math.Abs(trip.DistanceMeters-want) > 1 {
		t.Fatalf("trip is %.2f meters, path is %.2f", trip.DistanceMeters, want)
	}
	if math.Abs(walk.DistanceMeters-1000) > 5 {
		t.Fatalf("walk is %.2f meters, want about 1000", walk.DistanceMeters)
	}
}

func TestWithoutActivityIsOneCarLegFromSpeed(t *testing.T) {
	trip := onlyTrip(t, BuildTrips(visits(0, 25, 11000), walkThenCarPositions(), nil))
	if len(trip.Legs) != 1 {
		t.Fatalf("got %d legs, want 1", len(trip.Legs))
	}
	leg := trip.Legs[0]
	if leg.DetectedTravelMode != TravelModeCar || leg.DetectionSource != DetectionSourceSpeed {
		t.Fatalf("leg is %q from %q", leg.DetectedTravelMode, leg.DetectionSource)
	}
	if leg.DistanceMeters != trip.DistanceMeters {
		t.Fatalf("leg is %.2f meters, trip is %.2f", leg.DistanceMeters, trip.DistanceMeters)
	}
}

func TestPatchyActivityFallsBackToSpeed(t *testing.T) {
	activities := []MotionActivity{
		{StartedAt: at(0), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(5), ActivityKind: ActivityKindUnknown, ActivityConfidence: ActivityConfidenceLow},
	}
	trip := onlyTrip(t, BuildTrips(visits(0, 25, 11000), walkThenCarPositions(), activities))
	if len(trip.Legs) != 1 || trip.Legs[0].DetectionSource != DetectionSourceSpeed || trip.Legs[0].DetectedTravelMode != TravelModeCar {
		t.Fatalf("legs %+v", trip.Legs)
	}
}

func TestSlowShortTripIsWalk(t *testing.T) {
	trip := onlyTrip(t, BuildTrips(visits(0, 10, 300), nil, nil))
	if len(trip.Legs) != 1 || trip.Legs[0].DetectedTravelMode != TravelModeWalk {
		t.Fatalf("legs %+v", trip.Legs)
	}
}

// Four meters a second is as likely a slow drive as a ride: without motion
// activity it is a car, for the person to correct.
func TestFourMetersPerSecondWithoutActivityIsCar(t *testing.T) {
	trip := onlyTrip(t, BuildTrips(visits(0, 10, 2400), nil, nil))
	if trip.Legs[0].DetectedTravelMode != TravelModeCar {
		t.Fatalf("mode %q", trip.Legs[0].DetectedTravelMode)
	}
	activities := []MotionActivity{{StartedAt: at(0), ActivityKind: ActivityKindCycling, ActivityConfidence: ActivityConfidenceHigh}}
	trip = onlyTrip(t, BuildTrips(visits(0, 10, 2400), nil, activities))
	if trip.Legs[0].DetectedTravelMode != TravelModeBike {
		t.Fatalf("with cycling activity, mode %q", trip.Legs[0].DetectedTravelMode)
	}
}

// One visit ending the second the next begins, at a neighboring place, is no
// trip: a few hundred meters in a second is no flight.
func TestVisitsASecondApartMakeNoTrip(t *testing.T) {
	visitList := visits(0, 1.0/60, 800)
	if trips := BuildTrips(visitList, nil, nil); len(trips) != 0 {
		t.Fatalf("a second apart: %+v", trips)
	}
}

// Faster than a car over less than a flight's distance is visit times that
// are off, not a plane.
func TestFasterThanACarOverAShortWayIsNoTrip(t *testing.T) {
	if trips := BuildTrips(visits(0, 2, 20000), nil, nil); len(trips) != 0 {
		t.Fatalf("20 kilometers in two minutes: %+v", trips)
	}
}

func TestSixHundredKilometersInAnHourIsPlane(t *testing.T) {
	activities := []MotionActivity{{StartedAt: at(0), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh}}
	for _, activityList := range [][]MotionActivity{nil, activities} {
		trip := onlyTrip(t, BuildTrips(visits(0, 60, 600000), nil, activityList))
		if len(trip.Legs) != 1 || trip.Legs[0].DetectedTravelMode != TravelModePlane {
			t.Fatalf("legs %+v", trip.Legs)
		}
	}
}

func TestLongCarTripBelowPlaneSpeedStaysCar(t *testing.T) {
	activities := []MotionActivity{{StartedAt: at(0), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh}}
	trip := onlyTrip(t, BuildTrips(visits(0, 120, 200000), nil, activities))
	if trip.Legs[0].DetectedTravelMode != TravelModeCar {
		t.Fatalf("mode %q", trip.Legs[0].DetectedTravelMode)
	}
}

func TestOutlierPositionIsDropped(t *testing.T) {
	visitList := visits(0, 20, 15000)
	positions := along(0, 20, 1, 0, 15000)
	clean := onlyTrip(t, BuildTrips(visitList, positions, nil))
	jumped := append([]Position(nil), positions...)
	jumped[10].LatitudeDegrees += 1
	trip := onlyTrip(t, BuildTrips(visitList, jumped, nil))
	if math.Abs(trip.DistanceMeters-clean.DistanceMeters) > 0.001 {
		t.Fatalf("distance %.2f with the jump, %.2f without", trip.DistanceMeters, clean.DistanceMeters)
	}
}

func TestInaccuratePositionIsDroppedWhenBetterOnesExist(t *testing.T) {
	visitList := visits(0, 20, 15000)
	positions := along(0, 20, 1, 0, 15000)
	clean := onlyTrip(t, BuildTrips(visitList, positions, nil))
	positions[5].LatitudeDegrees += 300 / metersPerDegree
	positions[5].AccuracyMeters = 800
	trip := onlyTrip(t, BuildTrips(visitList, positions, nil))
	if math.Abs(trip.DistanceMeters-clean.DistanceMeters) > 0.001 {
		t.Fatalf("distance %.2f, want %.2f", trip.DistanceMeters, clean.DistanceMeters)
	}
}

func TestShortPathIsNotATrip(t *testing.T) {
	if trips := BuildTrips(visits(0, 10, 100), nil, nil); len(trips) != 0 {
		t.Fatalf("got %+v", trips)
	}
}

func TestShortCyclingLegInCarTripIsMergedIntoCar(t *testing.T) {
	positions := along(0, 20, 0.5, 0, 15000)
	activities := []MotionActivity{
		{StartedAt: at(0), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(10), ActivityKind: ActivityKindCycling, ActivityConfidence: ActivityConfidenceLow},
		{StartedAt: at(10.5), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh},
	}
	trip := onlyTrip(t, BuildTrips(visits(0, 20, 15000), positions, activities))
	if len(trip.Legs) != 1 || trip.Legs[0].DetectedTravelMode != TravelModeCar {
		t.Fatalf("legs %+v", trip.Legs)
	}
	if math.Abs(trip.Legs[0].DistanceMeters-trip.DistanceMeters) > 0.001 {
		t.Fatalf("leg %.2f, trip %.2f", trip.Legs[0].DistanceMeters, trip.DistanceMeters)
	}
}

func TestStationaryBetweenWaysMakesNoLeg(t *testing.T) {
	positions := along(0, 30, 1, 0, 3000)
	activities := []MotionActivity{
		{StartedAt: at(0), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(10), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(14), ActivityKind: ActivityKindCycling, ActivityConfidence: ActivityConfidenceHigh},
	}
	trip := onlyTrip(t, BuildTrips(visits(0, 30, 3000), positions, activities))
	if len(trip.Legs) != 2 || trip.Legs[0].DetectedTravelMode != TravelModeWalk || trip.Legs[1].DetectedTravelMode != TravelModeBike {
		t.Fatalf("legs %+v", trip.Legs)
	}
	if !trip.Legs[0].EndedAt.Equal(trip.Legs[1].StartedAt) {
		t.Fatalf("legs do not touch: %+v", trip.Legs)
	}
}

func TestVisitsWithoutDepartureOrArrivalMakeNoTrip(t *testing.T) {
	visitList := visits(0, 20, 5000)
	visitList[0].DepartedAt = nil
	if trips := BuildTrips(visitList, along(0, 20, 1, 0, 5000), nil); len(trips) != 0 {
		t.Fatalf("no departure: %+v", trips)
	}
	visitList = visits(0, 20, 5000)
	visitList[1].ArrivedAt = nil
	if trips := BuildTrips(visitList, along(0, 20, 1, 0, 5000), nil); len(trips) != 0 {
		t.Fatalf("no arrival: %+v", trips)
	}
	visitList = visits(20, 20, 5000)
	if trips := BuildTrips(visitList, nil, nil); len(trips) != 0 {
		t.Fatalf("arrival not after departure: %+v", trips)
	}
}

func TestUnsortedInputsGiveTheSameResult(t *testing.T) {
	visitList := append(visits(0, 25, 11000), Visit{
		VisitID: "shop", ArrivedAt: pointer(at(40)), DepartedAt: pointer(at(60)), LatitudeDegrees: northOf(11000), LongitudeDegrees: 20,
	})
	positions := walkThenCarPositions()
	activities := []MotionActivity{
		{StartedAt: at(0), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(15), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(25), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
	}
	want := BuildTrips(visitList, positions, activities)
	if len(want) != 1 || len(want[0].Legs) != 2 {
		t.Fatalf("sorted result %+v", want)
	}
	random := rand.New(rand.NewSource(1))
	shuffledVisits := append([]Visit(nil), visitList...)
	random.Shuffle(len(shuffledVisits), func(first, second int) {
		shuffledVisits[first], shuffledVisits[second] = shuffledVisits[second], shuffledVisits[first]
	})
	shuffledPositions := append([]Position(nil), positions...)
	random.Shuffle(len(shuffledPositions), func(first, second int) {
		shuffledPositions[first], shuffledPositions[second] = shuffledPositions[second], shuffledPositions[first]
	})
	shuffledActivities := append([]MotionActivity(nil), activities...)
	random.Shuffle(len(shuffledActivities), func(first, second int) {
		shuffledActivities[first], shuffledActivities[second] = shuffledActivities[second], shuffledActivities[first]
	})
	got := BuildTrips(shuffledVisits, shuffledPositions, shuffledActivities)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestModes(t *testing.T) {
	if len(TravelModes) != 8 || TravelModes[0] != TravelModeWalk || TravelModes[7] != TravelModePlane {
		t.Fatalf("modes %v", TravelModes)
	}
	for _, mode := range TravelModes {
		if !IsTravelMode(string(mode)) {
			t.Fatalf("%q is not a mode", mode)
		}
	}
	if IsTravelMode("") || IsTravelMode("rocket") {
		t.Fatal("accepted a way that does not exist")
	}
	for kind, want := range map[string]TravelMode{ActivityKindWalking: TravelModeWalk, ActivityKindRunning: TravelModeRun, ActivityKindCycling: TravelModeBike, ActivityKindAutomotive: TravelModeCar} {
		if got, isMode := TravelModeOfActivityKind(kind); !isMode || got != want {
			t.Fatalf("%s gave %q, %v", kind, got, isMode)
		}
	}
	for _, kind := range []string{ActivityKindStationary, ActivityKindUnknown, "flying"} {
		if _, isMode := TravelModeOfActivityKind(kind); isMode {
			t.Fatalf("%s should be no way", kind)
		}
	}
	if !IsActivityKind("unknown") || IsActivityKind("flying") || !IsActivityConfidence("medium") || IsActivityConfidence("certain") {
		t.Fatal("kind or confidence check is wrong")
	}
}

func TestVisitsMoreThanThirtyDaysApartMakeNoTrip(t *testing.T) {
	const thirtyDaysMinutes = 30 * 24 * 60
	// Motion activity says the person walked all the way, so the trip does
	// not hang on positions.
	activities := []MotionActivity{{StartedAt: at(0), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh}}
	if trips := BuildTrips(visits(0, thirtyDaysMinutes+1, 5000), nil, activities); len(trips) != 0 {
		t.Fatalf("a gap of over thirty days: %+v", trips)
	}
	if trips := BuildTrips(visits(0, thirtyDaysMinutes, 5000), nil, activities); len(trips) != 1 {
		t.Fatalf("a gap of thirty days is still a trip: %+v", trips)
	}
}

// Recording paused at one place in the morning and resumed at another in the
// afternoon: the phone said nothing in between, so how the person went is not
// known, and the afternoon is not a slow walk.
func TestLongSilenceGuessedFromSpeedIsNoTrip(t *testing.T) {
	if trips := BuildTrips(visits(0, 510, 12000), nil, nil); len(trips) != 0 {
		t.Fatalf("eight and a half hours with no position: %+v", trips)
	}
	// Positions for the first stretch, then silence.
	if trips := BuildTrips(visits(0, 510, 12000), along(1, 20, 1, 100, 3000), nil); len(trips) != 0 {
		t.Fatalf("a silence after the positions: %+v", trips)
	}
	// Positions every 15 minutes are no silence.
	trip := onlyTrip(t, BuildTrips(visits(0, 60, 12000), along(15, 45, 15, 3000, 9000), nil))
	if trip.Legs[0].DetectedTravelMode != TravelModeCar {
		t.Fatalf("mode %q", trip.Legs[0].DetectedTravelMode)
	}
}

// A flight is the one way a phone is often silent through: the silence is
// kept when it was a plane, even when the rest of the trip was slow.
func TestSilentFlightIsPlane(t *testing.T) {
	positions := append(along(0, 60, 5, 0, 30000), along(240, 300, 5, 1030000, 1050000)...)
	trip := onlyTrip(t, BuildTrips(visits(-120, 420, 1050000), positions, nil))
	if len(trip.Legs) != 1 || trip.Legs[0].DetectedTravelMode != TravelModePlane {
		t.Fatalf("legs %+v", trip.Legs)
	}
}

// iOS reports a fast train as driving. At 70 meters a second over 250
// kilometers it stays a car, for the person to correct, not a plane.
func TestFastTrainReportedAsDrivingIsNotPlane(t *testing.T) {
	activities := []MotionActivity{{StartedAt: at(0), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh}}
	minutes := 250000.0 / 70 / 60
	trip := onlyTrip(t, BuildTrips(visits(0, minutes, 250000), along(0, minutes, 5, 0, 250000), activities))
	if len(trip.Legs) != 1 || trip.Legs[0].DetectedTravelMode != TravelModeCar {
		t.Fatalf("legs %+v", trip.Legs)
	}
}

// touchingVisits are a visit that iOS ended the moment the next began, at the
// given minutes, and the next the given meters north of it.
func touchingVisits(meetMinutes, meters float64) []Visit {
	return []Visit{
		{VisitID: "home", ArrivedAt: pointer(at(-600)), DepartedAt: pointer(at(meetMinutes)), LatitudeDegrees: northOf(0), LongitudeDegrees: 20},
		{VisitID: "park", ArrivedAt: pointer(at(meetMinutes)), DepartedAt: pointer(at(meetMinutes + 300)), LatitudeDegrees: northOf(meters), LongitudeDegrees: 20},
	}
}

// iOS told of leaving home only when it told of reaching the park, so the
// visits leave no time between them; the walk is found in the motion
// activity around that moment.
func TestTripFoundInMotionWhenVisitsLeaveNoTime(t *testing.T) {
	activities := []MotionActivity{
		{StartedAt: at(-30), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(0), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(7), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
	}
	trip := onlyTrip(t, BuildTrips(touchingVisits(8, 450), nil, activities))
	if trip.FromVisitID != "home" || trip.ToVisitID != "park" || !trip.StartedAt.Equal(at(0)) || !trip.EndedAt.Equal(at(7)) {
		t.Fatalf("trip: %+v", trip)
	}
	if len(trip.Legs) != 1 || trip.Legs[0].DetectedTravelMode != TravelModeWalk || trip.Legs[0].DetectionSource != DetectionSourceMotion {
		t.Fatalf("legs: %+v", trip.Legs)
	}
}

// A stop at a crossing between a walk and a drive does not end the trip, and
// the drive's way is kept beside the walk's.
func TestStopOnTheWayStaysInTheTrip(t *testing.T) {
	activities := []MotionActivity{
		{StartedAt: at(0), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(3), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceMedium},
		{StartedAt: at(5), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(20), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
	}
	trip := onlyTrip(t, BuildTrips(touchingVisits(22, 12000), nil, activities))
	if !trip.StartedAt.Equal(at(0)) || !trip.EndedAt.Equal(at(20)) {
		t.Fatalf("trip: %+v", trip)
	}
	if len(trip.Legs) != 2 || trip.Legs[0].DetectedTravelMode != TravelModeWalk || trip.Legs[1].DetectedTravelMode != TravelModeCar {
		t.Fatalf("legs: %+v", trip.Legs)
	}
}

// iOS told of leaving the park late, a second before reaching home; the
// person was seen at the park, then on the way, so the trip runs from the
// last position at the park to the arrival.
func TestTripFoundInPositionsWhenTheDepartureWasLate(t *testing.T) {
	visitList := []Visit{
		{VisitID: "park", ArrivedAt: pointer(at(-21)), DepartedAt: pointer(at(0)), LatitudeDegrees: northOf(450), LongitudeDegrees: 20},
		{VisitID: "home", ArrivedAt: pointer(at(1.0 / 60)), LatitudeDegrees: northOf(0), LongitudeDegrees: 20},
	}
	positions := []Position{
		{MeasuredAt: at(-15), LatitudeDegrees: northOf(455), LongitudeDegrees: 20, AccuracyMeters: 3},
		{MeasuredAt: at(-9), LatitudeDegrees: northOf(445), LongitudeDegrees: 20, AccuracyMeters: 6},
		{MeasuredAt: at(-1), LatitudeDegrees: northOf(200), LongitudeDegrees: 20, AccuracyMeters: 2},
	}
	trip := onlyTrip(t, BuildTrips(visitList, positions, nil))
	if trip.FromVisitID != "park" || !trip.StartedAt.Equal(at(-9)) || !trip.EndedAt.Equal(at(1.0/60)) {
		t.Fatalf("trip: %+v", trip)
	}
	if len(trip.Legs) != 1 || trip.Legs[0].DetectedTravelMode != TravelModeWalk || trip.Legs[0].DetectionSource != DetectionSourceSpeed {
		t.Fatalf("legs: %+v", trip.Legs)
	}
}

// Moving seen long before the visits meet, or with no position away from
// both places, says nothing of the trip between them.
func TestMovingFarFromWhereVisitsMeetIsNoTrip(t *testing.T) {
	activities := []MotionActivity{
		{StartedAt: at(0), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(5), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
	}
	if trips := BuildTrips(touchingVisits(30, 450), nil, activities); len(trips) != 0 {
		t.Fatalf("a walk 25 minutes before: %+v", trips)
	}
	positions := []Position{
		{MeasuredAt: at(-20), LatitudeDegrees: northOf(0), LongitudeDegrees: 20, AccuracyMeters: 5},
		{MeasuredAt: at(40), LatitudeDegrees: northOf(450), LongitudeDegrees: 20, AccuracyMeters: 5},
	}
	if trips := BuildTrips(touchingVisits(30, 450), positions, nil); len(trips) != 0 {
		t.Fatalf("positions only at the places: %+v", trips)
	}
}

// Neighboring places the phone moved between at once are not looked at,
// however the person moved about.
func TestNeighboringTouchingVisitsMakeNoTrip(t *testing.T) {
	activities := []MotionActivity{{StartedAt: at(0), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh}}
	if trips := BuildTrips(touchingVisits(5, 100), nil, activities); len(trips) != 0 {
		t.Fatalf("100 meters apart: %+v", trips)
	}
}

// A trip found this way starts no earlier than the trip before it ended, and
// no earlier than the first visit's arrival.
func TestFoundTripStartsAfterTheVisitBegan(t *testing.T) {
	// The phone says the drive went on until -3, past the arrival home at
	// -6 where the drive's trip ends; the walk found next is clipped there.
	visitList := []Visit{
		{VisitID: "office", ArrivedAt: pointer(at(-60)), DepartedAt: pointer(at(-20)), LatitudeDegrees: northOf(5000), LongitudeDegrees: 20},
		{VisitID: "home", ArrivedAt: pointer(at(-6)), DepartedAt: pointer(at(5)), LatitudeDegrees: northOf(0), LongitudeDegrees: 20},
		{VisitID: "park", ArrivedAt: pointer(at(5)), DepartedAt: pointer(at(200)), LatitudeDegrees: northOf(450), LongitudeDegrees: 20},
	}
	activities := []MotionActivity{
		{StartedAt: at(-21), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(-3), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(4), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
	}
	trips := BuildTrips(visitList, nil, activities)
	if len(trips) != 2 {
		t.Fatalf("got %d trips, want 2: %+v", len(trips), trips)
	}
	if trips[1].FromVisitID != "home" || !trips[1].StartedAt.Equal(at(-6)) || !trips[1].EndedAt.Equal(at(4)) {
		t.Fatalf("the walk: %+v", trips[1])
	}

	// With no trip before, the first visit's arrival bounds it.
	trips = BuildTrips(visitList[1:], nil, activities)
	if trip := onlyTrip(t, trips); !trip.StartedAt.Equal(at(-6)) {
		t.Fatalf("from the arrival: %+v", trip)
	}
}

// A stretch of moving that goes on long after the visits meet (the phone
// told of stopping only much later) ends ten minutes after the moment.
func TestFoundTripEndsSoonAfterVisitsMeet(t *testing.T) {
	activities := []MotionActivity{
		{StartedAt: at(-2), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(90), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
	}
	trip := onlyTrip(t, BuildTrips(touchingVisits(5, 450), nil, activities))
	if !trip.StartedAt.Equal(at(-2)) || !trip.EndedAt.Equal(at(15)) {
		t.Fatalf("trip: %+v", trip)
	}
}

// When the next visit begins before the first ends, a trip found after that
// arrival starts at it, so it is not shown after the visit it ends at.
func TestFoundTripStartsByTheNextArrival(t *testing.T) {
	visitList := []Visit{
		{VisitID: "home", ArrivedAt: pointer(at(-600)), DepartedAt: pointer(at(10)), LatitudeDegrees: northOf(0), LongitudeDegrees: 20},
		{VisitID: "park", ArrivedAt: pointer(at(8)), DepartedAt: pointer(at(300)), LatitudeDegrees: northOf(450), LongitudeDegrees: 20},
	}
	activities := []MotionActivity{
		{StartedAt: at(9), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(16), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
	}
	trip := onlyTrip(t, BuildTrips(visitList, nil, activities))
	if !trip.StartedAt.Equal(at(8)) || !trip.EndedAt.Equal(at(16)) {
		t.Fatalf("trip: %+v", trip)
	}
}

// A position at the first place long before the one on the way says little of
// when the person set off: no trip is guessed from it.
func TestOldPositionAtThePlaceStartsNoTrip(t *testing.T) {
	visitList := []Visit{
		{VisitID: "office", ArrivedAt: pointer(at(-120)), DepartedAt: pointer(at(0)), LatitudeDegrees: northOf(2000), LongitudeDegrees: 20},
		{VisitID: "home", ArrivedAt: pointer(at(1.0 / 60)), LatitudeDegrees: northOf(0), LongitudeDegrees: 20},
	}
	positions := []Position{
		{MeasuredAt: at(-16), LatitudeDegrees: northOf(2000), LongitudeDegrees: 20, AccuracyMeters: 5},
		{MeasuredAt: at(-2), LatitudeDegrees: northOf(1000), LongitudeDegrees: 20, AccuracyMeters: 5},
	}
	if trips := BuildTrips(visitList, positions, nil); len(trips) != 0 {
		t.Fatalf("from a position 14 minutes before: %+v", trips)
	}
}

// The last activity the phone sent is not taken to go on past the moment the
// visits meet: what came after it is not known yet.
func TestLastActivityEndsWhereVisitsMeet(t *testing.T) {
	activities := []MotionActivity{
		{StartedAt: at(-30), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(2), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
	}
	trip := onlyTrip(t, BuildTrips(touchingVisits(9, 450), nil, activities))
	if !trip.StartedAt.Equal(at(2)) || !trip.EndedAt.Equal(at(9)) {
		t.Fatalf("trip: %+v", trip)
	}
}

// Moving a found trip's start back to the next arrival does not take it
// before the trip before it ended.
func TestStartAtTheNextArrivalKeepsAfterTheTripBefore(t *testing.T) {
	visitList := []Visit{
		{VisitID: "office", ArrivedAt: pointer(at(-120)), DepartedAt: pointer(at(0)), LatitudeDegrees: northOf(3000), LongitudeDegrees: 20},
		{VisitID: "home", ArrivedAt: pointer(at(0)), DepartedAt: pointer(at(10)), LatitudeDegrees: northOf(0), LongitudeDegrees: 20},
		{VisitID: "park", ArrivedAt: pointer(at(2)), DepartedAt: pointer(at(300)), LatitudeDegrees: northOf(450), LongitudeDegrees: 20},
	}
	activities := []MotionActivity{
		{StartedAt: at(-6), ActivityKind: ActivityKindAutomotive, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(3), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(9), ActivityKind: ActivityKindWalking, ActivityConfidence: ActivityConfidenceHigh},
		{StartedAt: at(15), ActivityKind: ActivityKindStationary, ActivityConfidence: ActivityConfidenceHigh},
	}
	trips := BuildTrips(visitList, nil, activities)
	for index := 1; index < len(trips); index++ {
		if trips[index].StartedAt.Before(trips[index-1].EndedAt) {
			t.Fatalf("trips overlap: %+v", trips)
		}
	}
	if len(trips) != 2 || !trips[0].EndedAt.Equal(at(3)) || !trips[1].StartedAt.Equal(at(3)) {
		t.Fatalf("trips: %+v", trips)
	}
}
