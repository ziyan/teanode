// Package travel works out how a person got from one visit to the next: the
// trips between their visits and the legs of each trip, each leg with a way of
// travel. It is pure: it takes visits, positions and motion activity and
// returns trips, and knows nothing of the database, so that the rules can be
// tested on their own and applied again whenever a late report changes a day.
package travel

import (
	"math"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/place"
)

// Mode is a way of travel.
type Mode string

const (
	ModeWalk  Mode = "walk"
	ModeRun   Mode = "run"
	ModeBike  Mode = "bike"
	ModeCar   Mode = "car"
	ModeBus   Mode = "bus"
	ModeTrain Mode = "train"
	ModeBoat  Mode = "boat"
	ModePlane Mode = "plane"
)

// Modes lists the eight ways of travel in the order they are shown.
var Modes = []Mode{ModeWalk, ModeRun, ModeBike, ModeCar, ModeBus, ModeTrain, ModeBoat, ModePlane}

// IsMode reports whether the text is one of the eight ways of travel.
func IsMode(text string) bool {
	for _, mode := range Modes {
		if string(mode) == text {
			return true
		}
	}
	return false
}

// The kinds of motion activity iOS reports.
const (
	ActivityKindStationary = "stationary"
	ActivityKindWalking    = "walking"
	ActivityKindRunning    = "running"
	ActivityKindCycling    = "cycling"
	ActivityKindAutomotive = "automotive"
	ActivityKindUnknown    = "unknown"
)

// The confidences iOS gives a motion activity.
const (
	ActivityConfidenceLow    = "low"
	ActivityConfidenceMedium = "medium"
	ActivityConfidenceHigh   = "high"
)

// ActivityKinds lists every kind of motion activity a report can carry.
var ActivityKinds = []string{
	ActivityKindStationary, ActivityKindWalking, ActivityKindRunning,
	ActivityKindCycling, ActivityKindAutomotive, ActivityKindUnknown,
}

// ActivityConfidences lists every confidence a report can carry.
var ActivityConfidences = []string{ActivityConfidenceLow, ActivityConfidenceMedium, ActivityConfidenceHigh}

// IsActivityKind reports whether the text is a kind of motion activity.
func IsActivityKind(text string) bool {
	return contains(ActivityKinds, text)
}

// IsActivityConfidence reports whether the text is a confidence of motion
// activity.
func IsActivityConfidence(text string) bool {
	return contains(ActivityConfidences, text)
}

func contains(list []string, text string) bool {
	for _, item := range list {
		if item == text {
			return true
		}
	}
	return false
}

// ModeOfActivity is the way of travel a kind of motion activity stands for.
// Stationary and unknown stand for none: they say the person was not moving
// or that the phone could not tell, which is no way of travel.
func ModeOfActivity(activityKind string) (Mode, bool) {
	switch activityKind {
	case ActivityKindWalking:
		return ModeWalk, true
	case ActivityKindRunning:
		return ModeRun, true
	case ActivityKindCycling:
		return ModeBike, true
	case ActivityKindAutomotive:
		return ModeCar, true
	}
	return "", false
}

// The sources a leg's way of travel can be detected from.
const (
	DetectionSourceMotion = "motion"
	DetectionSourceSpeed  = "speed"
)

const (
	// MinimumTripDistanceMeters is the shortest path that is a trip. Shorter
	// than this the person moved about within one place, and the positions'
	// own noise is as long as the path.
	MinimumTripDistanceMeters = 150

	// MaximumAccuracyMeters is the worst accuracy a position can have and
	// still be used, when better positions exist. Worse than this the phone
	// is placing the person by a cell tower, which can be a kilometer off.
	MaximumAccuracyMeters = 500

	// MaximumSpeedMetersPerSecond is the speed above which a position is
	// taken to be a jump, not travel. It is faster than any airliner, so no
	// real movement is dropped, and a position that needs it was misplaced.
	MaximumSpeedMetersPerSecond = 300

	// MinimumLegDuration and MinimumLegDistanceMeters are the size below
	// which a leg is a blip of the phone's activity detection, not a change
	// of the way of travel, such as a crossing of a road between two bus
	// rides, and is merged into the leg beside it.
	MinimumLegDuration       = time.Minute
	MinimumLegDistanceMeters = 100

	// MinimumMotionCoverage is the share of a trip's time that motion
	// activity must name a way for, or the way is guessed from speed. Below
	// it the activity is too patchy to say how most of the trip went.
	MinimumMotionCoverage = 0.5

	// The fastest average speeds, in meters a second, of walking (a brisk
	// walk), cycling and driving. Faster than the last is a plane. Running is
	// never guessed from speed: it is too close to a fast walk or a slow bike.
	MaximumWalkSpeedMetersPerSecond = 2.2
	MaximumBikeSpeedMetersPerSecond = 5
	MaximumCarSpeedMetersPerSecond  = 75

	// MinimumPlaneDistanceMeters and MinimumPlaneSpeedMetersPerSecond are the
	// size of a leg that is a plane whatever the activity says, because iOS
	// reports a plane as driving: it is at least 100 kilometers at an average
	// of 55 meters a second, which no car holds over that distance.
	MinimumPlaneDistanceMeters       = 100000
	MinimumPlaneSpeedMetersPerSecond = 55
)

