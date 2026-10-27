package db

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// LocationOperation is where a person has been: the positions their phone or
// browser reported, the places those are at and the visits to them. All of it
// belongs to one person, and every method takes that person, so that reading
// or changing somebody else's is not a question of which call to make.
//
// A person can be asked about only through these. Sharing a person's location
// with a circle is a later addition to this interface, not part of it.
type LocationOperation interface {
	// InsertLocationPoints stores positions and says how many were new. A
	// position already stored (the same person, instant and source) adds
	// nothing, which is what lets a phone send a batch again until it knows
	// the server has it. ErrInvalidArguments for coordinates that are not on
	// the earth, a negative accuracy or an unknown source.
	InsertLocationPoints(userId string, points []*models.LocationPoint) (newCount int, err error)

	// GetLatestLocationPoint is the most recently measured position, or nil
	// when there is none.
	GetLatestLocationPoint(userId string) (*models.LocationPoint, error)

	// GetLatestLocationReportAt is the latest time the person's phone reported
	// anything: the latest of a position's measured time and the arrival or
	// departure of a visit. It is the zero time when there is none.
	GetLatestLocationReportAt(userId string) (time.Time, error)

	// GetLatestLocationPoints is GetLatestLocationPoint for many people in one
	// query. A person with no position is not in the answer.
	GetLatestLocationPoints(userIds []string) (map[string]*models.LocationPoint, error)

	// ListLocationPoints reads the positions measured in a range, oldest
	// first, at most limitCount of them. A caller that must refuse a range
	// holding more asks for one more than it will take.
	ListLocationPoints(userId string, from, until time.Time, limitCount int) ([]*models.LocationPoint, error)

	// FindPlaceAt is the person's place whose radius holds the point, the
	// nearest center first, or nil when there is none.
	FindPlaceAt(userId string, latitudeDegrees, longitudeDegrees float64) (*models.Place, error)

	// ListPlacesNear is the person's places that hold the point or whose
	// center is within radiusMeters of it, nearest first, at most limitCount.
	// Each carries its distance.
	ListPlacesNear(userId string, latitudeDegrees, longitudeDegrees, radiusMeters float64, limitCount int) ([]*PlaceDistance, error)

	// FindPlaceIDsByOSM maps the OpenStreetMap objects the person already has
	// a place for to that place. A place with no OpenStreetMap object is never
	// in the answer.
	FindPlaceIDsByOSM(userId string, references []OSMReference) (map[OSMReference]string, error)

	// SummarizePlaceVisits counts the visits to each of the places and says
	// when the last one was, for the places that have any.
	SummarizePlaceVisits(userId string, placeIds []string) (map[string]*PlaceVisitSummary, error)

	// CreatePlace makes a place for the person. ErrInvalidArguments for a
	// position off the earth, a radius outside what a person can set (zero
	// takes the default), or a label or OpenStreetMap type nobody knows. Names,
	// categories and addresses are cut to the width of their columns and have
	// line breaks and control characters taken out, as SetPlaceNamed does.
	CreatePlace(userId string, place *models.Place) (*models.Place, error)

	// SavePlace changes a place and answers it as it is now. A place moved
	// farther than its radius loses what the provider said about the old
	// position (the OpenStreetMap object, name and address, and the time it was
	// named), so that it is named again. ErrNotFound unless it is the person's,
	// ErrInvalidArguments for a change that is not allowed.
	SavePlace(userId, placeId string, changes PlaceChanges) (*models.Place, error)

	// GetPlace is one of the person's places, or ErrNotFound.
	GetPlace(userId, placeId string) (*models.Place, error)

	// ForgetPlace deletes a place and every visit to it, and with them the
	// location journal documents of the days those visits were on, in the
	// same transaction. ErrNotFound unless it is the person's.
	ForgetPlace(userId, placeId string) error

	// ListPlaces pages the person's places, each with its number of visits and
	// the time of the last one: labeled places first, then the ones the
	// person named, then the rest, each group by last visit. nextOffset is
	// the offset that reads on, and zero when this page is the last.
	ListPlaces(userId string, filter PlaceFilter) (places []*models.Place, nextOffset int, err error)

	// PutPlaceVisit stores a visit, or completes the one stored under the same
	// client_visit_key: a later report fills in the departure, and the
	// arrival if it was missing. Visits to one place less than ten minutes
	// apart are then joined into one, so that iOS splitting a visit in two
	// does not show as two; the key of a visit that was joined into another
	// stays known, and a report under it completes the visit it became. A
	// departure before the arrival already stored is the arrival. A new visit
	// with an arrival ends every other visit of the person that is still open
	// and began no later, at that arrival. An arrival delivered after the
	// visits that followed it is ended by NormalizePlaceVisits, which the caller
	// runs once its report is stored. isNew is whether the key was
	// unknown. The visit passed in is updated to what is stored. ErrNotFound
	// unless the place is the person's, ErrInvalidArguments for a visit with
	// neither time or that ends before it starts.
	PutPlaceVisit(userId string, visit *models.PlaceVisit) (isNew bool, err error)

	// GetPlaceVisitByClientKey is the visit the phone calls by this key, or the
	// visit that one was joined into, with its place, or nil when there is none.
	GetPlaceVisitByClientKey(userId, clientVisitKey string) (*models.PlaceVisit, error)

	// GetPlaceVisitByArrival is the person's visit from iOS visit monitoring
	// (VisitSourceVisit) that arrived in the same second, or nil when there is
	// none. iOS reports a visit again with its departure and a position it
	// has made more exact, and the phone keys a visit by its position, so the
	// departure can come under a key the server has not seen.
	GetPlaceVisitByArrival(userId string, arrivedAt time.Time) (*models.PlaceVisit, error)

	// DeletePassByVisits deletes the person's pass-bys that ended in the
	// range, or near enough to it for a position in it to tell of them, and
	// answers the ids of the visits it deleted. A pass-by is a
	// visit from a place's circle (VisitSourceRegion) shorter than
	// PassByDurationMost, with a position near it at
	// PassBySpeedMetersPerSecondLeast or faster and none during it at
	// PassByStoppedSpeedMetersPerSecondMost or slower: the phone crossing the
	// circle on the way somewhere else. Call it once the positions and the
	// visits of a report are stored.
	DeletePassByVisits(userId string, from, until time.Time) ([]string, error)

	// GetOpenPlaceVisit is the visit in progress, the latest one with no
	// departure, with its place; nil when there is none.
	GetOpenPlaceVisit(userId string) (*models.PlaceVisit, error)

	// GetOpenPlaceVisits is GetOpenPlaceVisit for many people in two queries.
	// A person with no visit in progress is not in the answer.
	GetOpenPlaceVisits(userIds []string) (map[string]*models.PlaceVisit, error)

	// NormalizePlaceVisits makes the person's visits that touch the range, and
	// the ten minutes round it, a sequence that does not overlap, whatever
	// order the reports of them arrived in: ordered by arrival, a visit still
	// open ends at the arrival of the next visit, a visit whose departure runs
	// past the arrival of the next visit at another place ends at that
	// arrival, a joined visit that has a visit at another place begin inside
	// it is split in two there, and visits to one place less than ten minutes
	// apart with nothing between them are joined. Call it once the visits of a
	// report are stored.
	NormalizePlaceVisits(userId string, from, until time.Time) error

	// ListOverlappedVisitRanges finds the people with a visit that began
	// after the arrival of a visit of theirs at another place and before it
	// ended, among visits that began since the instant, and for each the
	// range from the first such visit's arrival to the end of the last one,
	// for NormalizePlaceVisits. Visits put in order overlap none.
	ListOverlappedVisitRanges(since time.Time) ([]VisitRange, error)

	// EndOpenPlaceVisit sets the departure of the visit in progress and answers
	// it, or answers nil when there is none.
	EndOpenPlaceVisit(userId string, at time.Time) (*models.PlaceVisit, error)

	// GetPlaceVisit is one of the person's visits with its place, or nil when
	// they have no such visit.
	GetPlaceVisit(userId, visitId string) (*models.PlaceVisit, error)

	// ListPlaceVisits pages visits newest first, each with its place.
	// nextOffset is as in ListPlaces.
	ListPlaceVisits(userId string, filter PlaceVisitFilter) (visits []*models.PlaceVisit, nextOffset int, err error)

	// EraseLocationHistory deletes positions and visits that started in the
	// range (all of them when both ends are nil), and then the places left
	// with no visit that the person neither named nor labeled. Pauses that
	// began in the range and are over go too; the pause in effect stays, so
	// erasing does not resume recording. It deletes in
	// the same transaction the location journal documents of the days the
	// range touches, with their chunks, since those say where the person was
	// as plainly as the visits did. Anything the agent already learned from
	// those days stays in its memory until the person removes it.
	EraseLocationHistory(userId string, from, until *time.Time) (pointCount, visitCount, placeCount int, err error)

	// ClaimPlacesToName takes up to limitCount places that wait for a name and
	// that no instance has taken in the last ten minutes, oldest first and at
	// most a few of each person's, and marks them taken, so that two instances
	// do not ask a provider about the same place. Places of a person who may
	// not use Location are left alone.
	ClaimPlacesToName(limitCount int, now time.Time) ([]*models.Place, error)

	// ClaimPlaceToName takes one place that waits for a name, for the caller
	// that has just made it and names it at once, with the claim
	// ClaimPlacesToName makes. It answers nil when the place is gone, is named
	// already or was claimed in the last ten minutes, which is a place the
	// caller should leave alone.
	ClaimPlaceToName(placeId string, now time.Time) (*models.Place, error)

	// SetPlaceNamed records what a provider answered for a place. With an
	// errorMessage nothing else changes and the place stays unnamed, to be
	// claimed again once its claim is old. Without one, found is what the
	// provider knows about the position, or nil when it knows nothing, and either
	// way the place is named from now on. A name the person gave is never
	// replaced. ErrNotFound when there is no such place. claimedAt is the claim
	// the answer was asked under (Place.NamingClaimedAt); the answer is dropped
	// if the place has been moved, and so lost that claim, since it is about
	// where the place used to be. Nil skips that check.
	SetPlaceNamed(placeId string, claimedAt *time.Time, found *NamedPlace, errorMessage string, now time.Time) error

	// ReleasePlaceClaim gives back the claim on a place that was claimed to be
	// named but that nothing was asked about (the provider was busy), so that
	// the next run takes it at once instead of when the claim is old. The
	// errorMessage is kept on the place for the Location page. The claim is
	// kept if the place was claimed again since, and a place that is gone is
	// not an error.
	ReleasePlaceClaim(placeId string, claimedAt *time.Time, errorMessage string, now time.Time) error
}

