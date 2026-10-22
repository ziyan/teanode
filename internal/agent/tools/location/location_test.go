package location

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// routing answers each document with the answer registered for the operation
// it names, and keeps the variables it was sent.
type routing struct {
	lock    sync.Mutex
	answers map[string]string
	sent    map[string]map[string]any
}

func (self *routing) Permissions() *models.EffectivePermissions {
	return &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionLocationUse, models.PermissionCircleUse}}
}

func (self *routing) Execute(_ context.Context, document string, variables map[string]any, result any) error {
	self.lock.Lock()
	defer self.lock.Unlock()
	data := map[string]json.RawMessage{}
	for operation, answer := range self.answers {
		if strings.Contains(document, operation+"(") || strings.Contains(document, operation+" ") || strings.Contains(document, operation+"\n") {
			data[operation] = json.RawMessage(answer)
			if self.sent == nil {
				self.sent = map[string]map[string]any{}
			}
			self.sent[operation] = variables
		}
	}
	if result == nil {
		return nil
	}
	encoded, _ := json.Marshal(data)
	return json.Unmarshal(encoded, result)
}

// fakeRun is a run whose operations are these, in the person's zone. Where it
// has an answer for the browser, it is a run that can locate.
type fakeRun struct {
	tools.Run
	operations tools.Operations

	// locate is what asking the browser gives; nil makes it a run that cannot
	// ask.
	locate    func() (*tools.BrowserLocation, error)
	shownPath string
	canShow   bool
}

func (self *fakeRun) Operations() tools.Operations         { return self.operations }
func (self *fakeRun) Configuration() *config.Configuration { return config.Default() }
func (self *fakeRun) Owner() *models.User                  { return &models.User{Username: "ada", Timezone: "UTC"} }

// IsStartedByPerson makes this a run the person typed; unattendedRun is the
// one that something else started.
func (self *fakeRun) IsStartedByPerson() bool { return true }

type locatingRun struct{ *fakeRun }

func (self locatingRun) CanLocate() bool { return true }
func (self locatingRun) Locate(context.Context, string) (*tools.BrowserLocation, error) {
	return self.locate()
}

func (self *fakeRun) CanShowPages() bool   { return self.canShow }
func (self *fakeRun) ShowPage(path string) { self.shownPath = path }

func runOf(operations tools.Operations) *fakeRun { return &fakeRun{operations: operations} }

// act runs one action of the location tool as the model does: through the
// merged tool, naming the action.
func act(t *testing.T, run tools.Run, action, arguments string, confirmed bool) (map[string]any, *tools.Result, error) {
	t.Helper()
	merged := tools.Build().Get("location")
	if merged == nil {
		t.Fatal("no location tool")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(arguments), &fields); err != nil {
		t.Fatalf("%s: %s", err, arguments)
	}
	fields["action"] = action
	encoded, _ := json.Marshal(fields)
	result, err := merged.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call", Arguments: encoded, Confirmed: confirmed})
	if err != nil {
		return nil, nil, err
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		t.Fatalf("%s: %s", err, result.Content)
	}
	return answer, result, nil
}

func storedLocation(measuredAt time.Time) string {
	return fmt.Sprintf(`{"userId":"u-ada","username":"ada","name":"Ada","shareLevel":"owner",
		"latestPoint":{"latitudeDegrees":40.0005,"longitudeDegrees":-75.0003,"accuracyMeters":12,"measuredAt":%q,"locationSource":"phone"},
		"openVisit":{"arrivedAt":%q,"place":{"id":"p1","placeName":"Maple Park","placeLabel":"","visitCount":3}}}`,
		measuredAt.Format(time.RFC3339), measuredAt.Add(-25*time.Minute).Format(time.RFC3339))
}

