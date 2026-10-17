package db

import (
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// LocationOperation is where a person has been: the positions their phone or
// browser reported, the places those are at and the stays at them. All of it
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

	// ListLocationPoints reads the positions measured in a range, oldest
	// first, at most limitCount of them. A caller that must refuse a range
	// holding more asks for one more than it will take.
	ListLocationPoints(userId string, from, until time.Time, limitCount int) ([]*models.LocationPoint, error)

	// FindPlaceAt is the person's place whose radius holds the point, the
	// nearest centre first, or nil when there is none.
	FindPlaceAt(userId string, latitudeDegrees, longitudeDegrees float64) (*models.Place, error)

	// ListPlacesNear is the person's places that hold the point or whose
	// centre is within radiusMeters of it, nearest first, at most limitCount.
	// Each carries its distance.
	ListPlacesNear(userId string, latitudeDegrees, longitudeDegrees, radiusMeters float64, limitCount int) ([]*PlaceDistance, error)

	// FindPlaceIDsByOSM maps the OpenStreetMap objects the person already has
	// a place for to that place. A place with no OpenStreetMap object is never
	// in the answer.
	FindPlaceIDsByOSM(userId string, references []OSMReference) (map[OSMReference]string, error)

	// SummarizePlaceVisits counts the stays at each of the places and says
	// when the last one was, for the places that have any.
	SummarizePlaceVisits(userId string, placeIds []string) (map[string]*PlaceVisitSummary, error)

	// CreatePlace makes a place for the person. ErrInvalidArguments for a
	// position off the earth, a radius outside what a person can set (zero
	// takes the default), or a label or OpenStreetMap type nobody knows.
	CreatePlace(userId string, place *models.Place) (*models.Place, error)

	// SavePlace changes a place and answers it as it is now. ErrNotFound
	// unless it is the person's, ErrInvalidArguments for a change that is not
	// allowed.
	SavePlace(userId, placeId string, changes PlaceChanges) (*models.Place, error)

	// GetPlace is one of the person's places, or ErrNotFound.
	GetPlace(userId, placeId string) (*models.Place, error)

	// ForgetPlace deletes a place and every stay at it. ErrNotFound unless it
	// is the person's.
	ForgetPlace(userId, placeId string) error

	// ListPlaces pages the person's places, each with its number of visits and
	// the time of the last one: labeled places first, then the ones the
	// person named, then the rest, each group by last visit. nextOffset is
	// the offset that reads on, and zero when this page is the last.
	ListPlaces(userId string, filter PlaceFilter) (places []*models.Place, nextOffset int, err error)

	// PutPlaceVisit stores a stay, or completes the one stored under the same
	// client_visit_key: a later report fills in the departure, and the
	// arrival if it was missing. Stays at one place less than ten minutes
	// apart are then joined into one, so that iOS splitting a stay in two
	// does not show as two. isNew is whether the key was unknown. The visit
	// passed in is updated to what is stored. ErrNotFound unless the place is
	// the person's, ErrInvalidArguments for a stay with neither time or that
	// ends before it starts.
	PutPlaceVisit(userId string, visit *models.PlaceVisit) (isNew bool, err error)

	// GetPlaceVisitByClientKey is the stay the phone calls by this key, with
	// its place, or nil when there is none.
	GetPlaceVisitByClientKey(userId, clientVisitKey string) (*models.PlaceVisit, error)

	// GetOpenPlaceVisit is the stay in progress, the latest one with no
	// departure, with its place; nil when there is none.
	GetOpenPlaceVisit(userId string) (*models.PlaceVisit, error)

	// EndOpenPlaceVisit sets the departure of the stay in progress and answers
	// it, or answers nil when there is none.
	EndOpenPlaceVisit(userId string, at time.Time) (*models.PlaceVisit, error)

	// ListPlaceVisits pages stays newest first, each with its place.
	// nextOffset is as in ListPlaces.
	ListPlaceVisits(userId string, filter PlaceVisitFilter) (visits []*models.PlaceVisit, nextOffset int, err error)

	// ForgetLocationHistory deletes positions and stays that started in the
	// range (all of them when both ends are nil), and then the places left
	// with no stay that the person neither named nor labeled.
	ForgetLocationHistory(userId string, from, until *time.Time) (pointCount, visitCount, placeCount int, err error)

	// ClaimPlacesToName takes up to limitCount places that wait for a name and
	// that no instance has taken in the last ten minutes, oldest first, and
	// marks them taken, so that two instances do not ask a provider about the
	// same place.
	ClaimPlacesToName(limitCount int, now time.Time) ([]*models.Place, error)

	// SetPlaceNamed records what a provider answered for a place. With an
	// errorMessage nothing else changes and the place stays unnamed, to be
	// claimed again once its claim is old. Without one, found is what the
	// provider knows about the spot, or nil when it knows nothing, and either
	// way the place is named from now on. A name the person gave is never
	// replaced. ErrNotFound when there is no such place.
	SetPlaceNamed(placeId string, found *NamedPlace, errorMessage string, now time.Time) error
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

// PlaceVisitFilter narrows and pages a listing of stays.
type PlaceVisitFilter struct {
	// From and Until keep stays that overlap the range: those that ended at
	// or after From and began before Until. A stay still in progress has not
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

// PlaceDistance is one of the person's places and how far its centre is from
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

// NamedPlace is what a provider knows about a spot, for SetPlaceNamed.
type NamedPlace struct {
	PlaceName   string
	OSMType     string
	OSMID       int64
	OSMCategory string
	AddressText string
}

const (
	// placeListDefaultLimit and placeListMaximumLimit bound a page of places
	// or of stays when the caller names no limit or too large a one.
	placeListDefaultLimit = 50
	placeListMaximumLimit = 200

	// placeVisitJoinGap is how close two stays at one place must be to be one:
	// iOS sometimes reports a stay as two, with a few minutes between.
	placeVisitJoinGap = 10 * time.Minute

	// placeNamingClaimDuration is how long a place taken for naming is left
	// alone before another instance may take it.
	placeNamingClaimDuration = 10 * time.Minute

	// locationPointBatchSize is how many positions one INSERT carries.
	locationPointBatchSize = 200

	placeNameMaximumLength = 256
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

// pageLimit is the size of a page the caller asked for, kept to what a page
// may be.
func pageLimit(limitCount int) int {
	if limitCount <= 0 {
		return placeListDefaultLimit
	}
	return min(limitCount, placeListMaximumLimit)
}

// placeDistanceSQL is the great circle distance in meters between a place's
// centre and the point @latitude, @longitude: the haversine formula, with the
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
	now := time.Now()
	row := &placeModel{
		ID:                 newID(),
		UserID:             userId,
		PlaceName:          strings.TrimSpace(place.PlaceName),
		IsNamedByPerson:    place.IsNamedByPerson,
		PlaceLabel:         string(place.PlaceLabel),
		LatitudeDegrees:    place.LatitudeDegrees,
		LongitudeDegrees:   place.LongitudeDegrees,
		RadiusMeters:       radiusMeters,
		OSMType:            place.OSMType,
		OSMName:            place.OSMName,
		OSMCategory:        place.OSMCategory,
		AddressText:        place.AddressText,
		NamedAt:            place.NamedAt,
		NamingErrorMessage: place.NamingErrorMessage,
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
		name := strings.TrimSpace(*changes.PlaceName)
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
	}
	if changes.RadiusMeters != nil {
		updates["radius_meters"] = *changes.RadiusMeters
	}
	if err := self.tx.Model(&placeModel{}).Where("\"id\" = ?", placeId).Updates(updates).Error; err != nil {
		return nil, err
	}
	return self.GetPlace(userId, placeId)
}

func (self *transaction) ForgetPlace(userId, placeId string) error {
	result := self.tx.Where("\"id\" = ? AND \"user_id\" = ?", placeId, userId).Delete(&placeModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
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

// visitStart is when a stay began as far as is known: its arrival, or its
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

// visitEnd is when a stay ended as far as is known: its departure, or, while
// the person is still there, the moment it began, since nothing later has been
// reported.
func visitEnd(visit *placeVisitModel) time.Time {
	if visit.DepartedAt != nil {
		return *visit.DepartedAt
	}
	return visitStart(visit)
}

func (self *transaction) PutPlaceVisit(userId string, visit *models.PlaceVisit) (bool, error) {
	if userId == "" || visit == nil || visit.PlaceID == "" || !visit.VisitSource.IsValid() ||
		(visit.ArrivedAt == nil && visit.DepartedAt == nil) ||
		(visit.ArrivedAt != nil && visit.DepartedAt != nil && visit.DepartedAt.Before(*visit.ArrivedAt)) ||
		!isOnEarth(visit.LatitudeDegrees, visit.LongitudeDegrees) || visit.AccuracyMeters < 0 {
		return false, fmt.Errorf("%w: a stay needs a place, a source, coordinates on the earth and an arrival or a departure, and cannot end before it starts", ErrInvalidArguments)
	}
	// The place is locked, so that two reports of stays at it, which would
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
	var existing []placeVisitModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("\"user_id\" = ? AND \"client_visit_key\" = ?", userId, key).Limit(1).Find(&existing).Error; err != nil {
		return false, err
	}
	isNew := len(existing) == 0
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
	} else {
		// A later report of the same stay completes it. The place, the
		// position and the source are what the first report said.
		kept = existing[0]
		if visit.DepartedAt != nil {
			kept.DepartedAt = visit.DepartedAt
		}
		if kept.ArrivedAt == nil && visit.ArrivedAt != nil {
			kept.ArrivedAt = visit.ArrivedAt
		}
		if kept.ArrivedAt != nil && kept.DepartedAt != nil && kept.DepartedAt.Before(*kept.ArrivedAt) {
			return false, fmt.Errorf("%w: a stay cannot end before it starts", ErrInvalidArguments)
		}
		kept.UpdatedAt = now
		if err := self.tx.Model(&placeVisitModel{}).Where("\"id\" = ?", kept.ID).Updates(map[string]any{
			"arrived_at": kept.ArrivedAt, "departed_at": kept.DepartedAt, "updated_at": now,
		}).Error; err != nil {
			return false, err
		}
	}

	joined, err := self.joinPlaceVisits(&kept)
	if err != nil {
		return false, err
	}
	*visit = *joined.toModel()
	return isNew, nil
}

// joinPlaceVisits merges the stay with every other stay at its place that
// ends less than ten minutes before it starts, starts less than ten minutes
// after it ends, or overlaps it, and answers the one that remains. The one
// that began first stays, with the latest departure of them all, or none if
// the latest of them is still in progress; the others are deleted.
func (self *transaction) joinPlaceVisits(visit *placeVisitModel) (*placeVisitModel, error) {
	var group []placeVisitModel
	if err := self.tx.Where("\"user_id\" = ? AND \"place_id\" = ?", visit.UserID, visit.PlaceID).
		Order("COALESCE(\"arrived_at\", \"departed_at\") ASC, \"id\" ASC").Find(&group).Error; err != nil {
		return nil, err
	}
	// Walk the stays in the order they began, growing a run: each stay joins
	// the run if it begins before the run's end plus the gap, and starts a new
	// run if not. The run that holds this stay is the one to merge.
	var chosen []placeVisitModel
	runStart := 0
	runEnd := visitEnd(&group[0])
	for index := 1; index <= len(group); index++ {
		if index < len(group) && visitStart(&group[index]).Before(runEnd.Add(placeVisitJoinGap)) {
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
		if visitEnd(&chosen[index]).After(visitEnd(latest)) {
			latest = &chosen[index]
		}
	}
	survivor.DepartedAt = latest.DepartedAt
	// A stay iOS saw only the end of takes the arrival of the one that did.
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

// loadPlacesById reads places by identifier for a listing of stays.
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
		// A stay with no departure has not ended, so it reaches any From.
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

func (self *transaction) ForgetLocationHistory(userId string, from, until *time.Time) (int, int, int, error) {
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
	pointResult := points.Delete(&locationPointModel{})
	if pointResult.Error != nil {
		return 0, 0, 0, pointResult.Error
	}
	visitResult := visits.Delete(&placeVisitModel{})
	if visitResult.Error != nil {
		return 0, 0, 0, visitResult.Error
	}
	// A place nobody went to, that the person did not name or label, was only
	// ever a guess from where they were.
	placeResult := self.tx.Where(`"user_id" = ? AND "is_named_by_person" = false AND "place_label" = ''
		AND NOT EXISTS (SELECT 1 FROM "place_visit" WHERE "place_visit"."place_id" = "place"."id")`, userId).
		Delete(&placeModel{})
	if placeResult.Error != nil {
		return 0, 0, 0, placeResult.Error
	}
	return int(pointResult.RowsAffected), int(visitResult.RowsAffected), int(placeResult.RowsAffected), nil
}

func (self *transaction) ClaimPlacesToName(limitCount int, now time.Time) ([]*models.Place, error) {
	if limitCount <= 0 {
		return nil, nil
	}
	var rows []placeModel
	if err := self.tx.Raw(`UPDATE "place" SET "naming_claimed_at" = @now
		WHERE "id" IN (
			SELECT "id" FROM "place"
			WHERE "named_at" IS NULL AND ("naming_claimed_at" IS NULL OR "naming_claimed_at" < @cutoff)
			ORDER BY "created_at" ASC, "id" ASC
			LIMIT @limit
			FOR UPDATE SKIP LOCKED)
		RETURNING *`,
		sql.Named("now", now), sql.Named("cutoff", now.Add(-placeNamingClaimDuration)), sql.Named("limit", limitCount)).
		Scan(&rows).Error; err != nil {
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

func (self *transaction) SetPlaceNamed(placeId string, found *NamedPlace, errorMessage string, now time.Time) error {
	var locked placeModel
	if err := lockRow(self.tx, &locked, placeId); err != nil {
		return err
	}
	updates := map[string]any{"updated_at": now}
	if errorMessage != "" {
		updates["naming_error_message"] = truncateRunes(errorMessage, 512)
	} else {
		updates["named_at"] = now
		updates["naming_error_message"] = ""
		if found != nil {
			if found.OSMType != "" && found.OSMType != models.OSMTypeNode && found.OSMType != models.OSMTypeWay && found.OSMType != models.OSMTypeRelation {
				return fmt.Errorf("%w: %q is not an OpenStreetMap type", ErrInvalidArguments, found.OSMType)
			}
			updates["osm_type"] = found.OSMType
			updates["osm_name"] = truncateRunes(found.PlaceName, placeNameMaximumLength)
			updates["osm_category"] = truncateRunes(found.OSMCategory, 128)
			updates["address_text"] = truncateRunes(found.AddressText, 512)
			if found.OSMID != 0 {
				updates["osm_id"] = found.OSMID
			} else {
				updates["osm_id"] = nil
			}
			if !locked.IsNamedByPerson && found.PlaceName != "" {
				updates["place_name"] = truncateRunes(found.PlaceName, placeNameMaximumLength)
			}
		}
	}
	return self.tx.Model(&placeModel{}).Where("\"id\" = ?", placeId).Updates(updates).Error
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
	var found []placeVisitModel
	if err := self.tx.Where("\"user_id\" = ? AND \"client_visit_key\" = ?", userId, clientVisitKey).Limit(1).Find(&found).Error; err != nil {
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