// PlaceChanges is what a person may change about a place. A nil field is left
// as it is.
type PlaceChanges struct {
	// PlaceName is the person's name for the place. Empty, on a place the
	// person named, takes their name away: the OpenStreetMap name comes back,
	// or, with none, no name at all and the place waits to be named again.
	PlaceName *string

	// PlaceLabel is the label; the pointer to the empty label takes it off.
	PlaceLabel *models.PlaceLabel

	// LatitudeDegrees and LongitudeDegrees move the place and are changed
	// together.
	LatitudeDegrees  *float64
	LongitudeDegrees *float64

	RadiusMeters *float64

	// AddressText is an address the person typed; empty takes a typed address
	// away, and the place waits to be named again. Typing one drops what a
	// provider said about the spot (object, name, category) whether or not the
	// place moves: the person said the place is at this address, and the map's
	// name for the spot is not this place. With the coordinates of a match the
	// person picked, the place moves there; without, it stays where it is.
	// Either way the place keeps this address: it counts as named already, so
	// the naming job does not replace it. A place the person did not name is
	// called by the first part of the address.
	AddressText *string

	// MovedPlaceName is the name a place the person did not name takes when it
	// moves farther than its radius: what was learned about the old position is
	// dropped, and the place goes back to the name of its city until it is
	// named again. The caller looks the city up, since this package cannot.
	// Empty leaves the place with no name until the provider names it.
	MovedPlaceName string
}

// PlaceFilter narrows and pages a listing of places.
type PlaceFilter struct {
	// PlaceLabel keeps places with this label.
	PlaceLabel models.PlaceLabel

	// Query keeps places whose name, OpenStreetMap name or address contains
	// these words, whatever the case.
	Query string

	Offset     int
	LimitCount int
}

// PlaceVisitFilter narrows and pages a listing of visits.
type PlaceVisitFilter struct {
	// From and Until keep visits that overlap the range: those that ended at
	// or after From and began before Until. A visit still in progress has not
	// ended.
	From  *time.Time
	Until *time.Time

	PlaceID    string
	PlaceLabel models.PlaceLabel

	Offset     int
	LimitCount int
}

// OSMReference names an object on the OpenStreetMap map.
type OSMReference struct {
	OSMType string
	OSMID   int64
}

// PlaceDistance is one of the person's places and how far its center is from
// a point that was asked about.
type PlaceDistance struct {
	Place          *models.Place
	DistanceMeters float64
}

// PlaceVisitSummary is how often a place was visited and when last.
type PlaceVisitSummary struct {
	VisitCount  int
	LastVisitAt *time.Time
}

// ErrPlaceCountMost is the answer to making a place for a person who has
// PlaceCountMost of them. It is an ErrInvalidArguments, and one a caller that
// has other things to store can tell apart and go on without.
var ErrPlaceCountMost = fmt.Errorf("%w: a person may have at most %d places; forget some first", ErrInvalidArguments, PlaceCountMost)

// NamedPlace is what a provider knows about a position, for SetPlaceNamed.
type NamedPlace struct {
	PlaceName   string
	OSMType     string
	OSMID       int64
	OSMCategory string
	AddressText string
}

const (
	// PlacePageDefault and PlacePageMost bound a page of places
	// or of visits when the caller names no limit or too large a one.
	PlacePageDefault = 50
	PlacePageMost    = 200

	// placeVisitJoinGap is how close two Visits to one place must be to be one:
	// iOS sometimes reports a visit as two, with a few minutes between.
	placeVisitJoinGap = 10 * time.Minute

	// PassByDurationMost is how long a visit from a place's circle may last
	// and still be a pass-by: iOS tells of crossing a watched circle the
	// moment it happens, with no least stay, so driving past a place the
	// person labeled would otherwise be a visit to it.
	PassByDurationMost = 3 * time.Minute

	// PassBySpeedMetersPerSecondLeast is a speed that says the person was
	// travelling through: faster than a jog (4 m/s is 14 km/h).
	PassBySpeedMetersPerSecondLeast = 4.0

	// PassByStoppedSpeedMetersPerSecondMost is a speed that says the person
	// stopped, as for a drop-off at a school: a short visit with such a
	// position during it is a visit.
	PassByStoppedSpeedMetersPerSecondMost = 0.5

	// passByPositionWindow is how long before the arrival and after the
	// departure a position still says how the person went through the
	// circle: in the background iOS gives a position every few minutes.
	passByPositionWindow = 2 * time.Minute

	// placeNamingPerPersonMost is how many of one person's places the naming
	// job takes in a run.
	placeNamingPerPersonMost = 3

	// PlaceCountMost is how many places one person may have.
	PlaceCountMost = 5000

	// placeNamingClaimDuration is how long a place taken for naming is left
	// alone before another instance may take it.
	placeNamingClaimDuration = 10 * time.Minute

	// locationPointBatchSize is how many positions one INSERT carries.
	locationPointBatchSize = 200

	// The widths of the columns a provider's answer is kept in.
	placeNameMaximumLength        = 256
	placeCategoryMaximumLength    = 128
	placeAddressMaximumLength     = 512
	placeNamingErrorMaximumLength = 512
)

type locationPointModel struct {
	ID                   string    `gorm:"column:id;primaryKey"`
	UserID               string    `gorm:"column:user_id"`
	LatitudeDegrees      float64   `gorm:"column:latitude_degrees"`
	LongitudeDegrees     float64   `gorm:"column:longitude_degrees"`
	AccuracyMeters       float64   `gorm:"column:accuracy_meters"`
	AltitudeMeters       *float64  `gorm:"column:altitude_meters"`
	SpeedMetersPerSecond *float64  `gorm:"column:speed_meters_per_second"`
	CourseDegrees        *float64  `gorm:"column:course_degrees"`
	MeasuredAt           time.Time `gorm:"column:measured_at"`
	ReceivedAt           time.Time `gorm:"column:received_at"`
	LocationSource       string    `gorm:"column:location_source"`
	AgentPhoneID         *string   `gorm:"column:agent_phone_id"`
}

func (locationPointModel) TableName() string { return "location_point" }

func (self *locationPointModel) toModel() *models.LocationPoint {
	point := &models.LocationPoint{
		ID:                   self.ID,
		UserID:               self.UserID,
		LatitudeDegrees:      self.LatitudeDegrees,
		LongitudeDegrees:     self.LongitudeDegrees,
		AccuracyMeters:       self.AccuracyMeters,
		AltitudeMeters:       self.AltitudeMeters,
		SpeedMetersPerSecond: self.SpeedMetersPerSecond,
		CourseDegrees:        self.CourseDegrees,
		MeasuredAt:           self.MeasuredAt.In(time.Local),
		ReceivedAt:           self.ReceivedAt.In(time.Local),
		LocationSource:       models.LocationSource(self.LocationSource),
	}
	if self.AgentPhoneID != nil {
		point.AgentPhoneID = *self.AgentPhoneID
	}
	return point
}

type placeModel struct {
	ID                 string     `gorm:"column:id;primaryKey"`
	UserID             string     `gorm:"column:user_id"`
	PlaceName          string     `gorm:"column:place_name"`
	IsNamedByPerson    bool       `gorm:"column:is_named_by_person"`
	PlaceLabel         string     `gorm:"column:place_label"`
	LatitudeDegrees    float64    `gorm:"column:latitude_degrees"`
	LongitudeDegrees   float64    `gorm:"column:longitude_degrees"`
	RadiusMeters       float64    `gorm:"column:radius_meters"`
	OSMType            string     `gorm:"column:osm_type"`
	OSMID              *int64     `gorm:"column:osm_id"`
	OSMName            string     `gorm:"column:osm_name"`
	OSMCategory        string     `gorm:"column:osm_category"`
	AddressText        string     `gorm:"column:address_text"`
	IsAddressTyped     bool       `gorm:"column:is_address_typed"`
	NamedAt            *time.Time `gorm:"column:named_at"`
	NamingClaimedAt    *time.Time `gorm:"column:naming_claimed_at"`
	NamingErrorMessage string     `gorm:"column:naming_error_message"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (placeModel) TableName() string { return "place" }

func (self *placeModel) toModel() *models.Place {
	place := &models.Place{
		ID:                 self.ID,
		UserID:             self.UserID,
		PlaceName:          self.PlaceName,
		IsNamedByPerson:    self.IsNamedByPerson,
		PlaceLabel:         models.PlaceLabel(self.PlaceLabel),
		LatitudeDegrees:    self.LatitudeDegrees,
		LongitudeDegrees:   self.LongitudeDegrees,
		RadiusMeters:       self.RadiusMeters,
		OSMType:            self.OSMType,
		OSMName:            self.OSMName,
		OSMCategory:        self.OSMCategory,
		AddressText:        self.AddressText,
		IsAddressTyped:     self.IsAddressTyped,
		NamedAt:            localTime(self.NamedAt),
		NamingClaimedAt:    localTime(self.NamingClaimedAt),
		NamingErrorMessage: self.NamingErrorMessage,
		CreatedAt:          self.CreatedAt.In(time.Local),
		UpdatedAt:          self.UpdatedAt.In(time.Local),
	}
	if self.OSMID != nil {
		place.OSMID = *self.OSMID
	}
	return place
}

// placeListRow is a place with what the listing adds to it.
type placeListRow struct {
	Place       placeModel `gorm:"embedded"`
	VisitCount  int        `gorm:"column:visit_count"`
	LastVisitAt *time.Time `gorm:"column:last_visit_at"`
}

type placeVisitModel struct {
	ID               string     `gorm:"column:id;primaryKey"`
	UserID           string     `gorm:"column:user_id"`
	PlaceID          string     `gorm:"column:place_id"`
	ArrivedAt        *time.Time `gorm:"column:arrived_at"`
	DepartedAt       *time.Time `gorm:"column:departed_at"`
	LatitudeDegrees  float64    `gorm:"column:latitude_degrees"`
	LongitudeDegrees float64    `gorm:"column:longitude_degrees"`
	AccuracyMeters   float64    `gorm:"column:accuracy_meters"`
	VisitSource      string     `gorm:"column:visit_source"`
	ClientVisitKey   string     `gorm:"column:client_visit_key"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
}

