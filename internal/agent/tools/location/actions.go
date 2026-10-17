package location

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// The documents the actions send, as the person. They ask for what an answer
// is made of and no more.
const (
	pointFields = `latitudeDegrees longitudeDegrees accuracyMeters measuredAt locationSource`
	placeFields = `id placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmCategory addressText visitCount lastVisitAt`
	visitFields = `arrivedAt departedAt durationSeconds place { ` + placeFields + ` }`
	foundFields = `placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId`

	documentGetLocation = `query ($userId: String) {
  GetLocation(userId: $userId) { username name locationShareLevel latestPoint { ` + pointFields + ` } openVisit { ` + visitFields + ` } }
}`
	documentListSharedLocations = `query {
  ListSharedLocations { userId username name locationShareLevel latestPoint { ` + pointFields + ` } openVisit { ` + visitFields + ` } }
}`
	documentListVisits = `query ($userId: String, $from: DateTime, $until: DateTime, $placeId: String, $placeLabel: String, $offset: Int, $limitCount: Int) {
  ListLocationVisits(userId: $userId, from: $from, until: $until, placeId: $placeId, placeLabel: $placeLabel, offset: $offset, limitCount: $limitCount) {
    nextOffset placeVisits { ` + visitFields + ` }
  }
}`
	documentListPlaces = `query ($userId: String, $placeLabel: String, $query: String, $offset: Int, $limitCount: Int) {
  ListPlaces(userId: $userId, placeLabel: $placeLabel, query: $query, offset: $offset, limitCount: $limitCount) {
    nextOffset places { ` + placeFields + ` }
  }
}`
	documentListNearby = `query ($latitudeDegrees: Float!, $longitudeDegrees: Float!, $radiusMeters: Float) {
  ListNearbyPlaces(latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, radiusMeters: $radiusMeters) { ` + foundFields + ` }
}`
	documentSearchPlaces = `query ($query: String!, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeId: String, $radiusMeters: Float, $offset: Int, $limitCount: Int) {
  SearchPlaces(query: $query, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeId: $placeId, radiusMeters: $radiusMeters, offset: $offset, limitCount: $limitCount) {
    nextOffset foundPlaces { ` + foundFields + ` }
  }
}`
	documentSavePlace = `mutation ($placeId: String, $placeName: String, $placeLabel: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $radiusMeters: Float) {
  SavePlace(placeId: $placeId, placeName: $placeName, placeLabel: $placeLabel, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, radiusMeters: $radiusMeters) { ` + placeFields + ` }
}`
	documentForgetPlace = `mutation ($placeId: String!) { ForgetPlace(placeId: $placeId) }`
	documentCheckIn     = `mutation ($placeId: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeName: String, $osmType: String, $osmId: Int64, $osmCategory: String, $addressText: String) {
  CheckIn(placeId: $placeId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeName: $placeName, osmType: $osmType, osmId: $osmId, osmCategory: $osmCategory, addressText: $addressText) { ` + visitFields + ` }
}`
	documentErase = `mutation ($from: DateTime, $until: DateTime) {
  ForgetLocationHistory(from: $from, until: $until) { pointCount visitCount placeCount }
}`
	documentListShares = `query { ListLocationShares { id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel } }`
	documentShare      = `mutation ($granteeUserId: String, $granteeCircleId: String, $shareLevel: String!) {
  ShareLocation(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel) { id shareLevel }
}`
	documentUnshare = `mutation ($shareId: String!) { UnshareLocation(shareId: $shareId) }`
)

// The pieces of an answer, as the API names them.
type pointData struct {
	LatitudeDegrees  float64   `json:"latitudeDegrees"`
	LongitudeDegrees float64   `json:"longitudeDegrees"`
	AccuracyMeters   float64   `json:"accuracyMeters"`
	MeasuredAt       time.Time `json:"measuredAt"`
	LocationSource   string    `json:"locationSource"`
}

