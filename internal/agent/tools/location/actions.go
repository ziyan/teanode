package location

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/place"
)

// The documents the actions send, as the person. They ask for what an answer
// is made of and no more.
const (
	pointFields       = `latitudeDegrees longitudeDegrees accuracyMeters measuredAt locationSource`
	placeFields       = `id placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmName osmCategory addressText isAddressTyped visitCount lastVisitAt`
	visitFields       = `arrivedAt departedAt durationSeconds place { ` + placeFields + ` }`
	listedVisitFields = visitFields + ` photoCount photosTakenBefore`
	pauseFields       = `startedAt endedAt`
	foundFields       = `placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId placeLabel isNamedByPerson`

	documentGetLocation = `query ($userId: String) {
  GetLocation(userId: $userId) { username name shareLevel latestPoint { ` + pointFields + ` } openVisit { ` + visitFields + ` } pause { ` + pauseFields + ` } }
}`
	documentListSharedLocations = `query {
  ListSharedLocations { userId username name shareLevel latestPoint { ` + pointFields + ` } openVisit { ` + visitFields + ` } pause { ` + pauseFields + ` } }
}`
	documentListVisits = `query ($userId: String, $from: DateTime, $until: DateTime, $placeId: String, $placeLabel: String, $offset: Int, $limitCount: Int) {
  ListLocationVisits(userId: $userId, from: $from, until: $until, placeId: $placeId, placeLabel: $placeLabel, offset: $offset, limitCount: $limitCount) {
    nextOffset placeVisits { ` + listedVisitFields + ` }
  }
}`
	documentListPauses = `query ($userId: String, $from: DateTime!, $to: DateTime!) {
  ListLocationPauses(userId: $userId, from: $from, to: $to) { ` + pauseFields + ` }
}`
	documentListPoints = `query ($userId: String, $from: DateTime!, $until: DateTime!) {
  ListLocationPoints(userId: $userId, from: $from, until: $until) { ` + pointFields + ` }
}`
	documentListPlaces = `query ($userId: String, $placeLabel: String, $query: String, $offset: Int, $limitCount: Int) {
  ListPlaces(userId: $userId, placeLabel: $placeLabel, query: $query, offset: $offset, limitCount: $limitCount) {
    nextOffset places { ` + placeFields + ` }
  }
}`
	documentReadPlace = `query ($placeId: String!, $userId: String) {
  ReadPlace(placeId: $placeId, userId: $userId) { ` + placeFields + ` }
}`
	documentListNearby = `query ($latitudeDegrees: Float!, $longitudeDegrees: Float!, $radiusMeters: Float) {
  ListNearbyPlaces(latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, radiusMeters: $radiusMeters) { ` + foundFields + ` }
}`
	documentSearchPlaces = `query ($query: String!, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeId: String, $radiusMeters: Float, $offset: Int, $limitCount: Int) {
  SearchPlaces(query: $query, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeId: $placeId, radiusMeters: $radiusMeters, offset: $offset, limitCount: $limitCount) {
    nextOffset foundPlaces { ` + foundFields + ` }
  }
}`
	documentSearchAddresses = `query ($query: String!, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeId: String, $limitCount: Int) {
  SearchAddresses(query: $query, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeId: $placeId, limitCount: $limitCount) {
    placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId placeLabel isNamedByPerson
  }
}`
	documentSavePlace = `mutation ($placeId: String, $placeName: String, $placeLabel: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $radiusMeters: Float, $addressText: String) {
  SavePlace(placeId: $placeId, placeName: $placeName, placeLabel: $placeLabel, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, radiusMeters: $radiusMeters, addressText: $addressText) { ` + placeFields + ` }
}`
	documentForgetPlace = `mutation ($placeId: String!) { ForgetPlace(placeId: $placeId) }`
	documentCheckIn     = `mutation ($placeId: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeName: String, $osmType: String, $osmId: Int64, $osmCategory: String, $addressText: String, $accuracyMeters: Float) {
  CheckIn(placeId: $placeId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeName: $placeName, osmType: $osmType, osmId: $osmId, osmCategory: $osmCategory, addressText: $addressText, accuracyMeters: $accuracyMeters) { ` + visitFields + ` }
}`
	documentPause  = `mutation ($endedAt: DateTime) { PauseLocation(endedAt: $endedAt) { ` + pauseFields + ` } }`
	documentResume = `mutation { ResumeLocation }`
	documentErase  = `mutation ($from: DateTime, $until: DateTime) {
  EraseLocationHistory(from: $from, until: $until) { pointCount visitCount placeCount }
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
	OSMName          string     `json:"osmName"`
	OSMCategory      string     `json:"osmCategory"`
	AddressText      string     `json:"addressText"`
	IsAddressTyped   bool       `json:"isAddressTyped"`
	VisitCount       int        `json:"visitCount"`
	LastVisitAt      *time.Time `json:"lastVisitAt"`
}

type visitData struct {
	ArrivedAt         *time.Time `json:"arrivedAt"`
	DepartedAt        *time.Time `json:"departedAt"`
	DurationSeconds   *int       `json:"durationSeconds"`
	Place             *placeData `json:"place"`
	PhotoCount        *int       `json:"photoCount"`
	PhotosTakenBefore *time.Time `json:"photosTakenBefore"`
}

type pauseData struct {
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt"`
}