func (placeVisitModel) TableName() string { return "place_visit" }

func (self *placeVisitModel) toModel() *models.PlaceVisit {
	return &models.PlaceVisit{
		ID:               self.ID,
		UserID:           self.UserID,
		PlaceID:          self.PlaceID,
		ArrivedAt:        localTime(self.ArrivedAt),
		DepartedAt:       localTime(self.DepartedAt),
		LatitudeDegrees:  self.LatitudeDegrees,
		LongitudeDegrees: self.LongitudeDegrees,
		AccuracyMeters:   self.AccuracyMeters,
		VisitSource:      models.VisitSource(self.VisitSource),
		ClientVisitKey:   self.ClientVisitKey,
		CreatedAt:        self.CreatedAt.In(time.Local),
		UpdatedAt:        self.UpdatedAt.In(time.Local),
	}
}

// isOnEarth is whether coordinates are ones a position can have. A value that
// is not a number fails every comparison.
func isOnEarth(latitudeDegrees, longitudeDegrees float64) bool {
	return latitudeDegrees >= -90 && latitudeDegrees <= 90 && longitudeDegrees >= -180 && longitudeDegrees <= 180
}

// isRadiusAllowed is whether a person may give a place this radius.
func isRadiusAllowed(radiusMeters float64) bool {
	return radiusMeters >= models.PlaceRadiusMinimumMeters && radiusMeters <= models.PlaceRadiusMaximumMeters
}

// cleanPlaceText is text a provider or a caller gave for a place, fit to keep:
// line breaks and tabs become spaces, other control characters are dropped,
// the ends are trimmed and it is cut to the width of its column. A name is
// read back to the model, and a column has a width.
func cleanPlaceText(text string, limit int) string {
	cleaned := strings.Map(func(character rune) rune {
		switch {
		case unicode.IsSpace(character):
			return ' '
		case unicode.IsControl(character):
			return -1
		}
		return character
	}, text)
	return truncateRunes(strings.TrimSpace(cleaned), limit)
}

// pageLimit is the size of a page the caller asked for, kept to what a page
// may be.
func pageLimit(limitCount int) int {
	if limitCount <= 0 {
		return PlacePageDefault
	}
	return min(limitCount, PlacePageMost)
}

// placeDistanceSQL is the great circle distance in meters between a place's
// center and the point @latitude, @longitude: the haversine formula, with the
// same earth radius as the one in internal/place that measures the same thing
// in Go. It is done here because choosing the place a point is in is a filter
// over a person's places, and because internal/place cannot be imported from
// the database package.
const placeDistanceSQL = `(2 * 6371000 * asin(least(1, sqrt(
	power(sin(radians(@latitude - place.latitude_degrees) / 2), 2) +
	cos(radians(place.latitude_degrees)) * cos(radians(@latitude)) *
	power(sin(radians(@longitude - place.longitude_degrees) / 2), 2)))))`

func (self *transaction) InsertLocationPoints(userId string, points []*models.LocationPoint) (int, error) {
	if userId == "" {
		return 0, ErrInvalidArguments
	}
	now := time.Now()
	rows := make([]locationPointModel, 0, len(points))
	for _, point := range points {
		if point == nil || !isOnEarth(point.LatitudeDegrees, point.LongitudeDegrees) ||
			point.AccuracyMeters < 0 || math.IsNaN(point.AccuracyMeters) || point.MeasuredAt.IsZero() ||
			!point.LocationSource.IsValid() {
			return 0, fmt.Errorf("%w: a position needs coordinates on the earth, a time, a source and an accuracy that is not negative", ErrInvalidArguments)
		}
		row := locationPointModel{
			ID:                   newID(),
			UserID:               userId,
			LatitudeDegrees:      point.LatitudeDegrees,
			LongitudeDegrees:     point.LongitudeDegrees,
			AccuracyMeters:       point.AccuracyMeters,
			AltitudeMeters:       point.AltitudeMeters,
			SpeedMetersPerSecond: point.SpeedMetersPerSecond,
			CourseDegrees:        point.CourseDegrees,
			MeasuredAt:           point.MeasuredAt,
			ReceivedAt:           now,
			LocationSource:       string(point.LocationSource),
			AgentPhoneID:         nullableText(point.AgentPhoneID),
		}
		if !point.ReceivedAt.IsZero() {
			row.ReceivedAt = point.ReceivedAt
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	result := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "measured_at"}, {Name: "location_source"}},
		DoNothing: true,
	}).CreateInBatches(&rows, locationPointBatchSize)
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func (self *transaction) GetLatestLocationPoint(userId string) (*models.LocationPoint, error) {
	var found []locationPointModel
	if err := self.tx.Where("\"user_id\" = ?", userId).
		Order("\"measured_at\" DESC, \"id\" DESC").Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) GetLatestLocationReportAt(userId string) (time.Time, error) {
	var reportedAt struct {
		LatestReportAt *time.Time `gorm:"column:latest_report_at"`
	}
	if err := self.tx.Raw(`SELECT GREATEST(
			(SELECT max(measured_at) FROM "location_point" WHERE user_id = @user),
			(SELECT max(arrived_at) FROM "place_visit" WHERE user_id = @user),
			(SELECT max(departed_at) FROM "place_visit" WHERE user_id = @user)) AS latest_report_at`,
		sql.Named("user", userId)).Scan(&reportedAt).Error; err != nil {
		return time.Time{}, err
	}
	if reportedAt.LatestReportAt == nil {
		return time.Time{}, nil
	}
	return *reportedAt.LatestReportAt, nil
}

func (self *transaction) GetLatestLocationPoints(userIds []string) (map[string]*models.LocationPoint, error) {
	latest := map[string]*models.LocationPoint{}
	userIds = uniqueStrings(userIds)
	if len(userIds) == 0 {
		return latest, nil
	}
	var rows []locationPointModel
	if err := self.tx.Raw(`SELECT DISTINCT ON (point.user_id) point.* FROM "location_point" point
		WHERE point.user_id IN ?
		ORDER BY point.user_id, point.measured_at DESC, point.id DESC`, userIds).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		latest[rows[index].UserID] = rows[index].toModel()
	}
	return latest, nil
}

