package apigraph

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
	"github.com/ziyan/teanode/internal/travel"
	"github.com/ziyan/teanode/internal/travel/rebuild"
)

// Where a person is and where they have been: the positions their phone and
// browser report, the places those are at, the visits at them, and the places
// near them that OpenStreetMap knows. docs/subsystems/location.md says what is
// recorded and docs/decisions/20261017-places-are-named-from-openstreetmap.md
// where the names come from.
//
// Every operation here is about one person's own records. Whose records an
// operation reads is decided in one place, graph.reachLocation, so that
// letting somebody read what a person shared with them is a change to that
// one function and not to every operation.

// LocationQuery reads where people are and have been.
type LocationQuery interface {
	// Where the caller is: the latest position they reported, and the visit in
	// progress with its place. A visit whose departure the phone never sent is
	// not reported as in progress once a later position is somewhere else.
	// Needs location:use.
	GetLocation(ctx context.Context, arguments GetLocationArguments) (*LocationView, error)

	// Visits, newest first, paged. A visit is listed when any of it falls in
	// the range. Needs location:use.
	ListLocationVisits(ctx context.Context, arguments ListLocationVisitsArguments) (*PlaceVisitPageView, error)

	// The caller's places, paged: the ones with a label first, then the ones
	// they named, then the rest, each by when they were last visited. Needs
	// location:use.
	ListPlaces(ctx context.Context, arguments ListPlacesArguments) (*PlacePageView, error)

	// One place by its id, as a row of ListPlaces is: the caller's own, or
	// somebody else's who shares their history with the caller. Not found
	// when the id is unknown or the place is not the caller's to read. Needs
	// location:use.
	ReadPlace(ctx context.Context, arguments ReadPlaceArguments) (*PlaceView, error)

	// What is at and around a position, nearest first, for a check-in: the named
	// place OpenStreetMap knows at the position, and the caller's own places
	// there; only the caller's own while the place provider is switched off.
	// Needs location:use.
	ListNearbyPlaces(ctx context.Context, arguments ListNearbyPlacesArguments) ([]*FoundPlaceView, error)

	// Places matching words near a position, from OpenStreetMap, nearest first.
	// Near the position given, or the place given, or where the caller was last
	// reported to be. Needs location:use.
	SearchPlaces(ctx context.Context, arguments SearchPlacesArguments) (*FoundPlacePageView, error)

	// Addresses matching the words, from the place provider, anywhere (not near
	// a position), best match first. Needs location:use.
	SearchAddresses(ctx context.Context, arguments SearchAddressesArguments) ([]*FoundPlaceView, error)

	// The positions of up to two days, oldest first, for drawing the path
	// taken. Needs location:use.
	ListLocationPoints(ctx context.Context, arguments ListLocationPointsArguments) ([]*LocationPointView, error)

	// The pauses that overlap a range, newest first: the times the caller's
	// location was not recorded, or another person's who shares their history
	// with the caller. Needs location:use.
	ListLocationPauses(ctx context.Context, arguments ListLocationPausesArguments) ([]*LocationPauseView, error)

	// List the trips between visits that overlap the range, newest first,
	// each with its legs and their ways of travel. A trip is the travel from
	// leaving one visit to arriving at the next; a leg is a part of it with
	// one way of travel: walk, run, bike, car, bus, train, boat or plane.
	// Walk, run, bike and car are detected from the phone's motion activity,
	// plane from distance and speed, and the rest come from the person's
	// corrections. Someone else's trips need history. Needs location:use.
	ListTrips(ctx context.Context, arguments ListTripsArguments) (*TripPageView, error)

	// How far a person traveled each way per day, week (from Monday), month
	// or year in their time zone, newest period first, for legs that began in
	// the range; at most 400 periods. Someone else's need history. Needs
	// location:use.
	ListTravelTotals(ctx context.Context, arguments ListTravelTotalsArguments) ([]*TravelTotalView, error)

	// Every person whose location is shared with the caller, directly or
	// through a circle, at the highest level any share gives: their latest
	// position and the visit in progress, or that they have paused. Needs
	// location:use.
	ListSharedLocations(ctx context.Context) ([]*LocationView, error)

	// The shares the caller has made of their own location, and with whom.
	// Needs location:use.
	ListLocationShares(ctx context.Context) ([]*LocationShareView, error)
}

// LocationMutation records and changes them.
type LocationMutation interface {
	// A phone's batch of up to 500 positions, 50 visits and 500 motion
	// activity readings. After it is stored, the trips of the days it touched
	// are built again (ListTrips). Sending the same
	// batch again adds nothing, which is how a phone can send until it knows
	// the server has them. What was measured while the caller had paused is
	// not stored, and is counted; the answer says the pause in effect, so that
	// a phone learns of a pause set elsewhere. Answers without asking
	// OpenStreetMap anything: a place made here is named afterwards. Needs
	// location:use.
	ReportLocation(ctx context.Context, arguments ReportLocationArguments) (*ReportLocationView, error)

	// Name, rename, label, move or resize a place, or make one at coordinates
	// when no place is given. A name sets the caller's own name for it; an
	// empty name goes back to OpenStreetMap's. An empty label takes the label
	// off. Needs location:use.
	SavePlace(ctx context.Context, arguments SavePlaceArguments) (*PlaceView, error)

	// Delete a place and every visit to it. Needs location:use.
	ForgetPlace(ctx context.Context, arguments ForgetPlaceArguments) (bool, error)

	// Record that the caller is at a place now: one of their own, or a place
	// found nearby, which becomes one of theirs. Ends the visit in progress
	// and starts a visit that was recorded by hand. Needs location:use.
	CheckIn(ctx context.Context, arguments CheckInArguments) (*PlaceVisitView, error)

	// Stop recording where the caller is, now, until endedAt or, with none,
	// until they resume. The visit in progress ends. While paused the server
	// stores no position and no visit measured, and the people the location
	// is shared with see that it is paused and no position. Pausing while
	// paused changes the end of the pause. endedAt must be after now and at
	// most 30 days ahead. Needs location:use.
	PauseLocation(ctx context.Context, arguments PauseLocationArguments) (*LocationPauseView, error)

	// End the pause in effect now. True when there was one. Needs
	// location:use.
	ResumeLocation(ctx context.Context) (bool, error)

	// Stop telling the caller's phones that somebody sharing their location
	// with the caller arrived at or left a place, until mutedUntil or, with
	// none, until the caller unmutes. Muting again changes the end. mutedUntil
	// must be after now and at most 30 days ahead. Needs location:use.
	MuteLocationNotifications(ctx context.Context, arguments MuteLocationNotificationsArguments) (*LocationNotificationMuteView, error)

	// Tell the caller again where that person arrives and leaves. True when a
	// mute was in effect. Needs location:use.
	UnmuteLocationNotifications(ctx context.Context, arguments UnmuteLocationNotificationsArguments) (bool, error)

	// Correct the way of travel of one of the caller's trip legs; leave
	// travelMode out to go back to the detected way. The correction is kept
	// for the leg's span of time, so it holds when the trip is built again.
	// Needs location:use.
	SetTripLegTravelMode(ctx context.Context, arguments SetTripLegTravelModeArguments) (*TripLegView, error)

	// Erases your positions and visits (in the range), the pauses that began
	// in it and are over, and the places left with no visit that you did not
	// name or label. Places you named or labeled stay, and so does the pause
	// in effect. Anything your agent already learned from those days stays
	// in its memory until you remove it. Without a range, all of them. This
	// cannot be undone. Needs location:use.
	EraseLocationHistory(ctx context.Context, arguments EraseLocationHistoryArguments) (*EraseLocationHistoryView, error)

	// Share the caller's own location with one person or one circle at a
	// level: current (where they are now, and the place) or history (that, and
	// their visits). The person must share a circle with the caller, and the
	// caller must be in the circle. Sharing again changes the level. Needs
	// location:use.
	ShareLocation(ctx context.Context, arguments ShareLocationArguments) (*LocationShareView, error)

	// Take a share back. Only the person whose location it is can. Needs
	// location:use.
	UnshareLocation(ctx context.Context, arguments UnshareLocationArguments) (bool, error)
}

// LocationSubscription follows the people the caller can see.
type LocationSubscription interface {
	// An event each time a person the caller may see the location of reports
	// where they are, shares or stops sharing it, or erases it, the caller
	// included; read the location again to see what. At most 20 are open for
	// one account. Needs location:use.
	LocationEvents(ctx context.Context) (<-chan *LocationEventView, error)
}

// LocationView is where a person is.
type LocationView struct {
	// The person.
	UserID           string  `json:"userId"`
	Username         string  `json:"username"`
	Name             string  `json:"name"`
	AvatarURL        *string `json:"avatarUrl"`
	AvatarColorIndex int     `json:"avatarColorIndex"`

	// ShareLevel is owner for the caller's own location, otherwise the highest
	// level any share gives the caller: current or history.
	ShareLevel string `json:"shareLevel"`

	// LatestPoint is the newest position reported, null when there is none.
	LatestPoint *LocationPointView `json:"latestPoint"`

	// OpenVisit is the visit in progress, null when there is none.
	OpenVisit *PlaceVisitView `json:"openVisit"`

	// Pause is the pause in effect, null when the person is not paused. For
	// anybody but the person themselves a pause hides LatestPoint and
	// OpenVisit: they are null while it lasts.
	Pause *LocationPauseView `json:"pause"`

	// NotificationMute is the caller's mute of this person's arrivals and
	// departures, null when the caller is told of them. Always null on the
	// caller's own location.
	NotificationMute *LocationNotificationMuteView `json:"notificationMute"`
}

// LocationPointView is one position a phone or browser reported.
type LocationPointView struct {
	LatitudeDegrees      float64   `json:"latitudeDegrees"`
	LongitudeDegrees     float64   `json:"longitudeDegrees"`
	AccuracyMeters       float64   `json:"accuracyMeters"`
	AltitudeMeters       *float64  `json:"altitudeMeters"`
	SpeedMetersPerSecond *float64  `json:"speedMetersPerSecond"`
	CourseDegrees        *float64  `json:"courseDegrees"`
	MeasuredAt           time.Time `json:"measuredAt"`

	// LocationSource is phone or browser.
	LocationSource string `json:"locationSource"`
}

// PlaceView is a place with a name and a radius, belonging to one person.
type PlaceView struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`

	// PlaceName is the caller's own name for it if they gave one, otherwise
	// OpenStreetMap's, otherwise the street address, otherwise the city.
	PlaceName       string `json:"placeName"`
	IsNamedByPerson bool   `json:"isNamedByPerson"`

	// PlaceLabel is empty, or home, work, school, family or gym.
	PlaceLabel string `json:"placeLabel"`

	LatitudeDegrees  float64 `json:"latitudeDegrees"`
	LongitudeDegrees float64 `json:"longitudeDegrees"`
	RadiusMeters     float64 `json:"radiusMeters"`

	// What OpenStreetMap knows about the place. OSMType is empty and OSMID
	// null when it knows no object there.
	OSMType     string `json:"osmType"`
	OSMID       *int64 `json:"osmId"`
	OSMName     string `json:"osmName"`
	OSMCategory string `json:"osmCategory"`
	AddressText string `json:"addressText"`

	// IsAddressTyped says AddressText is one the person typed: moving the
	// place keeps it and OpenStreetMap does not replace it.
	IsAddressTyped bool `json:"isAddressTyped"`

	// VisitCount and LastVisitAt are filled in where a place is listed or
	// saved, and are zero and null where it is shown as part of a visit.
	VisitCount  int        `json:"visitCount"`
	LastVisitAt *time.Time `json:"lastVisitAt"`

	// NamingErrorMessage says why OpenStreetMap could not be asked about the
	// place the last time it was tried, empty when it has not failed.
	NamingErrorMessage string `json:"namingErrorMessage"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PlaceVisitView is one visit at a place.
type PlaceVisitView struct {
	ID    string     `json:"id"`
	Place *PlaceView `json:"place"`

	// ArrivedAt is null for a visit whose arrival the phone did not see.
	// DepartedAt is null while the person is still there.
	ArrivedAt  *time.Time `json:"arrivedAt"`
	DepartedAt *time.Time `json:"departedAt"`

	// DurationSeconds is how long the visit lasted, null until it has both an
	// arrival and a departure.
	DurationSeconds *int `json:"durationSeconds"`

	LatitudeDegrees  float64 `json:"latitudeDegrees"`
	LongitudeDegrees float64 `json:"longitudeDegrees"`
	AccuracyMeters   float64 `json:"accuracyMeters"`

	// VisitSource is visit, region, check_in or browser.
	VisitSource string `json:"visitSource"`

	// PhotoCount is how many of the caller's own library photos were taken
	// during the visit, and Photos the first visitPhotoPreviewCount of them,
	// earliest first. Both are null unless the visits are the caller's own
	// and the caller may use Photos, and for a visit with no arrival.
	PhotoCount *int             `json:"photoCount"`
	Photos     *[]*models.Photo `json:"photos"`

	// PhotosTakenBefore is when the photos counted end: the departure, the
	// person's latest position for a visit they left without a departure
	// being seen, or the time of the answer for a visit still going. A
	// window opened on Photos ends there, so it holds the photos counted.
	// Null when PhotoCount is.
	PhotosTakenBefore *time.Time `json:"photosTakenBefore"`
}