type locationData struct {
	UserID      string     `json:"userId"`
	Username    string     `json:"username"`
	Name        string     `json:"name"`
	ShareLevel  string     `json:"shareLevel"`
	LatestPoint *pointData `json:"latestPoint"`
	OpenVisit   *visitData `json:"openVisit"`
	Pause       *pauseData `json:"pause"`
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
	PlaceLabel       string  `json:"placeLabel"`
	IsNamedByPerson  bool    `json:"isNamedByPerson"`
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

// exactTimeText is a time in the person's zone with its offset and any
// fraction of a second, for a value the model passes on to another tool.
func (self *scope) exactTimeText(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(self.where).Format(time.RFC3339Nano)
}

func (self *scope) timeText(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(self.where).Format("2006-01-02 15:04")
}

// errOthersInChat is what an action answers when it is asked for somebody
// else's location in a chat: the answer would be posted to everybody in the
// room, and the person shared it with the owner of the agent alone.
var errOthersInChat = errors.New("another person's location is not repeated into a chat, because everybody in it would read it: " +
	"the person can ask in a private conversation with you instead")

// errOthersNotByPerson is what an action answers when it is asked for
// somebody else's location in a run the person did not start: an inbound
// mail, a webhook or a schedule the agent wrote is steered by content the
// person did not write, and could have it read out to whoever wrote it.
var errOthersNotByPerson = errors.New("another person's location is read only in a conversation the person started themselves, " +
	"not in a run that a mail, a webhook or a schedule of yours began: the person can ask you directly instead")

// checkOthersAllowed is nil when this run may read somebody else's location:
// the person started it, and its words do not go to a chat.
func (self *scope) checkOthersAllowed() error {
	if tools.IsAnsweringInChat(self.run) {
		return errOthersInChat
	}
	if !tools.IsStartedByPerson(self.run) {
		return errOthersNotByPerson
	}
	return nil
}

// personId is the account of somebody who shares their location with the
// person, by username; empty is the person's own. With doesNeedHistory, somebody
// who shares only where they are now is refused in those words. In a chat
// turn, or a run the person did not start, nobody else's location is read.
func (self *scope) personId(ctx context.Context, username string, doesNeedHistory bool) (*string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	if err := self.checkOthersAllowed(); err != nil {
		return nil, err
	}
	var result struct {
		ListSharedLocations []locationData `json:"ListSharedLocations"`
	}
	if err := self.operations.Execute(ctx, documentListSharedLocations, nil, &result); err != nil {
		return nil, err
	}
	for _, each := range result.ListSharedLocations {
		if strings.EqualFold(each.Username, username) {
			if doesNeedHistory && each.ShareLevel != "history" {
				return nil, fmt.Errorf("%s shares only where they are now", each.Username)
			}
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

// placeDisplayName is what to call a place: the name the person gave it; else,
// for a place they labeled, the label word (Home, Work); else the name
// OpenStreetMap gave it, then its address, then the name it has until it is
// named (its city).
func placeDisplayName(place *placeData) string {
	switch {
	case place.IsNamedByPerson && place.PlaceName != "":
		return place.PlaceName
	case place.PlaceLabel != "":
		return strings.ToUpper(place.PlaceLabel[:1]) + place.PlaceLabel[1:]
	case place.OSMName != "":
		return place.OSMName
	case place.AddressText != "":
		return place.AddressText
	}
	return place.PlaceName
}

// placeRow is a place as the model reads it. place_display_name is what to call
// it; place_label and osm_name are the pieces it was chosen from, and place_id
// is what the actions that take a place take.
func (self *scope) placeRow(place *placeData) map[string]any {
	row := map[string]any{"place_id": place.ID, "place_display_name": placeDisplayName(place)}
	if place.PlaceLabel != "" {
		row["place_label"] = place.PlaceLabel
	}
	if place.OSMName != "" {
		row["osm_name"] = place.OSMName
	}
	if place.AddressText != "" {
		row["address_text"] = place.AddressText
		if place.IsAddressTyped {
			row["is_address_typed"] = true
		}
	}
	if place.OSMCategory != "" {
		row["osm_category"] = place.OSMCategory
	}
	return row
}

// openVisitRow is where the person is, as far as is known: the visit in
// progress, or the word that there is none.
func (self *scope) openVisitRow(visit *visitData) map[string]any {
	if visit == nil || visit.Place == nil {
		return map[string]any{"is_in_progress": false}
	}
	row := self.placeRow(visit.Place)
	row["is_in_progress"] = true
	if visit.ArrivedAt != nil {
		row["arrived"] = self.timeText(visit.ArrivedAt)
		row["visit_duration_so_far"] = durationWords(self.now.Sub(*visit.ArrivedAt))
	}
	return row
}

// pauseEndWords is when a pause ends, as the person says it: "until 15:00",
// with the date when it is not today, or "until they resume".
func (self *scope) pauseEndWords(pause *pauseData) string {
	if pause.EndedAt == nil {
		return "until they resume"
	}
	local := pause.EndedAt.In(self.where)
	if now := self.now.In(self.where); local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return "until " + local.Format("15:04")
	}
	return "until " + local.Format("2006-01-02 15:04")
}

// pausedRow is what is said instead of where somebody is while they have
// paused: they share no position until they resume.
func (self *scope) pausedRow(name string, pause *pauseData) map[string]any {
	return map[string]any{
		"is_paused":    true,
		"paused_since": self.timeText(&pause.StartedAt),
		"paused_text":  name + " has paused sharing where they are (" + self.pauseEndWords(pause) + ")",
	}
}

// withPaging passes on the page asked for. What is left out is the API's
// default (50 visits or places, 20 results of a search), and a number it
// refuses is refused in its words.
func withPaging(variables map[string]any, offset, limit *int) {
	if offset != nil {
		variables["offset"] = *offset
	}
	if limit != nil {
		variables["limitCount"] = *limit
	}
}

// withNext says whether a page was the last, and where the next one starts.
func withNext(answer map[string]any, nextOffset *int) {
	answer["has_more"] = nextOffset != nil
	if nextOffset != nil {
		answer["next_offset"] = *nextOffset
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
	userId, err := here.personId(ctx, arguments.Person, false)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	if userId != nil {
		variables["userId"] = *userId
	}

	var liveNote string
	if userId == nil {
		if locating, ok := here.run.(tools.Locating); ok && locating.CanLocate() {
			found, askErr := locating.Locate(ctx, call.ID)
			if askErr == nil {
				// The API stored the answer as the latest position, so the
				// visit in progress is worked out from it.
				var current struct {
					GetLocation *locationData `json:"GetLocation"`
				}
				answer := map[string]any{"timezone": here.where.String(), "asked_just_now": true}
				answer["position"] = here.position(&pointData{
					LatitudeDegrees: found.LatitudeDegrees, LongitudeDegrees: found.LongitudeDegrees,
					AccuracyMeters: found.AccuracyMeters, MeasuredAt: nonZero(found.MeasuredAt, here.now), LocationSource: "browser",
				})
				if here.operations.Execute(ctx, documentGetLocation, variables, &current) == nil && current.GetLocation != nil {
					answer["at"] = here.openVisitRow(current.GetLocation.OpenVisit)
					here.addRecordingPaused(answer, current.GetLocation.Pause)
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
	// Somebody who paused shares no position, which is not the same as not
	// having one: they are said to have paused.
	if found != nil && found.Pause != nil && userId != nil {
		answer := here.pausedRow(found.Name, found.Pause)
		answer["person"], answer["share_level"] = found.Username, found.ShareLevel
		return tools.JSONResult(answer)
	}
	if found == nil || (found.LatestPoint == nil && found.OpenVisit == nil) {
		if liveNote != "" {
			return nil, fmt.Errorf("%s", liveNote)
		}
		who := "the person"
		if userId != nil {
			who = arguments.Person
		}
		return nil, fmt.Errorf("no position of %s is kept, and this turn cannot ask a browser or phone for one. Location can be set up in the TeaNode app on their iPhone (set_up opens the page), or ask them where they are", who)
	}
	answer := map[string]any{"timezone": here.where.String(), "asked_just_now": false}
	if userId != nil {
		answer["person"] = found.Username
		answer["share_level"] = found.ShareLevel
	}
	if found.LatestPoint != nil {
		answer["position"] = here.position(found.LatestPoint)
	}
	answer["at"] = here.openVisitRow(found.OpenVisit)
	here.addRecordingPaused(answer, found.Pause)
	if liveNote != "" {
		answer["note"] = "a fresh position could not be had (" + liveNote + "); this is the last one kept"
	}
	return untrusted(tools.JSONResult(answer))
}

// addRecordingPaused says in the answer about the person's own location that
// they have paused recording, so that a position that is old is not read as a
// phone that stopped working.
func (self *scope) addRecordingPaused(answer map[string]any, pause *pauseData) {
	if pause != nil {
		answer["recording_paused"] = "recording is paused (" + self.pauseEndWords(pause) + "); nothing new is kept until it ends or the person resumes"
	}
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
	From       string `json:"from"`
	Until      string `json:"until"`
	PlaceID    string `json:"place_id"`
	PlaceLabel string `json:"place_label"`
	Person     string `json:"person"`
	Limit      *int   `json:"limit"`
	Offset     *int   `json:"offset"`
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
	if label := strings.TrimSpace(arguments.PlaceLabel); label != "" {
		variables["placeLabel"] = strings.ToLower(label)
	}
	userId, err := here.personId(ctx, arguments.Person, true)
	if err != nil {
		return nil, err
	}
	if userId != nil {
		variables["userId"] = *userId
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
			row["visit_duration"] = durationWords(time.Duration(*visit.DurationSeconds) * time.Second)
		}
		if visit.PhotoCount != nil && *visit.PhotoCount > 0 {
			row["photo_count"] = *visit.PhotoCount
			// Exact, with the zone's offset, so the photo tool's taken_after
			// and taken_before given them list exactly the photos counted.
			row["photos_taken_after"] = here.exactTimeText(visit.ArrivedAt)
			row["photos_taken_before"] = here.exactTimeText(visit.PhotosTakenBefore)
		}
		rows = append(rows, row)
	}
	answer := map[string]any{"timezone": here.where.String(), "visits": rows}
	withNext(answer, result.ListLocationVisits.NextOffset)
	if arguments.PlaceID == "" && arguments.PlaceLabel == "" {
		pauses, err := here.pausesAmongVisits(ctx, userId, variables, result.ListLocationVisits.PlaceVisits, arguments.Offset)
		if err != nil {
			return nil, err
		}
		if len(pauses) > 0 {
			answer["pauses"] = pauses
		}
	}
	return untrusted(tools.JSONResult(answer))
}

// pausesAmongVisits are the times the location was paused that belong beside a
// page of visits, newest first: those in the range asked for, or else those
// from when the oldest visit of the page began to now (to when the newest
// began, on a later page). A pause belongs to no place, so a listing of one
// place's visits has none. A pause between two pages is on neither; asking
// with from and until gives a range of one's own.
func (self *scope) pausesAmongVisits(ctx context.Context, userId *string, variables map[string]any, visits []visitData, offset *int) ([]map[string]any, error) {
	startOf := func(visit *visitData) time.Time {
		switch {
		case visit.ArrivedAt != nil:
			return *visit.ArrivedAt
		case visit.DepartedAt != nil:
			return *visit.DepartedAt
		}
		return time.Time{}
	}
	rangeEnd := self.now
	if text, isGiven := variables["until"].(string); isGiven {
		rangeEnd, _ = time.Parse(time.RFC3339, text)
	} else if offset != nil && *offset > 0 && len(visits) > 0 {
		rangeEnd = startOf(&visits[0])
	}
	var rangeStart time.Time
	switch {
	case variables["from"] != nil:
		rangeStart, _ = time.Parse(time.RFC3339, variables["from"].(string))
	case len(visits) > 0:
		rangeStart = startOf(&visits[len(visits)-1])
	default:
		rangeStart = rangeEnd.AddDate(0, 0, -30)
	}
	if rangeStart.IsZero() || rangeEnd.IsZero() || !rangeStart.Before(rangeEnd) {
		return nil, nil
	}
	pauseVariables := map[string]any{"from": rangeStart.UTC().Format(time.RFC3339), "to": rangeEnd.UTC().Format(time.RFC3339)}
	if userId != nil {
		pauseVariables["userId"] = *userId
	}
	var result struct {
		ListLocationPauses []pauseData `json:"ListLocationPauses"`
	}
	if err := self.operations.Execute(ctx, documentListPauses, pauseVariables, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListLocationPauses))
	for index := range result.ListLocationPauses {
		pause := &result.ListLocationPauses[index]
		row := map[string]any{"paused_from": self.timeText(&pause.StartedAt)}
		if pause.EndedAt == nil {
			row["paused_until"] = "they resume"
		} else {
			row["paused_until"] = self.timeText(pause.EndedAt)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// pathPointsMost is how many positions of a day's path the answer lists; a
// day holds far more, so the rest are left out evenly.
const pathPointsMost = 60

// pathReadHours is how much of a day one reading of its positions covers.
const pathReadHours = 6

type pathArguments struct {
	Day    string `json:"day"`
	Person string `json:"person"`
}

// runPath answers one day of the person's path: how many positions there are,
// the first and last time, how far they went, and a sample of the positions.
func runPath(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[pathArguments](call)
	if err != nil {
		return nil, err
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	day := here.now.In(here.where)
	if given := strings.TrimSpace(arguments.Day); given != "" {
		parsed, err := time.ParseInLocation("2006-01-02", given, here.where)
		if err != nil {
			return nil, fmt.Errorf("%q is not a day: write YYYY-MM-DD", given)
		}
		day = parsed
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, here.where)
	until := from.AddDate(0, 0, 1)
	userId, err := here.personId(ctx, arguments.Person, true)
	if err != nil {
		return nil, err
	}
	// The day is read a quarter at a time: the server answers at most a few
	// thousand positions at once, and a phone recording every few seconds
	// passes that in a day.
	var points []pointData
	for start := from; start.Before(until); start = start.Add(pathReadHours * time.Hour) {
		end := start.Add(pathReadHours * time.Hour)
		if end.After(until) {
			end = until
		}
		variables := map[string]any{"from": start.UTC().Format(time.RFC3339), "until": end.UTC().Format(time.RFC3339)}
		if userId != nil {
			variables["userId"] = *userId
		}
		var result struct {
			ListLocationPoints []pointData `json:"ListLocationPoints"`
		}
		if err := here.operations.Execute(ctx, documentListPoints, variables, &result); err != nil {
			return nil, err
		}
		points = append(points, result.ListLocationPoints...)
	}
	answer := map[string]any{"timezone": here.where.String(), "day": from.Format("2006-01-02"), "point_count": len(points)}
	if len(points) > 0 {
		var travelledMeters float64
		for index := 1; index < len(points); index++ {
			travelledMeters += place.DistanceMeters(points[index-1].LatitudeDegrees, points[index-1].LongitudeDegrees, points[index].LatitudeDegrees, points[index].LongitudeDegrees)
		}
		answer["first_at"] = points[0].MeasuredAt.In(here.where).Format("15:04:05")
		answer["last_at"] = points[len(points)-1].MeasuredAt.In(here.where).Format("15:04:05")
		answer["distance_travelled_kilometers"] = roundTo(travelledMeters/1000, 2)
		sampled := make([]map[string]any, 0, pathPointsMost)
		for _, index := range sampleIndexes(len(points), pathPointsMost) {
			sampled = append(sampled, map[string]any{
				"time":              points[index].MeasuredAt.In(here.where).Format("15:04:05"),
				"latitude_degrees":  roundTo(points[index].LatitudeDegrees, 5),
				"longitude_degrees": roundTo(points[index].LongitudeDegrees, 5),
			})
		}
		answer["points"] = sampled
		answer["is_truncated"] = len(points) > pathPointsMost
	}
	return untrusted(tools.JSONResult(answer))
}

// sampleIndexes is up to most indexes of a list of count, evenly spread, the
// first and the last among them.
func sampleIndexes(count, most int) []int {
	if count <= most {
		indexes := make([]int, count)
		for index := range indexes {
			indexes[index] = index
		}
		return indexes
	}
	indexes := make([]int, most)
	for index := range indexes {
		indexes[index] = index * (count - 1) / (most - 1)
	}
	return indexes
}

type placesArguments struct {
	PlaceID    string `json:"place_id"`
	PlaceLabel string `json:"place_label"`
	Query      string `json:"query"`
	Person     string `json:"person"`
	Limit      *int   `json:"limit"`
	Offset     *int   `json:"offset"`
}

// runReadPlace answers one place by its id, as a row of the list is.
func runReadPlace(ctx context.Context, here *scope, placeId, person string) (*tools.Result, error) {
	variables := map[string]any{"placeId": placeId}
	userId, err := here.personId(ctx, person, true)
	if err != nil {
		return nil, err
	}
	if userId != nil {
		variables["userId"] = *userId
	}
	var result struct {
		ReadPlace *placeData `json:"ReadPlace"`
	}
	if err := here.operations.Execute(ctx, documentReadPlace, variables, &result); err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	if result.ReadPlace != nil {
		row := here.placeRow(result.ReadPlace)
		row["visit_count"] = result.ReadPlace.VisitCount
		if result.ReadPlace.LastVisitAt != nil {
			row["last_visit"] = here.timeText(result.ReadPlace.LastVisitAt)
		}
		rows = append(rows, row)
	}
	return untrusted(tools.JSONResult(map[string]any{"timezone": here.where.String(), "places": rows}))
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
	if placeId := strings.TrimSpace(arguments.PlaceID); placeId != "" {
		return runReadPlace(ctx, here, placeId, arguments.Person)
	}
	variables := map[string]any{}
	withPaging(variables, arguments.Offset, arguments.Limit)
	if label := strings.TrimSpace(arguments.PlaceLabel); label != "" {
		variables["placeLabel"] = strings.ToLower(label)
	}
	if query := strings.TrimSpace(arguments.Query); query != "" {
		variables["query"] = query
	}
	userId, err := here.personId(ctx, arguments.Person, true)
	if err != nil {
		return nil, err
	}
	if userId != nil {
		variables["userId"] = *userId
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
// that look around a position and were given none.
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

// foundDisplayName is what to call a found place: the person's own name for
// it, else its label word, else the map's name, then the address.
func foundDisplayName(found *foundData) string {
	switch {
	case found.IsNamedByPerson && found.PlaceName != "":
		return found.PlaceName
	case found.PlaceLabel != "":
		return strings.ToUpper(found.PlaceLabel[:1]) + found.PlaceLabel[1:]
	case found.PlaceName != "":
		return found.PlaceName
	}
	return found.AddressText
}

func (self *scope) foundRow(found *foundData) map[string]any {
	row := map[string]any{
		"place_display_name": foundDisplayName(found), "place_name": found.PlaceName, "distance_meters": math.Round(found.DistanceMeters),
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
		row["is_named_by_person"] = found.IsNamedByPerson
		if found.PlaceLabel != "" {
			row["place_label"] = found.PlaceLabel
		}
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

// aroundPoint is the position given, or the person's last reported position.
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
	Limit            *int     `json:"limit"`
	Offset           *int     `json:"offset"`
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

type findAddressArguments struct {
	Query            string   `json:"query"`
	PlaceID          string   `json:"place_id"`
	LatitudeDegrees  *float64 `json:"latitude_degrees"`
	LongitudeDegrees *float64 `json:"longitude_degrees"`
	Limit            *int     `json:"limit"`
}

// runFindAddress is the addresses matching words, nearest first, for a person who
// says where a place is. The rows are the ones nearby and search give, so that
// a match's address_text and coordinates go to save_place as they were found.
func runFindAddress(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[findAddressArguments](call)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		return nil, fmt.Errorf("say which address to look for, in query")
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"query": query}
	if arguments.Limit != nil {
		variables["limitCount"] = *arguments.Limit
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
		SearchAddresses []foundData `json:"SearchAddresses"`
	}
	if err := here.operations.Execute(ctx, documentSearchAddresses, variables, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.SearchAddresses))
	for index := range result.SearchAddresses {
		rows = append(rows, here.foundRow(&result.SearchAddresses[index]))
	}
	return untrusted(tools.JSONResult(map[string]any{"addresses": rows}))
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
	if arguments.PlaceName != nil {
		variables["placeName"] = *arguments.PlaceName
	}
	if arguments.PlaceLabel != nil {
		label := strings.ToLower(strings.TrimSpace(*arguments.PlaceLabel))
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
	if arguments.AddressText != nil {
		if strings.TrimSpace(*arguments.AddressText) == "" && variables["placeId"] == nil {
			return nil, fmt.Errorf("address_text is the address a match of find_address gave, or one the person typed; leave it out to keep the address as it is")
		}
		if strings.TrimSpace(*arguments.AddressText) != "" && variables["placeId"] == nil && (variables["latitudeDegrees"] == nil || variables["longitudeDegrees"] == nil) {
			return nil, fmt.Errorf("give the position of the address too: latitude_degrees and longitude_degrees of the match")
		}
		variables["addressText"] = strings.TrimSpace(*arguments.AddressText)
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
	return tools.JSONResult(map[string]any{"is_forgotten": true, "place_id": place})
}

type pauseArguments struct {
	EndedAt string `json:"ended_at"`
}

// runPause stops recording where the person is, until the time given or until
// they resume. The API refuses an end that is not ahead or is too far ahead.
func runPause(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[pauseArguments](call)
	if err != nil {
		return nil, err
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	endedAt, err := here.moment(arguments.EndedAt)
	if err != nil {
		return nil, err
	}
	if endedAt != nil {
		variables["endedAt"] = *endedAt
	}
	var result struct {
		PauseLocation pauseData `json:"PauseLocation"`
	}
	if err := here.operations.Execute(ctx, documentPause, variables, &result); err != nil {
		return nil, err
	}
	answer := map[string]any{"is_paused": true, "timezone": here.where.String(), "paused_since": here.timeText(&result.PauseLocation.StartedAt)}
	if result.PauseLocation.EndedAt == nil {
		answer["paused_until"] = "the person resumes"
	} else {
		answer["paused_until"] = here.timeText(result.PauseLocation.EndedAt)
	}
	return tools.JSONResult(answer)
}

// runResume ends the pause now. It widens what is recorded and shown to the
// people the location is shared with, so it waits for the person.
func runResume(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	if !call.Confirmed {
		return nil, fmt.Errorf("resume needs the person's confirmation")
	}
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	var result struct {
		ResumeLocation bool `json:"ResumeLocation"`
	}
	if err := here.operations.Execute(ctx, documentResume, nil, &result); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"is_recording": true, "was_paused": result.ResumeLocation})
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
	if arguments.AccuracyMeters != nil {
		variables["accuracyMeters"] = *arguments.AccuracyMeters
	}
	var result struct {
		CheckIn visitData `json:"CheckIn"`
	}
	if err := here.operations.Execute(ctx, documentCheckIn, variables, &result); err != nil {
		return nil, err
	}
	if result.CheckIn.Place == nil {
		return tools.JSONResult(map[string]any{"is_checked_in": true})
	}
	answer := here.placeRow(result.CheckIn.Place)
	answer["is_checked_in"] = true
	answer["arrived"] = here.timeText(result.CheckIn.ArrivedAt)
	// Coordinates alone make a place called after its city, named from
	// OpenStreetMap right after; say so, so that it is not read as the name.
	place := result.CheckIn.Place
	if place.VisitCount <= 1 && !place.IsNamedByPerson && place.OSMName == "" && place.AddressText == "" {
		answer["is_new_place_named_shortly"] = true
	}
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
		EraseLocationHistory struct {
			PointCount int `json:"pointCount"`
			VisitCount int `json:"visitCount"`
			PlaceCount int `json:"placeCount"`
		} `json:"EraseLocationHistory"`
	}
	if err := here.operations.Execute(ctx, documentErase, variables, &result); err != nil {
		return nil, err
	}
	erased := result.EraseLocationHistory
	return tools.JSONResult(map[string]any{"erased_position_count": erased.PointCount, "erased_visit_count": erased.VisitCount, "erased_place_count": erased.PlaceCount})
}

func runPeople(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	here, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	if err := here.checkOthersAllowed(); err != nil {
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
		row := map[string]any{"person": each.Username, "name": each.Name, "share_level": each.ShareLevel}
		if each.Pause != nil {
			for key, value := range here.pausedRow(each.Name, each.Pause) {
				row[key] = value
			}
			rows = append(rows, row)
			continue
		}
		if each.LatestPoint != nil {
			row["position"] = here.position(each.LatestPoint)
		}
		row["at"] = here.openVisitRow(each.OpenVisit)
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
	return tools.JSONResult(map[string]any{"is_unshared": true})
}

// pagePath is the Location page of the dashboard.
const pagePath = "/location"

// placeNamesWords says where place names come from and what leaves the server.
const placeNamesWords = "Place names come from OpenStreetMap: your server sends the position of each new place to the place provider to look up its name."

// runSetUp takes the dashboard the person reads this in to the Location page,
// or says where to find it where there is no dashboard to take.
func runSetUp(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	showing, ok := tools.MustRun(ctx).(tools.Showing)
	if !ok || !showing.CanShowPages() {
		return tools.JSONResult(map[string]any{
			"is_shown": false,
			"setup_instructions": "Location is in the side menu of the dashboard, and in the TeaNode app on their iPhone, which is what records where they go. " +
				placeNamesWords,
		})
	}
	showing.ShowPage(pagePath)
	result, err := tools.JSONResult(map[string]any{
		"is_shown":  true,
		"page_path": pagePath,
		"setup_instructions": "Location is open. Recording where they go happens in the TeaNode app on their iPhone: install it, then allow Location when it asks. " +
			placeNamesWords,
	})
	if err != nil {
		return nil, err
	}
	result.Note = "Opened " + pagePath
	return result, nil
}

// placeNameOf is the name of one of the person's places, for a card, or empty
// when it cannot be looked up. It reads the first page of places, which holds
// the place for anybody but a person with a great many.
func placeNameOf(ctx context.Context, placeId string) string {
	placeId = strings.TrimSpace(placeId)
	if placeId == "" {
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
		if place.ID == placeId {
			return placeDisplayName(&place)
		}
	}
	return ""
}