// Where nobody can be asked, now is the last position kept, with its age and
// the place; and with nothing kept it says how to get Location going.
func TestNowAnswersWhatIsKeptWhereNobodyCanBeAsked(t *testing.T) {
	operations := &routing{answers: map[string]string{"GetLocation": storedLocation(time.Now().Add(-4 * time.Minute))}}
	answer, result, err := act(t, runOf(operations), "now", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	position := answer["position"].(map[string]any)
	if position["latitude_degrees"] != 40.0005 || position["age"] != "4 minutes ago" || position["reported_by"] != "phone" {
		t.Errorf("the position and how old it is: %v", position)
	}
	if at := answer["at"].(map[string]any); at["place_display_name"] != "Maple Park" || at["is_in_progress"] != true {
		t.Errorf("the place they are at: %v", at)
	}
	if !result.Untrusted {
		t.Error("a place name is data, not an instruction")
	}

	nothing := &routing{answers: map[string]string{"GetLocation": `{"userId":"u-ada","username":"ada","shareLevel":"owner","latestPoint":null,"openVisit":null}`}}
	if _, _, err := act(t, runOf(nothing), "now", `{}`, false); err == nil || !strings.Contains(err.Error(), "iPhone") {
		t.Errorf("nothing kept should point at the app: %v", err)
	}
}

// Where a browser can be asked it is, and the fresh position is the answer;
// when the asking fails the last one kept is, and says so.
func TestNowAsksWhereItCanAndFallsBack(t *testing.T) {
	operations := &routing{answers: map[string]string{"GetLocation": storedLocation(time.Now())}}
	asked := 0
	run := locatingRun{&fakeRun{operations: operations, locate: func() (*tools.BrowserLocation, error) {
		asked++
		return &tools.BrowserLocation{LatitudeDegrees: 41.25, LongitudeDegrees: -74.5, AccuracyMeters: 30, MeasuredAt: time.Now()}, nil
	}}}
	answer, _, err := act(t, run, "now", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if asked != 1 || answer["asked_just_now"] != true || answer["position"].(map[string]any)["latitude_degrees"] != 41.25 {
		t.Errorf("the fresh position: asked %d, %v", asked, answer)
	}

	failing := locatingRun{&fakeRun{operations: operations, locate: func() (*tools.BrowserLocation, error) {
		return nil, fmt.Errorf("location_unavailable: the person did not allow it")
	}}}
	answer, _, err = act(t, failing, "now", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if answer["asked_just_now"] != false || !strings.Contains(fmt.Sprint(answer["note"]), "did not allow") {
		t.Errorf("the fallback says why: %v", answer)
	}
}

// Visits are read in the person's zone and paged.
func TestVisitsAreReadInTheirZoneAndPaged(t *testing.T) {
	operations := &routing{answers: map[string]string{"ListLocationVisits": `{"nextOffset":20,"placeVisits":[
		{"arrivedAt":"2026-10-17T09:30:30Z","departedAt":"2026-10-17T10:15:40Z","durationSeconds":2710,"photoCount":3,"photosTakenBefore":"2026-10-17T10:15:40.7Z","place":{"id":"p1","placeName":"Maple Park","osmName":"Maple Park","placeLabel":"home"}},
		{"arrivedAt":"2026-10-17T11:00:00Z","place":{"id":"p2","placeName":"Elm Library"}}]}`}}
	answer, result, err := act(t, runOf(operations), "visits", `{"from":"2026-10-17","until":"2026-10-19","place_label":"Home","limit":20}`, false)
	if err != nil {
		t.Fatal(err)
	}
	visits := answer["visits"].([]any)
	first, second := visits[0].(map[string]any), visits[1].(map[string]any)
	if first["place_display_name"] != "Home" || first["osm_name"] != "Maple Park" || first["place_label"] != "home" || first["visit_duration"] != "45 minutes" || first["arrived"] != "2026-10-17 09:30" {
		t.Errorf("a finished visit: %v", first)
	}
	if first["photo_count"] != float64(3) || second["photo_count"] != nil {
		t.Errorf("the photo count is told when there are photos: %v %v", first, second)
	}
	// The window is given exactly, a fraction of a second included, and reads
	// back as the same instants.
	for key, wanted := range map[string]string{"photos_taken_after": "2026-10-17T09:30:30Z", "photos_taken_before": "2026-10-17T10:15:40.7Z"} {
		given, _ := first[key].(string)
		parsed, err := tools.ParseTime(given, time.UTC, time.Now())
		if err != nil || parsed.UTC().Format(time.RFC3339Nano) != wanted {
			t.Errorf("%s is %q, read back as %v (%v)", key, given, parsed, err)
		}
	}
	if second["left"] != "still there" {
		t.Errorf("a visit in progress: %v", second)
	}
	if answer["next_offset"] != float64(20) || answer["has_more"] != true {
		t.Errorf("it says there is more: %v", answer)
	}
	sent := operations.sent["ListLocationVisits"]
	if sent["from"] != "2026-10-17T00:00:00Z" || sent["until"] != "2026-10-19T00:00:00Z" || sent["placeLabel"] != "home" || sent["limitCount"] != 20 {
		t.Errorf("what was sent: %v", sent)
	}
	if !result.Untrusted {
		t.Error("place names are data")
	}
	if _, _, err := act(t, runOf(operations), "visits", `{"from":"last weekend maybe"}`, false); err == nil {
		t.Error("a date nobody can read should be refused")
	}
}

// Search is around where the person was last, unless told otherwise, and
// names the places that are already theirs.
func TestSearchAndNearbyAreAroundWhereTheyWere(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"GetLocation":      storedLocation(time.Now()),
		"ListNearbyPlaces": `[{"placeName":"Corner Cafe","osmType":"node","osmId":42,"osmCategory":"amenity=cafe","latitudeDegrees":40.0006,"longitudeDegrees":-75.0004,"distanceMeters":15.4}]`,
		"SearchPlaces":     `{"foundPlaces":[{"placeName":"Elm Playground","osmType":"way","osmId":7,"latitudeDegrees":40.01,"longitudeDegrees":-75,"distanceMeters":900,"placeId":"p9"}]}`,
	}}
	answer, _, err := act(t, runOf(operations), "nearby", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["ListNearbyPlaces"]; sent["latitudeDegrees"] != 40.0005 {
		t.Errorf("around the last position: %v", sent)
	}
	cafe := answer["places"].([]any)[0].(map[string]any)
	if cafe["place_name"] != "Corner Cafe" || cafe["osm_id"] != float64(42) || cafe["distance_meters"] != float64(15) {
		t.Errorf("what check_in takes is given back as it is: %v", cafe)
	}

	answer, _, err = act(t, runOf(operations), "search", `{"query":"playground","place_id":"p1","radius_meters":3000}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["SearchPlaces"]; sent["placeId"] != "p1" || sent["radiusMeters"] != 3000.0 || sent["latitudeDegrees"] != nil {
		t.Errorf("around one of their places: %v", sent)
	}
	if found := answer["places"].([]any)[0].(map[string]any); found["place_id"] != "p9" || found["is_the_persons_place"] != true {
		t.Errorf("already theirs: %v", found)
	}

	operations.answers["SearchPlaces"] = `{"foundPlaces":[{"placeName":"Elm Playground","osmType":"way","osmId":7,"distanceMeters":900,"placeId":"p9","placeLabel":"gym","isNamedByPerson":false}]}`
	answer, _, err = act(t, runOf(operations), "search", `{"query":"playground"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if found := answer["places"].([]any)[0].(map[string]any); found["place_display_name"] != "Gym" || found["place_name"] != "Elm Playground" || found["place_label"] != "gym" || found["is_named_by_person"] != false {
		t.Errorf("a labeled place is called by its label word: %v", found)
	}
	if _, _, err := act(t, runOf(operations), "search", `{}`, false); err == nil {
		t.Error("search needs words")
	}
}

// Saving sends only what was given; forgetting and erasing wait on the person.
func TestWritesAndTheirRisks(t *testing.T) {
	merged := tools.Build().Get("location")
	risks := map[string]tools.Risk{
		"now": tools.RiskRead, "visits": tools.RiskRead, "path": tools.RiskRead, "places": tools.RiskRead, "nearby": tools.RiskRead, "search": tools.RiskRead, "find_address": tools.RiskRead,
		"people": tools.RiskRead, "shares": tools.RiskRead, "set_up": tools.RiskRead,
		"save_place": tools.RiskWrite, "check_in": tools.RiskWrite, "unshare": tools.RiskWrite,
		"forget_place": tools.RiskDestructive, "erase": tools.RiskDestructive, "share": tools.RiskGranting,
	}
	for action, want := range risks {
		if got := merged.RiskFor(json.RawMessage(`{"action":"` + action + `"}`)); got != want {
			t.Errorf("%s is %s, want %s", action, got, want)
		}
	}
	for _, action := range []string{"save_place", "forget_place", "check_in", "erase", "share", "unshare"} {
		if line := merged.PreviewLine(context.Background(), json.RawMessage(`{"action":"`+action+`"}`)); strings.TrimSpace(line) == "" || strings.Contains(line, `":`) {
			t.Errorf("%s: the card says %q", action, line)
		}
	}

	operations := &routing{answers: map[string]string{
		"SavePlace":            `{"id":"p1","placeName":"Grandma's","placeLabel":"family","radiusMeters":75,"isNamedByPerson":true}`,
		"ForgetPlace":          `true`,
		"EraseLocationHistory": `{"pointCount":3,"visitCount":1,"placeCount":0}`,
	}}
	answer, _, err := act(t, runOf(operations), "save_place", `{"place_id":"p1","place_name":"Grandma's","place_label":"family"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	sent := operations.sent["SavePlace"]
	if answer["place_display_name"] != "Grandma's" || sent["placeName"] != "Grandma's" || sent["placeLabel"] != "family" || sent["radiusMeters"] != nil {
		t.Errorf("only what was given: %v %v", answer, sent)
	}
	if _, _, err := act(t, runOf(operations), "save_place", `{"place_name":"Nowhere"}`, false); err == nil {
		t.Error("a new place needs coordinates")
	}

	if _, _, err := act(t, runOf(operations), "forget_place", `{"place_id":"p1"}`, false); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Errorf("forget_place unconfirmed: %v", err)
	}
	if _, _, err := act(t, runOf(operations), "forget_place", `{"place_id":"p1"}`, true); err != nil {
		t.Error(err)
	}
	if _, _, err := act(t, runOf(operations), "erase", `{}`, false); err == nil {
		t.Error("erase unconfirmed")
	}
	answer, _, err = act(t, runOf(operations), "erase", `{"until":"2026-10-01"}`, true)
	if err != nil || answer["erased_position_count"] != float64(3) {
		t.Errorf("erase: %v %v", answer, err)
	}
}

// Checking in at a found place sends what the map called it.
func TestCheckInSendsAFoundPlaceAsItWasFound(t *testing.T) {
	operations := &routing{answers: map[string]string{"CheckIn": `{"arrivedAt":"2026-10-17T12:00:00Z","place":{"id":"p5","placeName":"Corner Cafe"}}`}}
	answer, _, err := act(t, runOf(operations), "check_in", `{"place_name":"Corner Cafe","latitude_degrees":40.0006,"longitude_degrees":-75.0004,"osm_type":"node","osm_id":42,"osm_category":"amenity=cafe"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	sent := operations.sent["CheckIn"]
	if sent["osmType"] != "node" || sent["osmId"] != int64(42) || sent["placeName"] != "Corner Cafe" || sent["placeId"] != nil {
		t.Errorf("what the map called it: %v", sent)
	}
	if answer["is_checked_in"] != true || answer["place_id"] != "p5" {
		t.Errorf("the answer: %v", answer)
	}
	if _, _, err := act(t, runOf(operations), "check_in", `{"latitude_degrees":40,"longitude_degrees":-75,"accuracy_meters":850.5}`, false); err != nil {
		t.Fatal(err)
	}
	if sent = operations.sent["CheckIn"]; sent["accuracyMeters"] != 850.5 {
		t.Errorf("the accuracy was sent: %v", sent)
	}
	if _, _, err := act(t, runOf(operations), "check_in", `{}`, false); err == nil {
		t.Error("a check-in needs a place")
	}
}

// People are read by username, and set_up takes the dashboard to Location.
func TestPeopleAndSettingUp(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListSharedLocations": `[{"userId":"u-ben","username":"ben","name":"Ben","shareLevel":"history","latestPoint":null,"openVisit":null}]`,
		"ListLocationVisits":  `{"placeVisits":[]}`,
	}}
	answer, _, err := act(t, runOf(operations), "people", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if person := answer["people"].([]any)[0].(map[string]any); person["person"] != "ben" || person["share_level"] != "history" {
		t.Errorf("who shares: %v", person)
	}
	if _, _, err := act(t, runOf(operations), "visits", `{"person":"ben"}`, false); err != nil {
		t.Fatal(err)
	}
	if operations.sent["ListLocationVisits"]["userId"] != "u-ben" {
		t.Errorf("ben's account: %v", operations.sent["ListLocationVisits"])
	}
	if _, _, err := act(t, runOf(operations), "visits", `{"person":"stranger"}`, false); err == nil {
		t.Error("somebody who shares nothing")
	}

	run := runOf(operations)
	run.canShow = true
	answer, _, err = act(t, run, "set_up", `{}`, false)
	if err != nil || run.shownPath != "/location" || answer["is_shown"] != true || answer["page_path"] != "/location" || !strings.Contains(fmt.Sprint(answer["setup_instructions"]), "OpenStreetMap") {
		t.Errorf("the dashboard was taken to Location: %v %q %v", answer, run.shownPath, err)
	}
	answer, _, err = act(t, runOf(operations), "set_up", `{}`, false)
	if err != nil || answer["is_shown"] != false {
		t.Errorf("where there is no dashboard it says where Location is: %v %v", answer, err)
	}
}

// Sharing resolves who it is for through the circles, as the calendar does.
func TestShareResolvesWhoItIsFor(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCirclePeople": `[{"userId":"u-ben","username":"ben"}]`,
		"ListCircles":      `[]`,
		"ShareLocation":    `{"id":"s1","shareLevel":"current"}`,
	}}
	answer, _, err := act(t, runOf(operations), "share", `{"with":"ben","share_level":"current"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["ShareLocation"]; sent["granteeUserId"] != "u-ben" || sent["shareLevel"] != "current" || answer["share_id"] != "s1" {
		t.Errorf("shared with ben: %v %v", sent, answer)
	}
	if _, _, err := act(t, runOf(operations), "share", `{"with":"nobody","share_level":"history"}`, true); err == nil {
		t.Error("nobody to share with")
	}
}

// A person who shares only where they are now is said to, in those words,
// rather than answered as if the place did not exist.
func TestHistoryOfSomebodySharingOnlyNowIsRefusedInWords(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListSharedLocations": `[{"userId":"u-cleo","username":"cleo","name":"Cleo","shareLevel":"current","latestPoint":null,"openVisit":null}]`,
		"ListLocationVisits":  `{"placeVisits":[]}`,
		"GetLocation":         `{"userId":"u-cleo","username":"cleo","shareLevel":"current","latestPoint":null,"openVisit":{"place":{"id":"p1","placeName":"Elm Cafe"}}}`,
	}}
	for _, action := range []string{"visits", "path", "places"} {
		if _, _, err := act(t, runOf(operations), action, `{"person":"cleo"}`, false); err == nil || !strings.Contains(err.Error(), "cleo shares only where they are now") {
			t.Errorf("%s: %v", action, err)
		}
	}
	if _, _, err := act(t, runOf(operations), "now", `{"person":"cleo"}`, false); err != nil {
		t.Errorf("where they are now is shared: %v", err)
	}
}

// chatRun is a run whose words are posted to the people of a chat.
type chatRun struct{ *fakeRun }

func (self chatRun) IsAnsweringInChat() bool { return true }

// In a chat the tool answers only about the person's own location: what
// somebody shared with the person is not repeated to everybody there.
func TestAnotherPersonsLocationIsNotRepeatedIntoAChat(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListSharedLocations": `[{"userId":"u-ben","username":"ben","name":"Ben","shareLevel":"history","latestPoint":null,"openVisit":null}]`,
		"ListLocationVisits":  `{"placeVisits":[]}`,
		"GetLocation":         storedLocation(time.Now().Add(-4 * time.Minute)),
	}}
	run := chatRun{runOf(operations)}
	if _, _, err := act(t, run, "people", `{}`, false); err == nil || !strings.Contains(err.Error(), "not repeated into a chat") {
		t.Errorf("people in a chat: %v", err)
	}
	for _, action := range []string{"now", "visits", "path", "places"} {
		if _, _, err := act(t, run, action, `{"person":"ben"}`, false); err == nil || !strings.Contains(err.Error(), "not repeated into a chat") {
			t.Errorf("%s about somebody else in a chat: %v", action, err)
		}
	}
	if _, _, err := act(t, run, "now", `{}`, false); err != nil {
		t.Errorf("the person's own location is read in a chat: %v", err)
	}
	if _, _, err := act(t, run, "visits", `{}`, false); err != nil {
		t.Errorf("the person's own visits are read in a chat: %v", err)
	}
}

// unattendedRun is a run that an inbound mail, a webhook or a schedule of the
// agent's began, not the person.
type unattendedRun struct{ *fakeRun }

func (self unattendedRun) IsStartedByPerson() bool { return false }

// A run the person did not start reads only the person's own location:
// content that began it could have somebody else's read out to its writer.
func TestAnotherPersonsLocationIsNotReadInARunThePersonDidNotStart(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListSharedLocations": `[{"userId":"u-ben","username":"ben","name":"Ben","shareLevel":"history","latestPoint":null,"openVisit":null}]`,
		"ListLocationVisits":  `{"placeVisits":[]}`,
		"GetLocation":         storedLocation(time.Now().Add(-4 * time.Minute)),
	}}
	run := unattendedRun{runOf(operations)}
	if _, _, err := act(t, run, "people", `{}`, false); err == nil || !strings.Contains(err.Error(), "started themselves") {
		t.Errorf("people in a run nobody started: %v", err)
	}
	for _, action := range []string{"now", "visits", "path", "places"} {
		if _, _, err := act(t, run, action, `{"person":"ben"}`, false); err == nil || !strings.Contains(err.Error(), "started themselves") {
			t.Errorf("%s about somebody else in a run nobody started: %v", action, err)
		}
	}
	if _, _, err := act(t, run, "now", `{}`, false); err != nil {
		t.Errorf("the person's own location is read: %v", err)
	}
	if _, _, err := act(t, run, "visits", `{}`, false); err != nil {
		t.Errorf("the person's own visits are read: %v", err)
	}
}

// A page is asked for as given, so that the API's defaults (50, at most 200
// for visits and places; 20, at most 50 for a search) and its refusals are the
// ones said.
func TestPagingIsPassedOnAsGiven(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListLocationVisits": `{"placeVisits":[]}`,
		"ListPlaces":         `{"places":[]}`,
		"SearchPlaces":       `{"foundPlaces":[]}`,
	}}
	if answer, _, err := act(t, runOf(operations), "visits", `{}`, false); err != nil || answer["has_more"] != false {
		t.Fatalf("a last page says so: %v %v", answer, err)
	}
	if sent := operations.sent["ListLocationVisits"]; sent["limitCount"] != nil || sent["offset"] != nil {
		t.Errorf("nothing asked for, so the API's defaults: %v", sent)
	}
	if _, _, err := act(t, runOf(operations), "places", `{"limit":200,"offset":50}`, false); err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["ListPlaces"]; sent["limitCount"] != 200 || sent["offset"] != 50 {
		t.Errorf("200 is passed on and not cut to 50: %v", sent)
	}
	if _, _, err := act(t, runOf(operations), "search", `{"query":"bakery","limit":0}`, false); err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["SearchPlaces"]; sent["limitCount"] != 0 {
		t.Errorf("a number the API refuses is left to it to refuse: %v", sent)
	}
}

// A place is called by the name the person gave it, else by its label, else by
// what the map calls it, else by its address, else by its city.
func TestAPlaceIsCalledByTheNameThePersonGaveThenTheLabelThenTheMap(t *testing.T) {
	cases := map[string]placeData{
		"Grandma's":    {PlaceName: "Grandma's", IsNamedByPerson: true, PlaceLabel: "family", OSMName: "Elm Court"},
		"Family":       {PlaceName: "Elm Court", PlaceLabel: "family", OSMName: "Elm Court"},
		"Elm Court":    {PlaceName: "Elm Court", OSMName: "Elm Court", AddressText: "1 Elm Street"},
		"1 Elm Street": {PlaceName: "Exampleton", AddressText: "1 Elm Street"},
		"Exampleton":   {PlaceName: "Exampleton"},
	}
	for expected, place := range cases {
		if got := placeDisplayName(&place); got != expected {
			t.Errorf("%+v is called %q, expected %q", place, got, expected)
		}
	}
}

// Finding an address asks for matches anywhere and gives back what save_place
// takes: the address and the position of each match.
func TestFindAddressGivesWhatSavePlaceTakes(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"SearchAddresses": `[{"placeName":"6 Rue Example","addressText":"6 Rue Example, Exampleton, Testland","latitudeDegrees":40.5,"longitudeDegrees":-75.5,"distanceMeters":1200.4},
			{"placeName":"8 Rue Example","addressText":"8 Rue Example, Exampleton, Testland","latitudeDegrees":40.6,"longitudeDegrees":-75.6,"distanceMeters":0,"placeId":"p9","placeLabel":"work"}]`,
		"SavePlace": `{"id":"p1","placeName":"6 Rue Example","placeLabel":"home","radiusMeters":75,"isNamedByPerson":false,"addressText":"6 Rue Example, Exampleton, Testland"}`,
	}}
	answer, _, err := act(t, runOf(operations), "find_address", `{"query":" 6 rue example ","limit":3}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["SearchAddresses"]; sent["query"] != "6 rue example" || sent["limitCount"] != 3 {
		t.Errorf("the words and the count: %v", sent)
	}
	addresses := answer["addresses"].([]any)
	first, second := addresses[0].(map[string]any), addresses[1].(map[string]any)
	if len(addresses) != 2 || first["address_text"] != "6 Rue Example, Exampleton, Testland" || first["latitude_degrees"] != 40.5 || first["longitude_degrees"] != -75.5 {
		t.Errorf("what save_place takes: %v", addresses)
	}
	if second["place_id"] != "p9" || second["is_the_persons_place"] != true {
		t.Errorf("a match that is already their place says so: %v", second)
	}
	if _, _, err := act(t, runOf(operations), "find_address", `{}`, false); err == nil {
		t.Error("an address search needs words")
	}
	if _, _, err := act(t, runOf(operations), "find_address", `{"query":"rue example","place_id":" p1 "}`, false); err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["SearchAddresses"]; sent["placeId"] != "p1" || sent["latitudeDegrees"] != nil {
		t.Errorf("the place the matches are nearest to: %v", sent)
	}
	if _, _, err := act(t, runOf(operations), "find_address", `{"query":"rue example","latitude_degrees":40.1,"longitude_degrees":-75.2}`, false); err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["SearchAddresses"]; sent["placeId"] != nil || sent["latitudeDegrees"] != 40.1 || sent["longitudeDegrees"] != -75.2 {
		t.Errorf("the point the matches are nearest to: %v", sent)
	}
	if _, _, err := act(t, runOf(operations), "find_address", `{"query":"rue example","latitude_degrees":40.1}`, false); err == nil {
		t.Error("half a position is refused")
	}

	saved, _, err := act(t, runOf(operations), "save_place", `{"place_id":"p1","place_label":"home","latitude_degrees":40.5,"longitude_degrees":-75.5,"address_text":" 6 Rue Example, Exampleton, Testland "}`, false)
	if err != nil {
		t.Fatal(err)
	}
	sent := operations.sent["SavePlace"]
	if sent["addressText"] != "6 Rue Example, Exampleton, Testland" || sent["latitudeDegrees"] != 40.5 || sent["longitudeDegrees"] != -75.5 || sent["placeLabel"] != "home" || sent["placeName"] != nil {
		t.Errorf("the address and its position were sent: %v", sent)
	}
	if saved["place_display_name"] != "Home" {
		t.Errorf("the place is called by its label: %v", saved)
	}
	if _, _, err := act(t, runOf(operations), "save_place", `{"place_label":"home","address_text":"6 Rue Example"}`, false); err == nil || !strings.Contains(err.Error(), "position of the address") {
		t.Errorf("an address without its position or a place: %v", err)
	}
	// An address no map knows is kept as typed on a place that stays.
	delete(operations.sent, "SavePlace")
	if _, _, err := act(t, runOf(operations), "save_place", `{"place_id":"p1","address_text":" 12 Lane Example "}`, false); err != nil {
		t.Errorf("an address as typed: %v", err)
	}
	if sent := operations.sent["SavePlace"]; sent["addressText"] != "12 Lane Example" || sent["latitudeDegrees"] != nil || sent["placeId"] != "p1" {
		t.Errorf("only the address was sent: %v", sent)
	}
	// A blank address with a place takes the typed one away; without one it means nothing.
	if _, _, err := act(t, runOf(operations), "save_place", `{"place_id":"p1","address_text":"  "}`, false); err != nil {
		t.Errorf("a blank address takes the typed one away: %v", err)
	}
	if sent := operations.sent["SavePlace"]; sent["addressText"] != "" {
		t.Errorf("the blank address was sent: %v", sent)
	}
	if _, _, err := act(t, runOf(operations), "save_place", `{"place_label":"home","latitude_degrees":40.5,"longitude_degrees":-75.5,"address_text":"  "}`, false); err == nil {
		t.Error("a blank address makes no place")
	}
	// Making home from an address needs no place_id.
	if _, _, err := act(t, runOf(operations), "save_place", `{"place_label":"home","latitude_degrees":40.5,"longitude_degrees":-75.5,"address_text":"6 Rue Example, Exampleton, Testland"}`, false); err != nil {
		t.Errorf("a place made from an address: %v", err)
	}
	merged := tools.Build().Get("location")
	line := merged.PreviewLine(context.Background(), json.RawMessage(`{"action":"save_place","latitude_degrees":40.5,"longitude_degrees":-75.5,"address_text":"6 Rue Example"}`))
	if !strings.Contains(line, "6 Rue Example") || strings.Contains(line, `":`) {
		t.Errorf("the card says %q", line)
	}
}

// dayOfPositions answers ListLocationPoints with the positions inside the
// window asked, as the server does, and keeps each window asked.
type dayOfPositions struct {
	routing
	measuredTimes []time.Time
	positions     []string
	asked         [][2]string
}

func (self *dayOfPositions) Execute(ctx context.Context, document string, variables map[string]any, result any) error {
	from, _ := time.Parse(time.RFC3339, variables["from"].(string))
	until, _ := time.Parse(time.RFC3339, variables["until"].(string))
	self.asked = append(self.asked, [2]string{variables["from"].(string), variables["until"].(string)})
	var inside []string
	for index, measuredAt := range self.measuredTimes {
		if !measuredAt.Before(from) && measuredAt.Before(until) {
			inside = append(inside, self.positions[index])
		}
	}
	self.answers = map[string]string{"ListLocationPoints": "[" + strings.Join(inside, ",") + "]"}
	return self.routing.Execute(ctx, document, variables, result)
}

// A day's path is read for the day in the person's zone, a quarter at a time,
// and answers the count, the first and last time, the distance and a sample
// of at most 60.
func TestPathAnswersADayWithASample(t *testing.T) {
	operations := &dayOfPositions{}
	for index := 0; index < 100; index++ {
		// From 04:00 for 100 minutes, across the first quarter's end.
		measuredAt := time.Date(2026, 10, 17, 4, 50, 0, 0, time.UTC).Add(time.Duration(index) * time.Minute)
		operations.measuredTimes = append(operations.measuredTimes, measuredAt)
		operations.positions = append(operations.positions, fmt.Sprintf(`{"latitudeDegrees":%v,"longitudeDegrees":-75,"accuracyMeters":10,"measuredAt":%q,"locationSource":"phone"}`, 40+float64(index)*0.001, measuredAt.Format(time.RFC3339)))
	}
	answer, result, err := act(t, runOf(operations), "path", `{"day":"2026-10-17"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if answer["point_count"] != float64(100) || answer["first_at"] != "04:50:00" || answer["last_at"] != "06:29:00" || answer["is_truncated"] != true {
		t.Errorf("the day: %v", answer)
	}
	// 99 steps of 0.001 degrees of latitude are about 11 kilometers.
	if kilometers := answer["distance_travelled_kilometers"].(float64); kilometers < 10.9 || kilometers > 11.1 {
		t.Errorf("the distance: %v", kilometers)
	}
	sampled := answer["points"].([]any)
	if len(sampled) != 60 || sampled[0].(map[string]any)["time"] != "04:50:00" || sampled[59].(map[string]any)["time"] != "06:29:00" {
		t.Errorf("the sample keeps the first and the last: %d %v", len(sampled), sampled)
	}
	wanted := [][2]string{
		{"2026-10-17T00:00:00Z", "2026-10-17T06:00:00Z"},
		{"2026-10-17T06:00:00Z", "2026-10-17T12:00:00Z"},
		{"2026-10-17T12:00:00Z", "2026-10-17T18:00:00Z"},
		{"2026-10-17T18:00:00Z", "2026-10-18T00:00:00Z"},
	}
	if fmt.Sprint(operations.asked) != fmt.Sprint(wanted) {
		t.Errorf("what was asked: %v", operations.asked)
	}
	if !result.Untrusted {
		t.Error("a path is the person's data")
	}
	if _, _, err := act(t, runOf(operations), "path", `{"day":"yesterday"}`, false); err == nil {
		t.Error("a day nobody can read should be refused")
	}
}

func TestPathOfADayWithNoPositionsSaysSo(t *testing.T) {
	operations := &routing{answers: map[string]string{"ListLocationPoints": `[]`}}
	answer, _, err := act(t, runOf(operations), "path", `{}`, false)
	if err != nil || answer["point_count"] != float64(0) || answer["points"] != nil {
		t.Errorf("%v %v", answer, err)
	}
}

// One place is read by its id, and the id is passed on as given.
func TestPlaceIsReadByItsID(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadPlace": `{"id":"p7","placeName":"Fixture Park","visitCount":4,"lastVisitAt":"2026-10-17T09:30:00Z"}`}}
	answer, _, err := act(t, runOf(operations), "places", `{"place_id":"p7"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	rows := answer["places"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["visit_count"] != float64(4) || rows[0].(map[string]any)["last_visit"] != "2026-10-17 09:30" {
		t.Errorf("the place: %v", answer)
	}
	if operations.sent["ReadPlace"]["placeId"] != "p7" {
		t.Errorf("what was sent: %v", operations.sent["ReadPlace"])
	}
}