// PlaceVisitPageView is a page of visits.
type PlaceVisitPageView struct {
	PlaceVisits []*PlaceVisitView `json:"placeVisits"`

	// NextOffset is the offset that reads on, null when this is the last page.
	NextOffset *int `json:"nextOffset"`
}

// PlacePageView is a page of places.
type PlacePageView struct {
	Places []*PlaceView `json:"places"`

	// NextOffset is the offset that reads on, null when this is the last page.
	NextOffset *int `json:"nextOffset"`
}

// FoundPlaceView is a place on the map: one OpenStreetMap found, or one of the
// caller's own that is near.
type FoundPlaceView struct {
	PlaceName        string  `json:"placeName"`
	OSMType          string  `json:"osmType"`
	OSMID            int64   `json:"osmId"`
	OSMCategory      string  `json:"osmCategory"`
	AddressText      string  `json:"addressText"`
	LatitudeDegrees  float64 `json:"latitudeDegrees"`
	LongitudeDegrees float64 `json:"longitudeDegrees"`
	DistanceMeters   float64 `json:"distanceMeters"`

	// PlaceID is the caller's own place there, null when they have none.
	// For such a row PlaceName is the caller's own name for it when they named
	// it (otherwise the OpenStreetMap name), PlaceLabel is its label (empty
	// when it has none) and IsNamedByPerson says whose name PlaceName is.
	PlaceID         *string `json:"placeId"`
	PlaceLabel      string  `json:"placeLabel"`
	IsNamedByPerson bool    `json:"isNamedByPerson"`
}

// FoundPlacePageView is a page of found places.
type FoundPlacePageView struct {
	FoundPlaces []*FoundPlaceView `json:"foundPlaces"`

	// NextOffset is the offset that reads on, null when this is the last page.
	NextOffset *int `json:"nextOffset"`
}

// ReportLocationView says how much of a batch was new.
type ReportLocationView struct {
	NewPointCount     int `json:"newPointCount"`
	NewVisitCount     int `json:"newVisitCount"`
	UpdatedVisitCount int `json:"updatedVisitCount"`

	// RefusedVisitCount is how many visits were left out because each needed a
	// new place and the person has as many places as they may have. Their
	// positions, and the visits at places they have, are stored all the same.
	RefusedVisitCount int `json:"refusedVisitCount"`

	// NewActivityCount is how many motion activity readings were new.
	NewActivityCount int `json:"newActivityCount"`

	// PausedPointCount and PausedVisitCount are how many positions and visits
	// were left out because they were measured while the person had paused.
	PausedPointCount int `json:"pausedPointCount"`
	PausedVisitCount int `json:"pausedVisitCount"`

	// Pause is the pause in effect after the report, null when the person is
	// not paused, so that a phone learns of a pause set somewhere else.
	Pause *LocationPauseView `json:"pause"`
}

// EraseLocationHistoryView says what was erased.
type EraseLocationHistoryView struct {
	PointCount int `json:"pointCount"`
	VisitCount int `json:"visitCount"`
	PlaceCount int `json:"placeCount"`
}

type GetLocationArguments struct {
	// UserID is whose location; empty is the caller's own.
	UserID string `json:"userId" graphapi:"nullable"`
}

type ListLocationVisitsArguments struct {
	// UserID is whose visits; empty is the caller's own.
	UserID string `json:"userId" graphapi:"nullable"`

	// From and Until keep the visits that overlap the range.
	From  *time.Time `json:"from" graphapi:"nullable"`
	Until *time.Time `json:"until" graphapi:"nullable"`

	// PlaceID keeps the visits at one place, PlaceLabel the visits at places
	// with a label.
	PlaceID    string `json:"placeId" graphapi:"nullable"`
	PlaceLabel string `json:"placeLabel" graphapi:"nullable"`

	Offset     *int `json:"offset" graphapi:"nullable"`
	LimitCount *int `json:"limitCount" graphapi:"nullable"`
}

type ListPlacesArguments struct {
	// UserID is whose places; empty is the caller's own.
	UserID string `json:"userId" graphapi:"nullable"`

	PlaceLabel string `json:"placeLabel" graphapi:"nullable"`

	// Query keeps the places whose name or address contains these words.
	Query string `json:"query" graphapi:"nullable"`

	Offset     *int `json:"offset" graphapi:"nullable"`
	LimitCount *int `json:"limitCount" graphapi:"nullable"`
}

type ReadPlaceArguments struct {
	PlaceID string `json:"placeId"`

	// UserID is whose place; empty is the caller's own.
	UserID string `json:"userId" graphapi:"nullable"`
}

type ListNearbyPlacesArguments struct {
	LatitudeDegrees  float64 `json:"latitudeDegrees"`
	LongitudeDegrees float64 `json:"longitudeDegrees"`

	// RadiusMeters is 150 by default and at most 1000.
	RadiusMeters *float64 `json:"radiusMeters" graphapi:"nullable"`
}

type SearchPlacesArguments struct {
	Query string `json:"query"`

	// Near a position, or near one of the caller's places; with neither, near
	// where the caller was last reported to be.
	LatitudeDegrees  *float64 `json:"latitudeDegrees" graphapi:"nullable"`
	LongitudeDegrees *float64 `json:"longitudeDegrees" graphapi:"nullable"`
	PlaceID          string   `json:"placeId" graphapi:"nullable"`

	// RadiusMeters is 5000 by default and at most 50000.
	RadiusMeters *float64 `json:"radiusMeters" graphapi:"nullable"`

	Offset *int `json:"offset" graphapi:"nullable"`

	// LimitCount is 20 by default and at most 50.
	LimitCount *int `json:"limitCount" graphapi:"nullable"`
}

type SearchAddressesArguments struct {
	// Query is 1 to 200 characters.
	Query string `json:"query"`

	// Nearest first to a position, or to one of the caller's places (the one
	// being given an address); with neither, to where the caller was last
	// reported to be, and in the provider's order with no position at all.
	LatitudeDegrees  *float64 `json:"latitudeDegrees" graphapi:"nullable"`
	LongitudeDegrees *float64 `json:"longitudeDegrees" graphapi:"nullable"`
	PlaceID          string   `json:"placeId" graphapi:"nullable"`

	// LimitCount is 8 by default and at most 20.
	LimitCount *int `json:"limitCount" graphapi:"nullable"`
}

type ListLocationPointsArguments struct {
	// UserID is whose positions; empty is the caller's own.
	UserID string `json:"userId" graphapi:"nullable"`

	// From and Until are at most two days apart.
	From  time.Time `json:"from"`
	Until time.Time `json:"until"`
}

type ReportLocationArguments struct {
	// AgentPhoneID is the install that reports, one of the caller's own.
	AgentPhoneID string `json:"agentPhoneId" graphapi:"nullable"`

	Points []ReportedPoint `json:"points"`
	Visits []ReportedVisit `json:"visits"`

	// Activities is what the phone's motion activity said the person was
	// doing, each from its start until the next one's; a phone without
	// Motion & Fitness, or an older app, sends none.
	Activities []ReportedActivity `json:"activities" graphapi:"nullable"`
}

// ReportedActivity is one of the phone's motion activity readings.
type ReportedActivity struct {
	StartedAt time.Time `json:"startedAt"`

	// ActivityKind is stationary, walking, running, cycling, automotive or
	// unknown; ActivityConfidence is low, medium or high.
	ActivityKind       string `json:"activityKind"`
	ActivityConfidence string `json:"activityConfidence"`
}

// ReportedPoint is one position a phone measured.
type ReportedPoint struct {
	LatitudeDegrees      float64   `json:"latitudeDegrees"`
	LongitudeDegrees     float64   `json:"longitudeDegrees"`
	AccuracyMeters       float64   `json:"accuracyMeters"`
	AltitudeMeters       *float64  `json:"altitudeMeters" graphapi:"nullable"`
	SpeedMetersPerSecond *float64  `json:"speedMetersPerSecond" graphapi:"nullable"`
	CourseDegrees        *float64  `json:"courseDegrees" graphapi:"nullable"`
	MeasuredAt           time.Time `json:"measuredAt"`
}

// ReportedVisit is an arrival, a departure or both, as a phone saw them. An
// arrival and its departure are reported separately under one ClientVisitKey,
// and the departure has no arrival time.
type ReportedVisit struct {
	ClientVisitKey   string     `json:"clientVisitKey"`
	LatitudeDegrees  float64    `json:"latitudeDegrees"`
	LongitudeDegrees float64    `json:"longitudeDegrees"`
	AccuracyMeters   float64    `json:"accuracyMeters"`
	ArrivedAt        *time.Time `json:"arrivedAt" graphapi:"nullable"`
	DepartedAt       *time.Time `json:"departedAt" graphapi:"nullable"`

	// VisitSource is visit, for iOS visit monitoring, or region, for entering
	// or leaving a place the person named.
	VisitSource string `json:"visitSource"`

	// PlaceID is the place a region event watched, one of the caller's own.
	PlaceID string `json:"placeId" graphapi:"nullable"`
}

type SavePlaceArguments struct {
	// PlaceID is the place to change; empty makes a new one at the
	// coordinates.
	PlaceID string `json:"placeId" graphapi:"nullable"`

	// PlaceName is left as it is when absent, and an empty name goes back to
	// OpenStreetMap's.
	PlaceName *string `json:"placeName" graphapi:"nullable"`

	// PlaceLabel is left as it is when absent, and an empty label takes it off.
	PlaceLabel *string `json:"placeLabel" graphapi:"nullable"`

	LatitudeDegrees  *float64 `json:"latitudeDegrees" graphapi:"nullable"`
	LongitudeDegrees *float64 `json:"longitudeDegrees" graphapi:"nullable"`
	RadiusMeters     *float64 `json:"radiusMeters" graphapi:"nullable"`

	// AddressText is an address picked from SearchAddresses, given with the
	// latitudeDegrees and longitudeDegrees of that match. The place moves
	// there and keeps this address, which the place provider does not replace
	// afterwards; a place the person did not name is called by its first part.
	// A new place (no placeID) at a position inside one of the caller's own
	// places is that place changed. With a placeID and no coordinates the
	// place stays where it is and only its address is set, for an address the
	// place provider does not know; without a placeID the position is needed.
	// The address is then kept as typed (isAddressTyped): moving the place
	// later keeps it. An empty address takes the typed address away and lets
	// the place be named from OpenStreetMap again.
	AddressText *string `json:"addressText" graphapi:"nullable"`
}

type ForgetPlaceArguments struct {
	PlaceID string `json:"placeId"`
}

type CheckInArguments struct {
	// PlaceID is one of the caller's places. Without it the rest describe a
	// place found nearby: its coordinates, and what OpenStreetMap called it.
	PlaceID          string   `json:"placeId" graphapi:"nullable"`
	LatitudeDegrees  *float64 `json:"latitudeDegrees" graphapi:"nullable"`
	LongitudeDegrees *float64 `json:"longitudeDegrees" graphapi:"nullable"`
	PlaceName        string   `json:"placeName" graphapi:"nullable"`
	OSMType          string   `json:"osmType" graphapi:"nullable"`
	OSMID            *int64   `json:"osmId" graphapi:"nullable"`
	OSMCategory      string   `json:"osmCategory" graphapi:"nullable"`
	AddressText      string   `json:"addressText" graphapi:"nullable"`

	// AccuracyMeters is how far off the position may be, as the device said;
	// it is kept on the visit. Left out, the visit records no accuracy (0).
	AccuracyMeters *float64 `json:"accuracyMeters" graphapi:"nullable"`
}

type EraseLocationHistoryArguments struct {
	// From and Until bound what is erased; leave both out to erase it all.
	From  *time.Time `json:"from" graphapi:"nullable"`
	Until *time.Time `json:"until" graphapi:"nullable"`
}

const (
	// reportPointsMost and reportVisitsMost bound one batch. A phone that has
	// more sends it in several.
	reportPointsMost = 500
	reportVisitsMost = 50

	// reportActivitiesMost bounds the motion activity readings of a report,
	// as reportPointsMost bounds its positions.
	reportActivitiesMost = 500

	// reportFutureMost is how far ahead of the server's clock a time may be:
	// a phone's clock is not exactly this one's, but a day ahead is a bug.
	reportFutureMost = 24 * time.Hour

	// reportPastMost is how far behind the server's clock a time may be. A
	// phone that was off for a year still sends its last readings, but a
	// time decades back is a bug, and the trips of every day since would be
	// built again for it.
	reportPastMost = 400 * 24 * time.Hour

	// clientVisitKeyLongest bounds the key a phone gives a visit.
	clientVisitKeyLongest = 128

	// locationPathSpanMost and locationPathPointsMost bound a request for the
	// positions of a path: a day or two is what a map draws, and the phone
	// sends a position only when it has moved.
	locationPathSpanMost   = 48 * time.Hour
	locationPathPointsMost = 5000

	// The radius of a search for places near a position, in meters. A check-in
	// list is what is within walking distance, and a search is what is within
	// a drive.
	nearbyRadiusDefaultMeters = 150
	nearbyRadiusMostMeters    = 1000
	searchRadiusDefaultMeters = 5000
	searchRadiusMostMeters    = 50000

	// searchLimitDefault and searchLimitMost bound a page of found places.
	searchLimitDefault = 20
	searchLimitMost    = 50

	// searchQueryMostRunes bounds the words of a search, which go to the
	// place provider.
	searchQueryMostRunes = 200

	// addressLimitDefault and addressLimitMost bound the matches of an address
	// search.
	addressLimitDefault = 8
	addressLimitMost    = 20

	// nearbyOwnPlacesMost is how many of the person's own places a nearby
	// listing shows.
	nearbyOwnPlacesMost = 20
)