// Activity is what the phone said the person was doing from a moment on. It
// lasts until the next activity starts.
type Activity struct {
	StartedAt          time.Time
	ActivityKind       string
	ActivityConfidence string
}

// Stay is a visit: the person was at one place from arrival to departure. A
// visit still going on has no departure.
type Stay struct {
	VisitID                           string
	ArrivedAt, DepartedAt             *time.Time
	LatitudeDegrees, LongitudeDegrees float64
}

// Position is where the phone measured the person to be.
type Position struct {
	MeasuredAt                                        time.Time
	LatitudeDegrees, LongitudeDegrees, AccuracyMeters float64
}

// Leg is a part of a trip with one way of travel.
type Leg struct {
	StartedAt, EndedAt time.Time
	DetectedMode       Mode
	DetectionSource    string
	DistanceMeters     float64
}

// Trip is the travel between the departure of one visit and the arrival of the
// next. Its legs follow one another and cover it, and their distances add up
// to the trip's.
type Trip struct {
	FromVisitID, ToVisitID string
	StartedAt, EndedAt     time.Time
	DistanceMeters         float64
	Legs                   []Leg
}

// pathPoint is a point on a trip's path.
type pathPoint struct {
	measuredAt                        time.Time
	latitudeDegrees, longitudeDegrees float64
	// cumulativeMeters is the length of the path from its start to here.
	cumulativeMeters float64
}

// BuildTrips builds the trips between consecutive stays. The inputs may come
// in any order; they are not changed.
func BuildTrips(stays []Stay, positions []Position, activities []Activity) []Trip {
	stays = append([]Stay(nil), stays...)
	sort.SliceStable(stays, func(first, second int) bool {
		return stayOrderTime(stays[first]).Before(stayOrderTime(stays[second]))
	})
	positions = append([]Position(nil), positions...)
	sort.SliceStable(positions, func(first, second int) bool {
		return positions[first].MeasuredAt.Before(positions[second].MeasuredAt)
	})
	activities = append([]Activity(nil), activities...)
	sort.SliceStable(activities, func(first, second int) bool {
		return activities[first].StartedAt.Before(activities[second].StartedAt)
	})

	var trips []Trip
	for index := 0; index+1 < len(stays); index++ {
		from, to := stays[index], stays[index+1]
		if from.DepartedAt == nil || to.ArrivedAt == nil || !to.ArrivedAt.After(*from.DepartedAt) {
			continue
		}
		trip, isTrip := buildTrip(from, to, positions, activities)
		if isTrip {
			trips = append(trips, trip)
		}
	}
	return trips
}

// stayOrderTime is the time a stay is ordered by: its arrival, or its
// departure when the arrival is not known.
func stayOrderTime(stay Stay) time.Time {
	if stay.ArrivedAt != nil {
		return *stay.ArrivedAt
	}
	if stay.DepartedAt != nil {
		return *stay.DepartedAt
	}
	return time.Time{}
}

