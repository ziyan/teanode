package client

import (
	"context"
	"time"
)

// Where a person is and has been: the operations behind `teanode location`.
// Reporting is left out, because only a phone reports.

// LocationPoint is one position a phone or browser reported.
type LocationPoint struct {
	LatitudeDegrees      float64   `json:"latitudeDegrees"`
	LongitudeDegrees     float64   `json:"longitudeDegrees"`
	AccuracyMeters       float64   `json:"accuracyMeters"`
	AltitudeMeters       *float64  `json:"altitudeMeters"`
	SpeedMetersPerSecond *float64  `json:"speedMetersPerSecond"`
	CourseDegrees        *float64  `json:"courseDegrees"`
	MeasuredAt           time.Time `json:"measuredAt"`
	LocationSource       string    `json:"locationSource"`
}

// Place is a spot with a name and a radius, belonging to one person.
type Place struct {
	ID                 string     `json:"id"`
	UserID             string     `json:"userId"`
	PlaceName          string     `json:"placeName"`
	IsNamedByPerson    bool       `json:"isNamedByPerson"`
	PlaceLabel         string     `json:"placeLabel"`
	LatitudeDegrees    float64    `json:"latitudeDegrees"`
	LongitudeDegrees   float64    `json:"longitudeDegrees"`
	RadiusMeters       float64    `json:"radiusMeters"`
	OSMType            string     `json:"osmType"`
	OSMID              *int64     `json:"osmId"`
	OSMName            string     `json:"osmName"`
	OSMCategory        string     `json:"osmCategory"`
	AddressText        string     `json:"addressText"`
	VisitCount         int        `json:"visitCount"`
	LastVisitAt        *time.Time `json:"lastVisitAt"`
	NamingErrorMessage string     `json:"namingErrorMessage"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

// PlaceVisit is one visit at a place.
type PlaceVisit struct {
	ID               string     `json:"id"`
	Place            *Place     `json:"place"`
	ArrivedAt        *time.Time `json:"arrivedAt"`
	DepartedAt       *time.Time `json:"departedAt"`
	DurationSeconds  *int       `json:"durationSeconds"`
	LatitudeDegrees  float64    `json:"latitudeDegrees"`
	LongitudeDegrees float64    `json:"longitudeDegrees"`
	AccuracyMeters   float64    `json:"accuracyMeters"`
	VisitSource      string     `json:"visitSource"`
}

// Location is where a person is.
type Location struct {
	UserID      string         `json:"userId"`
	Username    string         `json:"username"`
	Name        string         `json:"name"`
	ShareLevel  string         `json:"shareLevel"`
	LatestPoint *LocationPoint `json:"latestPoint"`
	OpenVisit   *PlaceVisit    `json:"openVisit"`
}

// PlaceVisitPage is a page of visits; NextOffset is null on the last.
type PlaceVisitPage struct {
	PlaceVisits []*PlaceVisit `json:"placeVisits"`
	NextOffset  *int          `json:"nextOffset"`
}

// PlacePage is a page of places; NextOffset is null on the last.
type PlacePage struct {
	Places     []*Place `json:"places"`
	NextOffset *int     `json:"nextOffset"`
}

// FoundPlace is a place on the map, found by OpenStreetMap or one of the
// caller's own that is near; PlaceID is the caller's own place there, if any.
type FoundPlace struct {
	PlaceName        string  `json:"placeName"`
	OSMType          string  `json:"osmType"`
	OSMID            int64   `json:"osmId"`
	OSMCategory      string  `json:"osmCategory"`
	AddressText      string  `json:"addressText"`
	LatitudeDegrees  float64 `json:"latitudeDegrees"`
	LongitudeDegrees float64 `json:"longitudeDegrees"`
	DistanceMeters   float64 `json:"distanceMeters"`
	PlaceID          *string `json:"placeId"`
}

// FoundPlacePage is a page of found places; NextOffset is null on the last.
type FoundPlacePage struct {
	FoundPlaces []*FoundPlace `json:"foundPlaces"`
	NextOffset  *int          `json:"nextOffset"`
}

// LocationHistoryErased says what erasing history deleted.
type LocationHistoryErased struct {
	PointCount int `json:"pointCount"`
	VisitCount int `json:"visitCount"`
	PlaceCount int `json:"placeCount"`
}

const (
	locationPointFields = `latitudeDegrees longitudeDegrees accuracyMeters altitudeMeters speedMetersPerSecond courseDegrees measuredAt locationSource`

	placeFields = `id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters
    osmType osmId osmName osmCategory addressText visitCount lastVisitAt namingErrorMessage createdAt updatedAt`

	placeVisitFields = `id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
    place { ` + placeFields + ` }`

	foundPlaceFields = `placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId`

	DocumentGetLocation = `query ($userId: String) {
  GetLocation(userId: $userId) {
    userId username name shareLevel
    latestPoint { ` + locationPointFields + ` }
    openVisit { ` + placeVisitFields + ` }
  }
}`

	DocumentListLocationVisits = `query ($userId: String, $from: DateTime, $until: DateTime, $placeId: String, $placeLabel: String, $offset: Int, $limitCount: Int) {
  ListLocationVisits(userId: $userId, from: $from, until: $until, placeId: $placeId, placeLabel: $placeLabel, offset: $offset, limitCount: $limitCount) {
    nextOffset
    placeVisits { ` + placeVisitFields + ` }
  }
}`

	DocumentListPlaces = `query ($userId: String, $placeLabel: String, $query: String, $offset: Int, $limitCount: Int) {
  ListPlaces(userId: $userId, placeLabel: $placeLabel, query: $query, offset: $offset, limitCount: $limitCount) {
    nextOffset
    places { ` + placeFields + ` }
  }
}`

	DocumentListNearbyPlaces = `query ($latitudeDegrees: Float!, $longitudeDegrees: Float!, $radiusMeters: Float) {
  ListNearbyPlaces(latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, radiusMeters: $radiusMeters) {
    ` + foundPlaceFields + `
  }
}`

	DocumentSearchPlaces = `query ($query: String!, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeId: String, $radiusMeters: Float, $offset: Int, $limitCount: Int) {
  SearchPlaces(query: $query, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeId: $placeId, radiusMeters: $radiusMeters, offset: $offset, limitCount: $limitCount) {
    nextOffset
    foundPlaces { ` + foundPlaceFields + ` }
  }
}`

	DocumentListLocationPoints = `query ($userId: String, $from: DateTime!, $until: DateTime!) {
  ListLocationPoints(userId: $userId, from: $from, until: $until) { ` + locationPointFields + ` }
}`

	DocumentSavePlace = `mutation ($placeId: String, $placeName: String, $placeLabel: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $radiusMeters: Float) {
  SavePlace(placeId: $placeId, placeName: $placeName, placeLabel: $placeLabel, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, radiusMeters: $radiusMeters) {
    ` + placeFields + `
  }
}`

	DocumentForgetPlace = `mutation ($placeId: String!) { ForgetPlace(placeId: $placeId) }`

	DocumentCheckIn = `mutation ($placeId: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeName: String, $osmType: String, $osmId: Int64, $osmCategory: String, $addressText: String) {
  CheckIn(placeId: $placeId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeName: $placeName, osmType: $osmType, osmId: $osmId, osmCategory: $osmCategory, addressText: $addressText) {
    ` + placeVisitFields + `
  }
}`

	DocumentEraseLocationHistory = `mutation ($from: DateTime, $until: DateTime) {
  EraseLocationHistory(from: $from, until: $until) { pointCount visitCount placeCount }
}`
)

// GetLocation is where the caller is now.
func GetLocation(ctx context.Context, connection *Client) (*Location, error) {
	return GetLocationOf(ctx, connection, "")
}

// GetLocationOf is where a person is, the caller or somebody who shares their
// location with them, by account identifier.
func GetLocationOf(ctx context.Context, connection *Client, userId string) (*Location, error) {
	variables := map[string]any{}
	if userId != "" {
		variables["userId"] = userId
	}
	var result struct {
		GetLocation *Location `json:"GetLocation"`
	}
	if err := connection.Execute(ctx, DocumentGetLocation, variables, &result); err != nil {
		return nil, err
	}
	return result.GetLocation, nil
}

// LocationVisitsQuery narrows and pages a listing of visits. A zero field is
// left out.
type LocationVisitsQuery struct {
	UserID      string
	From, Until *time.Time
	PlaceID     string
	PlaceLabel  string
	Offset      int
	LimitCount  int
}

// ListLocationVisits is a page of the caller's visits, newest first.
func ListLocationVisits(ctx context.Context, connection *Client, query LocationVisitsQuery) (*PlaceVisitPage, error) {
	variables := map[string]any{}
	if query.UserID != "" {
		variables["userId"] = query.UserID
	}
	if query.From != nil {
		variables["from"] = query.From.UTC().Format(time.RFC3339)
	}
	if query.Until != nil {
		variables["until"] = query.Until.UTC().Format(time.RFC3339)
	}
	if query.PlaceID != "" {
		variables["placeId"] = query.PlaceID
	}
	if query.PlaceLabel != "" {
		variables["placeLabel"] = query.PlaceLabel
	}
	if query.Offset > 0 {
		variables["offset"] = query.Offset
	}
	if query.LimitCount > 0 {
		variables["limitCount"] = query.LimitCount
	}
	var result struct {
		ListLocationVisits *PlaceVisitPage `json:"ListLocationVisits"`
	}
	if err := connection.Execute(ctx, DocumentListLocationVisits, variables, &result); err != nil {
		return nil, err
	}
	return result.ListLocationVisits, nil
}

// PlacesQuery narrows and pages a listing of places. A zero field is left out.
type PlacesQuery struct {
	UserID     string
	PlaceLabel string
	Query      string
	Offset     int
	LimitCount int
}

// ListPlaces is a page of the caller's places.
func ListPlaces(ctx context.Context, connection *Client, query PlacesQuery) (*PlacePage, error) {
	variables := map[string]any{}
	if query.UserID != "" {
		variables["userId"] = query.UserID
	}
	if query.PlaceLabel != "" {
		variables["placeLabel"] = query.PlaceLabel
	}
	if query.Query != "" {
		variables["query"] = query.Query
	}
	if query.Offset > 0 {
		variables["offset"] = query.Offset
	}
	if query.LimitCount > 0 {
		variables["limitCount"] = query.LimitCount
	}
	var result struct {
		ListPlaces *PlacePage `json:"ListPlaces"`
	}
	if err := connection.Execute(ctx, DocumentListPlaces, variables, &result); err != nil {
		return nil, err
	}
	return result.ListPlaces, nil
}

// ListNearbyPlaces is what is at and around a point; a nil radius is the
// server's default.
func ListNearbyPlaces(ctx context.Context, connection *Client, latitudeDegrees, longitudeDegrees float64, radiusMeters *float64) ([]*FoundPlace, error) {
	variables := map[string]any{"latitudeDegrees": latitudeDegrees, "longitudeDegrees": longitudeDegrees}
	if radiusMeters != nil {
		variables["radiusMeters"] = *radiusMeters
	}
	var result struct {
		ListNearbyPlaces []*FoundPlace `json:"ListNearbyPlaces"`
	}
	if err := connection.Execute(ctx, DocumentListNearbyPlaces, variables, &result); err != nil {
		return nil, err
	}
	return result.ListNearbyPlaces, nil
}

// PlaceSearch is a search for places by words. Near is a point, a place, or
// neither, which is where the caller last was.
type PlaceSearch struct {
	Query            string
	LatitudeDegrees  *float64
	LongitudeDegrees *float64
	PlaceID          string
	RadiusMeters     *float64
	Offset           int
	LimitCount       int
}

// SearchPlaces is a page of places matching words, nearest first.
func SearchPlaces(ctx context.Context, connection *Client, search PlaceSearch) (*FoundPlacePage, error) {
	variables := map[string]any{"query": search.Query}
	if search.LatitudeDegrees != nil {
		variables["latitudeDegrees"] = *search.LatitudeDegrees
	}
	if search.LongitudeDegrees != nil {
		variables["longitudeDegrees"] = *search.LongitudeDegrees
	}
	if search.PlaceID != "" {
		variables["placeId"] = search.PlaceID
	}
	if search.RadiusMeters != nil {
		variables["radiusMeters"] = *search.RadiusMeters
	}
	if search.Offset > 0 {
		variables["offset"] = search.Offset
	}
	if search.LimitCount > 0 {
		variables["limitCount"] = search.LimitCount
	}
	var result struct {
		SearchPlaces *FoundPlacePage `json:"SearchPlaces"`
	}
	if err := connection.Execute(ctx, DocumentSearchPlaces, variables, &result); err != nil {
		return nil, err
	}
	return result.SearchPlaces, nil
}

// ListLocationPoints is the positions of up to two days, oldest first, of the
// caller or, with a userId, of somebody who shares their history with them.
func ListLocationPoints(ctx context.Context, connection *Client, userId string, from, until time.Time) ([]*LocationPoint, error) {
	var result struct {
		ListLocationPoints []*LocationPoint `json:"ListLocationPoints"`
	}
	variables := map[string]any{"from": from.UTC().Format(time.RFC3339), "until": until.UTC().Format(time.RFC3339)}
	if userId != "" {
		variables["userId"] = userId
	}
	if err := connection.Execute(ctx, DocumentListLocationPoints, variables, &result); err != nil {
		return nil, err
	}
	return result.ListLocationPoints, nil
}

// PlaceChange is what to change about a place, or what to make a new one
// from. A nil field is left as it is, and a pointer to an empty name or label
// is the empty one.
type PlaceChange struct {
	PlaceID          string
	PlaceName        *string
	PlaceLabel       *string
	LatitudeDegrees  *float64
	LongitudeDegrees *float64
	RadiusMeters     *float64
}

// SavePlace names, labels, moves or resizes a place, or makes one when there
// is no PlaceID.
func SavePlace(ctx context.Context, connection *Client, change PlaceChange) (*Place, error) {
	variables := map[string]any{}
	if change.PlaceID != "" {
		variables["placeId"] = change.PlaceID
	}
	if change.PlaceName != nil {
		variables["placeName"] = *change.PlaceName
	}
	if change.PlaceLabel != nil {
		variables["placeLabel"] = *change.PlaceLabel
	}
	if change.LatitudeDegrees != nil {
		variables["latitudeDegrees"] = *change.LatitudeDegrees
	}
	if change.LongitudeDegrees != nil {
		variables["longitudeDegrees"] = *change.LongitudeDegrees
	}
	if change.RadiusMeters != nil {
		variables["radiusMeters"] = *change.RadiusMeters
	}
	var result struct {
		SavePlace *Place `json:"SavePlace"`
	}
	if err := connection.Execute(ctx, DocumentSavePlace, variables, &result); err != nil {
		return nil, err
	}
	return result.SavePlace, nil
}

// ForgetPlace deletes a place and every visit at it.
func ForgetPlace(ctx context.Context, connection *Client, placeId string) error {
	var result struct {
		ForgetPlace bool `json:"ForgetPlace"`
	}
	return connection.Execute(ctx, DocumentForgetPlace, map[string]any{"placeId": placeId}, &result)
}

// CheckInAt is where to check in: one of the caller's places, or a place that
// was found, with what OpenStreetMap called it.
type CheckInAt struct {
	PlaceID          string
	LatitudeDegrees  *float64
	LongitudeDegrees *float64
	PlaceName        string
	OSMType          string
	OSMID            int64
	OSMCategory      string
	AddressText      string
}

// CheckIn records that the caller is at a place now.
func CheckIn(ctx context.Context, connection *Client, at CheckInAt) (*PlaceVisit, error) {
	variables := map[string]any{}
	if at.PlaceID != "" {
		variables["placeId"] = at.PlaceID
	}
	if at.LatitudeDegrees != nil {
		variables["latitudeDegrees"] = *at.LatitudeDegrees
	}
	if at.LongitudeDegrees != nil {
		variables["longitudeDegrees"] = *at.LongitudeDegrees
	}
	if at.PlaceName != "" {
		variables["placeName"] = at.PlaceName
	}
	if at.OSMType != "" {
		variables["osmType"] = at.OSMType
	}
	if at.OSMID != 0 {
		variables["osmId"] = at.OSMID
	}
	if at.OSMCategory != "" {
		variables["osmCategory"] = at.OSMCategory
	}
	if at.AddressText != "" {
		variables["addressText"] = at.AddressText
	}
	var result struct {
		CheckIn *PlaceVisit `json:"CheckIn"`
	}
	if err := connection.Execute(ctx, DocumentCheckIn, variables, &result); err != nil {
		return nil, err
	}
	return result.CheckIn, nil
}

// EraseLocationHistory erases positions and visits in a range, or all of them
// when both ends are nil, and the places that leaves with no visit that were
// not named or labeled.
func EraseLocationHistory(ctx context.Context, connection *Client, from, until *time.Time) (*LocationHistoryErased, error) {
	variables := map[string]any{}
	if from != nil {
		variables["from"] = from.UTC().Format(time.RFC3339)
	}
	if until != nil {
		variables["until"] = until.UTC().Format(time.RFC3339)
	}
	var result struct {
		EraseLocationHistory *LocationHistoryErased `json:"EraseLocationHistory"`
	}
	if err := connection.Execute(ctx, DocumentEraseLocationHistory, variables, &result); err != nil {
		return nil, err
	}
	return result.EraseLocationHistory, nil
}

// LocationShare is one share of the caller's location.
type LocationShare struct {
	ID                string    `json:"id"`
	GranteeUserID     *string   `json:"granteeUserId"`
	GranteeUsername   string    `json:"granteeUsername"`
	GranteeName       string    `json:"granteeName"`
	GranteeCircleID   *string   `json:"granteeCircleId"`
	GranteeCircleName string    `json:"granteeCircleName"`
	ShareLevel        string    `json:"shareLevel"`
	CreatedAt         time.Time `json:"createdAt"`
}

const (
	locationShareFields = `id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel createdAt`

	DocumentListSharedLocations = `query {
  ListSharedLocations {
    userId username name shareLevel
    latestPoint { ` + locationPointFields + ` }
    openVisit { ` + placeVisitFields + ` }
  }
}`

	DocumentListLocationShares = `query { ListLocationShares { ` + locationShareFields + ` } }`

	DocumentShareLocation = `mutation ($granteeUserId: String, $granteeCircleId: String, $shareLevel: String!) {
  ShareLocation(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel) { ` + locationShareFields + ` }
}`

	DocumentUnshareLocation = `mutation ($shareId: String!) { UnshareLocation(shareId: $shareId) }`
)

// ListSharedLocations are the people whose location is shared with the caller.
func ListSharedLocations(ctx context.Context, connection *Client) ([]*Location, error) {
	var result struct {
		ListSharedLocations []*Location `json:"ListSharedLocations"`
	}
	if err := connection.Execute(ctx, DocumentListSharedLocations, nil, &result); err != nil {
		return nil, err
	}
	return result.ListSharedLocations, nil
}

// ListLocationShares are who the caller's location is shared with.
func ListLocationShares(ctx context.Context, connection *Client) ([]*LocationShare, error) {
	var result struct {
		ListLocationShares []*LocationShare `json:"ListLocationShares"`
	}
	if err := connection.Execute(ctx, DocumentListLocationShares, nil, &result); err != nil {
		return nil, err
	}
	return result.ListLocationShares, nil
}

// ShareLocation shares the caller's location with a person or a circle, given
// by identifier (one of the two), or changes the level of a share already made.
func ShareLocation(ctx context.Context, connection *Client, granteeUserId, granteeCircleId, shareLevel string) (*LocationShare, error) {
	variables := map[string]any{"shareLevel": shareLevel, "granteeUserId": nil, "granteeCircleId": nil}
	if granteeUserId != "" {
		variables["granteeUserId"] = granteeUserId
	}
	if granteeCircleId != "" {
		variables["granteeCircleId"] = granteeCircleId
	}
	var result struct {
		ShareLocation *LocationShare `json:"ShareLocation"`
	}
	if err := connection.Execute(ctx, DocumentShareLocation, variables, &result); err != nil {
		return nil, err
	}
	return result.ShareLocation, nil
}

// UnshareLocation takes a share back.
func UnshareLocation(ctx context.Context, connection *Client, shareId string) error {
	var result struct {
		UnshareLocation bool `json:"UnshareLocation"`
	}
	return connection.Execute(ctx, DocumentUnshareLocation, map[string]any{"shareId": shareId}, &result)
}