// errPlaceProviderOff is what a listing of found places answers while the
// operator has switched the place provider off.
var errPlaceProviderOff = fmt.Errorf("%w: names and places near you come from OpenStreetMap through a place provider, "+
	"which the operator of this server has switched off in the server settings", api.ErrInvalidArguments)

// requireLocationPerson is the caller, if they may record where they go. The
// console is not a person and is nowhere.
func (self *graph) requireLocationPerson(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requirePermission(ctx, models.PermissionLocationUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// locationReach is whose location a call is about, and what the caller may do
// with it.
type locationReach struct {
	User *models.User

	// ShareLevel is owner for the caller's own location, otherwise the
	// highest level any share gives the caller.
	ShareLevel models.LocationShareLevel
}

// reachLocation is the person whose location a call asks about, if the caller
// reaches it at the level asked for or better: their own, named by an empty
// userId or their own, or another person's shared with them (directly or
// through a circle both are still in) at that level. Anything else, and a level
// that falls short, is not found, the same as a person who does not exist,
// because whether somebody exists and shares nothing with the caller is not the
// caller's business.
//
// This is the one question "may this person read this person's location, and
// how much of it", and the answer is the database's GetLocationShareLevel.
func (self *graph) reachLocation(ctx context.Context, principal *api.Principal, userId string, neededLevel models.LocationShareLevel) (*locationReach, error) {
	userId = strings.TrimSpace(userId)
	if userId == "" || userId == principal.User.ID {
		return &locationReach{User: principal.User, ShareLevel: models.LocationShareLevelOwner}, nil
	}
	tx := self.transaction(ctx)
	level, err := tx.GetLocationShareLevel(principal.User.ID, userId)
	if err != nil {
		return nil, err
	}
	if !level.AtLeast(neededLevel) {
		return nil, api.ErrNotFound
	}
	owner, err := tx.GetUser(userId)
	if err != nil {
		return nil, err
	}
	if owner == nil || owner.Disabled() {
		return nil, api.ErrNotFound
	}
	return &locationReach{User: owner, ShareLevel: level}, nil
}

// locationFailure is an error from the database as the API reports it: a
// refusal keeps the words that say what was wrong, which translateError, made
// for refusals nobody can fix, drops.
func locationFailure(err error) error {
	if errors.Is(err, db.ErrInvalidArguments) {
		detail := strings.TrimPrefix(err.Error(), db.ErrInvalidArguments.Error())
		detail = strings.TrimPrefix(detail, ": ")
		if detail == "" {
			return api.ErrInvalidArguments
		}
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, detail)
	}
	return translateError(err)
}

// placeProviderFailure is a failed question to the place provider as the
// person is told it: a busy provider is a refusal they can act on by asking
// again in a moment, anything else says the provider could not be asked.
func placeProviderFailure(err error) error {
	if errors.Is(err, place.ErrProviderBusy) {
		return fmt.Errorf("%w: the place provider is busy; ask again in a moment", api.ErrInvalidArguments)
	}
	return fmt.Errorf("the place provider could not be asked just now: %s", place.FailureSentence(err))
}

// optionalPlaceProvider is the provider the server's settings name now, nil
// while the operator has switched it off, or the refusal to say that it is not
// set up properly.
func (self *graph) optionalPlaceProvider() (place.Provider, error) {
	provider, err := place.ProviderFor(self.config.Current())
	if err != nil {
		return nil, fmt.Errorf("%w: the place provider is not set up properly: %s", api.ErrInvalidArguments, err)
	}
	return provider, nil
}

// placeProvider is the provider the server's settings name now, or the
// refusal to say that there is none.
func (self *graph) placeProvider() (place.Provider, error) {
	provider, err := self.optionalPlaceProvider()
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, errPlaceProviderOff
	}
	return provider, nil
}

func describeLocationPoint(point *models.LocationPoint) *LocationPointView {
	return &LocationPointView{
		LatitudeDegrees: point.LatitudeDegrees, LongitudeDegrees: point.LongitudeDegrees, AccuracyMeters: point.AccuracyMeters,
		AltitudeMeters: point.AltitudeMeters, SpeedMetersPerSecond: point.SpeedMetersPerSecond, CourseDegrees: point.CourseDegrees,
		MeasuredAt: point.MeasuredAt, LocationSource: string(point.LocationSource),
	}
}

func describePlace(found *models.Place) *PlaceView {
	if found == nil {
		return nil
	}
	view := &PlaceView{
		ID: found.ID, UserID: found.UserID, PlaceName: found.PlaceName, IsNamedByPerson: found.IsNamedByPerson,
		PlaceLabel: string(found.PlaceLabel), LatitudeDegrees: found.LatitudeDegrees, LongitudeDegrees: found.LongitudeDegrees,
		RadiusMeters: found.RadiusMeters, OSMType: found.OSMType, OSMName: found.OSMName, OSMCategory: found.OSMCategory,
		AddressText: found.AddressText, IsAddressTyped: found.IsAddressTyped, VisitCount: found.VisitCount, LastVisitAt: found.LastVisitAt,
		NamingErrorMessage: found.NamingErrorMessage, CreatedAt: found.CreatedAt, UpdatedAt: found.UpdatedAt,
	}
	if found.OSMID != 0 {
		osmId := found.OSMID
		view.OSMID = &osmId
	}
	return view
}

// describePlaceForViewer is a place as somebody it was shared with sees it: its
// name, label, address, coordinates and category, which a map and a list of
// visits need, and not when the owner first went there, how the owner's
// naming went, the radius or the OpenStreetMap object, which are the owner's
// own records.
func describePlaceForViewer(found *models.Place) *PlaceView {
	if found == nil {
		return nil
	}
	return &PlaceView{
		ID: found.ID, UserID: found.UserID, PlaceName: found.PlaceName, PlaceLabel: string(found.PlaceLabel),
		LatitudeDegrees: found.LatitudeDegrees, LongitudeDegrees: found.LongitudeDegrees,
		OSMCategory: found.OSMCategory, AddressText: found.AddressText,
	}
}

// describePlaceVisit is a visit as the caller sees it: whole for the person's
// own, with the reduced place for anybody a location is shared with.
func describePlaceVisit(visit *models.PlaceVisit, isOwner bool) *PlaceVisitView {
	described := describePlace(visit.Place)
	if !isOwner {
		described = describePlaceForViewer(visit.Place)
	}
	view := &PlaceVisitView{
		ID: visit.ID, Place: described, ArrivedAt: visit.ArrivedAt, DepartedAt: visit.DepartedAt,
		LatitudeDegrees: visit.LatitudeDegrees, LongitudeDegrees: visit.LongitudeDegrees, AccuracyMeters: visit.AccuracyMeters,
		VisitSource: string(visit.VisitSource),
	}
	if visit.ArrivedAt != nil && visit.DepartedAt != nil {
		seconds := int(visit.DepartedAt.Sub(*visit.ArrivedAt) / time.Second)
		view.DurationSeconds = &seconds
	}
	return view
}

// describeFoundPlace is a place OpenStreetMap found; placeId is the caller's
// own place for the same object, if they have one (own, nil when they have
// none or it could not be read).
func describeFoundPlace(found *place.Found, placeId string, own *models.Place) *FoundPlaceView {
	view := &FoundPlaceView{
		PlaceName: found.PlaceName, OSMType: found.OSMType, OSMID: found.OSMID, OSMCategory: found.OSMCategory,
		AddressText: found.AddressText, LatitudeDegrees: found.LatitudeDegrees, LongitudeDegrees: found.LongitudeDegrees,
		DistanceMeters: found.DistanceMeters,
	}
	if placeId != "" {
		view.PlaceID = &placeId
	}
	if own != nil {
		view.PlaceLabel = string(own.PlaceLabel)
		view.IsNamedByPerson = own.IsNamedByPerson
		if own.IsNamedByPerson {
			view.PlaceName = own.PlaceName
		}
	}
	return view
}

// pageOf reads the offset and limit a caller asked for, with the default and
// the most a page may be.
func pageOf(offset, limitCount *int, defaultLimit, mostLimit int) (int, int, error) {
	pageOffset, pageLimit := 0, defaultLimit
	if offset != nil {
		if *offset < 0 {
			return 0, 0, fmt.Errorf("%w: the offset cannot be negative", api.ErrInvalidArguments)
		}
		pageOffset = *offset
	}
	if limitCount != nil {
		if *limitCount < 1 {
			return 0, 0, fmt.Errorf("%w: ask for at least one result", api.ErrInvalidArguments)
		}
		pageLimit = min(*limitCount, mostLimit)
	}
	return pageOffset, pageLimit, nil
}

func nextOffsetOf(nextOffset int) *int {
	if nextOffset == 0 {
		return nil
	}
	return &nextOffset
}

// placeLabelOf reads a label a caller gave: empty for none, or one of the
// labels.
func placeLabelOf(text string) (models.PlaceLabel, error) {
	label := models.PlaceLabel(strings.ToLower(strings.TrimSpace(text)))
	if label != "" && !label.IsValid() {
		names := make([]string, 0, len(models.PlaceLabels))
		for _, each := range models.PlaceLabels {
			names = append(names, string(each))
		}
		return "", fmt.Errorf("%w: a place label is %s, or empty for none", api.ErrInvalidArguments, strings.Join(names, ", "))
	}
	return label, nil
}

// requireOnEarth refuses coordinates that are not on the earth.
func requireOnEarth(latitudeDegrees, longitudeDegrees float64) error {
	if !place.IsOnEarth(latitudeDegrees, longitudeDegrees) {
		return fmt.Errorf("%w: latitude is between -90 and 90 degrees and longitude between -180 and 180", api.ErrInvalidArguments)
	}
	return nil
}

// attachVisitSummaries fills in how often each place was visited and when
// last, for places shown outside the listing of places, which does it itself.
func attachVisitSummaries(tx db.Transaction, userId string, places []*models.Place) error {
	placeIds := make([]string, 0, len(places))
	for _, each := range places {
		if each != nil {
			placeIds = append(placeIds, each.ID)
		}
	}
	summaries, err := tx.SummarizePlaceVisits(userId, placeIds)
	if err != nil {
		return err
	}
	for _, each := range places {
		if each == nil {
			continue
		}
		if summary := summaries[each.ID]; summary != nil {
			each.VisitCount, each.LastVisitAt = summary.VisitCount, summary.LastVisitAt
		}
	}
	return nil
}

// openVisitOf is the visit the person is believed to be in: the newest one with
// no departure, unless the positions reported since say they left. The one
// place that decides, so that every surface agrees about whether somebody is
// at a place.
func openVisitOf(tx db.Transaction, userId string, latestPoint *models.LocationPoint) (*models.PlaceVisit, error) {
	visit, err := tx.GetOpenPlaceVisit(userId)
	if err != nil || visit == nil {
		return nil, err
	}
	if !place.IsStillThere(visit, latestPoint) {
		return nil, nil
	}
	return visit, nil
}

func (self *graph) GetLocation(ctx context.Context, arguments GetLocationArguments) (*LocationView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachLocation(ctx, principal, arguments.UserID, models.LocationShareLevelCurrent)
	if err != nil {
		return nil, err
	}
	view, err := describeLocation(self.transaction(ctx), reach)
	if err != nil || reach.ShareLevel == models.LocationShareLevelOwner {
		return view, err
	}
	mutes, err := self.transaction(ctx).GetLocationNotificationMutes(principal.User.ID, []string{reach.User.ID}, time.Now())
	if err != nil {
		return nil, err
	}
	view.NotificationMute = describeLocationNotificationMute(mutes[reach.User.ID])
	return view, nil
}

// describeLocation is where a person is, for a viewer who reaches it.
func describeLocation(tx db.Transaction, reach *locationReach) (*LocationView, error) {
	latest, err := tx.GetLatestLocationPoint(reach.User.ID)
	if err != nil {
		return nil, err
	}
	visit, err := openVisitOf(tx, reach.User.ID, latest)
	if err != nil {
		return nil, err
	}
	pause, err := tx.GetLocationPauseInEffect(reach.User.ID, time.Now())
	if err != nil {
		return nil, err
	}
	return locationViewOf(reach, latest, visit, pause), nil
}

// locationViewOf is where a person is, from what was read about them. While
// the person has paused nobody else is shown the latest position or the visit
// in progress, whatever level they reach: the pause is what they are shown.
func locationViewOf(reach *locationReach, latest *models.LocationPoint, visit *models.PlaceVisit, pause *models.LocationPause) *LocationView {
	view := &LocationView{
		UserID: reach.User.ID, Username: reach.User.Username, Name: reach.User.Name,
		AvatarURL: avatarUrl(reach.User), AvatarColorIndex: avatar.InitialColorIndex(reach.User.ID),
		ShareLevel: string(reach.ShareLevel), Pause: describeLocationPause(pause),
	}
	if pause != nil && reach.ShareLevel != models.LocationShareLevelOwner {
		return view
	}
	if latest != nil {
		view.LatestPoint = describeLocationPoint(latest)
	}
	if visit != nil && place.IsStillThere(visit, latest) {
		view.OpenVisit = describePlaceVisit(visit, reach.ShareLevel == models.LocationShareLevelOwner)
	}
	return view
}

