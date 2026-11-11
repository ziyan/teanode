package models

import "time"

// LocationSource is what reported a position: the iPhone app, or a browser
// answering the agent's location tool.
type LocationSource string

// The sources of a position.
const (
	LocationSourcePhone   LocationSource = "phone"
	LocationSourceBrowser LocationSource = "browser"
)

// IsValid reports whether this is one of the sources.
func (self LocationSource) IsValid() bool {
	switch self {
	case LocationSourcePhone, LocationSourceBrowser:
		return true
	}
	return false
}

// VisitSource is how a visit came to be recorded.
type VisitSource string

// The sources of a visit.
const (
	// VisitSourceVisit is iOS visit monitoring, which reports arrivals and
	// departures on its own schedule.
	VisitSourceVisit VisitSource = "visit"
	// VisitSourceRegion is the phone entering or leaving a place the person
	// named.
	VisitSourceRegion VisitSource = "region"
	// VisitSourceCheckIn is a visit the person recorded by hand.
	VisitSourceCheckIn VisitSource = "check_in"
	// VisitSourceBrowser is a visit recorded from the dashboard.
	VisitSourceBrowser VisitSource = "browser"
)

// IsValid reports whether this is one of the sources.
func (self VisitSource) IsValid() bool {
	switch self {
	case VisitSourceVisit, VisitSourceRegion, VisitSourceCheckIn, VisitSourceBrowser:
		return true
	}
	return false
}

// PlaceLabel is what a place means to its person, from a short fixed list. A
// name cannot be acted on and a label can: it puts an icon on the map, and it
// is what "at home" and "at work" mean to the agent wherever those are. The
// empty label is a place that means nothing in particular.
type PlaceLabel string

// The labels.
const (
	PlaceLabelHome   PlaceLabel = "home"
	PlaceLabelWork   PlaceLabel = "work"
	PlaceLabelSchool PlaceLabel = "school"
	// PlaceLabelFamily is somebody else's home that the person visits.
	PlaceLabelFamily PlaceLabel = "family"
	PlaceLabelGym    PlaceLabel = "gym"
)

// PlaceLabels lists the labels in the order a picker offers them.
var PlaceLabels = []PlaceLabel{
	PlaceLabelHome, PlaceLabelWork, PlaceLabelSchool, PlaceLabelFamily, PlaceLabelGym,
}

// IsValid reports whether this is one of the labels. The empty label is not:
// it is the absence of one, and callers that accept it say so.
func (self PlaceLabel) IsValid() bool {
	for _, label := range PlaceLabels {
		if self == label {
			return true
		}
	}
	return false
}

// The bounds of a place's radius, in meters. Below the lower bound the phone's
// own error puts a person outside the place they are standing in, and above
// the upper one a place swallows its neighbors.
const (
	PlaceRadiusMinimumMeters = 25
	PlaceRadiusMaximumMeters = 2000
	// PlaceRadiusDefaultMeters is the radius of a place made from a report.
	PlaceRadiusDefaultMeters = 75
)

// OSM types of a place, as OpenStreetMap names its kinds of object.
const (
	OSMTypeNode     = "node"
	OSMTypeWay      = "way"
	OSMTypeRelation = "relation"
)

// LocationShareLevel is how much of a person's location somebody may see. A
// person shares their location with a circle or with one person in a circle
// they share, at one of these two.
type LocationShareLevel string

// The levels, from least to most.
const (
	// LocationShareLevelCurrent is where the person is now, and the place.
	LocationShareLevelCurrent LocationShareLevel = "current"
	// LocationShareLevelHistory adds the person's visits.
	LocationShareLevelHistory LocationShareLevel = "history"
	// LocationShareLevelOwner is a person looking at their own location. It
	// is not a level anybody can be given: it lets "how much of this
	// person's location may this person see" have one answer for the owner
	// too.
	LocationShareLevelOwner LocationShareLevel = "owner"
)

// IsGrantable reports whether a share may be made at this level.
func (self LocationShareLevel) IsGrantable() bool {
	switch self {
	case LocationShareLevelCurrent, LocationShareLevelHistory:
		return true
	}
	return false
}

// Rank orders the levels; zero is no access at all.
func (self LocationShareLevel) Rank() int {
	switch self {
	case LocationShareLevelCurrent:
		return 1
	case LocationShareLevelHistory:
		return 2
	case LocationShareLevelOwner:
		return 3
	}
	return 0
}

// AtLeast reports whether this level reaches the other.
func (self LocationShareLevel) AtLeast(other LocationShareLevel) bool {
	return self.Rank() >= other.Rank() && self.Rank() > 0
}

// LessLocationShareLevel is the lower of two levels, the empty level when
// either is empty.
func LessLocationShareLevel(first, second LocationShareLevel) LocationShareLevel {
	if first.Rank() <= second.Rank() {
		return first
	}
	return second
}