type placeData struct {
	ID               string     `json:"id"`
	PlaceName        string     `json:"placeName"`
	IsNamedByPerson  bool       `json:"isNamedByPerson"`
	PlaceLabel       string     `json:"placeLabel"`
	LatitudeDegrees  float64    `json:"latitudeDegrees"`
	LongitudeDegrees float64    `json:"longitudeDegrees"`
	RadiusMeters     float64    `json:"radiusMeters"`
	OSMCategory      string     `json:"osmCategory"`
	AddressText      string     `json:"addressText"`
	VisitCount       int        `json:"visitCount"`
	LastVisitAt      *time.Time `json:"lastVisitAt"`
}

type visitData struct {
	ArrivedAt       *time.Time `json:"arrivedAt"`
	DepartedAt      *time.Time `json:"departedAt"`
	DurationSeconds *int       `json:"durationSeconds"`
	Place           *placeData `json:"place"`
}

type locationData struct {
	UserID             string     `json:"userId"`
	Username           string     `json:"username"`
	Name               string     `json:"name"`
	LocationShareLevel string     `json:"locationShareLevel"`
	LatestPoint        *pointData `json:"latestPoint"`
	OpenVisit          *visitData `json:"openVisit"`
}

type foundData struct {
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

// scope is what every action needs: the run, the person's zone and now.
type scope struct {
	run        tools.Run
	operations tools.Operations
	where      *time.Location
	now        time.Time
}

func scopeOf(ctx context.Context) (*scope, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	return &scope{run: run, operations: run.Operations(), where: tools.Location(run.Owner()), now: time.Now()}, nil
}

// untrusted marks an answer carrying place names, which OpenStreetMap or
// somebody else wrote: data, never instructions.
func untrusted(result *tools.Result, err error) (*tools.Result, error) {
	if result != nil {
		result.Untrusted = true
	}
	return result, err
}

// moment reads a from or until in the person's zone; empty is nil.
func (self *scope) moment(value string) (*string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := tools.ParseTime(value, self.where, self.now)
	if err != nil {
		return nil, fmt.Errorf("%q is not a date or time: write YYYY-MM-DD or YYYY-MM-DDTHH:MM", value)
	}
	text := parsed.UTC().Format(time.RFC3339)
	return &text, nil
}

func (self *scope) timeText(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(self.where).Format("2006-01-02 15:04")
}

// personID is the account of somebody who shares their location with the
// person, by username; empty is the person's own.
func (self *scope) personID(ctx context.Context, username string) (*string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	var result struct {
		ListSharedLocations []locationData `json:"ListSharedLocations"`
	}
	if err := self.operations.Execute(ctx, documentListSharedLocations, nil, &result); err != nil {
		return nil, err
	}
	for _, each := range result.ListSharedLocations {
		if strings.EqualFold(each.Username, username) {
			return &each.UserID, nil
		}
	}
	return nil, fmt.Errorf("%q does not share their location with the person; people says who does", username)
}

func roundTo(value float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(value*scale) / scale
}

// ageWords is how long ago something was, as a person says it.
func ageWords(age time.Duration) string {
	switch {
	case age < time.Minute:
		return "less than a minute ago"
	case age < time.Hour:
		return countWords(int(age/time.Minute), "minute") + " ago"
	case age < 48*time.Hour:
		return countWords(int(age/time.Hour), "hour") + " ago"
	}
	return countWords(int(age/(24*time.Hour)), "day") + " ago"
}

// durationWords is how long something lasted.
func durationWords(duration time.Duration) string {
	switch {
	case duration < time.Minute:
		return "under a minute"
	case duration < time.Hour:
		return countWords(int(duration/time.Minute), "minute")
	}
	hours, minutes := int(duration/time.Hour), int(duration%time.Hour/time.Minute)
	if minutes == 0 || hours >= 10 {
		return countWords(hours, "hour")
	}
	return countWords(hours, "hour") + " " + countWords(minutes, "minute")
}

func countWords(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func (self *scope) position(point *pointData) map[string]any {
	return map[string]any{
		"latitude_degrees":  roundTo(point.LatitudeDegrees, 5),
		"longitude_degrees": roundTo(point.LongitudeDegrees, 5),
		"accuracy_meters":   math.Round(point.AccuracyMeters),
		"measured_at":       point.MeasuredAt.In(self.where).Format("2006-01-02 15:04"),
		"age":               ageWords(self.now.Sub(point.MeasuredAt)),
		"reported_by":       point.LocationSource,
	}
}

// placeRow is a place as the model reads it. The keys are the ones the actions
// that take a place take.
func (self *scope) placeRow(place *placeData) map[string]any {
	row := map[string]any{"place_id": place.ID, "place_name": place.PlaceName}
	if place.PlaceLabel != "" {
		row["place_label"] = place.PlaceLabel
	}
	if place.AddressText != "" {
		row["address_text"] = place.AddressText
	}
	if place.OSMCategory != "" {
		row["osm_category"] = place.OSMCategory
	}
	return row
}

// stay is where the person is, as far as is known.
func (self *scope) stay(visit *visitData) map[string]any {
	if visit == nil || visit.Place == nil {
		return map[string]any{"in_progress": false}
	}
	row := self.placeRow(visit.Place)
	row["in_progress"] = true
	if visit.ArrivedAt != nil {
		row["arrived"] = self.timeText(visit.ArrivedAt)
		row["stayed_so_far"] = durationWords(self.now.Sub(*visit.ArrivedAt))
	}
	return row
}

func pageOf(offset, limit int) (int, int) {
	if limit <= 0 {
		limit = 20
	}
	return max(offset, 0), min(limit, 50)
}

func withPaging(variables map[string]any, offset, limit int) {
	offset, limit = pageOf(offset, limit)
	variables["offset"], variables["limitCount"] = offset, limit
}

func withNext(answer map[string]any, nextOffset *int) {
	if nextOffset != nil {
		answer["next_offset"] = *nextOffset
		answer["more"] = fmt.Sprintf("there are more; ask again with offset %d", *nextOffset)
	}
}

type nowArguments struct {
	Reason string `json:"reason"`
	Person string `json:"person"`
}

// runNow is where the person is. Where the turn can ask a browser or phone it
// does, and the answer is kept by the API; anywhere else, or when the asking
// fails, it answers the last position kept, with its age.
func runNow(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[nowArguments](call)
	if err != nil {
		return nil, err
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	userID, err := here.personID(ctx, arguments.Person)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	if userID != nil {
		variables["userId"] = *userID
	}

	var liveNote string
	if userID == nil {
		if locating, ok := here.run.(tools.Locating); ok && locating.CanLocate() {
			found, askErr := locating.Locate(ctx, call.ID)
			if askErr == nil {
				// The API stored the answer as the latest position, so the
				// stay in progress is worked out from it.
				var current struct {
					GetLocation *locationData `json:"GetLocation"`
				}
				answer := map[string]any{"timezone": here.where.String(), "asked_just_now": true}
				answer["position"] = here.position(&pointData{
					LatitudeDegrees: found.LatitudeDegrees, LongitudeDegrees: found.LongitudeDegrees,
					AccuracyMeters: found.AccuracyMeters, MeasuredAt: nonZero(found.MeasuredAt, here.now), LocationSource: "browser",
				})
				if here.operations.Execute(ctx, documentGetLocation, variables, &current) == nil && current.GetLocation != nil {
					answer["at"] = here.stay(current.GetLocation.OpenVisit)
				}
				result, err := tools.JSONResult(answer)
				if err != nil {
					return nil, err
				}
				result.Note = fmt.Sprintf("Located within about %s", metersWords(found.AccuracyMeters))
				if reason := strings.TrimSpace(arguments.Reason); reason != "" {
					result.Note += ": " + reason
				}
				return untrusted(result, nil)
			}
			liveNote = askErr.Error()
		}
	}

	var stored struct {
		GetLocation *locationData `json:"GetLocation"`
	}
	if err := here.operations.Execute(ctx, documentGetLocation, variables, &stored); err != nil {
		return nil, err
	}
	found := stored.GetLocation
	if found == nil || (found.LatestPoint == nil && found.OpenVisit == nil) {
		if liveNote != "" {
			return nil, fmt.Errorf("%s", liveNote)
		}
		who := "the person"
		if userID != nil {
			who = arguments.Person
		}
		return nil, fmt.Errorf("no position of %s is kept, and this turn cannot ask a browser or phone for one. Location can be set up in the TeaNode app on their iPhone (set_up opens the page), or ask them where they are", who)
	}
	answer := map[string]any{"timezone": here.where.String(), "asked_just_now": false}
	if userID != nil {
		answer["person"] = found.Username
		answer["share_level"] = found.LocationShareLevel
	}
	if found.LatestPoint != nil {
		answer["position"] = here.position(found.LatestPoint)
	}
	answer["at"] = here.stay(found.OpenVisit)
	if liveNote != "" {
		answer["note"] = "a fresh position could not be had (" + liveNote + "); this is the last one kept"
	}
	return untrusted(tools.JSONResult(answer))
}

func nonZero(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

// metersWords is how close a fix is, for the drawer's line.
func metersWords(meters float64) string {
	if meters >= 1000 {
		return fmt.Sprintf("%.0f km", meters/1000)
	}
	return fmt.Sprintf("%.0f m", meters)
}

type visitsArguments struct {
	From    string `json:"from"`
	Until   string `json:"until"`
	PlaceID string `json:"place_id"`
	Label   string `json:"label"`
	Person  string `json:"person"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

func runVisits(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[visitsArguments](call)
	if err != nil {
		return nil, err
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	withPaging(variables, arguments.Offset, arguments.Limit)
	for name, value := range map[string]string{"from": arguments.From, "until": arguments.Until} {
		moment, err := here.moment(value)
		if err != nil {
			return nil, err
		}
		if moment != nil {
			variables[name] = *moment
		}
	}
	if place := strings.TrimSpace(arguments.PlaceID); place != "" {
		variables["placeId"] = place
	}
	if label := strings.TrimSpace(arguments.Label); label != "" {
		variables["placeLabel"] = strings.ToLower(label)
	}
	userID, err := here.personID(ctx, arguments.Person)
	if err != nil {
		return nil, err
	}
	if userID != nil {
		variables["userId"] = *userID
	}
	var result struct {
		ListLocationVisits struct {
			NextOffset  *int        `json:"nextOffset"`
			PlaceVisits []visitData `json:"placeVisits"`
		} `json:"ListLocationVisits"`
	}
	if err := here.operations.Execute(ctx, documentListVisits, variables, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListLocationVisits.PlaceVisits))
	for index := range result.ListLocationVisits.PlaceVisits {
		visit := &result.ListLocationVisits.PlaceVisits[index]
		if visit.Place == nil {
			continue
		}
		row := here.placeRow(visit.Place)
		row["arrived"] = here.timeText(visit.ArrivedAt)
		if visit.DepartedAt == nil {
			row["left"] = "still there"
		} else {
			row["left"] = here.timeText(visit.DepartedAt)
		}
		if visit.DurationSeconds != nil {
			row["stayed"] = durationWords(time.Duration(*visit.DurationSeconds) * time.Second)
		}
		rows = append(rows, row)
	}
	answer := map[string]any{"timezone": here.where.String(), "visits": rows}
	withNext(answer, result.ListLocationVisits.NextOffset)
	return untrusted(tools.JSONResult(answer))
}

type placesArguments struct {
	Label  string `json:"label"`
	Query  string `json:"query"`
	Person string `json:"person"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func runPlaces(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[placesArguments](call)
	if err != nil {
		return nil, err
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	withPaging(variables, arguments.Offset, arguments.Limit)
	if label := strings.TrimSpace(arguments.Label); label != "" {
		variables["placeLabel"] = strings.ToLower(label)
	}
	if query := strings.TrimSpace(arguments.Query); query != "" {
		variables["query"] = query
	}
	userID, err := here.personID(ctx, arguments.Person)
	if err != nil {
		return nil, err
	}
	if userID != nil {
		variables["userId"] = *userID
	}
	var result struct {
		ListPlaces struct {
			NextOffset *int        `json:"nextOffset"`
			Places     []placeData `json:"places"`
		} `json:"ListPlaces"`
	}
	if err := here.operations.Execute(ctx, documentListPlaces, variables, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListPlaces.Places))
	for index := range result.ListPlaces.Places {
		place := &result.ListPlaces.Places[index]
		row := here.placeRow(place)
		row["visit_count"] = place.VisitCount
		if place.LastVisitAt != nil {
			row["last_visit"] = here.timeText(place.LastVisitAt)
		}
		rows = append(rows, row)
	}
	answer := map[string]any{"timezone": here.where.String(), "places": rows}
	withNext(answer, result.ListPlaces.NextOffset)
	return untrusted(tools.JSONResult(answer))
}

// lastPosition is where the person was last reported to be, for the actions
// that look around a point and were given none.
func (self *scope) lastPosition(ctx context.Context) (float64, float64, error) {
	var result struct {
		GetLocation *locationData `json:"GetLocation"`
	}
	if err := self.operations.Execute(ctx, documentGetLocation, map[string]any{}, &result); err != nil {
		return 0, 0, err
	}
	if result.GetLocation == nil || result.GetLocation.LatestPoint == nil {
		return 0, 0, fmt.Errorf("no position of the person is kept to look around: give latitude_degrees and longitude_degrees, or ask them where they are")
	}
	return result.GetLocation.LatestPoint.LatitudeDegrees, result.GetLocation.LatestPoint.LongitudeDegrees, nil
}

func (self *scope) foundRow(found *foundData) map[string]any {
	row := map[string]any{
		"place_name": found.PlaceName, "distance_meters": math.Round(found.DistanceMeters),
		"latitude_degrees": roundTo(found.LatitudeDegrees, 6), "longitude_degrees": roundTo(found.LongitudeDegrees, 6),
	}
	if found.OSMCategory != "" {
		row["osm_category"] = found.OSMCategory
	}
	if found.AddressText != "" {
		row["address_text"] = found.AddressText
	}
	if found.OSMType != "" {
		row["osm_type"], row["osm_id"] = found.OSMType, found.OSMID
	}
	if found.PlaceID != nil {
		row["place_id"] = *found.PlaceID
		row["is_the_persons_place"] = true
	}
	return row
}

type nearbyArguments struct {
	LatitudeDegrees  *float64 `json:"latitude_degrees"`
	LongitudeDegrees *float64 `json:"longitude_degrees"`
	RadiusMeters     *float64 `json:"radius_meters"`
}

func runNearby(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[nearbyArguments](call)
	if err != nil {
		return nil, err
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	latitude, longitude, err := here.aroundPoint(ctx, arguments.LatitudeDegrees, arguments.LongitudeDegrees)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"latitudeDegrees": latitude, "longitudeDegrees": longitude}
	if arguments.RadiusMeters != nil {
		variables["radiusMeters"] = *arguments.RadiusMeters
	}
	var result struct {
		ListNearbyPlaces []foundData `json:"ListNearbyPlaces"`
	}
	if err := here.operations.Execute(ctx, documentListNearby, variables, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListNearbyPlaces))
	for index := range result.ListNearbyPlaces {
		rows = append(rows, here.foundRow(&result.ListNearbyPlaces[index]))
	}
	return untrusted(tools.JSONResult(map[string]any{"places": rows}))
}

// aroundPoint is the point given, or the person's last reported position.
func (self *scope) aroundPoint(ctx context.Context, latitude, longitude *float64) (float64, float64, error) {
	if (latitude == nil) != (longitude == nil) {
		return 0, 0, fmt.Errorf("give both latitude_degrees and longitude_degrees")
	}
	if latitude != nil {
		return *latitude, *longitude, nil
	}
	return self.lastPosition(ctx)
}

type searchArguments struct {
	Query            string   `json:"query"`
	PlaceID          string   `json:"place_id"`
	LatitudeDegrees  *float64 `json:"latitude_degrees"`
	LongitudeDegrees *float64 `json:"longitude_degrees"`
	RadiusMeters     *float64 `json:"radius_meters"`
	Limit            int      `json:"limit"`
	Offset           int      `json:"offset"`
}

func runSearch(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[searchArguments](call)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		return nil, fmt.Errorf("say what to search for, in query")
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"query": query}
	withPaging(variables, arguments.Offset, arguments.Limit)
	if arguments.RadiusMeters != nil {
		variables["radiusMeters"] = *arguments.RadiusMeters
	}
	switch {
	case arguments.LatitudeDegrees != nil || arguments.LongitudeDegrees != nil:
		latitude, longitude, err := here.aroundPoint(ctx, arguments.LatitudeDegrees, arguments.LongitudeDegrees)
		if err != nil {
			return nil, err
		}
		variables["latitudeDegrees"], variables["longitudeDegrees"] = latitude, longitude
	case strings.TrimSpace(arguments.PlaceID) != "":
		variables["placeId"] = strings.TrimSpace(arguments.PlaceID)
	}
	var result struct {
		SearchPlaces struct {
			NextOffset  *int        `json:"nextOffset"`
			FoundPlaces []foundData `json:"foundPlaces"`
		} `json:"SearchPlaces"`
	}
	if err := here.operations.Execute(ctx, documentSearchPlaces, variables, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.SearchPlaces.FoundPlaces))
	for index := range result.SearchPlaces.FoundPlaces {
		rows = append(rows, here.foundRow(&result.SearchPlaces.FoundPlaces[index]))
	}
	answer := map[string]any{"places": rows}
	withNext(answer, result.SearchPlaces.NextOffset)
	return untrusted(tools.JSONResult(answer))
}

func runSavePlace(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[savePlaceArguments](call)
	if err != nil {
		return nil, err
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	if place := strings.TrimSpace(arguments.PlaceID); place != "" {
		variables["placeId"] = place
	}
	if arguments.Name != nil {
		variables["placeName"] = *arguments.Name
	}
	if arguments.Label != nil {
		label := strings.ToLower(strings.TrimSpace(*arguments.Label))
		if label == "none" {
			label = ""
		}
		variables["placeLabel"] = label
	}
	if arguments.LatitudeDegrees != nil {
		variables["latitudeDegrees"] = *arguments.LatitudeDegrees
	}
	if arguments.LongitudeDegrees != nil {
		variables["longitudeDegrees"] = *arguments.LongitudeDegrees
	}
	if arguments.RadiusMeters != nil {
		variables["radiusMeters"] = *arguments.RadiusMeters
	}
	if variables["placeId"] == nil && (variables["latitudeDegrees"] == nil || variables["longitudeDegrees"] == nil) {
		return nil, fmt.Errorf("say which place to change in place_id, or give latitude_degrees and longitude_degrees to make a new one")
	}
	var result struct {
		SavePlace placeData `json:"SavePlace"`
	}
	if err := here.operations.Execute(ctx, documentSavePlace, variables, &result); err != nil {
		return nil, err
	}
	answer := here.placeRow(&result.SavePlace)
	answer["radius_meters"] = result.SavePlace.RadiusMeters
	answer["is_named_by_person"] = result.SavePlace.IsNamedByPerson
	return untrusted(tools.JSONResult(answer))
}

func runForgetPlace(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[struct {
		PlaceID string `json:"place_id"`
	}](call)
	if err != nil {
		return nil, err
	}
	place := strings.TrimSpace(arguments.PlaceID)
	if place == "" {
		return nil, fmt.Errorf("which place? give place_id, from places")
	}
	if !call.Confirmed {
		return nil, fmt.Errorf("forget_place needs the person's confirmation")
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	var discard map[string]any
	if err := here.operations.Execute(ctx, documentForgetPlace, map[string]any{"placeId": place}, &discard); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"forgotten": true, "place_id": place})
}

func runCheckIn(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[checkInArguments](call)
	if err != nil {
		return nil, err
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	if place := strings.TrimSpace(arguments.PlaceID); place != "" {
		variables["placeId"] = place
	} else {
		if arguments.LatitudeDegrees == nil || arguments.LongitudeDegrees == nil {
			return nil, fmt.Errorf("give place_id, or a found place's place_name, latitude_degrees and longitude_degrees")
		}
		variables["latitudeDegrees"], variables["longitudeDegrees"] = *arguments.LatitudeDegrees, *arguments.LongitudeDegrees
		for name, value := range map[string]string{"placeName": arguments.PlaceName, "osmType": arguments.OSMType, "osmCategory": arguments.OSMCategory, "addressText": arguments.AddressText} {
			if strings.TrimSpace(value) != "" {
				variables[name] = strings.TrimSpace(value)
			}
		}
		if arguments.OSMID != 0 {
			variables["osmId"] = arguments.OSMID
		}
	}
	var result struct {
		CheckIn visitData `json:"CheckIn"`
	}
	if err := here.operations.Execute(ctx, documentCheckIn, variables, &result); err != nil {
		return nil, err
	}
	if result.CheckIn.Place == nil {
		return tools.JSONResult(map[string]any{"checked_in": true})
	}
	answer := here.placeRow(result.CheckIn.Place)
	answer["checked_in"] = true
	answer["arrived"] = here.timeText(result.CheckIn.ArrivedAt)
	return untrusted(tools.JSONResult(answer))
}

func runErase(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[windowArguments](call)
	if err != nil {
		return nil, err
	}
	if !call.Confirmed {
		return nil, fmt.Errorf("erase needs the person's confirmation")
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	for name, value := range map[string]string{"from": arguments.From, "until": arguments.Until} {
		moment, err := here.moment(value)
		if err != nil {
			return nil, err
		}
		if moment != nil {
			variables[name] = *moment
		}
	}
	var result struct {
		ForgetLocationHistory struct {
			PointCount int `json:"pointCount"`
			VisitCount int `json:"visitCount"`
			PlaceCount int `json:"placeCount"`
		} `json:"ForgetLocationHistory"`
	}
	if err := here.operations.Execute(ctx, documentErase, variables, &result); err != nil {
		return nil, err
	}
	erased := result.ForgetLocationHistory
	return tools.JSONResult(map[string]any{"deleted_position_count": erased.PointCount, "deleted_visit_count": erased.VisitCount, "deleted_place_count": erased.PlaceCount})
}

func runPeople(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	var result struct {
		ListSharedLocations []locationData `json:"ListSharedLocations"`
	}
	if err := here.operations.Execute(ctx, documentListSharedLocations, nil, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListSharedLocations))
	for index := range result.ListSharedLocations {
		each := &result.ListSharedLocations[index]
		row := map[string]any{"person": each.Username, "name": each.Name, "share_level": each.LocationShareLevel}
		if each.LatestPoint != nil {
			row["position"] = here.position(each.LatestPoint)
		}
		row["at"] = here.stay(each.OpenVisit)
		rows = append(rows, row)
	}
	return untrusted(tools.JSONResult(map[string]any{"timezone": here.where.String(), "people": rows}))
}

func runShares(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	var result struct {
		ListLocationShares []struct {
			ID                string  `json:"id"`
			GranteeUserID     *string `json:"granteeUserId"`
			GranteeUsername   string  `json:"granteeUsername"`
			GranteeCircleID   *string `json:"granteeCircleId"`
			GranteeCircleName string  `json:"granteeCircleName"`
			ShareLevel        string  `json:"shareLevel"`
		} `json:"ListLocationShares"`
	}
	if err := here.operations.Execute(ctx, documentListShares, nil, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListLocationShares))
	for _, share := range result.ListLocationShares {
		with := share.GranteeUsername
		if share.GranteeCircleID != nil {
			with = fmt.Sprintf("the circle %q", share.GranteeCircleName)
		}
		rows = append(rows, map[string]any{"with": with, "share_level": share.ShareLevel, "share_id": share.ID})
	}
	return untrusted(tools.JSONResult(map[string]any{"shares": rows}))
}

func runShare(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[struct {
		With       string `json:"with"`
		ShareLevel string `json:"share_level"`
	}](call)
	if err != nil {
		return nil, err
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	grantee, err := tools.ResolveGrantee(ctx, here.operations, arguments.With)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"shareLevel": strings.ToLower(strings.TrimSpace(arguments.ShareLevel)), "granteeUserId": nil, "granteeCircleId": nil}
	if grantee.UserID != "" {
		variables["granteeUserId"] = grantee.UserID
	} else {
		variables["granteeCircleId"] = grantee.CircleID
	}
	var result struct {
		ShareLocation struct {
			ID         string `json:"id"`
			ShareLevel string `json:"shareLevel"`
		} `json:"ShareLocation"`
	}
	if err := here.operations.Execute(ctx, documentShare, variables, &result); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"with": grantee.Description, "share_level": result.ShareLocation.ShareLevel, "share_id": result.ShareLocation.ID})
}