func (self *graph) ListLocationVisits(ctx context.Context, arguments ListLocationVisitsArguments) (*PlaceVisitPageView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachLocation(ctx, principal, arguments.UserID, models.LocationShareLevelHistory)
	if err != nil {
		return nil, err
	}
	offset, limitCount, err := pageOf(arguments.Offset, arguments.LimitCount, db.PlacePageDefault, db.PlacePageMost)
	if err != nil {
		return nil, err
	}
	if arguments.From != nil && arguments.Until != nil && !arguments.From.Before(*arguments.Until) {
		return nil, fmt.Errorf("%w: the range must start before it ends", api.ErrInvalidArguments)
	}
	placeLabel, err := placeLabelOf(arguments.PlaceLabel)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	visits, nextOffset, err := tx.ListPlaceVisits(reach.User.ID, db.PlaceVisitFilter{
		From: arguments.From, Until: arguments.Until, PlaceID: strings.TrimSpace(arguments.PlaceID), PlaceLabel: placeLabel,
		Offset: offset, LimitCount: limitCount,
	})
	if err != nil {
		return nil, locationFailure(err)
	}
	places := make([]*models.Place, 0, len(visits))
	for _, visit := range visits {
		places = append(places, visit.Place)
	}
	if err := attachVisitSummaries(tx, reach.User.ID, places); err != nil {
		return nil, err
	}
	page := &PlaceVisitPageView{PlaceVisits: make([]*PlaceVisitView, 0, len(visits)), NextOffset: nextOffsetOf(nextOffset)}
	for _, visit := range visits {
		page.PlaceVisits = append(page.PlaceVisits, describePlaceVisit(visit, reach.ShareLevel == models.LocationShareLevelOwner))
	}
	if reach.ShareLevel == models.LocationShareLevelOwner {
		if err := self.attachVisitPhotos(ctx, principal, visits, page.PlaceVisits); err != nil {
			return nil, err
		}
	}
	return page, nil
}

// visitPhotoPreviewCount is how many pictures a visit carries.
const visitPhotoPreviewCount = 4

// attachVisitPhotos fills in the photos the caller took during each of their
// own visits. A caller who may not use Photos (the service is off, or the
// permission is missing) gets nulls and no error: Location works without
// Photos. A visit with no arrival has no window, so it stays null. A visit
// nobody ended that the person is no longer at (the phone never sent the
// departure) ends at their latest position, by the rule GetLocation uses;
// only a visit the person is still at runs to now. The views are the visits,
// in the same order.
func (self *graph) attachVisitPhotos(ctx context.Context, principal *api.Principal, visits []*models.PlaceVisit, views []*PlaceVisitView) error {
	if !self.isServiceEnabled(models.ServicePhotos) || !principal.Permissions.Has(models.PermissionPhotosUse) {
		return nil
	}
	now := time.Now().UTC()
	var latest *models.LocationPoint
	for _, visit := range visits {
		if visit.DepartedAt == nil {
			var err error
			if latest, err = self.transaction(ctx).GetLatestLocationPoint(principal.User.ID); err != nil {
				return translateError(err)
			}
			break
		}
	}
	windows := make([]db.VisitPhotoWindow, 0, len(views))
	endedAtOf := make(map[string]time.Time, len(views))
	for index, view := range views {
		if view.ArrivedAt == nil {
			continue
		}
		endedAt := now
		if view.DepartedAt != nil {
			endedAt = *view.DepartedAt
		} else if !place.IsStillThere(visits[index], latest) {
			endedAt = latest.MeasuredAt
		}
		windows = append(windows, db.VisitPhotoWindow{VisitID: view.ID, StartedAt: *view.ArrivedAt, EndedAt: endedAt})
		endedAtOf[view.ID] = endedAt
	}
	if len(windows) == 0 {
		return nil
	}
	found, err := self.transaction(ctx).ListVisitPhotos(principal.User.ID, windows, visitPhotoPreviewCount)
	if err != nil {
		return translateError(err)
	}
	var shown []*models.Photo
	for _, view := range views {
		visitPhotos := found[view.ID]
		if view.ArrivedAt == nil || visitPhotos == nil {
			continue
		}
		photoCount := visitPhotos.PhotoCount
		endedAt := endedAtOf[view.ID]
		view.PhotoCount, view.Photos, view.PhotosTakenBefore = &photoCount, &visitPhotos.Photos, &endedAt
		shown = append(shown, visitPhotos.Photos...)
	}
	if len(shown) == 0 {
		return nil
	}
	return translateError(self.withFilesAndPreviews(ctx, principal.User.ID, shown, ""))
}

func (self *graph) ListPlaces(ctx context.Context, arguments ListPlacesArguments) (*PlacePageView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachLocation(ctx, principal, arguments.UserID, models.LocationShareLevelHistory)
	if err != nil {
		return nil, err
	}
	offset, limitCount, err := pageOf(arguments.Offset, arguments.LimitCount, db.PlacePageDefault, db.PlacePageMost)
	if err != nil {
		return nil, err
	}
	placeLabel, err := placeLabelOf(arguments.PlaceLabel)
	if err != nil {
		return nil, err
	}
	places, nextOffset, err := self.transaction(ctx).ListPlaces(reach.User.ID, db.PlaceFilter{
		PlaceLabel: placeLabel, Query: arguments.Query, Offset: offset, LimitCount: limitCount,
	})
	if err != nil {
		return nil, locationFailure(err)
	}
	page := &PlacePageView{Places: make([]*PlaceView, 0, len(places)), NextOffset: nextOffsetOf(nextOffset)}
	for _, each := range places {
		if reach.ShareLevel == models.LocationShareLevelOwner {
			page.Places = append(page.Places, describePlace(each))
		} else {
			page.Places = append(page.Places, describePlaceForViewer(each))
		}
	}
	return page, nil
}

func (self *graph) ReadPlace(ctx context.Context, arguments ReadPlaceArguments) (*PlaceView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachLocation(ctx, principal, arguments.UserID, models.LocationShareLevelHistory)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	found, err := tx.GetPlace(reach.User.ID, strings.TrimSpace(arguments.PlaceID))
	if err != nil {
		return nil, locationFailure(err)
	}
	if err := attachVisitSummaries(tx, reach.User.ID, []*models.Place{found}); err != nil {
		return nil, locationFailure(err)
	}
	if reach.ShareLevel == models.LocationShareLevelOwner {
		return describePlace(found), nil
	}
	return describePlaceForViewer(found), nil
}

// ListNearbyPlaces waits on the place provider, so it reads in two short
// phases of its own (isModelBackedQuery) with the call outside both, and a
// slow provider holds no database connection while it takes its time.
func (self *graph) ListNearbyPlaces(ctx context.Context, arguments ListNearbyPlacesArguments) ([]*FoundPlaceView, error) {
	var userId string
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		principal, err := self.requireLocationPerson(phaseContext)
		if err != nil {
			return err
		}
		userId = principal.User.ID
		return nil
	}); err != nil {
		return nil, err
	}
	if err := requireOnEarth(arguments.LatitudeDegrees, arguments.LongitudeDegrees); err != nil {
		return nil, err
	}
	radiusMeters := float64(nearbyRadiusDefaultMeters)
	if arguments.RadiusMeters != nil {
		radiusMeters = *arguments.RadiusMeters
	}
	if !(radiusMeters > 0) || radiusMeters > nearbyRadiusMostMeters {
		return nil, fmt.Errorf("%w: look within 1 to %d meters", api.ErrInvalidArguments, nearbyRadiusMostMeters)
	}
	provider, err := self.optionalPlaceProvider()
	if err != nil {
		return nil, err
	}
	// The provider names the one place at the position (neither server can
	// list what is around without words to match), and the caller's own
	// places there are added to it, so that a check-in list always offers the
	// places they have been to before. With the provider off, it is their own
	// places alone.
	var found []*place.Found
	if provider != nil {
		if err := self.allowPlaceLookup(userId); err != nil {
			return nil, err
		}
		if found, err = provider.Search(ctx, "", arguments.LatitudeDegrees, arguments.LongitudeDegrees, radiusMeters, 1); err != nil {
			return nil, placeProviderFailure(err)
		}
	}
	var rows []*FoundPlaceView
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		// Asked again after the wait, so that a permission taken away while
		// it lasted releases nothing.
		if _, err := self.requireLocationPerson(phaseContext); err != nil {
			return err
		}
		tx := self.transaction(phaseContext)
		near, err := tx.ListPlacesNear(userId, arguments.LatitudeDegrees, arguments.LongitudeDegrees, radiusMeters, nearbyOwnPlacesMost)
		if err != nil {
			return err
		}
		rows, err = self.mergeFoundWithOwn(tx, userId, found, near)
		return err
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

// mergeFoundWithOwn is what the provider found and the caller's own places
// near the same position as one list, nearest first. A found place that is one
// the caller already has is shown once, as theirs, with their name for it.
func (self *graph) mergeFoundWithOwn(tx db.Transaction, userId string, found []*place.Found, near []*db.PlaceDistance) ([]*FoundPlaceView, error) {
	references := make([]db.OSMReference, 0, len(found))
	for _, each := range found {
		references = append(references, db.OSMReference{OSMType: each.OSMType, OSMID: each.OSMID})
	}
	ownByObject, err := tx.FindPlaceIDsByOSM(userId, references)
	if err != nil {
		return nil, err
	}
	rows := make([]*FoundPlaceView, 0, len(found)+len(near))
	listed := map[string]bool{}
	for _, own := range near {
		listed[own.Place.ID] = true
		rows = append(rows, &FoundPlaceView{
			PlaceName: own.Place.PlaceName, OSMType: own.Place.OSMType, OSMID: own.Place.OSMID, OSMCategory: own.Place.OSMCategory,
			AddressText: own.Place.AddressText, LatitudeDegrees: own.Place.LatitudeDegrees, LongitudeDegrees: own.Place.LongitudeDegrees,
			DistanceMeters: own.DistanceMeters, PlaceID: &own.Place.ID,
			PlaceLabel: string(own.Place.PlaceLabel), IsNamedByPerson: own.Place.IsNamedByPerson,
		})
	}
	for _, each := range found {
		placeId := ownByObject[db.OSMReference{OSMType: each.OSMType, OSMID: each.OSMID}]
		if placeId != "" && listed[placeId] {
			continue
		}
		var ownPlace *models.Place
		if placeId != "" {
			if ownPlace, err = tx.GetPlace(userId, placeId); err != nil {
				return nil, err
			}
		}
		rows = append(rows, describeFoundPlace(each, placeId, ownPlace))
	}
	sortFoundByDistance(rows)
	return rows, nil
}

func sortFoundByDistance(rows []*FoundPlaceView) {
	// A stable insertion sort: the lists are a few rows long, and the order
	// of rows at the same distance stays the order they were added in.
	for index := 1; index < len(rows); index++ {
		for earlier := index; earlier > 0 && rows[earlier].DistanceMeters < rows[earlier-1].DistanceMeters; earlier-- {
			rows[earlier], rows[earlier-1] = rows[earlier-1], rows[earlier]
		}
	}
}

// SearchPlaces reads in phases for the reason ListNearbyPlaces does.
func (self *graph) SearchPlaces(ctx context.Context, arguments SearchPlacesArguments) (*FoundPlacePageView, error) {
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: say what to search for", api.ErrInvalidArguments)
	}
	if utf8.RuneCountInString(query) > searchQueryMostRunes {
		return nil, fmt.Errorf("%w: search for at most %d characters", api.ErrInvalidArguments, searchQueryMostRunes)
	}
	offset, limitCount, err := pageOf(arguments.Offset, arguments.LimitCount, searchLimitDefault, searchLimitMost)
	if err != nil {
		return nil, err
	}
	radiusMeters := float64(searchRadiusDefaultMeters)
	if arguments.RadiusMeters != nil {
		radiusMeters = *arguments.RadiusMeters
	}
	if !(radiusMeters > 0) || radiusMeters > searchRadiusMostMeters {
		return nil, fmt.Errorf("%w: search within 1 to %d meters", api.ErrInvalidArguments, searchRadiusMostMeters)
	}
	var userId string
	var latitudeDegrees, longitudeDegrees float64
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		principal, err := self.requireLocationPerson(phaseContext)
		if err != nil {
			return err
		}
		userId = principal.User.ID
		latitudeDegrees, longitudeDegrees, err = self.searchCenter(self.transaction(phaseContext), userId, arguments)
		return err
	}); err != nil {
		return nil, err
	}
	provider, err := self.placeProvider()
	if err != nil {
		return nil, err
	}
	if err := self.allowPlaceLookup(userId); err != nil {
		return nil, err
	}
	// One more than the page, to know whether there is a next one.
	found, err := place.SearchKinds(ctx, provider, query, latitudeDegrees, longitudeDegrees, radiusMeters, offset+limitCount+1)
	if err != nil {
		return nil, placeProviderFailure(err)
	}
	nextOffset := 0
	if len(found) > offset+limitCount {
		nextOffset = offset + limitCount
	}
	if offset >= len(found) {
		found = nil
	} else {
		found = found[offset:min(len(found), offset+limitCount)]
	}
	references := make([]db.OSMReference, 0, len(found))
	for _, each := range found {
		references = append(references, db.OSMReference{OSMType: each.OSMType, OSMID: each.OSMID})
	}
	var ownByObject map[db.OSMReference]string
	ownPlaces := map[string]*models.Place{}
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		if _, err := self.requireLocationPerson(phaseContext); err != nil {
			return err
		}
		tx := self.transaction(phaseContext)
		var err error
		if ownByObject, err = tx.FindPlaceIDsByOSM(userId, references); err != nil {
			return err
		}
		for _, placeId := range ownByObject {
			if ownPlaces[placeId] != nil {
				continue
			}
			if ownPlaces[placeId], err = tx.GetPlace(userId, placeId); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	page := &FoundPlacePageView{FoundPlaces: make([]*FoundPlaceView, 0, len(found)), NextOffset: nextOffsetOf(nextOffset)}
	for _, each := range found {
		placeId := ownByObject[db.OSMReference{OSMType: each.OSMType, OSMID: each.OSMID}]
		page.FoundPlaces = append(page.FoundPlaces, describeFoundPlace(each, placeId, ownPlaces[placeId]))
	}
	return page, nil
}