// LocationPhonePermission is what a person's phone lets TeaNode record of
// where they are.
type LocationPhonePermission string

const (
	// LocationPhonePermissionAlways records visits in the background.
	LocationPhonePermissionAlways LocationPhonePermission = "always"
	// LocationPhonePermissionWhileUsing records only where they are while
	// the app is open, so no visits.
	LocationPhonePermissionWhileUsing LocationPhonePermission = "while_using"
	// LocationPhonePermissionOff records nothing: Location is off for the
	// app, or the person has not answered the phone's question yet.
	LocationPhonePermissionOff LocationPhonePermission = "off"
)

// IsKnown reports whether the permission is one of the three.
func (self LocationPhonePermission) IsKnown() bool {
	switch self {
	case LocationPhonePermissionAlways, LocationPhonePermissionWhileUsing, LocationPhonePermissionOff:
		return true
	}
	return false
}

// LocationPermission is what a person's app last said about their phone: its
// permission, and whether the app's own switch for recording is on.
type LocationPermission struct {
	UserID          string                  `json:"userId"`
	PhonePermission LocationPhonePermission `json:"phonePermission"`
	IsRecordingOn   bool                    `json:"isRecordingOn"`
	ReportedAt      time.Time               `json:"reportedAt"`
}

// LocationSharingLimitReason is why a person's own location shares less than
// their shares say, and so why they see less of other people's.
type LocationSharingLimitReason string

const (
	// LocationSharingLimitReasonNone is no limit: the shares say it all.
	LocationSharingLimitReasonNone LocationSharingLimitReason = ""
	// LocationSharingLimitReasonPaused is a pause in effect.
	LocationSharingLimitReasonPaused LocationSharingLimitReason = "paused"
	// LocationSharingLimitReasonRecordingOff is the app's switch for
	// recording turned off.
	LocationSharingLimitReasonRecordingOff LocationSharingLimitReason = "recording_off"
	// LocationSharingLimitReasonPhoneLocationOff is Location off for the app
	// on the phone.
	LocationSharingLimitReasonPhoneLocationOff LocationSharingLimitReason = "phone_location_off"
	// LocationSharingLimitReasonPhoneWhileUsing is Location set to While
	// Using on the phone, which records no visits.
	LocationSharingLimitReasonPhoneWhileUsing LocationSharingLimitReason = "phone_while_using"
)

// LocationSharingLimit is the most of a person's location their phone and
// their pause let anybody see, whatever their shares say, and why. Nobody
// sees more of another person's location than they share with them, so it is
// also the most they see of anybody else's.
type LocationSharingLimit struct {
	ShareLevelLimit    LocationShareLevel
	SharingLimitReason LocationSharingLimitReason
}

// LocationSharingLimitOf is the limit for a person with this permission (nil
// for an app that never reported one, which is judged by its shares alone)
// and this pause in effect (nil for none).
func LocationSharingLimitOf(permission *LocationPermission, pause *LocationPause) LocationSharingLimit {
	switch {
	case pause != nil:
		return LocationSharingLimit{SharingLimitReason: LocationSharingLimitReasonPaused}
	case permission == nil:
		return LocationSharingLimit{ShareLevelLimit: LocationShareLevelHistory}
	case !permission.IsRecordingOn:
		return LocationSharingLimit{SharingLimitReason: LocationSharingLimitReasonRecordingOff}
	case permission.PhonePermission == LocationPhonePermissionAlways:
		return LocationSharingLimit{ShareLevelLimit: LocationShareLevelHistory}
	case permission.PhonePermission == LocationPhonePermissionWhileUsing:
		return LocationSharingLimit{ShareLevelLimit: LocationShareLevelCurrent, SharingLimitReason: LocationSharingLimitReasonPhoneWhileUsing}
	}
	return LocationSharingLimit{SharingLimitReason: LocationSharingLimitReasonPhoneLocationOff}
}

// Words returns what the limit means for what the person sees, and what to
// change, in a sentence; empty for no limit. The command line and the agent
// say it as the app's banner does.
func (self LocationSharingLimitReason) Words() string {
	switch self {
	case LocationSharingLimitReasonPaused:
		return "Your location is paused, so you see nobody's location until you resume."
	case LocationSharingLimitReasonRecordingOff:
		return "Recording is off in the TeaNode app, so you see nobody's location. Turn recording on to see people again."
	case LocationSharingLimitReasonPhoneLocationOff:
		return "Location is off for TeaNode on your phone, so you see nobody's location. Set it to Always in the phone's Settings to see where people are and their visits."
	case LocationSharingLimitReasonPhoneWhileUsing:
		return "Location is set to While Using on your phone, so TeaNode cannot record your visits, and you see where people are but not their visits. Set it to Always in the phone's Settings to see visits again."
	}
	return ""
}