func buildTrip(from, to Stay, positions []Position, activities []Activity) (Trip, bool) {
	startedAt, endedAt := *from.DepartedAt, *to.ArrivedAt
	path := buildPath(from, to, positions)
	totalMeters := path[len(path)-1].cumulativeMeters
	if totalMeters < MinimumTripDistanceMeters {
		return Trip{}, false
	}
	trip := Trip{
		FromVisitID:    from.VisitID,
		ToVisitID:      to.VisitID,
		StartedAt:      startedAt,
		EndedAt:        endedAt,
		DistanceMeters: totalMeters,
	}
	legs := motionLegs(path, startedAt, endedAt, activities)
	if legs == nil {
		legs = []Leg{{
			StartedAt:       startedAt,
			EndedAt:         endedAt,
			DetectedMode:    modeOfSpeed(totalMeters / endedAt.Sub(startedAt).Seconds()),
			DetectionSource: DetectionSourceSpeed,
			DistanceMeters:  totalMeters,
		}}
	}
	for index := range legs {
		seconds := legs[index].EndedAt.Sub(legs[index].StartedAt).Seconds()
		if legs[index].DistanceMeters >= MinimumPlaneDistanceMeters &&
			legs[index].DistanceMeters/seconds >= MinimumPlaneSpeedMetersPerSecond {
			legs[index].DetectedMode = ModePlane
		}
	}
	trip.Legs = legs
	return trip, true
}

// buildPath is the first stay's coordinates, the positions measured during the
// trip that are believable, and the second stay's coordinates.
func buildPath(from, to Stay, positions []Position) []pathPoint {
	startedAt, endedAt := *from.DepartedAt, *to.ArrivedAt
	var inside []Position
	hasAccuratePosition := false
	for _, position := range positions {
		if position.MeasuredAt.Before(startedAt) || position.MeasuredAt.After(endedAt) {
			continue
		}
		inside = append(inside, position)
		if position.AccuracyMeters <= MaximumAccuracyMeters {
			hasAccuratePosition = true
		}
	}
	path := []pathPoint{{measuredAt: startedAt, latitudeDegrees: from.LatitudeDegrees, longitudeDegrees: from.LongitudeDegrees}}
	for _, position := range inside {
		if hasAccuratePosition && position.AccuracyMeters > MaximumAccuracyMeters {
			continue
		}
		previous := path[len(path)-1]
		meters := place.DistanceMeters(previous.latitudeDegrees, previous.longitudeDegrees, position.LatitudeDegrees, position.LongitudeDegrees)
		seconds := math.Max(position.MeasuredAt.Sub(previous.measuredAt).Seconds(), 1)
		if meters/seconds > MaximumSpeedMetersPerSecond {
			continue
		}
		path = append(path, pathPoint{
			measuredAt:       position.MeasuredAt,
			latitudeDegrees:  position.LatitudeDegrees,
			longitudeDegrees: position.LongitudeDegrees,
			cumulativeMeters: previous.cumulativeMeters + meters,
		})
	}
	previous := path[len(path)-1]
	path = append(path, pathPoint{
		measuredAt:       endedAt,
		latitudeDegrees:  to.LatitudeDegrees,
		longitudeDegrees: to.LongitudeDegrees,
		cumulativeMeters: previous.cumulativeMeters + place.DistanceMeters(previous.latitudeDegrees, previous.longitudeDegrees, to.LatitudeDegrees, to.LongitudeDegrees),
	})
	return path
}

// distanceAt is how far along the path the person was at a moment, with the
// path interpolated in time between its points.
func distanceAt(path []pathPoint, moment time.Time) float64 {
	if !moment.After(path[0].measuredAt) {
		return 0
	}
	last := path[len(path)-1]
	if !moment.Before(last.measuredAt) {
		return last.cumulativeMeters
	}
	for index := len(path) - 2; index >= 0; index-- {
		if moment.Before(path[index].measuredAt) {
			continue
		}
		span := path[index+1].measuredAt.Sub(path[index].measuredAt).Seconds()
		if span <= 0 {
			return path[index].cumulativeMeters
		}
		share := moment.Sub(path[index].measuredAt).Seconds() / span
		return path[index].cumulativeMeters + share*(path[index+1].cumulativeMeters-path[index].cumulativeMeters)
	}
	return 0
}