func runUnshare(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[struct {
		ShareID string `json:"share_id"`
	}](call)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.ShareID) == "" {
		return nil, fmt.Errorf("which share? give share_id, from shares")
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	var discard map[string]any
	if err := here.operations.Execute(ctx, documentUnshare, map[string]any{"shareId": strings.TrimSpace(arguments.ShareID)}, &discard); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"unshared": true})
}

// runSetUp takes the dashboard the person reads this in to the Location page,
// or says where to find it where there is no dashboard to take.
func runSetUp(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	const path = "/location"
	showing, ok := tools.MustRun(ctx).(tools.Showing)
	if !ok || !showing.CanShowPages() {
		return &tools.Result{Content: fmt.Sprintf(`{"shown": false, "where": %q}`,
			"Location is in the side menu of the dashboard, and in the TeaNode app on their iPhone, which is what records where they go")}, nil
	}
	showing.ShowPage(path)
	return &tools.Result{Content: fmt.Sprintf(`{"shown": %q, "note": %q}`, path,
		"Location is open. Recording where they go happens in the TeaNode app on their iPhone: install it, then allow Location when it asks."),
		Note: "Opened " + path}, nil
}

// placeNameOf is the name of one of the person's places, for a card, or empty
// when it cannot be looked up. It reads the first page of places, which holds
// the place for anybody but a person with a great many.
func placeNameOf(ctx context.Context, placeID string) string {
	placeID = strings.TrimSpace(placeID)
	if placeID == "" {
		return ""
	}
	here, err := scopeOf(ctx)
	if err != nil || here.operations == nil {
		return ""
	}
	var result struct {
		ListPlaces struct {
			Places []placeData `json:"places"`
		} `json:"ListPlaces"`
	}
	if here.operations.Execute(ctx, documentListPlaces, map[string]any{"limitCount": 200}, &result) != nil {
		return ""
	}
	for _, place := range result.ListPlaces.Places {
		if place.ID == placeID {
			return place.PlaceName
		}
	}
	return ""
}