// LocationShareBackWords says why a person sees less of someone's location
// than that person shares, when what they share back is why: seen is what
// they see, granted what is shared with them, and limit their own limit.
// Empty when what they share back is not why.
func LocationShareBackWords(name string, seen, granted, limit LocationShareLevel) string {
	if seen.Rank() >= LessLocationShareLevel(granted, limit).Rank() {
		return ""
	}
	if seen.Rank() == 0 {
		return "You do not share your location with " + name + ", so you see nothing of theirs. Share yours to see where they are."
	}
	return "You share only where you are with " + name + ", so you see where they are but not their visits. Share your visits to see theirs."
}

// LocationPoint is one position a phone or browser reported. The optional
// measurements are nil when the device did not give them.
type LocationPoint struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`

	LatitudeDegrees  float64 `json:"latitudeDegrees"`
	LongitudeDegrees float64 `json:"longitudeDegrees"`
	AccuracyMeters   float64 `json:"accuracyMeters"`

	AltitudeMeters       *float64 `json:"altitudeMeters,omitempty"`
	SpeedMetersPerSecond *float64 `json:"speedMetersPerSecond,omitempty"`
	CourseDegrees        *float64 `json:"courseDegrees,omitempty"`

	// MeasuredAt is when the device took the position and ReceivedAt when the
	// server got it; a phone that was offline reports late.
	MeasuredAt time.Time `json:"measuredAt"`
	ReceivedAt time.Time `json:"receivedAt"`

	LocationSource LocationSource `json:"locationSource"`

	// AgentPhoneID is the install that reported it, empty for a browser or
	// once that phone was removed.
	AgentPhoneID string `json:"agentPhoneId,omitempty"`
}

// Place is somewhere with a name and a radius, belonging to one person.
type Place struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`

	// PlaceName is what the person sees: the name they gave it, otherwise
	// the OpenStreetMap name, otherwise the street address, otherwise the
	// city.
	PlaceName string `json:"placeName"`

	// IsNamedByPerson is whether PlaceName is the person's own, which no
	// later answer from OpenStreetMap replaces.
	IsNamedByPerson bool `json:"isNamedByPerson"`

	// PlaceLabel is empty for a place with no label.
	PlaceLabel PlaceLabel `json:"placeLabel"`

	LatitudeDegrees  float64 `json:"latitudeDegrees"`
	LongitudeDegrees float64 `json:"longitudeDegrees"`
	RadiusMeters     float64 `json:"radiusMeters"`

	// OSMType is node, way or relation, or empty when no OpenStreetMap
	// object was found. OSMID is zero then.
	OSMType     string `json:"osmType,omitempty"`
	OSMID       int64  `json:"osmId,omitempty"`
	OSMName     string `json:"osmName,omitempty"`
	OSMCategory string `json:"osmCategory,omitempty"`
	AddressText string `json:"addressText,omitempty"`

	// IsAddressTyped is whether AddressText is one the person typed, which
	// moving the place keeps and no answer from OpenStreetMap replaces.
	IsAddressTyped bool `json:"isAddressTyped"`

	// NamedAt is when the provider answered, whatever it answered, and nil
	// while the place waits for a name. NamingClaimedAt is when an instance
	// took the place to name. NamingErrorMessage says why the last try failed.
	NamedAt            *time.Time `json:"namedAt,omitempty"`
	NamingClaimedAt    *time.Time `json:"namingClaimedAt,omitempty"`
	NamingErrorMessage string     `json:"namingErrorMessage,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// VisitCount and LastVisitAt are filled in by the listing of places.
	VisitCount  int        `json:"visitCount"`
	LastVisitAt *time.Time `json:"lastVisitAt,omitempty"`
}

// PlaceVisit is one visit to a place.
type PlaceVisit struct {
	ID      string `json:"id"`
	UserID  string `json:"userId"`
	PlaceID string `json:"placeId"`

	// ArrivedAt is nil when iOS reported a departure whose arrival it did not
	// see. DepartedAt is nil while the person is still there.
	ArrivedAt  *time.Time `json:"arrivedAt,omitempty"`
	DepartedAt *time.Time `json:"departedAt,omitempty"`

	LatitudeDegrees  float64 `json:"latitudeDegrees"`
	LongitudeDegrees float64 `json:"longitudeDegrees"`
	AccuracyMeters   float64 `json:"accuracyMeters"`

	VisitSource VisitSource `json:"visitSource"`

	// ClientVisitKey is what the phone calls the visit, so that a report sent
	// twice is one visit. A visit recorded by hand is given one.
	ClientVisitKey string `json:"clientVisitKey"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Place is filled in by the listing of visits.
	Place *Place `json:"place,omitempty"`
}