// SearchAddresses waits on the place provider, so it reads in phases for the
// reason ListNearbyPlaces does. The matches come nearest first to the place
// or the position asked about, else to where the caller was last reported to
// be, with their distances from it; without any position the provider's
// order stands and the distances are zero.
func (self *graph) SearchAddresses(ctx context.Context, arguments SearchAddressesArguments) ([]*FoundPlaceView, error) {
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: type some of the address", api.ErrInvalidArguments)
	}
	if utf8.RuneCountInString(query) > searchQueryMostRunes {
		return nil, fmt.Errorf("%w: search for at most %d characters", api.ErrInvalidArguments, searchQueryMostRunes)
	}
	limitCount := addressLimitDefault
	if arguments.LimitCount != nil {
		limitCount = *arguments.LimitCount
	}
	if limitCount < 1 || limitCount > addressLimitMost {
		return nil, fmt.Errorf("%w: ask for 1 to %d addresses", api.ErrInvalidArguments, addressLimitMost)
	}
	var userId string
	var near *place.Position
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		principal, err := self.requireLocationPerson(phaseContext)
		if err != nil {
			return err
		}
		userId = principal.User.ID
		near, err = self.addressCenter(self.transaction(phaseContext), userId, arguments)
		return err
	}); err != nil {
		return nil, err
	}
	provider, err := self.placeProvider()
	if err != nil {
		return nil, err
	}
	if err := self.allowPlaceLookup(userId); err != nil {
		return nil, err
	}
	found, err := provider.Addresses(ctx, query, near, limitCount)
	if err != nil {
		return nil, placeProviderFailure(err)
	}
	if len(found) > limitCount {
		found = found[:limitCount]
	}
	var ownPlaces []*models.Place
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		if _, err := self.requireLocationPerson(phaseContext); err != nil {
			return err
		}
		tx := self.transaction(phaseContext)
		ownPlaces = make([]*models.Place, len(found))
		for index, each := range found {
			var err error
			if ownPlaces[index], err = tx.FindPlaceAt(userId, each.LatitudeDegrees, each.LongitudeDegrees); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	views := make([]*FoundPlaceView, 0, len(found))
	for index, each := range found {
		placeId := ""
		if ownPlaces[index] != nil {
			placeId = ownPlaces[index].ID
		}
		views = append(views, describeFoundPlace(each, placeId, ownPlaces[index]))
	}
	return views, nil
}

// searchCenter is the position a search is around: the one given, one of the
// caller's places, or where the caller was last reported to be.
func (self *graph) searchCenter(tx db.Transaction, userId string, arguments SearchPlacesArguments) (float64, float64, error) {
	center, err := self.positionAsked(tx, userId, arguments.LatitudeDegrees, arguments.LongitudeDegrees, arguments.PlaceID)
	if err != nil {
		return 0, 0, err
	}
	if center == nil {
		return 0, 0, fmt.Errorf("%w: there is no position to search around: give latitudeDegrees and longitudeDegrees, "+
			"or a placeId, or report a location first (the TeaNode app on the iPhone does)", api.ErrInvalidArguments)
	}
	return center.LatitudeDegrees, center.LongitudeDegrees, nil
}

// addressCenter is the position an address search puts nearest first, as
// searchCenter finds it, or nil when the caller has never reported one: an
// address can be looked up anywhere.
func (self *graph) addressCenter(tx db.Transaction, userId string, arguments SearchAddressesArguments) (*place.Position, error) {
	return self.positionAsked(tx, userId, arguments.LatitudeDegrees, arguments.LongitudeDegrees, arguments.PlaceID)
}

// positionAsked is the position given, else the position of one of the
// caller's places, else where the caller was last reported to be, else nil.
func (self *graph) positionAsked(tx db.Transaction, userId string, latitudeDegrees, longitudeDegrees *float64, placeId string) (*place.Position, error) {
	if (latitudeDegrees == nil) != (longitudeDegrees == nil) {
		return nil, fmt.Errorf("%w: give both latitudeDegrees and longitudeDegrees", api.ErrInvalidArguments)
	}
	if latitudeDegrees != nil {
		if err := requireOnEarth(*latitudeDegrees, *longitudeDegrees); err != nil {
			return nil, err
		}
		return &place.Position{LatitudeDegrees: *latitudeDegrees, LongitudeDegrees: *longitudeDegrees}, nil
	}
	if placeId := strings.TrimSpace(placeId); placeId != "" {
		found, err := tx.GetPlace(userId, placeId)
		if err != nil {
			return nil, translateError(err)
		}
		return &place.Position{LatitudeDegrees: found.LatitudeDegrees, LongitudeDegrees: found.LongitudeDegrees}, nil
	}
	latest, err := tx.GetLatestLocationPoint(userId)
	if err != nil || latest == nil {
		return nil, err
	}
	return &place.Position{LatitudeDegrees: latest.LatitudeDegrees, LongitudeDegrees: latest.LongitudeDegrees}, nil
}

func (self *graph) ListLocationPoints(ctx context.Context, arguments ListLocationPointsArguments) ([]*LocationPointView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachLocation(ctx, principal, arguments.UserID, models.LocationShareLevelHistory)
	if err != nil {
		return nil, err
	}
	if !arguments.From.Before(arguments.Until) {
		return nil, fmt.Errorf("%w: the range must start before it ends", api.ErrInvalidArguments)
	}
	if arguments.Until.Sub(arguments.From) > locationPathSpanMost {
		return nil, fmt.Errorf("%w: ask for at most two days of positions at a time", api.ErrInvalidArguments)
	}
	points, err := self.transaction(ctx).ListLocationPoints(reach.User.ID, arguments.From, arguments.Until, locationPathPointsMost+1)
	if err != nil {
		return nil, locationFailure(err)
	}
	if len(points) > locationPathPointsMost {
		return nil, fmt.Errorf("%w: that range holds more than %d positions; ask for a shorter one", api.ErrInvalidArguments, locationPathPointsMost)
	}
	views := make([]*LocationPointView, 0, len(points))
	for _, point := range points {
		views = append(views, describeLocationPoint(point))
	}
	return views, nil
}

// validateReport refuses a whole batch for the first thing wrong with it,
// before anything is stored, in words a person reading the phone's log can
// act on.
func validateReport(arguments ReportLocationArguments, now time.Time) error {
	if len(arguments.Points) > reportPointsMost {
		return fmt.Errorf("%w: send at most %d positions at a time", api.ErrInvalidArguments, reportPointsMost)
	}
	if len(arguments.Visits) > reportVisitsMost {
		return fmt.Errorf("%w: send at most %d visits at a time", api.ErrInvalidArguments, reportVisitsMost)
	}
	latest, earliest := now.Add(reportFutureMost), now.Add(-reportPastMost)
	for index, point := range arguments.Points {
		switch {
		case !place.IsOnEarth(point.LatitudeDegrees, point.LongitudeDegrees):
			return fmt.Errorf("%w: position %d is not on the earth", api.ErrInvalidArguments, index+1)
		case !(point.AccuracyMeters >= 0) || math.IsInf(point.AccuracyMeters, 0):
			return fmt.Errorf("%w: position %d has an accuracy that is not zero or more", api.ErrInvalidArguments, index+1)
		case point.MeasuredAt.IsZero():
			return fmt.Errorf("%w: position %d has no time", api.ErrInvalidArguments, index+1)
		case point.MeasuredAt.After(latest):
			return fmt.Errorf("%w: position %d is measured more than a day in the future", api.ErrInvalidArguments, index+1)
		case point.MeasuredAt.Before(earliest):
			return fmt.Errorf("%w: position %d is measured more than 400 days ago", api.ErrInvalidArguments, index+1)
		}
	}
	if len(arguments.Activities) > reportActivitiesMost {
		return fmt.Errorf("%w: send at most %d motion activity readings at a time", api.ErrInvalidArguments, reportActivitiesMost)
	}
	for index, activity := range arguments.Activities {
		switch {
		case activity.StartedAt.IsZero():
			return fmt.Errorf("%w: activity %d has no time", api.ErrInvalidArguments, index+1)
		case activity.StartedAt.After(latest):
			return fmt.Errorf("%w: activity %d starts more than a day in the future", api.ErrInvalidArguments, index+1)
		case activity.StartedAt.Before(earliest):
			return fmt.Errorf("%w: activity %d starts more than 400 days ago", api.ErrInvalidArguments, index+1)
		case !travel.IsActivityKind(activity.ActivityKind):
			return fmt.Errorf("%w: activity %d has the kind %q; one of stationary, walking, running, cycling, automotive, unknown", api.ErrInvalidArguments, index+1, activity.ActivityKind)
		case !travel.IsActivityConfidence(activity.ActivityConfidence):
			return fmt.Errorf("%w: activity %d has the confidence %q; one of low, medium, high", api.ErrInvalidArguments, index+1, activity.ActivityConfidence)
		}
	}
	for index, visit := range arguments.Visits {
		key := strings.TrimSpace(visit.ClientVisitKey)
		switch {
		case key == "" || len(key) > clientVisitKeyLongest:
			return fmt.Errorf("%w: visit %d needs a clientVisitKey of 1 to %d characters", api.ErrInvalidArguments, index+1, clientVisitKeyLongest)
		case !place.IsOnEarth(visit.LatitudeDegrees, visit.LongitudeDegrees):
			return fmt.Errorf("%w: visit %d is not on the earth", api.ErrInvalidArguments, index+1)
		case !(visit.AccuracyMeters >= 0) || math.IsInf(visit.AccuracyMeters, 0):
			return fmt.Errorf("%w: visit %d has an accuracy that is not zero or more", api.ErrInvalidArguments, index+1)
		case visit.ArrivedAt == nil && visit.DepartedAt == nil:
			return fmt.Errorf("%w: visit %d has neither an arrival nor a departure", api.ErrInvalidArguments, index+1)
		case visit.ArrivedAt != nil && visit.ArrivedAt.After(latest), visit.DepartedAt != nil && visit.DepartedAt.After(latest):
			return fmt.Errorf("%w: visit %d is more than a day in the future", api.ErrInvalidArguments, index+1)
		case visit.ArrivedAt != nil && visit.ArrivedAt.Before(earliest), visit.DepartedAt != nil && visit.DepartedAt.Before(earliest):
			return fmt.Errorf("%w: visit %d is more than 400 days ago", api.ErrInvalidArguments, index+1)
		case visit.ArrivedAt != nil && visit.DepartedAt != nil && visit.DepartedAt.Before(*visit.ArrivedAt):
			return fmt.Errorf("%w: visit %d departs before it arrives", api.ErrInvalidArguments, index+1)
		}
		switch models.VisitSource(visit.VisitSource) {
		case models.VisitSourceVisit, models.VisitSourceRegion:
		default:
			return fmt.Errorf("%w: visit %d has the source %q; a phone reports visit or region", api.ErrInvalidArguments, index+1, visit.VisitSource)
		}
	}
	return nil
}

// placeNameFor is the name a place has until it is named: the city from the
// table built into the server, or the coordinates where there is no city
// near. The job that names places replaces it when the provider knows better.
func placeNameFor(latitudeDegrees, longitudeDegrees float64) string {
	if city, isFound := place.Nearest(latitudeDegrees, longitudeDegrees); isFound {
		return city
	}
	return fmt.Sprintf("%.4f, %.4f", latitudeDegrees, longitudeDegrees)
}