// motionLegs cuts the trip into legs by motion activity. It returns nil when
// the activity names a way for less than half of the trip's time.
func motionLegs(path []pathPoint, startedAt, endedAt time.Time, activities []Activity) []Leg {
	type span struct {
		startedAt, endedAt time.Time
		mode               Mode
	}
	// The spans with a way, clipped to the trip. An activity lasts until the
	// next one starts, so the one in effect when the trip began counts.
	var spans []span
	var coveredSeconds float64
	for index, activity := range activities {
		mode, hasMode := ModeOfActivity(activity.ActivityKind)
		if !hasMode {
			continue
		}
		spanStart, spanEnd := activity.StartedAt, endedAt
		if index+1 < len(activities) {
			spanEnd = activities[index+1].StartedAt
		}
		if spanStart.Before(startedAt) {
			spanStart = startedAt
		}
		if spanEnd.After(endedAt) {
			spanEnd = endedAt
		}
		if !spanEnd.After(spanStart) {
			continue
		}
		if count := len(spans); count > 0 && spans[count-1].mode == mode && spans[count-1].endedAt.Equal(spanStart) {
			spans[count-1].endedAt = spanEnd
		} else {
			spans = append(spans, span{startedAt: spanStart, endedAt: spanEnd, mode: mode})
		}
		coveredSeconds += spanEnd.Sub(spanStart).Seconds()
	}
	if len(spans) == 0 || coveredSeconds < MinimumMotionCoverage*endedAt.Sub(startedAt).Seconds() {
		return nil
	}

	// Cut halfway through each stretch with no way between two spans, so that
	// it extends both neighbors. The ends of the trip are the ends of the
	// first and last legs.
	legs := make([]Leg, 0, len(spans))
	for index, current := range spans {
		leg := Leg{StartedAt: current.startedAt, EndedAt: current.endedAt, DetectedMode: current.mode, DetectionSource: DetectionSourceMotion}
		if index == 0 {
			leg.StartedAt = startedAt
		}
		if index+1 == len(spans) {
			leg.EndedAt = endedAt
		} else {
			next := spans[index+1]
			leg.EndedAt = current.endedAt.Add(next.startedAt.Sub(current.endedAt) / 2)
		}
		if index > 0 {
			leg.StartedAt = legs[index-1].EndedAt
		}
		legs = append(legs, leg)
	}
	legs = mergeLegs(legs)
	for index := range legs {
		legs[index].DistanceMeters = distanceAt(path, legs[index].EndedAt) - distanceAt(path, legs[index].StartedAt)
	}
	// Shorter legs are merged by their path length, which is only known now.
	for {
		shortIndex := shortestBlip(legs)
		if shortIndex < 0 {
			break
		}
		legs = absorb(legs, shortIndex)
		for index := range legs {
			legs[index].DistanceMeters = distanceAt(path, legs[index].EndedAt) - distanceAt(path, legs[index].StartedAt)
		}
	}
	return legs
}

// mergeLegs joins neighbors that have the same way.
func mergeLegs(legs []Leg) []Leg {
	merged := legs[:0:0]
	for _, leg := range legs {
		if count := len(merged); count > 0 && merged[count-1].DetectedMode == leg.DetectedMode {
			merged[count-1].EndedAt = leg.EndedAt
			continue
		}
		merged = append(merged, leg)
	}
	return merged
}

// shortestBlip is the index of the shortest leg that is too short in time or
// distance to stand alone, or -1 when there is none or only one leg is left.
func shortestBlip(legs []Leg) int {
	if len(legs) < 2 {
		return -1
	}
	blipIndex := -1
	for index, leg := range legs {
		duration := leg.EndedAt.Sub(leg.StartedAt)
		if duration >= MinimumLegDuration && leg.DistanceMeters >= MinimumLegDistanceMeters {
			continue
		}
		if blipIndex < 0 || duration < legs[blipIndex].EndedAt.Sub(legs[blipIndex].StartedAt) {
			blipIndex = index
		}
	}
	return blipIndex
}

// absorb merges a leg into the neighbor it touches longest, which is the
// longer of the two, and joins any neighbors left with the same way.
func absorb(legs []Leg, index int) []Leg {
	target := index - 1
	if index == 0 {
		target = 1
	} else if index+1 < len(legs) {
		before := legs[index-1].EndedAt.Sub(legs[index-1].StartedAt)
		after := legs[index+1].EndedAt.Sub(legs[index+1].StartedAt)
		if after > before {
			target = index + 1
		}
	}
	if target < index {
		legs[target].EndedAt = legs[index].EndedAt
	} else {
		legs[target].StartedAt = legs[index].StartedAt
	}
	legs = append(legs[:index], legs[index+1:]...)
	return mergeLegs(legs)
}

// modeOfSpeed guesses the way of travel from an average speed.
func modeOfSpeed(metersPerSecond float64) Mode {
	switch {
	case metersPerSecond <= MaximumWalkSpeedMetersPerSecond:
		return ModeWalk
	case metersPerSecond <= MaximumBikeSpeedMetersPerSecond:
		return ModeBike
	case metersPerSecond <= MaximumCarSpeedMetersPerSecond:
		return ModeCar
	}
	return ModePlane
}