func (self *transaction) FindPlaceAt(userId string, latitudeDegrees, longitudeDegrees float64) (*models.Place, error) {
	if !isOnEarth(latitudeDegrees, longitudeDegrees) {
		return nil, nil
	}
	// The latitude band keeps the exact distance from being worked out for
	// every place the person has; no place reaches further than its largest
	// radius. Longitude is left to the distance, which handles the date line.
	bandDegrees := models.PlaceRadiusMaximumMeters/111000.0 + 0.001
	southDegrees, northDegrees := latitudeDegrees-bandDegrees, latitudeDegrees+bandDegrees
	var found []placeModel
	if err := self.tx.Raw(`SELECT place.* FROM "place" place
		WHERE place.user_id = @user
		  AND place.latitude_degrees BETWEEN @south AND @north
		  AND `+placeDistanceSQL+` <= place.radius_meters
		ORDER BY `+placeDistanceSQL+` ASC, place.created_at ASC, place.id ASC
		LIMIT 1`,
		sql.Named("user", userId), sql.Named("latitude", latitudeDegrees),
		sql.Named("longitude", longitudeDegrees),
		sql.Named("south", southDegrees), sql.Named("north", northDegrees)).Scan(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) CreatePlace(userId string, place *models.Place) (*models.Place, error) {
	if userId == "" || place == nil || !isOnEarth(place.LatitudeDegrees, place.LongitudeDegrees) {
		return nil, ErrInvalidArguments
	}
	radiusMeters := place.RadiusMeters
	if radiusMeters == 0 {
		radiusMeters = models.PlaceRadiusDefaultMeters
	}
	if !isRadiusAllowed(radiusMeters) {
		return nil, fmt.Errorf("%w: the radius of a place is between %d and %d meters", ErrInvalidArguments, models.PlaceRadiusMinimumMeters, models.PlaceRadiusMaximumMeters)
	}
	if place.PlaceLabel != "" && !place.PlaceLabel.IsValid() {
		return nil, fmt.Errorf("%w: %q is not a label a place can have", ErrInvalidArguments, place.PlaceLabel)
	}
	switch place.OSMType {
	case "", models.OSMTypeNode, models.OSMTypeWay, models.OSMTypeRelation:
	default:
		return nil, fmt.Errorf("%w: %q is not an OpenStreetMap type", ErrInvalidArguments, place.OSMType)
	}
	if utf8.RuneCountInString(place.PlaceName) > placeNameMaximumLength {
		return nil, fmt.Errorf("%w: a name is at most %d characters", ErrInvalidArguments, placeNameMaximumLength)
	}
	var placeCount int64
	if err := self.tx.Model(&placeModel{}).Where("\"user_id\" = ?", userId).Count(&placeCount).Error; err != nil {
		return nil, err
	}
	if placeCount >= PlaceCountMost {
		return nil, ErrPlaceCountMost
	}
	now := time.Now()
	row := &placeModel{
		ID:                 newID(),
		UserID:             userId,
		PlaceName:          cleanPlaceText(place.PlaceName, placeNameMaximumLength),
		IsNamedByPerson:    place.IsNamedByPerson,
		PlaceLabel:         string(place.PlaceLabel),
		LatitudeDegrees:    place.LatitudeDegrees,
		LongitudeDegrees:   place.LongitudeDegrees,
		RadiusMeters:       radiusMeters,
		OSMType:            place.OSMType,
		OSMName:            cleanPlaceText(place.OSMName, placeNameMaximumLength),
		OSMCategory:        cleanPlaceText(place.OSMCategory, placeCategoryMaximumLength),
		AddressText:        cleanPlaceText(place.AddressText, placeAddressMaximumLength),
		IsAddressTyped:     place.IsAddressTyped,
		NamedAt:            place.NamedAt,
		NamingErrorMessage: cleanPlaceText(place.NamingErrorMessage, placeNamingErrorMaximumLength),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if place.OSMID != 0 {
		osmId := place.OSMID
		row.OSMID = &osmId
	}
	if err := self.tx.Create(row).Error; err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) GetPlace(userId, placeId string) (*models.Place, error) {
	var found []placeModel
	if err := self.tx.Where("\"id\" = ? AND \"user_id\" = ?", placeId, userId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	return found[0].toModel(), nil
}

func (self *transaction) SavePlace(userId, placeId string, changes PlaceChanges) (*models.Place, error) {
	if (changes.LatitudeDegrees == nil) != (changes.LongitudeDegrees == nil) {
		return nil, fmt.Errorf("%w: a place moves by both coordinates", ErrInvalidArguments)
	}
	if changes.LatitudeDegrees != nil && !isOnEarth(*changes.LatitudeDegrees, *changes.LongitudeDegrees) {
		return nil, fmt.Errorf("%w: those coordinates are not on the earth", ErrInvalidArguments)
	}
	if changes.RadiusMeters != nil && !isRadiusAllowed(*changes.RadiusMeters) {
		return nil, fmt.Errorf("%w: the radius of a place is between %d and %d meters", ErrInvalidArguments, models.PlaceRadiusMinimumMeters, models.PlaceRadiusMaximumMeters)
	}
	if changes.PlaceLabel != nil && *changes.PlaceLabel != "" && !changes.PlaceLabel.IsValid() {
		return nil, fmt.Errorf("%w: %q is not a label a place can have", ErrInvalidArguments, *changes.PlaceLabel)
	}
	if changes.PlaceName != nil && utf8.RuneCountInString(*changes.PlaceName) > placeNameMaximumLength {
		return nil, fmt.Errorf("%w: a name is at most %d characters", ErrInvalidArguments, placeNameMaximumLength)
	}
	addressText := ""
	if changes.AddressText != nil {
		addressText = cleanPlaceText(*changes.AddressText, placeAddressMaximumLength)
	}

	var locked placeModel
	if err := lockRow(self.tx, &locked, placeId); err != nil || locked.UserID != userId {
		// Somebody else's place is no place at all.
		if err == nil {
			err = ErrNotFound
		}
		return nil, err
	}
	updates := map[string]any{"updated_at": time.Now()}
	if changes.PlaceName != nil {
		name := cleanPlaceText(*changes.PlaceName, placeNameMaximumLength)
		switch {
		case name != "":
			updates["place_name"] = name
			updates["is_named_by_person"] = true
		case locked.IsNamedByPerson:
			// Back to what the map calls it. A place the map never named has
			// nothing to go back to, so it waits to be named again.
			updates["is_named_by_person"] = false
			switch {
			case locked.OSMName != "":
				updates["place_name"] = locked.OSMName
			case locked.AddressText != "":
				updates["place_name"] = strings.TrimSpace(strings.SplitN(locked.AddressText, ",", 2)[0])
			default:
				updates["place_name"] = ""
				updates["named_at"] = nil
				updates["naming_claimed_at"] = nil
			}
		}
	}
	if changes.PlaceLabel != nil {
		updates["place_label"] = string(*changes.PlaceLabel)
	}
	if changes.LatitudeDegrees != nil {
		updates["latitude_degrees"] = *changes.LatitudeDegrees
		updates["longitude_degrees"] = *changes.LongitudeDegrees

		// Everything a provider said was said about the old position. A place moved
		// farther than its radius is somewhere else, so that is dropped and the
		// place waits to be named again; a place nudged within its radius is
		// still the same place.
		var movedMeters float64
		if err := self.tx.Raw(`SELECT `+placeDistanceSQL+` FROM "place" place WHERE place.id = @place`,
			sql.Named("latitude", *changes.LatitudeDegrees), sql.Named("longitude", *changes.LongitudeDegrees),
			sql.Named("place", placeId)).Scan(&movedMeters).Error; err != nil {
			return nil, err
		}
		if movedMeters > locked.RadiusMeters {
			updates["osm_type"] = ""
			updates["osm_id"] = nil
			updates["osm_name"] = ""
			updates["osm_category"] = ""
			// An address the person typed was not the map's to say, so moving
			// the place (to put its pin on the house) keeps it, keeps the
			// place named and leaves it out of the naming job.
			if !locked.IsAddressTyped || changes.AddressText != nil {
				updates["address_text"] = ""
				updates["named_at"] = nil
				updates["naming_claimed_at"] = nil
				updates["naming_error_message"] = ""
				isNamedByPerson := locked.IsNamedByPerson
				if value, isChanged := updates["is_named_by_person"].(bool); isChanged {
					isNamedByPerson = value
				}
				if !isNamedByPerson {
					updates["place_name"] = cleanPlaceText(changes.MovedPlaceName, placeNameMaximumLength)
				}
			}
		}
	}
	if changes.AddressText != nil {
		isNamedByPerson := locked.IsNamedByPerson
		if value, isChanged := updates["is_named_by_person"].(bool); isChanged {
			isNamedByPerson = value
		}
		if addressText == "" {
			// The typed address is taken away: the place waits to be named
			// again, and is called by what the map called it until it is.
			updates["is_address_typed"] = false
			updates["address_text"] = ""
			updates["named_at"] = nil
			updates["naming_claimed_at"] = nil
			updates["naming_error_message"] = ""
			if !isNamedByPerson {
				osmName := locked.OSMName
				if dropped, isDropped := updates["osm_name"].(string); isDropped {
					osmName = dropped
				}
				placeName := osmName
				if changes.LatitudeDegrees != nil && osmName == "" {
					placeName = changes.MovedPlaceName
				}
				updates["place_name"] = cleanPlaceText(placeName, placeNameMaximumLength)
			}
		} else {
			// The person said the place is at this address. What the map called
			// the spot (a car park, a neighbor's shop) is not this place, so it
			// goes with or without a move.
			updates["osm_type"] = ""
			updates["osm_id"] = nil
			updates["osm_name"] = ""
			updates["osm_category"] = ""
			updates["is_address_typed"] = true
			updates["address_text"] = addressText
			updates["named_at"] = time.Now()
			updates["naming_claimed_at"] = nil
			updates["naming_error_message"] = ""
			if !isNamedByPerson {
				updates["place_name"] = cleanPlaceText(strings.SplitN(addressText, ",", 2)[0], placeNameMaximumLength)
			}
		}
	}
	if changes.RadiusMeters != nil {
		updates["radius_meters"] = *changes.RadiusMeters
	}
	if err := self.tx.Model(&placeModel{}).Where("\"id\" = ?", placeId).Updates(updates).Error; err != nil {
		return nil, err
	}
	return self.GetPlace(userId, placeId)
}

// locationDayDocumentsSQL starts a statement over the location journal
// documents of one person (@user): one per finished day, in the location
// source of the person's agent, named location:YYYY-MM-DD. A condition on
// document.happened_at, the start of the day in the person's own time zone,
// follows it.
const locationDayDocumentsSQL = `FROM "agent_document" document
	JOIN "agent_source" location_source ON location_source.id = document.source_id AND location_source.kind = 'location'
	JOIN "agent" owner_agent ON owner_agent.id = location_source.agent_id AND owner_agent.user_id = @user
	WHERE document.external_id LIKE 'location:%'`

// locationDayEndSQL is when the day a journal document is about ended, for a
// document in a statement over locationDayDocumentsSQL. The database does not
// know the person's time zone, and a day is 25 hours long on the day the
// clocks go back, so the day ends where the next document begins when there is
// one within 25 hours, and 25 hours after it began if not: a day with no
// document after it had nothing in its last hour to be written down.
const locationDayEndSQL = `LEAST(document.happened_at + interval '25 hours', COALESCE(
	(SELECT min(later.happened_at) FROM "agent_document" later
		WHERE later.source_id = document.source_id AND later.external_id LIKE 'location:%' AND later.happened_at > document.happened_at),
	document.happened_at + interval '25 hours'))`

// deleteLocationDayDocuments deletes the location journal documents of the
// days a range touches, with their chunks and vectors (which go with the
// document), for all days when both ends are nil.
func (self *transaction) deleteLocationDayDocuments(userId string, from, until *time.Time) error {
	conditions := ""
	arguments := []any{sql.Named("user", userId)}
	if until != nil {
		conditions += ` AND document.happened_at < @until`
		arguments = append(arguments, sql.Named("until", *until))
	}
	if from != nil {
		conditions += ` AND ` + locationDayEndSQL + ` > @from`
		arguments = append(arguments, sql.Named("from", *from))
	}
	return self.tx.Exec(`DELETE FROM "agent_document" WHERE "id" IN (SELECT document.id `+locationDayDocumentsSQL+conditions+`)`,
		arguments...).Error
}

// deleteLocationDayDocumentsOfPlace deletes the journal documents of the days
// on which the person had a visit to the place, since those name it.
func (self *transaction) deleteLocationDayDocumentsOfPlace(userId, placeId string) error {
	return self.tx.Exec(`DELETE FROM "agent_document" WHERE "id" IN (SELECT document.id `+locationDayDocumentsSQL+`
		AND EXISTS (SELECT 1 FROM "place_visit" visit
			WHERE visit.user_id = @user AND visit.place_id = @place
			  AND COALESCE(visit.arrived_at, visit.departed_at) < `+locationDayEndSQL+`
			  AND COALESCE(visit.departed_at, now()) >= document.happened_at))`,
		sql.Named("user", userId), sql.Named("place", placeId)).Error
}

func (self *transaction) ForgetPlace(userId, placeId string) error {
	var locked placeModel
	if err := lockRow(self.tx, &locked, placeId); err != nil || locked.UserID != userId {
		// Somebody else's place is no place at all.
		if err == nil {
			err = ErrNotFound
		}
		return err
	}
	// Before the visits go: they say which days named the place.
	if err := self.deleteLocationDayDocumentsOfPlace(userId, placeId); err != nil {
		return err
	}
	return self.tx.Where("\"id\" = ? AND \"user_id\" = ?", placeId, userId).Delete(&placeModel{}).Error
}

func (self *transaction) ListPlaces(userId string, filter PlaceFilter) ([]*models.Place, int, error) {
	if filter.Offset < 0 || (filter.PlaceLabel != "" && !filter.PlaceLabel.IsValid()) {
		return nil, 0, ErrInvalidArguments
	}
	limit := pageLimit(filter.LimitCount)
	conditions := []string{`place.user_id = @user`}
	arguments := []any{sql.Named("user", userId)}
	if filter.PlaceLabel != "" {
		conditions = append(conditions, `place.place_label = @label`)
		arguments = append(arguments, sql.Named("label", string(filter.PlaceLabel)))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		conditions = append(conditions, `(place.place_name ILIKE @pattern OR place.osm_name ILIKE @pattern OR place.address_text ILIKE @pattern)`)
		arguments = append(arguments, sql.Named("pattern", "%"+escapeLike(query)+"%"))
	}
	arguments = append(arguments, sql.Named("offset", filter.Offset), sql.Named("limit", limit+1))

	var rows []placeListRow
	if err := self.tx.Raw(`SELECT place.*, COALESCE(visits.visit_count, 0) AS visit_count, visits.last_visit_at AS last_visit_at
		FROM "place" place
		LEFT JOIN (
			SELECT place_id, count(*) AS visit_count, max(GREATEST(arrived_at, departed_at)) AS last_visit_at
			FROM "place_visit" WHERE user_id = @user GROUP BY place_id
		) visits ON visits.place_id = place.id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY (place.place_label <> '') DESC, place.is_named_by_person DESC,
			visits.last_visit_at DESC NULLS LAST, place.created_at DESC, place.id DESC
		OFFSET @offset LIMIT @limit`, arguments...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	nextOffset := 0
	if len(rows) > limit {
		rows = rows[:limit]
		nextOffset = filter.Offset + limit
	}
	places := make([]*models.Place, 0, len(rows))
	for index := range rows {
		place := rows[index].Place.toModel()
		place.VisitCount = rows[index].VisitCount
		place.LastVisitAt = localTime(rows[index].LastVisitAt)
		places = append(places, place)
	}
	return places, nextOffset, nil
}

// visitStart is when a visit began as far as is known: its arrival, or its
// departure when iOS reported only that.
func visitStart(visit *placeVisitModel) time.Time {
	if visit.ArrivedAt != nil {
		return *visit.ArrivedAt
	}
	if visit.DepartedAt != nil {
		return *visit.DepartedAt
	}
	return visit.CreatedAt
}

// visitEnd is when a visit ended as far as is known: its departure, or, while
// the person is still there, the moment it began, since nothing later has been
// reported.
func visitEnd(visit *placeVisitModel) time.Time {
	if visit.DepartedAt != nil {
		return *visit.DepartedAt
	}
	return visitStart(visit)
}

// placeVisitKeyModel is the client key of a visit that was joined into
// another, and the visit it lives on in. The row of the visit that began first
// keeps its own key in place_visit; every key of the visits joined into it
// is here, so that a phone sending any of them again finds the visit they
// became.
type placeVisitKeyModel struct {
	UserID         string     `gorm:"column:user_id;primaryKey"`
	ClientVisitKey string     `gorm:"column:client_visit_key;primaryKey"`
	PlaceVisitID   string     `gorm:"column:place_visit_id"`
	ArrivedAt      *time.Time `gorm:"column:arrived_at"`
}

func (placeVisitKeyModel) TableName() string { return "place_visit_key" }

// findPlaceVisitByKey is the visit the phone calls by this key: the one with
// the key itself, or the one a visit with the key was joined into. Locked when
// isLocked, so that two reports of one visit take turns.
func (self *transaction) findPlaceVisitByKey(userId, clientVisitKey string, isLocked bool) (*placeVisitModel, error) {
	locking := ""
	if isLocked {
		locking = " FOR UPDATE OF visit"
	}
	var found []placeVisitModel
	if err := self.tx.Raw(`SELECT visit.* FROM "place_visit" visit
		WHERE visit.user_id = @user AND (visit.client_visit_key = @key OR visit.id IN (
			SELECT joined.place_visit_id FROM "place_visit_key" joined WHERE joined.user_id = @user AND joined.client_visit_key = @key))
		LIMIT 1`+locking, sql.Named("user", userId), sql.Named("key", clientVisitKey)).Scan(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

func (self *transaction) PutPlaceVisit(userId string, visit *models.PlaceVisit) (bool, error) {
	if userId == "" || visit == nil || visit.PlaceID == "" || !visit.VisitSource.IsValid() ||
		(visit.ArrivedAt == nil && visit.DepartedAt == nil) ||
		(visit.ArrivedAt != nil && visit.DepartedAt != nil && visit.DepartedAt.Before(*visit.ArrivedAt)) ||
		!isOnEarth(visit.LatitudeDegrees, visit.LongitudeDegrees) || visit.AccuracyMeters < 0 {
		return false, fmt.Errorf("%w: a visit needs a place, a source, coordinates on the earth and an arrival or a departure, and cannot end before it starts", ErrInvalidArguments)
	}
	// The place is locked, so that two reports of visits to it, which would
	// each join the other, take turns.
	var lockedPlace placeModel
	if err := lockRow(self.tx, &lockedPlace, visit.PlaceID); err != nil || lockedPlace.UserID != userId {
		if err == nil {
			err = ErrNotFound
		}
		return false, err
	}

	now := time.Now()
	key := visit.ClientVisitKey
	if key == "" {
		key = newID()
	}
	existing, err := self.findPlaceVisitByKey(userId, key, true)
	if err != nil {
		return false, err
	}
	isNew := existing == nil
	var kept placeVisitModel
	if isNew {
		kept = placeVisitModel{
			ID:               newID(),
			UserID:           userId,
			PlaceID:          visit.PlaceID,
			ArrivedAt:        visit.ArrivedAt,
			DepartedAt:       visit.DepartedAt,
			LatitudeDegrees:  visit.LatitudeDegrees,
			LongitudeDegrees: visit.LongitudeDegrees,
			AccuracyMeters:   visit.AccuracyMeters,
			VisitSource:      string(visit.VisitSource),
			ClientVisitKey:   key,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := self.tx.Create(&kept).Error; err != nil {
			return false, err
		}
		// The person is at this visit's place from its arrival, so a visit
		// still open elsewhere, whose departure the phone never delivered, is
		// over. Without this it would stay open for ever and count as time
		// spent in two places at once.
		// A visit open at this same place is ended too, and the join below
		// makes the two one visit again, open if this one is: of two visits
		// that end at the same moment, the one still going is the later.
		if kept.ArrivedAt != nil {
			var opened []placeVisitModel
			if err := self.tx.Where("\"user_id\" = ? AND \"id\" <> ? AND \"departed_at\" IS NULL AND \"arrived_at\" <= ?",
				userId, kept.ID, *kept.ArrivedAt).Find(&opened).Error; err != nil {
				return false, err
			}
			for index := range opened {
				// A visit joined from several, the later of which came before
				// this arrival was heard of, ends here and the later parts go
				// on as a visit of their own.
				if isSplit, err := self.splitVisitAt(&opened[index], *kept.ArrivedAt); err != nil {
					return false, err
				} else if isSplit {
					continue
				}
				if err := self.tx.Model(&placeVisitModel{}).Where("\"id\" = ?", opened[index].ID).
					Updates(map[string]any{"departed_at": *kept.ArrivedAt, "updated_at": now}).Error; err != nil {
					return false, err
				}
			}
		}
	} else {
		// A later report of the same visit completes it. The place, the
		// position and the source are what the first report said.
		kept = *existing
		isEarlierPartEnded := false
		// A report under the key of a visit that was joined into this one
		// gives that part its arrival, which is what a later split at another
		// place's arrival needs to find the part again.
		if existing.ClientVisitKey != key && visit.ArrivedAt != nil {
			if err := self.tx.Model(&placeVisitKeyModel{}).
				Where("\"user_id\" = ? AND \"client_visit_key\" = ? AND \"arrived_at\" IS NULL", userId, key).
				Update("arrived_at", *visit.ArrivedAt).Error; err != nil {
				return false, err
			}
		}
		var absorbed struct {
			KeyCount        int        `gorm:"column:key_count"`
			LatestArrivedAt *time.Time `gorm:"column:latest_arrived_at"`
		}
		if err := self.tx.Raw(`SELECT count(*) AS key_count, max(arrived_at) AS latest_arrived_at
			FROM "place_visit_key" WHERE place_visit_id = ?`, kept.ID).Scan(&absorbed).Error; err != nil {
			return false, err
		}
		if kept.ArrivedAt == nil && visit.ArrivedAt != nil {
			kept.ArrivedAt = visit.ArrivedAt
		}
		if visit.DepartedAt != nil {
			// The latest arrival among the visits joined into this one: a
			// departure before it belongs to an earlier part of the visit.
			latestArrivedAt := kept.ArrivedAt
			if absorbed.LatestArrivedAt != nil && (latestArrivedAt == nil || absorbed.LatestArrivedAt.After(*latestArrivedAt)) {
				latestArrivedAt = absorbed.LatestArrivedAt
			}
			departedAt := *visit.DepartedAt
			switch {
			case latestArrivedAt == nil || !departedAt.Before(*latestArrivedAt):
				kept.DepartedAt = &departedAt
			case absorbed.KeyCount == 0:
				// A departure never comes before the arrival it ends. Clamped
				// rather than refused: a refusal would fail the whole batch,
				// and the phone would send it again for ever.
				kept.DepartedAt = latestArrivedAt
			default:
				// The earlier part of a joined visit ended here, and the
				// visit it was joined into is still going, or ended later:
				// the joined visit is split where the earlier part ended.
				isEarlierPartEnded = true
			}
		}
		kept.UpdatedAt = now
		if err := self.tx.Model(&placeVisitModel{}).Where("\"id\" = ?", kept.ID).Updates(map[string]any{
			"arrived_at": kept.ArrivedAt, "departed_at": kept.DepartedAt, "updated_at": now,
		}).Error; err != nil {
			return false, err
		}
		if isEarlierPartEnded {
			if err := self.splitPlaceVisit(&kept, *visit.DepartedAt); err != nil {
				return false, err
			}
		}
	}

	// A visit left open by an arrival delivered after later visits is ended
	// by NormalizePlaceVisits, which the caller runs once the report is
	// stored: it ends an open visit at the next arrival at any place and joins
	// the visits to one place. Ending it here at the next arrival at another
	// place would skip a later visit to this same place and lose its departure.

	joined, err := self.joinPlaceVisits(&kept)
	if err != nil {
		return false, err
	}
	*visit = *joined.toModel()
	return isNew, nil
}

// joinPlaceVisits merges the visit with every other visit to its place that
// ends less than ten minutes before it starts, starts less than ten minutes
// after it ends, or overlaps it, and answers the one that remains. The one
// that began first stays, with the latest departure of them all, or none if
// the latest of them is still in progress; the others are deleted, and their
// client keys are kept in place_visit_key.
func (self *transaction) joinPlaceVisits(visit *placeVisitModel) (*placeVisitModel, error) {
	// Only the visits that can join this one are read: those that begin before
	// it ends plus the gap and end after it begins minus the gap (a visit with
	// no departure ends where it begins). A home or a workplace has thousands
	// of visits after a year, and the rest cannot take part.
	// A visit known only by its departure is not joined to anything: it may be
	// the end of a visit that began long before, or of one the person left and
	// came back to, and joining it would lose which. It joins once its arrival
	// is reported.
	if visit.ArrivedAt == nil {
		return visit, nil
	}
	var group []placeVisitModel
	if err := self.tx.Where("\"user_id\" = ? AND \"place_id\" = ? AND \"arrived_at\" IS NOT NULL AND COALESCE(\"arrived_at\", \"departed_at\") < ? AND COALESCE(\"departed_at\", \"arrived_at\") > ?",
		visit.UserID, visit.PlaceID, visitEnd(visit).Add(placeVisitJoinGap), visitStart(visit).Add(-placeVisitJoinGap)).
		Order("COALESCE(\"arrived_at\", \"departed_at\") ASC, \"id\" ASC").Find(&group).Error; err != nil {
		return nil, err
	}
	if len(group) == 0 {
		return visit, nil
	}
	// The person was somewhere else in between when a visit at another place
	// began after one of these began and before the next did: they came back,
	// and the two are not one visit however close they are.
	var otherStarts []time.Time
	if len(group) > 1 {
		if err := self.tx.Model(&placeVisitModel{}).
			Where("\"user_id\" = ? AND \"place_id\" <> ? AND COALESCE(\"arrived_at\", \"departed_at\") > ? AND COALESCE(\"arrived_at\", \"departed_at\") < ?",
				visit.UserID, visit.PlaceID, visitStart(&group[0]), visitStart(&group[len(group)-1])).
			Pluck("COALESCE(\"arrived_at\", \"departed_at\")", &otherStarts).Error; err != nil {
			return nil, err
		}
	}
	isSomewhereElseBetween := func(earlier, later time.Time) bool {
		for _, otherStart := range otherStarts {
			if otherStart.After(earlier) && otherStart.Before(later) {
				return true
			}
		}
		return false
	}
	// A pause that began between two of them is time nobody knows where the
	// person was, which a visit that ended where the pause began and one that
	// began soon after it must not close over, however short the pause.
	var pauseStarts []time.Time
	if len(group) > 1 {
		if err := self.tx.Model(&locationPauseModel{}).
			Where("\"user_id\" = ? AND \"started_at\" >= ? AND \"started_at\" < ?", visit.UserID, visitStart(&group[0]), visitStart(&group[len(group)-1])).
			Pluck("\"started_at\"", &pauseStarts).Error; err != nil {
			return nil, err
		}
	}
	isPausedBetween := func(earlierEnd, later time.Time) bool {
		for _, pauseStart := range pauseStarts {
			if !pauseStart.Before(earlierEnd) && pauseStart.Before(later) {
				return true
			}
		}
		return false
	}
	// Walk the visits in the order they began, growing a run: each visit joins
	// the run if it begins before the run's end plus the gap, and starts a new
	// run if not. The run that holds this visit is the one to merge.
	var chosen []placeVisitModel
	runStart := 0
	runEnd := visitEnd(&group[0])
	for index := 1; index <= len(group); index++ {
		if index < len(group) && visitStart(&group[index]).Before(runEnd.Add(placeVisitJoinGap)) &&
			!isSomewhereElseBetween(visitStart(&group[index-1]), visitStart(&group[index])) &&
			!isPausedBetween(visitEnd(&group[index-1]), visitStart(&group[index])) {
			if end := visitEnd(&group[index]); end.After(runEnd) {
				runEnd = end
			}
			continue
		}
		for _, candidate := range group[runStart:index] {
			if candidate.ID == visit.ID {
				chosen = group[runStart:index]
			}
		}
		if chosen != nil || index == len(group) {
			break
		}
		runStart, runEnd = index, visitEnd(&group[index])
	}
	if len(chosen) <= 1 {
		return visit, nil
	}

	survivor := &chosen[0]
	latest := &chosen[0]
	for index := range chosen {
		// At the same moment a visit still going is later than one that ended.
		if visitEnd(&chosen[index]).After(visitEnd(latest)) ||
			(visitEnd(&chosen[index]).Equal(visitEnd(latest)) && chosen[index].DepartedAt == nil && latest.DepartedAt != nil) {
			latest = &chosen[index]
		}
	}
	survivor.DepartedAt = latest.DepartedAt
	// A visit iOS saw only the end of takes the arrival of the one that did.
	if survivor.ArrivedAt == nil {
		for _, candidate := range chosen[1:] {
			if candidate.ArrivedAt != nil {
				survivor.ArrivedAt = candidate.ArrivedAt
				break
			}
		}
	}
	survivor.UpdatedAt = time.Now()
	removedIds := make([]string, 0, len(chosen)-1)
	for _, candidate := range chosen[1:] {
		removedIds = append(removedIds, candidate.ID)
		// The phone calls the visit it sent by its key, and will send that key
		// again: it has to find the visit it became, not a new one.
		if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&placeVisitKeyModel{
			UserID: candidate.UserID, ClientVisitKey: candidate.ClientVisitKey,
			PlaceVisitID: survivor.ID, ArrivedAt: candidate.ArrivedAt,
		}).Error; err != nil {
			return nil, err
		}
	}
	// The keys of visits joined earlier into the ones that go, before they go.
	if err := self.tx.Model(&placeVisitKeyModel{}).Where("\"place_visit_id\" IN ?", removedIds).
		Update("place_visit_id", survivor.ID).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Where("\"id\" IN ?", removedIds).Delete(&placeVisitModel{}).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Model(&placeVisitModel{}).Where("\"id\" = ?", survivor.ID).Updates(map[string]any{
		"arrived_at": survivor.ArrivedAt, "departed_at": survivor.DepartedAt, "updated_at": survivor.UpdatedAt,
	}).Error; err != nil {
		return nil, err
	}
	return survivor, nil
}

// loadPlacesById reads places by identifier for a listing of visits.
func (self *transaction) loadPlacesById(placeIds []string) (map[string]*models.Place, error) {
	places := map[string]*models.Place{}
	if len(placeIds) == 0 {
		return places, nil
	}
	var rows []placeModel
	if err := self.tx.Where("\"id\" IN ?", uniqueStrings(placeIds)).Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		places[rows[index].ID] = rows[index].toModel()
	}
	return places, nil
}

func (self *transaction) GetOpenPlaceVisit(userId string) (*models.PlaceVisit, error) {
	var found []placeVisitModel
	if err := self.tx.Where("\"user_id\" = ? AND \"departed_at\" IS NULL", userId).
		Order("\"arrived_at\" DESC NULLS LAST, \"id\" DESC").Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	visit := found[0].toModel()
	places, err := self.loadPlacesById([]string{visit.PlaceID})
	if err != nil {
		return nil, err
	}
	visit.Place = places[visit.PlaceID]
	return visit, nil
}

func (self *transaction) GetOpenPlaceVisits(userIds []string) (map[string]*models.PlaceVisit, error) {
	open := map[string]*models.PlaceVisit{}
	userIds = uniqueStrings(userIds)
	if len(userIds) == 0 {
		return open, nil
	}
	var rows []placeVisitModel
	if err := self.tx.Raw(`SELECT DISTINCT ON (visit.user_id) visit.* FROM "place_visit" visit
		WHERE visit.user_id IN ? AND visit.departed_at IS NULL
		ORDER BY visit.user_id, visit.arrived_at DESC NULLS LAST, visit.id DESC`, userIds).Scan(&rows).Error; err != nil {
		return nil, err
	}
	placeIds := make([]string, 0, len(rows))
	for index := range rows {
		placeIds = append(placeIds, rows[index].PlaceID)
	}
	places, err := self.loadPlacesById(placeIds)
	if err != nil {
		return nil, err
	}
	for index := range rows {
		visit := rows[index].toModel()
		visit.Place = places[visit.PlaceID]
		open[visit.UserID] = visit
	}
	return open, nil
}

func (self *transaction) EndOpenPlaceVisit(userId string, at time.Time) (*models.PlaceVisit, error) {
	var found []placeVisitModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("\"user_id\" = ? AND \"departed_at\" IS NULL", userId).
		Order("\"arrived_at\" DESC NULLS LAST, \"id\" DESC").Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	open := found[0]
	// A departure never comes before the arrival it ends.
	if open.ArrivedAt != nil && at.Before(*open.ArrivedAt) {
		at = *open.ArrivedAt
	}
	now := time.Now()
	if err := self.tx.Model(&placeVisitModel{}).Where("\"id\" = ?", open.ID).
		Updates(map[string]any{"departed_at": at, "updated_at": now}).Error; err != nil {
		return nil, err
	}
	open.DepartedAt, open.UpdatedAt = &at, now
	visit := open.toModel()
	places, err := self.loadPlacesById([]string{visit.PlaceID})
	if err != nil {
		return nil, err
	}
	visit.Place = places[visit.PlaceID]
	return visit, nil
}

func (self *transaction) ListPlaceVisits(userId string, filter PlaceVisitFilter) ([]*models.PlaceVisit, int, error) {
	if filter.Offset < 0 || (filter.PlaceLabel != "" && !filter.PlaceLabel.IsValid()) {
		return nil, 0, ErrInvalidArguments
	}
	limit := pageLimit(filter.LimitCount)
	query := self.tx.Table(`"place_visit" visit`).Select("visit.*").
		Joins(`JOIN "place" place ON place.id = visit.place_id`).
		Where("visit.user_id = ?", userId)
	if filter.PlaceID != "" {
		query = query.Where("visit.place_id = ?", filter.PlaceID)
	}
	if filter.PlaceLabel != "" {
		query = query.Where("place.place_label = ?", string(filter.PlaceLabel))
	}
	if filter.Until != nil {
		query = query.Where("COALESCE(visit.arrived_at, visit.departed_at) < ?", *filter.Until)
	}
	if filter.From != nil {
		// A visit with no departure has not ended, so it reaches any From.
		query = query.Where("(visit.departed_at IS NULL OR visit.departed_at >= ?)", *filter.From)
	}
	var rows []placeVisitModel
	if err := query.Order("COALESCE(visit.arrived_at, visit.departed_at) DESC, visit.id DESC").
		Offset(filter.Offset).Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	nextOffset := 0
	if len(rows) > limit {
		rows = rows[:limit]
		nextOffset = filter.Offset + limit
	}
	placeIds := make([]string, 0, len(rows))
	for index := range rows {
		placeIds = append(placeIds, rows[index].PlaceID)
	}
	places, err := self.loadPlacesById(placeIds)
	if err != nil {
		return nil, 0, err
	}
	visits := make([]*models.PlaceVisit, 0, len(rows))
	for index := range rows {
		visit := rows[index].toModel()
		visit.Place = places[visit.PlaceID]
		visits = append(visits, visit)
	}
	return visits, nextOffset, nil
}

func (self *transaction) EraseLocationHistory(userId string, from, until *time.Time) (int, int, int, error) {
	if userId == "" {
		return 0, 0, 0, ErrInvalidArguments
	}
	if from != nil && until != nil && !from.Before(*until) {
		return 0, 0, 0, fmt.Errorf("%w: the range must start before it ends", ErrInvalidArguments)
	}
	points := self.tx.Where("\"user_id\" = ?", userId)
	visits := self.tx.Where("\"user_id\" = ?", userId)
	if from != nil {
		points = points.Where("\"measured_at\" >= ?", *from)
		visits = visits.Where("COALESCE(\"arrived_at\", \"departed_at\") >= ?", *from)
	}
	if until != nil {
		points = points.Where("\"measured_at\" < ?", *until)
		visits = visits.Where("COALESCE(\"arrived_at\", \"departed_at\") < ?", *until)
	}
	// The journal says where the person was as plainly as the visits do.
	if err := self.deleteLocationDayDocuments(userId, from, until); err != nil {
		return 0, 0, 0, err
	}
	if err := self.forgetJournalLocationCopies(userId, from, until); err != nil {
		return 0, 0, 0, err
	}
	// The gaps in recording are as much a part of where the person was as the
	// visits, so the ones that are over go with them.
	if err := self.deleteEndedLocationPauses(userId, from, until, time.Now()); err != nil {
		return 0, 0, 0, err
	}
	pointResult := points.Delete(&locationPointModel{})
	if pointResult.Error != nil {
		return 0, 0, 0, pointResult.Error
	}
	visitResult := visits.Delete(&placeVisitModel{})
	if visitResult.Error != nil {
		return 0, 0, 0, visitResult.Error
	}
	// A place nobody went to, that the person did not name or label, was only
	// ever a guess from where they were. A report that stores a visit at such a
	// place takes its row lock first, so the candidates are locked before the
	// delete: the delete then waits for that report, and looks again at whether
	// the place has a visit once it has the lock.
	var candidateIds []string
	if err := self.tx.Raw(`SELECT "id" FROM "place"
		WHERE "user_id" = ? AND "is_named_by_person" = false AND "place_label" = ''
		ORDER BY "id" FOR UPDATE`, userId).Scan(&candidateIds).Error; err != nil {
		return 0, 0, 0, err
	}
	placeCount := 0
	if len(candidateIds) > 0 {
		placeResult := self.tx.Where(`"id" IN ? AND NOT EXISTS (SELECT 1 FROM "place_visit" WHERE "place_visit"."place_id" = "place"."id")`, candidateIds).
			Delete(&placeModel{})
		if placeResult.Error != nil {
			return 0, 0, 0, placeResult.Error
		}
		placeCount = int(placeResult.RowsAffected)
	}
	return int(pointResult.RowsAffected), int(visitResult.RowsAffected), placeCount, nil
}

func (self *transaction) ClaimPlacesToName(limitCount int, now time.Time) ([]*models.Place, error) {
	if limitCount <= 0 {
		return nil, nil
	}
	// A few places of each person at most, oldest first, so that one account
	// with thousands of waiting places does not stand in front of everyone
	// else's, and only for people who may still use Location. The rows are
	// locked with SKIP LOCKED, so that two instances at once take different
	// places.
	var candidateIds []string
	if err := self.tx.Raw(`SELECT claimable.id FROM (
			SELECT DISTINCT "user_id" FROM "place" WHERE "named_at" IS NULL AND ("naming_claimed_at" IS NULL OR "naming_claimed_at" < @cutoff)) waiting
		CROSS JOIN LATERAL (
			SELECT place.id, place.created_at FROM "place" place
			WHERE place.user_id = waiting.user_id AND place.named_at IS NULL AND (place.naming_claimed_at IS NULL OR place.naming_claimed_at < @cutoff)
			AND `+userHasPermissionSQL("place.user_id")+`
			ORDER BY place.created_at ASC, place.id ASC
			LIMIT @per_user
			FOR UPDATE SKIP LOCKED) claimable
		ORDER BY claimable.created_at ASC, claimable.id ASC
		LIMIT @limit`,
		sql.Named("cutoff", now.Add(-placeNamingClaimDuration)), sql.Named("limit", limitCount),
		sql.Named("per_user", min(placeNamingPerPersonMost, limitCount)), sql.Named("permission", string(models.PermissionLocationUse))).
		Scan(&candidateIds).Error; err != nil {
		return nil, err
	}
	if len(candidateIds) == 0 {
		return nil, nil
	}
	var rows []placeModel
	if err := self.tx.Raw(`UPDATE "place" SET "naming_claimed_at" = @now WHERE "id" IN @ids RETURNING *`,
		sql.Named("now", now), sql.Named("ids", candidateIds)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	places := make([]*models.Place, 0, len(rows))
	for index := range rows {
		places = append(places, rows[index].toModel())
	}
	// RETURNING has no order, and the job should name the oldest first.
	slices.SortFunc(places, func(first, second *models.Place) int {
		if byTime := first.CreatedAt.Compare(second.CreatedAt); byTime != 0 {
			return byTime
		}
		return strings.Compare(first.ID, second.ID)
	})
	return places, nil
}

func (self *transaction) ClaimPlaceToName(placeId string, now time.Time) (*models.Place, error) {
	var rows []placeModel
	if err := self.tx.Raw(`UPDATE "place" SET "naming_claimed_at" = @now
		WHERE "id" = (
			SELECT "id" FROM "place"
			WHERE "id" = @place AND "named_at" IS NULL AND ("naming_claimed_at" IS NULL OR "naming_claimed_at" < @cutoff)
			AND `+userHasPermissionSQL(`"place"."user_id"`)+`
			FOR UPDATE SKIP LOCKED)
		RETURNING *`,
		sql.Named("now", now), sql.Named("cutoff", now.Add(-placeNamingClaimDuration)), sql.Named("place", placeId),
		sql.Named("permission", string(models.PermissionLocationUse))).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].toModel(), nil
}

func (self *transaction) SetPlaceNamed(placeId string, claimedAt *time.Time, found *NamedPlace, errorMessage string, now time.Time) error {
	var locked placeModel
	if err := lockRow(self.tx, &locked, placeId); err != nil {
		return err
	}
	// A place moved while the provider was asked about it lost its claim, and
	// the answer is about where it used to be.
	if claimedAt != nil && (locked.NamingClaimedAt == nil || !locked.NamingClaimedAt.Equal(*claimedAt)) {
		return nil
	}
	updates := map[string]any{"updated_at": now}
	if errorMessage != "" {
		updates["naming_error_message"] = cleanPlaceText(errorMessage, placeNamingErrorMaximumLength)
	} else {
		updates["named_at"] = now
		updates["naming_error_message"] = ""
		if found != nil {
			if found.OSMType != "" && found.OSMType != models.OSMTypeNode && found.OSMType != models.OSMTypeWay && found.OSMType != models.OSMTypeRelation {
				return fmt.Errorf("%w: %q is not an OpenStreetMap type", ErrInvalidArguments, found.OSMType)
			}
			updates["osm_type"] = found.OSMType
			foundName := cleanPlaceText(found.PlaceName, placeNameMaximumLength)
			updates["osm_name"] = foundName
			updates["osm_category"] = cleanPlaceText(found.OSMCategory, placeCategoryMaximumLength)
			if !locked.IsAddressTyped {
				updates["address_text"] = cleanPlaceText(found.AddressText, placeAddressMaximumLength)
			}
			if found.OSMID != 0 {
				updates["osm_id"] = found.OSMID
			} else {
				updates["osm_id"] = nil
			}
			if !locked.IsNamedByPerson && foundName != "" {
				updates["place_name"] = foundName
			}
		}
	}
	return self.tx.Model(&placeModel{}).Where("\"id\" = ?", placeId).Updates(updates).Error
}

func (self *transaction) ReleasePlaceClaim(placeId string, claimedAt *time.Time, errorMessage string, now time.Time) error {
	var locked placeModel
	if err := lockRow(self.tx, &locked, placeId); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if claimedAt != nil && (locked.NamingClaimedAt == nil || !locked.NamingClaimedAt.Equal(*claimedAt)) {
		return nil
	}
	return self.tx.Model(&placeModel{}).Where("\"id\" = ?", placeId).Updates(map[string]any{
		"naming_claimed_at":    nil,
		"naming_error_message": cleanPlaceText(errorMessage, placeNamingErrorMaximumLength),
		"updated_at":           now,
	}).Error
}

func (self *transaction) ListLocationPoints(userId string, from, until time.Time, limitCount int) ([]*models.LocationPoint, error) {
	if limitCount <= 0 || !from.Before(until) {
		return nil, ErrInvalidArguments
	}
	var rows []locationPointModel
	if err := self.tx.Where("\"user_id\" = ? AND \"measured_at\" >= ? AND \"measured_at\" < ?", userId, from, until).
		Order("\"measured_at\" ASC, \"id\" ASC").Limit(limitCount).Find(&rows).Error; err != nil {
		return nil, err
	}
	points := make([]*models.LocationPoint, 0, len(rows))
	for index := range rows {
		points = append(points, rows[index].toModel())
	}
	return points, nil
}

func (self *transaction) ListPlacesNear(userId string, latitudeDegrees, longitudeDegrees, radiusMeters float64, limitCount int) ([]*PlaceDistance, error) {
	if !isOnEarth(latitudeDegrees, longitudeDegrees) || radiusMeters < 0 || math.IsNaN(radiusMeters) || limitCount <= 0 {
		return nil, ErrInvalidArguments
	}
	// The latitude band keeps the exact distance from being worked out for
	// every place the person has, as in FindPlaceAt: a place reaches no
	// further than the larger of the radius asked for and its own radius.
	bandDegrees := (math.Max(radiusMeters, models.PlaceRadiusMaximumMeters))/111000.0 + 0.001
	var rows []struct {
		Place          placeModel `gorm:"embedded"`
		DistanceMeters float64    `gorm:"column:distance_meters"`
	}
	if err := self.tx.Raw(`SELECT place.*, `+placeDistanceSQL+` AS distance_meters FROM "place" place
		WHERE place.user_id = @user
		  AND place.latitude_degrees BETWEEN @south AND @north
		  AND `+placeDistanceSQL+` <= GREATEST(@radius, place.radius_meters)
		ORDER BY distance_meters ASC, place.created_at ASC, place.id ASC
		LIMIT @limit`,
		sql.Named("user", userId), sql.Named("latitude", latitudeDegrees), sql.Named("longitude", longitudeDegrees),
		sql.Named("south", latitudeDegrees-bandDegrees), sql.Named("north", latitudeDegrees+bandDegrees),
		sql.Named("radius", radiusMeters), sql.Named("limit", limitCount)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	near := make([]*PlaceDistance, 0, len(rows))
	for index := range rows {
		near = append(near, &PlaceDistance{Place: rows[index].Place.toModel(), DistanceMeters: rows[index].DistanceMeters})
	}
	return near, nil
}

func (self *transaction) FindPlaceIDsByOSM(userId string, references []OSMReference) (map[OSMReference]string, error) {
	found := map[OSMReference]string{}
	if len(references) == 0 {
		return found, nil
	}
	conditions := make([]string, 0, len(references))
	arguments := []any{userId}
	for _, reference := range references {
		if reference.OSMType == "" || reference.OSMID == 0 {
			continue
		}
		conditions = append(conditions, `("osm_type" = ? AND "osm_id" = ?)`)
		arguments = append(arguments, reference.OSMType, reference.OSMID)
	}
	if len(conditions) == 0 {
		return found, nil
	}
	var rows []placeModel
	if err := self.tx.Where(`"user_id" = ? AND (`+strings.Join(conditions, " OR ")+`)`, arguments...).
		Order(`"created_at" DESC, "id" DESC`).Find(&rows).Error; err != nil {
		return nil, err
	}
	// Newest first, so that the oldest place for an object is the one left in
	// the map when a person has two.
	for index := range rows {
		if rows[index].OSMID != nil {
			found[OSMReference{OSMType: rows[index].OSMType, OSMID: *rows[index].OSMID}] = rows[index].ID
		}
	}
	return found, nil
}

func (self *transaction) SummarizePlaceVisits(userId string, placeIds []string) (map[string]*PlaceVisitSummary, error) {
	summaries := map[string]*PlaceVisitSummary{}
	if len(placeIds) == 0 {
		return summaries, nil
	}
	var rows []struct {
		PlaceID     string     `gorm:"column:place_id"`
		VisitCount  int        `gorm:"column:visit_count"`
		LastVisitAt *time.Time `gorm:"column:last_visit_at"`
	}
	if err := self.tx.Raw(`SELECT place_id, count(*) AS visit_count, max(GREATEST(arrived_at, departed_at)) AS last_visit_at
		FROM "place_visit" WHERE user_id = ? AND place_id IN ? GROUP BY place_id`, userId, uniqueStrings(placeIds)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		summaries[rows[index].PlaceID] = &PlaceVisitSummary{VisitCount: rows[index].VisitCount, LastVisitAt: localTime(rows[index].LastVisitAt)}
	}
	return summaries, nil
}

func (self *transaction) GetPlaceVisitByClientKey(userId, clientVisitKey string) (*models.PlaceVisit, error) {
	found, err := self.findPlaceVisitByKey(userId, clientVisitKey, false)
	if err != nil || found == nil {
		return nil, err
	}
	visit := found.toModel()
	places, err := self.loadPlacesById([]string{visit.PlaceID})
	if err != nil {
		return nil, err
	}
	visit.Place = places[visit.PlaceID]
	return visit, nil
}

func (self *transaction) GetPlaceVisit(userId, visitId string) (*models.PlaceVisit, error) {
	if userId == "" || visitId == "" {
		return nil, nil
	}
	var rows []placeVisitModel
	if err := self.tx.Where(`"user_id" = ? AND "id" = ?`, userId, visitId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	visit := rows[0].toModel()
	places, err := self.loadPlacesById([]string{visit.PlaceID})
	if err != nil {
		return nil, err
	}
	visit.Place = places[visit.PlaceID]
	return visit, nil
}