func (self *graph) ReportLocation(ctx context.Context, arguments ReportLocationArguments) (*ReportLocationView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateReport(arguments, time.Now()); err != nil {
		return nil, err
	}
	user := principal.User
	tx := self.transaction(ctx)

	// One report of a person at a time. A phone that sends a batch again while
	// the first send is still being stored would otherwise find no visit for a
	// key, and no place at a position, in both, and make one of each in both.
	if err := tx.LockLocation(user.ID); err != nil {
		return nil, err
	}
	// Taken after the lock, so that a pause that began while this report
	// waited for it is in effect at now and is seen.
	now := time.Now()

	// The install that reports must be one of the caller's own.
	phoneId := strings.TrimSpace(arguments.AgentPhoneID)
	if phoneId != "" {
		owner, err := tx.GetAgentByUser(user.ID)
		if err != nil {
			return nil, err
		}
		var phone *models.AgentPhone
		if owner != nil {
			if phone, err = tx.GetAgentPhone(owner.ID, phoneId); err != nil {
				return nil, translateError(err)
			}
		}
		if phone == nil {
			return nil, fmt.Errorf("%w: no phone of yours has that id", api.ErrNotFound)
		}
	}

	points := make([]*models.LocationPoint, 0, len(arguments.Points))
	for _, point := range arguments.Points {
		points = append(points, &models.LocationPoint{
			LatitudeDegrees: point.LatitudeDegrees, LongitudeDegrees: point.LongitudeDegrees, AccuracyMeters: point.AccuracyMeters,
			AltitudeMeters: point.AltitudeMeters, SpeedMetersPerSecond: point.SpeedMetersPerSecond, CourseDegrees: point.CourseDegrees,
			MeasuredAt: point.MeasuredAt, ReceivedAt: now, LocationSource: models.LocationSourcePhone, AgentPhoneID: phoneId,
		})
	}
	// What was measured while the person had paused is not stored, whatever
	// the phone has been told: a phone that was offline sends its day when it
	// is back, and must not fill the gap. The pauses of the whole report are
	// read at once.
	var pauses locationPauseSet
	if from, until, hasTimes := reportRange(arguments); hasTimes {
		if from, err = self.pauseRangeStart(tx, user.ID, from, arguments.Visits); err != nil {
			return nil, err
		}
		if pauses, err = readLocationPauseSet(tx, user.ID, from, until, now); err != nil {
			return nil, err
		}
	}
	points, pausedPointCount := pauses.keptPoints(points)
	newPointCount, err := tx.InsertLocationPoints(user.ID, points)
	if err != nil {
		return nil, locationFailure(err)
	}
	// Motion activity is kept like positions: none of what began inside a
	// pause.
	activities := make([]*models.MotionActivity, 0, len(arguments.Activities))
	for _, activity := range arguments.Activities {
		if pauses.covering(activity.StartedAt) != nil {
			continue
		}
		activities = append(activities, &models.MotionActivity{
			StartedAt: activity.StartedAt, ActivityKind: activity.ActivityKind, ActivityConfidence: activity.ActivityConfidence,
		})
	}
	newActivityCount, err := tx.PutMotionActivities(user.ID, activities)
	if err != nil {
		return nil, locationFailure(err)
	}

	// The agent's source of where the person went is made with the first
	// report, and is the same one afterwards.
	if owner, err := tx.GetAgentByUser(user.ID); err != nil {
		return nil, err
	} else if owner != nil {
		if _, err := tx.EnsureLocationSource(owner.ID); err != nil {
			return nil, err
		}
	}

	view := &ReportLocationView{NewPointCount: newPointCount, NewActivityCount: newActivityCount, PausedPointCount: pausedPointCount}
	// Read before the visits are stored, which end the visits they name.
	unseen, err := readUnseenDepartures(tx, user.ID, arguments.Visits)
	if err != nil {
		return nil, err
	}
	var notices []visitNotice
	var madePlaces []*models.Place
	// storedArrivedAt is the earliest arrival of a visit the report added or
	// changed, as stored.
	var storedArrivedAt *time.Time
	isChanged := newPointCount > 0 || newActivityCount > 0
	for _, reported := range arguments.Visits {
		stored, err := self.putReportedVisit(tx, user.ID, reported, pauses)
		if err != nil {
			return nil, err
		}
		if stored.IsPaused {
			view.PausedVisitCount++
			continue
		}
		if stored.IsRefused {
			view.RefusedVisitCount++
			continue
		}
		if stored.IsNew {
			view.NewVisitCount++
		} else if stored.IsChanged {
			// A batch sent again changes nothing, and says so.
			view.UpdatedVisitCount++
		}
		isChanged = isChanged || stored.IsNew || stored.IsChanged
		if (stored.IsNew || stored.IsChanged) && stored.ArrivedAt != nil && (storedArrivedAt == nil || stored.ArrivedAt.Before(*storedArrivedAt)) {
			storedArrivedAt = stored.ArrivedAt
		}
		if stored.Notice != nil {
			notices = append(notices, *stored.Notice)
		}
		if stored.MadePlace != nil {
			madePlaces = append(madePlaces, stored.MadePlace)
		}
	}
	if notices, err = unseen.filter(tx, user.ID, notices); err != nil {
		return nil, err
	}
	if view.RefusedVisitCount > 0 {
		log.Warningf("user %s is at %d places, the most there can be: %d visits of a report were left out for needing a new place",
			user.ID, db.PlaceCountMost, view.RefusedVisitCount)
	}
	// Visits delivered in any order end up as the sequence they would have
	// been in order.
	if len(arguments.Visits) > 0 {
		from, until := reportedVisitRange(arguments.Visits)
		// From the arrival of a visit the report completed, too: a departure
		// the phone sent late can carry a visit over the visits at other
		// places since it began, which the range of the report alone would
		// not reach.
		if storedArrivedAt != nil && storedArrivedAt.Before(from) {
			from = *storedArrivedAt
		}
		if err := tx.NormalizePlaceVisits(user.ID, from, until); err != nil {
			return nil, locationFailure(err)
		}
	}
	// Crossing a place's circle on the way somewhere else is not a visit, and
	// nobody is told of it.
	if from, until, hasTimes := reportRange(arguments); hasTimes {
		deletedIds, err := tx.DeletePassByVisits(user.ID, from, until)
		if err != nil {
			return nil, locationFailure(err)
		}
		if len(deletedIds) > 0 {
			isChanged = true
			notices = slices.DeleteFunc(notices, func(notice visitNotice) bool { return slices.Contains(deletedIds, notice.Visit.ID) })
			// A pass-by ended or split the visits around it at its arrival;
			// without it, those at one place close together are one again.
			if err := tx.NormalizePlaceVisits(user.ID, from, until); err != nil {
				return nil, locationFailure(err)
			}
		}
	}
	if err := tellDeparturesWhenTheyEnded(tx, user.ID, notices); err != nil {
		return nil, err
	}
	// The trips around each run of the times the report touched, from the
	// arrival of a visit it completed, are built again from what is stored
	// now. A run is times with no gap of over two days: a report holding two
	// distant days does not rebuild the days between.
	if times := reportTimes(arguments, storedArrivedAt); len(times) > 0 && isChanged {
		if err := rebuild.AroundTimes(tx, user.ID, times); err != nil {
			return nil, locationFailure(err)
		}
	}
	self.takeNewPlacesAfterCommit(tx, user.ID, len(madePlaces))
	// A batch the server already had changes nothing, so the people watching
	// the map are not told to read the same position again.
	if isChanged {
		self.afterLocationChange(ctx, tx, user.ID, notices, madePlaces)
	}
	pause, err := tx.GetLocationPauseInEffect(user.ID, time.Now())
	if err != nil {
		return nil, err
	}
	view.Pause = describeLocationPause(pause)
	return view, nil
}

// tellDeparturesWhenTheyEnded moves each departure notice to when its visit
// ended as stored. A departure the phone sent late, after visits at other
// places that began since, was over at the first of them, where
// NormalizePlaceVisits ended the visit; told by that time, the window of
// NotifyPlaceVisitAt keeps "left home" from being told hours later.
func tellDeparturesWhenTheyEnded(tx db.Transaction, userId string, notices []visitNotice) error {
	for index := range notices {
		notice := &notices[index]
		if notice.IsArrival || notice.Visit == nil {
			continue
		}
		current, err := tx.GetPlaceVisitByClientKey(userId, notice.Visit.ClientVisitKey)
		if err != nil {
			return err
		}
		if current != nil && current.DepartedAt != nil && current.DepartedAt.Before(notice.HappenedAt) {
			notice.HappenedAt = *current.DepartedAt
		}
	}
	return nil
}

// reportedVisitRange is the earliest and the latest time the visits of a
// report give.
func reportedVisitRange(reported []ReportedVisit) (time.Time, time.Time) {
	var from, until time.Time
	for _, visit := range reported {
		for _, at := range []*time.Time{visit.ArrivedAt, visit.DepartedAt} {
			if at == nil {
				continue
			}
			if from.IsZero() || at.Before(from) {
				from = *at
			}
			if until.IsZero() || at.After(until) {
				until = *at
			}
		}
	}
	return from, until
}

// reportRange is the earliest and the latest time the positions and the
// visits of a report give, and whether it gives any.
// reportTimes are the times a report gives: those of its positions, motion
// activity and visits, and the arrival of a visit it completed as stored.
func reportTimes(arguments ReportLocationArguments, storedArrivedAt *time.Time) []time.Time {
	times := make([]time.Time, 0, len(arguments.Points)+len(arguments.Activities)+2*len(arguments.Visits)+1)
	for _, point := range arguments.Points {
		times = append(times, point.MeasuredAt)
	}
	for _, activity := range arguments.Activities {
		times = append(times, activity.StartedAt)
	}
	for _, visit := range arguments.Visits {
		for _, at := range []*time.Time{visit.ArrivedAt, visit.DepartedAt} {
			if at != nil {
				times = append(times, *at)
			}
		}
	}
	if storedArrivedAt != nil {
		times = append(times, *storedArrivedAt)
	}
	return times
}

func reportRange(arguments ReportLocationArguments) (time.Time, time.Time, bool) {
	from, until := reportedVisitRange(arguments.Visits)
	times := make([]time.Time, 0, len(arguments.Points)+len(arguments.Activities))
	for _, point := range arguments.Points {
		times = append(times, point.MeasuredAt)
	}
	for _, activity := range arguments.Activities {
		times = append(times, activity.StartedAt)
	}
	for _, at := range times {
		if from.IsZero() || at.Before(from) {
			from = at
		}
		if until.IsZero() || at.After(until) {
			until = at
		}
	}
	return from, until, !from.IsZero()
}

// unseenDepartures are the departures of a report that nobody saw. When iOS
// says the person is inside a place the phone still counts them at (the app
// watched its places afresh, or the phone missed them leaving), the phone
// ends the visit it counted and begins another, and sends the two together: a
// region departure and a region arrival at the same place in the same second
// (LocationOutbox.arrive in the app). Nobody is told of that departure, since
// when the person left, or whether they did, is not known; the arrival is
// told only when a position since the earlier arrival was outside the place.
type unseenDepartures struct {
	// startedAt is, for each place and second (unseenDepartureKey), when the
	// visit the departure ends began, or nil when the server did not know it.
	startedAt map[string]*time.Time
}

func unseenDepartureKey(placeId string, at time.Time) string {
	return placeId + "\x00" + at.Truncate(time.Second).UTC().Format(time.RFC3339)
}

// readUnseenDepartures finds the departures of a report that come with an
// arrival at the same place in the same second, and when the visits they end
// began. It reads before the report is stored.
func readUnseenDepartures(tx db.Transaction, userId string, reported []ReportedVisit) (*unseenDepartures, error) {
	unseen := &unseenDepartures{startedAt: map[string]*time.Time{}}
	arrivals := map[string]bool{}
	for _, visit := range reported {
		if visit.VisitSource == string(models.VisitSourceRegion) && visit.PlaceID != "" && visit.ArrivedAt != nil && visit.DepartedAt == nil {
			arrivals[unseenDepartureKey(visit.PlaceID, *visit.ArrivedAt)] = true
		}
	}
	if len(arrivals) == 0 {
		return unseen, nil
	}
	for _, visit := range reported {
		if visit.VisitSource != string(models.VisitSourceRegion) || visit.PlaceID == "" || visit.ArrivedAt != nil || visit.DepartedAt == nil {
			continue
		}
		key := unseenDepartureKey(visit.PlaceID, *visit.DepartedAt)
		if !arrivals[key] {
			continue
		}
		known, err := tx.GetPlaceVisitByClientKey(userId, strings.TrimSpace(visit.ClientVisitKey))
		if err != nil {
			return nil, err
		}
		var startedAt *time.Time
		if known != nil {
			startedAt = known.ArrivedAt
		}
		unseen.startedAt[key] = startedAt
	}
	return unseen, nil
}

// filter takes out the notices of the departures nobody saw, and of the
// arrivals that came with them when the person stayed inside the place.
func (self *unseenDepartures) filter(tx db.Transaction, userId string, notices []visitNotice) ([]visitNotice, error) {
	if len(self.startedAt) == 0 {
		return notices, nil
	}
	kept := make([]visitNotice, 0, len(notices))
	for _, notice := range notices {
		if notice.Visit == nil {
			kept = append(kept, notice)
			continue
		}
		startedAt, isUnseen := self.startedAt[unseenDepartureKey(notice.Visit.PlaceID, notice.HappenedAt)]
		if !isUnseen {
			kept = append(kept, notice)
			continue
		}
		if !notice.IsArrival {
			continue
		}
		hasLeft, err := hasLeftPlace(tx, userId, notice.Visit.PlaceID, startedAt, notice.HappenedAt)
		if err != nil {
			return nil, err
		}
		if hasLeft {
			kept = append(kept, notice)
		}
	}
	return kept, nil
}

