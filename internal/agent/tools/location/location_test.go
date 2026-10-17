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
	if at := answer["at"].(map[string]any); at["place_name"] != "Maple Park" || at["is_in_progress"] != true {
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
		{"arrivedAt":"2026-10-17T09:30:00Z","departedAt":"2026-10-17T10:15:00Z","durationSeconds":2700,"place":{"id":"p1","placeName":"Maple Park","placeLabel":"home"}},
		{"arrivedAt":"2026-10-17T11:00:00Z","place":{"id":"p2","placeName":"Elm Library"}}]}`}}
	answer, result, err := act(t, runOf(operations), "visits", `{"from":"2026-10-17","until":"2026-10-19","place_label":"Home","limit":20}`, false)
	if err != nil {
		t.Fatal(err)
	}
	visits := answer["visits"].([]any)
	first, second := visits[0].(map[string]any), visits[1].(map[string]any)
	if first["place_name"] != "Maple Park" || first["place_label"] != "home" || first["visit_duration"] != "45 minutes" || first["arrived"] != "2026-10-17 09:30" {
		t.Errorf("a finished visit: %v", first)
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
	if _, _, err := act(t, runOf(operations), "search", `{}`, false); err == nil {
		t.Error("search needs words")
	}
}

// Saving sends only what was given; forgetting and erasing wait on the person.
func TestWritesAndTheirRisks(t *testing.T) {
	merged := tools.Build().Get("location")
	risks := map[string]tools.Risk{
		"now": tools.RiskRead, "visits": tools.RiskRead, "places": tools.RiskRead, "nearby": tools.RiskRead, "search": tools.RiskRead,
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
	if answer["place_name"] != "Grandma's" || sent["placeName"] != "Grandma's" || sent["placeLabel"] != "family" || sent["radiusMeters"] != nil {
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
	for _, action := range []string{"visits", "places"} {
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
	for _, action := range []string{"now", "visits", "places"} {
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