// LocationShare gives one person, or every member of one circle, a level of
// access to the sharer's location. Exactly one of GranteeUserID and
// GranteeCircleID is set.
type LocationShare struct {
	ID string `json:"id"`

	// UserID is the person whose location it is.
	UserID string `json:"userId"`

	GranteeUserID   string `json:"granteeUserId,omitempty"`
	GranteeCircleID string `json:"granteeCircleId,omitempty"`

	ShareLevel LocationShareLevel `json:"shareLevel"`

	CreatedAt time.Time `json:"createdAt"`

	// CreatedByUserID is empty once that account is gone.
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	// GranteeUsername, GranteeName and GranteeCircleName are filled in by the
	// listing, for a screen that says who a location is shared with.
	GranteeUsername   string `json:"granteeUsername,omitempty"`
	GranteeName       string `json:"granteeName,omitempty"`
	GranteeCircleName string `json:"granteeCircleName,omitempty"`
}

// LocationPause is a time during which a person's location is not recorded.
// It is in effect at an instant when StartedAt is at or before it and EndedAt
// is nil or after it; EndedAt is nil for a pause that lasts until the person
// resumes. Resuming sets it to that moment.
type LocationPause struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`

	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsInEffectAt reports whether the pause covers the instant.
func (self *LocationPause) IsInEffectAt(instant time.Time) bool {
	return !self.StartedAt.After(instant) && (self.EndedAt == nil || self.EndedAt.After(instant))
}

// LocationNotificationMute is a person's mute of the notices that somebody
// sharing their location with them arrived at or left a place. UserID is the
// person who muted, MutedUserID the person they no longer hear about. It is
// in effect at an instant when MutedUntil is nil, a mute that lasts until the
// person unmutes, or after the instant.
type LocationNotificationMute struct {
	UserID      string     `json:"userId"`
	MutedUserID string     `json:"mutedUserId"`
	MutedUntil  *time.Time `json:"mutedUntil,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsInEffectAt reports whether the mute covers the instant.
func (self *LocationNotificationMute) IsInEffectAt(instant time.Time) bool {
	return self.MutedUntil == nil || self.MutedUntil.After(instant)
}

// MotionActivity is what the phone's motion sensor said the person was doing
// from StartedAt on, until the next activity starts. ActivityKind is one of
// stationary, walking, running, cycling, automotive and unknown;
// ActivityConfidence is low, medium or high.
type MotionActivity struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`

	StartedAt          time.Time `json:"startedAt"`
	ActivityKind       string    `json:"activityKind"`
	ActivityConfidence string    `json:"activityConfidence"`

	CreatedAt time.Time `json:"createdAt"`
}

// Trip is the travel between the departure of one visit and the arrival of the
// next. It is derived data, built again whenever the visits, positions or
// motion activity around it change. FromVisitID and ToVisitID are empty when
// the visit is gone.
type Trip struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`

	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`

	FromVisitID string `json:"fromVisitId,omitempty"`
	ToVisitID   string `json:"toVisitId,omitempty"`

	DistanceMeters float64 `json:"distanceMeters"`

	CreatedAt time.Time `json:"createdAt"`

	// Legs are oldest first, filled in by the listing of trips.
	Legs []*TripLeg `json:"legs,omitempty"`
}

// TripLeg is a part of a trip made one way. DetectedTravelMode is what was
// worked out from motion activity or speed; TravelMode is the way after the
// person's corrections, which is the one to show and to count. They differ
// exactly when IsTravelModeCorrected. DetectionSource is motion or speed.
// TravelMode and IsTravelModeCorrected are filled in by the reads, not stored.
type TripLeg struct {
	ID     string `json:"id"`
	TripID string `json:"tripId"`
	UserID string `json:"userId"`

	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`

	TravelMode            string `json:"travelMode"`
	DetectedTravelMode    string `json:"detectedTravelMode"`
	IsTravelModeCorrected bool   `json:"isTravelModeCorrected"`
	DetectionSource       string `json:"detectionSource"`

	DistanceMeters float64 `json:"distanceMeters"`
}

// TravelModeCorrection is the person saying that the span of time from
// StartedAt to EndedAt was traveled the way TravelMode says.
type TravelModeCorrection struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`

	StartedAt  time.Time `json:"startedAt"`
	EndedAt    time.Time `json:"endedAt"`
	TravelMode string    `json:"travelMode"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TravelTotal is the distance and time traveled one way in one period.
// PeriodStartDay is the first day of the period, as YYYY-MM-DD in the
// traveler's time zone.
type TravelTotal struct {
	PeriodStartDay  string  `json:"periodStartDay"`
	TravelMode      string  `json:"travelMode"`
	DistanceMeters  float64 `json:"distanceMeters"`
	DurationSeconds int64   `json:"durationSeconds"`
	LegCount        int     `json:"legCount"`
}