// hasLeftPlace says whether a position measured from startedAt until the
// moment was outside the place, allowing for the error the phone gave with
// it. With no start known, the person is taken to have left, and the arrival
// is told as any other.
func hasLeftPlace(tx db.Transaction, userId, placeId string, startedAt *time.Time, until time.Time) (bool, error) {
	if startedAt == nil || !startedAt.Before(until) {
		return startedAt == nil, nil
	}
	visited, err := tx.GetPlace(userId, placeId)
	if err != nil || visited == nil {
		return true, err
	}
	points, err := tx.ListLocationPoints(userId, *startedAt, until, unseenDeparturePointCountMost)
	if err != nil {
		return false, err
	}
	for _, point := range points {
		if place.DistanceMeters(point.LatitudeDegrees, point.LongitudeDegrees, visited.LatitudeDegrees, visited.LongitudeDegrees) > visited.RadiusMeters+point.AccuracyMeters {
			return true, nil
		}
	}
	return false, nil
}

// unseenDeparturePointCountMost is how many positions hasLeftPlace reads: a
// day with the app open in the foreground all along.
const unseenDeparturePointCountMost = 20000

// visitNotice is an arrival or a departure the people a location is shared
// with are to be told of once the report is stored.
type visitNotice struct {
	Visit     *models.PlaceVisit
	IsArrival bool

	// HappenedAt is the time that was reported for the arrival or the
	// departure, not the stored visit's: a visit joined into an earlier one
	// keeps the earlier arrival.
	HappenedAt time.Time
}

// afterLocationChange tells what a stored change did, once it is committed and
// not before: the people watching the map are told that this person's location
// changed, the phones of the people it is shared with are told of arrivals and
// departures, and the places the change made are named. None of it can fail the
// change, which is stored.
func (self *graph) afterLocationChange(ctx context.Context, tx db.Transaction, userId string, notices []visitNotice, madePlaces []*models.Place) {
	notifier := self.phoneNotifier()
	detached := context.WithoutCancel(ctx)
	tx.AfterCommit(func() {
		if err := self.database.NotifyLocationEvent(userId); err != nil {
			log.Warningf("cannot tell the listeners that a location changed: %s", err)
		}
		self.namePlacesNow(detached, madePlaces)
		if notifier == nil {
			return
		}
		now := time.Now()
		for _, notice := range notices {
			dueAt := visitNoticeDueAt(notice)
			if !dueAt.After(now) {
				notifier.NotifyPlaceVisitAt(detached, userId, notice.Visit, notice.IsArrival, notice.HappenedAt)
				continue
			}
			time.AfterFunc(dueAt.Sub(now), func() { self.tellVisitNoticeIfKept(detached, notifier, userId, notice) })
		}
	})
}

// visitNoticeDueAt is when a notice may be told, the zero time for at once. A
// visit from a place's circle may still be a pass-by, which a later report
// deletes, until it has lasted db.PassByDurationMost: its arrival is told
// then, and a departure before then a second later, so the two are told in
// order. A notice that waits is lost if the server restarts before it is
// told.
func visitNoticeDueAt(notice visitNotice) time.Time {
	visit := notice.Visit
	if visit == nil || visit.VisitSource != models.VisitSourceRegion {
		return time.Time{}
	}
	if notice.IsArrival {
		return notice.HappenedAt.Add(db.PassByDurationMost)
	}
	if visit.ArrivedAt == nil {
		return time.Time{}
	}
	return visit.ArrivedAt.Add(db.PassByDurationMost + time.Second)
}

// tellVisitNoticeIfKept tells a notice that waited, unless its visit was
// deleted as a pass-by meanwhile.
func (self *graph) tellVisitNoticeIfKept(ctx context.Context, notifier *agent.PhoneNotifier, userId string, notice visitNotice) {
	var visit *models.PlaceVisit
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		visit, err = tx.GetPlaceVisitByClientKey(userId, notice.Visit.ClientVisitKey)
		return err
	}); err != nil {
		log.Warningf("cannot read a visit to tell of it: %s", err)
		return
	}
	if visit == nil {
		return
	}
	notifier.NotifyPlaceVisitAt(ctx, userId, visit, notice.IsArrival, notice.HappenedAt)
}

// storedVisit is what storing one reported visit did.
type storedVisit struct {
	// IsNew is true for a visit the server had not heard of, and IsChanged for
	// a known one that now has an arrival or a departure it did not have.
	IsNew     bool
	IsChanged bool

	// Notice is the arrival or departure to tell the people the location is
	// shared with, nil for none.
	Notice *visitNotice

	// IsRefused is true for a visit that was not stored because it needed a new
	// place and the person has the most places they may have.
	IsRefused bool

	// IsPaused is true for a visit that was not stored because all of it was
	// measured while the person had paused.
	IsPaused bool

	// MadePlace is the place the visit made because the person had none there,
	// nil when it was at a place they had.
	MadePlace *models.Place

	// ArrivedAt is the arrival of the visit as stored, nil when it has none.
	// A departure reported alone completes a visit that began before anything
	// in the report, and visits at other places since that arrival may need
	// it ended sooner.
	ArrivedAt *time.Time
}

// putReportedVisit stores one visit a phone reported, at a place chosen for it
// without asking a provider anything: the place the visit is already at when
// the key is known (a departure is reported at wherever the phone then is, and
// leaves the place the arrival was at), the place a region event watched, the
// person's place that holds the position, or a new one named after its city and
// named from the provider once the report is stored.
func (self *graph) putReportedVisit(tx db.Transaction, userId string, reported ReportedVisit, pauses locationPauseSet) (*storedVisit, error) {
	key := strings.TrimSpace(reported.ClientVisitKey)
	visit := &models.PlaceVisit{
		ArrivedAt: reported.ArrivedAt, DepartedAt: reported.DepartedAt,
		LatitudeDegrees: reported.LatitudeDegrees, LongitudeDegrees: reported.LongitudeDegrees, AccuracyMeters: reported.AccuracyMeters,
		VisitSource: models.VisitSource(reported.VisitSource), ClientVisitKey: key,
	}
	stored := &storedVisit{}
	known, err := tx.GetPlaceVisitByClientKey(userId, key)
	if err != nil {
		return nil, err
	}
	// The departure of a visit iOS reported comes with the position made more
	// exact, so under another key: it is the visit that arrived in that second.
	if known == nil && visit.VisitSource == models.VisitSourceVisit && reported.ArrivedAt != nil {
		if known, err = tx.GetPlaceVisitByArrival(userId, *reported.ArrivedAt); err != nil {
			return nil, err
		}
		if known != nil {
			visit.ClientVisitKey = known.ClientVisitKey
		}
	}
	// What a pause hides is taken from the visit before anything is made for
	// it, so that a visit measured during a pause makes no place either.
	clipped := pauses.clipVisit(reported.ArrivedAt, reported.DepartedAt, known)
	if clipped.IsDropped {
		return &storedVisit{IsPaused: true}, nil
	}
	visit.ArrivedAt, visit.DepartedAt = clipped.ArrivedAt, clipped.DepartedAt
	reported.ArrivedAt, reported.DepartedAt = clipped.ArrivedAt, clipped.DepartedAt
	switch {
	case known != nil:
		visit.PlaceID = known.PlaceID
	default:
		matched, isMade, err := self.placeForReportedVisit(tx, userId, reported)
		if errors.Is(err, db.ErrPlaceCountMost) {
			// The rest of the batch is stored: the person's positions and
			// their visits at places they have are not lost for this one.
			return &storedVisit{IsRefused: true}, nil
		}
		if err != nil {
			return nil, err
		}
		visit.PlaceID = matched.ID
		if isMade {
			stored.MadePlace = matched
		}
	}
	// An arrival at the place of the visit in progress is the phone saying
	// again where the person is, as iOS does when the app watches its places
	// afresh: it is joined into that visit, and nobody is told the person
	// arrived where they already were. Read before the visit is stored, which
	// ends the visit in progress.
	isAlreadyThere := false
	if known == nil && reported.ArrivedAt != nil && reported.DepartedAt == nil {
		open, err := tx.GetOpenPlaceVisit(userId)
		if err != nil {
			return nil, err
		}
		isAlreadyThere = open != nil && open.PlaceID == visit.PlaceID && open.ArrivedAt != nil && !open.ArrivedAt.After(*reported.ArrivedAt)
	}
	isNew, err := tx.PutPlaceVisit(userId, visit)
	if err != nil {
		return nil, locationFailure(err)
	}
	stored.IsNew = isNew
	stored.ArrivedAt = visit.ArrivedAt
	// known is nil when the key was not known at the lookup but is now: the
	// visit was stored between the two. That is an update with nothing to say
	// about a departure.
	stored.IsChanged = !isNew && (known == nil || addsATime(known.ArrivedAt, visit.ArrivedAt) || addsATime(known.DepartedAt, visit.DepartedAt))
	// An arrival is told when it is first heard of, and a departure when it
	// is added to a visit that had none.
	// A new visit that comes with its departure (the arrival report was lost)
	// is told by its latest event: the person has left.
	// A departure moved to the start of a pause is not told: the person did not
	// leave then as far as anybody knows. Nor is an arrival where the person
	// already was.
	switch {
	case clipped.IsDepartureCut, isAlreadyThere:
	case isNew && reported.DepartedAt != nil && reported.ArrivedAt != nil:
		stored.Notice = &visitNotice{Visit: visit, IsArrival: false, HappenedAt: *reported.DepartedAt}
	case isNew && reported.ArrivedAt != nil:
		stored.Notice = &visitNotice{Visit: visit, IsArrival: true, HappenedAt: *reported.ArrivedAt}
	case !isNew && known != nil && known.DepartedAt == nil && reported.DepartedAt != nil && visit.DepartedAt != nil:
		stored.Notice = &visitNotice{Visit: visit, IsArrival: false, HappenedAt: *reported.DepartedAt}
	}
	return stored, nil
}

// addsATime says whether a report gives a time that differs from the one
// stored: none reported adds nothing, since a departure is reported without
// the arrival it belongs to.
func addsATime(storedAt, reportedAt *time.Time) bool {
	return reportedAt != nil && (storedAt == nil || !storedAt.Equal(*reportedAt))
}

// placeForReportedVisit is the place a visit not seen before is at, and
// whether it was made for the visit.
func (self *graph) placeForReportedVisit(tx db.Transaction, userId string, reported ReportedVisit) (*models.Place, bool, error) {
	// A region event names the place it watched. One the caller no longer
	// has, deleted since the phone last refreshed the places it watches, is
	// matched by position like any other visit: refusing it would leave the
	// phone sending the same batch for ever.
	if placeId := strings.TrimSpace(reported.PlaceID); placeId != "" && models.VisitSource(reported.VisitSource) == models.VisitSourceRegion {
		found, err := tx.GetPlace(userId, placeId)
		if err == nil {
			return found, false, nil
		}
		if !errors.Is(err, db.ErrNotFound) {
			return nil, false, err
		}
	}
	found, err := tx.FindPlaceAt(userId, reported.LatitudeDegrees, reported.LongitudeDegrees)
	if err != nil || found != nil {
		return found, false, err
	}
	if err := self.requireNewPlaceAllowance(userId); err != nil {
		return nil, false, err
	}
	made, err := tx.CreatePlace(userId, &models.Place{
		PlaceName: placeNameFor(reported.LatitudeDegrees, reported.LongitudeDegrees), LatitudeDegrees: reported.LatitudeDegrees,
		LongitudeDegrees: reported.LongitudeDegrees,
	})
	if errors.Is(err, db.ErrPlaceCountMost) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, locationFailure(err)
	}
	return made, true, nil
}

