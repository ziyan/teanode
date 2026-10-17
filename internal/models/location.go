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

// VisitSource is how a stay came to be recorded.
type VisitSource string

// The sources of a visit.
const (
	// VisitSourceVisit is iOS visit monitoring, which reports arrivals and
	// departures on its own schedule.
	VisitSourceVisit VisitSource = "visit"
	// VisitSourceRegion is the phone entering or leaving a place the person
	// named.
	VisitSourceRegion VisitSource = "region"
	// VisitSourceCheckIn is a stay the person recorded by hand.
	VisitSourceCheckIn VisitSource = "check_in"
	// VisitSourceBrowser is a stay recorded from the dashboard.
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
	}
	return 0
}

// AtLeast reports whether this level reaches the other.
func (self LocationShareLevel) AtLeast(other LocationShareLevel) bool {
	return self.Rank() >= other.Rank() && self.Rank() > 0
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

// Place is a spot with a name and a radius, belonging to one person.
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

// PlaceVisit is one stay at a place.
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

	// ClientVisitKey is what the phone calls the stay, so that a report sent
	// twice is one visit. A stay recorded by hand is given one.
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
}