func (self *graph) SavePlace(ctx context.Context, arguments SavePlaceArguments) (*PlaceView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	var placeLabel *models.PlaceLabel
	if arguments.PlaceLabel != nil {
		label, err := placeLabelOf(*arguments.PlaceLabel)
		if err != nil {
			return nil, err
		}
		placeLabel = &label
	}
	if (arguments.LatitudeDegrees == nil) != (arguments.LongitudeDegrees == nil) {
		return nil, fmt.Errorf("%w: give both latitudeDegrees and longitudeDegrees", api.ErrInvalidArguments)
	}
	if arguments.LatitudeDegrees != nil {
		if err := requireOnEarth(*arguments.LatitudeDegrees, *arguments.LongitudeDegrees); err != nil {
			return nil, err
		}
	}
	// An empty address takes a typed address away, so it is not an address
	// to keep: only a place that exists has one to take away.
	addressText := ""
	if arguments.AddressText != nil {
		addressText = strings.TrimSpace(*arguments.AddressText)
		if addressText != "" && arguments.LatitudeDegrees == nil && strings.TrimSpace(arguments.PlaceID) == "" {
			return nil, fmt.Errorf("%w: give the position of the address too: latitudeDegrees and longitudeDegrees", api.ErrInvalidArguments)
		}
	}
	tx := self.transaction(ctx)
	userId := principal.User.ID

	placeId := strings.TrimSpace(arguments.PlaceID)
	if placeId == "" && addressText != "" {
		// Typing an address for home makes the place, or labels the one the
		// caller already has there, as a check-in does; two places at one
		// address would split the visits. One change of a person's location
		// at a time, as in CheckIn.
		if err := tx.LockLocation(userId); err != nil {
			return nil, err
		}
		holding, err := tx.FindPlaceAt(userId, *arguments.LatitudeDegrees, *arguments.LongitudeDegrees)
		if err != nil {
			return nil, locationFailure(err)
		}
		if holding != nil {
			placeId = holding.ID
		}
	}
	var saved *models.Place
	if placeId == "" {
		if arguments.LatitudeDegrees == nil {
			return nil, fmt.Errorf("%w: give the place to change as placeId, or latitudeDegrees and longitudeDegrees to make a new one", api.ErrInvalidArguments)
		}
		made := &models.Place{LatitudeDegrees: *arguments.LatitudeDegrees, LongitudeDegrees: *arguments.LongitudeDegrees}
		if arguments.RadiusMeters != nil {
			made.RadiusMeters = *arguments.RadiusMeters
		}
		if placeLabel != nil {
			made.PlaceLabel = *placeLabel
		}
		// A name the person gave is theirs; without one it is named for now
		// after its city and OpenStreetMap is asked afterwards.
		if arguments.PlaceName != nil && strings.TrimSpace(*arguments.PlaceName) != "" {
			made.PlaceName, made.IsNamedByPerson = strings.TrimSpace(*arguments.PlaceName), true
		} else {
			made.PlaceName = placeNameFor(made.LatitudeDegrees, made.LongitudeDegrees)
		}
		// A typed address is named already: the provider is not asked what is
		// there, since that would replace the address the person chose.
		if addressText != "" {
			now := time.Now()
			made.AddressText, made.IsAddressTyped, made.NamedAt = addressText, true, &now
			if !made.IsNamedByPerson {
				made.PlaceName = strings.TrimSpace(strings.SplitN(addressText, ",", 2)[0])
			}
		}
		if err := self.requireNewPlaceAllowance(userId); err != nil {
			return nil, err
		}
		if saved, err = tx.CreatePlace(userId, made); err != nil {
			return nil, locationFailure(err)
		}
		self.takeNewPlacesAfterCommit(tx, userId, 1)
		if !saved.IsNamedByPerson && saved.NamedAt == nil {
			detached := context.WithoutCancel(ctx)
			tx.AfterCommit(func() { self.namePlacesNow(detached, []*models.Place{saved}) })
		}
	} else {
		changes := db.PlaceChanges{
			PlaceName: arguments.PlaceName, PlaceLabel: placeLabel, LatitudeDegrees: arguments.LatitudeDegrees,
			LongitudeDegrees: arguments.LongitudeDegrees, RadiusMeters: arguments.RadiusMeters,
		}
		if arguments.AddressText != nil {
			// Empty takes the typed address away.
			changes.AddressText = &addressText
		}
		// A place moved farther than its radius forgets what was learned about
		// the old position and is called after its new city until it is named
		// again, which is asked for once this is committed.
		if arguments.LatitudeDegrees != nil && arguments.LongitudeDegrees != nil {
			changes.MovedPlaceName = placeNameFor(*arguments.LatitudeDegrees, *arguments.LongitudeDegrees)
		}
		if saved, err = tx.SavePlace(userId, placeId, changes); err != nil {
			return nil, locationFailure(err)
		}
		if !saved.IsNamedByPerson && saved.NamedAt == nil {
			detached := context.WithoutCancel(ctx)
			tx.AfterCommit(func() { self.namePlacesNow(detached, []*models.Place{saved}) })
		}
	}
	if err := attachVisitSummaries(tx, userId, []*models.Place{saved}); err != nil {
		return nil, err
	}
	return describePlace(saved), nil
}

// rebuildTripsMissingVisit builds again around the trips that lost a visit at
// either end (a place was forgotten, or visits were erased): the visits on
// both sides of what was lost get the trip between them, if there was one.
func rebuildTripsMissingVisit(tx db.Transaction, userId string) error {
	trips, err := tx.ListTripsMissingVisit(userId, db.TripsMissingVisitMost)
	if err != nil {
		return err
	}
	times := make([]time.Time, 0, 2*len(trips))
	for _, trip := range trips {
		times = append(times, trip.StartedAt, trip.EndedAt)
	}
	return rebuild.AroundTimes(tx, userId, times)
}

func (self *graph) ForgetPlace(ctx context.Context, arguments ForgetPlaceArguments) (bool, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	userId := principal.User.ID
	// One change of a person's location at a time, as in ReportLocation.
	if err := tx.LockLocation(userId); err != nil {
		return false, err
	}
	if err := tx.ForgetPlace(userId, strings.TrimSpace(arguments.PlaceID)); err != nil {
		return false, locationFailure(err)
	}
	// The trips into and out of the visits that went are left without a
	// visit; the visits beside them are joined by a trip of their own.
	if err := rebuildTripsMissingVisit(tx, userId); err != nil {
		return false, locationFailure(err)
	}
	return true, nil
}

func (self *graph) CheckIn(ctx context.Context, arguments CheckInArguments) (*PlaceVisitView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	accuracyMeters := float64(0)
	if arguments.AccuracyMeters != nil {
		accuracyMeters = *arguments.AccuracyMeters
		if !(accuracyMeters >= 0) || math.IsInf(accuracyMeters, 0) {
			return nil, fmt.Errorf("%w: an accuracy is zero or more meters", api.ErrInvalidArguments)
		}
	}
	tx := self.transaction(ctx)
	userId := principal.User.ID

	// One change of a person's location at a time, as in ReportLocation.
	if err := tx.LockLocation(userId); err != nil {
		return nil, err
	}
	// Taken after the lock, as in ReportLocation.
	now := time.Now()
	// A check-in is a visit measured now, which a pause does not store.
	if err := requireNotPaused(tx, principal.User, now); err != nil {
		return nil, err
	}

	var checkedIn *models.Place
	// A place made from coordinates alone is named from OpenStreetMap once the
	// check-in is committed, like one a report makes.
	var madePlaces []*models.Place
	if placeId := strings.TrimSpace(arguments.PlaceID); placeId != "" {
		if checkedIn, err = tx.GetPlace(userId, placeId); err != nil {
			return nil, locationFailure(err)
		}
	} else {
		var isUnnamed bool
		if checkedIn, isUnnamed, err = self.placeFromFound(tx, userId, arguments, now); err != nil {
			return nil, err
		}
		if isUnnamed {
			madePlaces = append(madePlaces, checkedIn)
		}
	}

	// The visit in progress ends, at the moment the person left it if the
	// positions since say so, otherwise now: a visit a phone never ended
	// would otherwise be made days long by checking in somewhere else.
	latest, err := tx.GetLatestLocationPoint(userId)
	if err != nil {
		return nil, err
	}
	endedAt := now
	open, err := tx.GetOpenPlaceVisit(userId)
	if err != nil {
		return nil, err
	}
	if open != nil && latest != nil && !place.IsStillThere(open, latest) {
		endedAt = latest.MeasuredAt
	} else if open != nil && open.PlaceID == checkedIn.ID {
		// Already there: the visit in progress is the check-in, and ending it
		// to start another would tell everyone it is shared with that the
		// person left and then arrived.
		open.Place = checkedIn
		if err := attachVisitSummaries(tx, userId, []*models.Place{checkedIn}); err != nil {
			return nil, err
		}
		return describePlaceVisit(open, true), nil
	}
	var notices []visitNotice
	ended, err := tx.EndOpenPlaceVisit(userId, endedAt)
	if err != nil {
		return nil, err
	}
	if ended != nil {
		notices = append(notices, visitNotice{Visit: ended, IsArrival: false, HappenedAt: endedAt})
	}

	visit := &models.PlaceVisit{
		PlaceID: checkedIn.ID, ArrivedAt: &now, LatitudeDegrees: checkedIn.LatitudeDegrees, LongitudeDegrees: checkedIn.LongitudeDegrees,
		AccuracyMeters: accuracyMeters, VisitSource: models.VisitSourceCheckIn, ClientVisitKey: "check_in:" + checkedIn.ID + ":" + now.UTC().Format(time.RFC3339Nano),
	}
	if _, err := tx.PutPlaceVisit(userId, visit); err != nil {
		return nil, locationFailure(err)
	}
	visit.Place = checkedIn
	notices = append(notices, visitNotice{Visit: visit, IsArrival: true, HappenedAt: now})
	// The check-in ends the trip that led here.
	if err := rebuild.Around(tx, userId, now, now); err != nil {
		return nil, locationFailure(err)
	}
	// A place found nearby arrives named, so only one made from coordinates
	// alone is left to name.
	self.afterLocationChange(ctx, tx, userId, notices, madePlaces)
	if err := attachVisitSummaries(tx, userId, []*models.Place{checkedIn}); err != nil {
		return nil, err
	}
	return describePlaceVisit(visit, true), nil
}

// placeFromFound is the caller's place for a place found nearby: the one they
// already have for the same OpenStreetMap object, or a new one named as
// OpenStreetMap named it, so that the provider is not asked about it again.
// With coordinates alone (no placeId, no OpenStreetMap object, no name or
// address) it is the caller's own place whose radius holds the point, or else a
// new place called after its city for now; isUnnamed says it is the latter, to
// be named from OpenStreetMap once the check-in is committed.
func (self *graph) placeFromFound(tx db.Transaction, userId string, arguments CheckInArguments, now time.Time) (made *models.Place, isUnnamed bool, err error) {
	if arguments.LatitudeDegrees == nil || arguments.LongitudeDegrees == nil {
		return nil, false, fmt.Errorf("%w: give the place as placeId, or as the latitudeDegrees and longitudeDegrees of a place that was found", api.ErrInvalidArguments)
	}
	if err := requireOnEarth(*arguments.LatitudeDegrees, *arguments.LongitudeDegrees); err != nil {
		return nil, false, err
	}
	osmType, osmId := strings.TrimSpace(arguments.OSMType), int64(0)
	if arguments.OSMID != nil {
		osmId = *arguments.OSMID
	}
	if (osmType == "") != (osmId == 0) {
		return nil, false, fmt.Errorf("%w: give both osmType and osmId, or neither", api.ErrInvalidArguments)
	}
	if osmType != "" {
		own, err := tx.FindPlaceIDsByOSM(userId, []db.OSMReference{{OSMType: osmType, OSMID: osmId}})
		if err != nil {
			return nil, false, err
		}
		if placeId := own[db.OSMReference{OSMType: osmType, OSMID: osmId}]; placeId != "" {
			found, err := tx.GetPlace(userId, placeId)
			return found, false, err
		}
	}
	placeName := strings.TrimSpace(arguments.PlaceName)
	addressText := strings.TrimSpace(arguments.AddressText)
	isCoordinatesOnly := osmType == "" && placeName == "" && addressText == ""
	if isCoordinatesOnly {
		holding, err := tx.FindPlaceAt(userId, *arguments.LatitudeDegrees, *arguments.LongitudeDegrees)
		if err != nil || holding != nil {
			return holding, false, err
		}
		placeName = placeNameFor(*arguments.LatitudeDegrees, *arguments.LongitudeDegrees)
	} else if placeName == "" {
		placeName = strings.TrimSpace(strings.SplitN(addressText, ",", 2)[0])
	}
	if placeName == "" {
		placeName = placeNameFor(*arguments.LatitudeDegrees, *arguments.LongitudeDegrees)
	}
	if err := self.requireNewPlaceAllowance(userId); err != nil {
		return nil, false, err
	}
	toMake := &models.Place{
		PlaceName: placeName, LatitudeDegrees: *arguments.LatitudeDegrees, LongitudeDegrees: *arguments.LongitudeDegrees,
		OSMType: osmType, OSMID: osmId, OSMName: strings.TrimSpace(arguments.PlaceName),
		OSMCategory: strings.TrimSpace(arguments.OSMCategory), AddressText: addressText,
	}
	if !isCoordinatesOnly {
		toMake.NamedAt = &now
	}
	made, err = tx.CreatePlace(userId, toMake)
	if err != nil {
		return nil, false, locationFailure(err)
	}
	self.takeNewPlacesAfterCommit(tx, userId, 1)
	return made, isCoordinatesOnly, nil
}

func (self *graph) EraseLocationHistory(ctx context.Context, arguments EraseLocationHistoryArguments) (*EraseLocationHistoryView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	if arguments.From != nil && arguments.Until != nil && !arguments.From.Before(*arguments.Until) {
		return nil, fmt.Errorf("%w: the range must start before it ends", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	// One change of a person's location at a time, as in ReportLocation.
	if err := tx.LockLocation(principal.User.ID); err != nil {
		return nil, err
	}
	pointCount, visitCount, placeCount, err := tx.EraseLocationHistory(principal.User.ID, arguments.From, arguments.Until)
	if err != nil {
		return nil, locationFailure(err)
	}
	// The trips that ran into or out of what was erased are gone; the visits
	// beside the range may now be joined by a trip across it, and the trips
	// left without a visit are built again.
	var edges []time.Time
	if arguments.From != nil {
		edges = append(edges, *arguments.From)
	}
	if arguments.Until != nil {
		edges = append(edges, *arguments.Until)
	}
	if err := rebuild.AroundTimes(tx, principal.User.ID, edges); err != nil {
		return nil, locationFailure(err)
	}
	if err := rebuildTripsMissingVisit(tx, principal.User.ID); err != nil {
		return nil, locationFailure(err)
	}
	// The people following this person on a map are told, so that a position
	// that was erased is not left on their screen.
	self.afterLocationChange(ctx, tx, principal.User.ID, nil, nil)
	return &EraseLocationHistoryView{PointCount: pointCount, VisitCount: visitCount, PlaceCount: placeCount}, nil
}
