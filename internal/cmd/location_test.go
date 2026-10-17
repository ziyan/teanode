package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Location is on the API, the command line and the agent's location tool under
// the same names. Only a phone reports a position, and a path is for drawing,
// so those two have no tool action, and ReportLocation has no subcommand.
func TestLocationParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.LocationQuery](), reflect.TypeFor[apigraph.LocationMutation](), reflect.TypeFor[apigraph.LocationSubscription]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	table := map[string]struct {
		subcommand string
		action     string
	}{
		"GetLocation":           {"now", "now"},
		"ListLocationVisits":    {"visits", "visits"},
		"ListPlaces":            {"places", "places"},
		"ListNearbyPlaces":      {"nearby", "nearby"},
		"SearchPlaces":          {"search", "search"},
		"ListLocationPoints":    {"path", ""},
		"ReportLocation":        {"", ""},
		"SavePlace":             {"save", "save_place"},
		"ForgetPlace":           {"forget", "forget_place"},
		"CheckIn":               {"checkin", "check_in"},
		"ForgetLocationHistory": {"erase", "erase"},
		"ListSharedLocations":   {"people", "people"},
		"ListLocationShares":    {"shares", "shares"},
		"ShareLocation":         {"share", "share"},
		"UnshareLocation":       {"unshare", "unshare"},
		// A subscription is followed by the dashboard and the app; nothing
		// on a command line or in a conversation follows a map.
		"LocationEvents": {"", ""},
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}

	commands := map[string]*cli.Command{}
	for _, subcommand := range NewLocationCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	actions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "location" {
			continue
		}
		properties, _ := tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				actions[name] = true
			}
		case []any:
			for _, name := range enum {
				actions[name.(string)] = true
			}
		}
	}
	if len(actions) == 0 {
		test.Fatal("the agent has no location tool")
	}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if names.subcommand != "" && commands[names.subcommand] == nil {
			test.Errorf("teanode location has no %s", names.subcommand)
		}
		if names.action != "" && !actions[names.action] {
			test.Errorf("the location tool has no action %s", names.action)
		}
	}
	if !actions["set_up"] {
		test.Error("the location tool cannot open the Location page")
	}
}

// locationServer answers the documents the location commands send with a small
// world and records the variables of each by operation name.
func locationServer(test *testing.T) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	place := `{"id":"p1","userId":"u-ada","placeName":"Maple Park","placeLabel":"","radiusMeters":75,"visitCount":2,"isNamedByPerson":false}`
	answers := map[string]string{
		"GetLocation": `{"userId":"u-ada","username":"ada","name":"Ada","locationShareLevel":"owner",
			"latestPoint":{"latitudeDegrees":40.0005,"longitudeDegrees":-75.0003,"accuracyMeters":12,"measuredAt":"2026-10-17T10:00:00Z","locationSource":"phone"},
			"openVisit":{"id":"v1","arrivedAt":"2026-10-17T09:30:00Z","place":` + place + `}}`,
		"ListLocationVisits": `{"nextOffset":50,"placeVisits":[{"id":"v1","arrivedAt":"2026-10-17T09:30:00Z","departedAt":"2026-10-17T09:50:00Z","durationSeconds":1200,"place":` + place + `},
			{"id":"v2","arrivedAt":"2026-10-17T11:00:00Z","place":` + place + `}]}`,
		"ListPlaces": `{"places":[` + place + `]}`,
		"ListNearbyPlaces": `[{"placeName":"Corner Cafe","osmType":"node","osmId":42,"osmCategory":"amenity=cafe","addressText":"1 Elm Street","latitudeDegrees":40.0006,"longitudeDegrees":-75.0004,"distanceMeters":15},
			{"placeName":"Maple Park","latitudeDegrees":40.0001,"longitudeDegrees":-75.0001,"distanceMeters":90,"placeId":"p1"}]`,
		"SearchPlaces":          `{"foundPlaces":[{"placeName":"Elm Playground","osmType":"way","osmId":7,"distanceMeters":900}]}`,
		"SavePlace":             place,
		"ForgetPlace":           `true`,
		"CheckIn":               `{"id":"v3","arrivedAt":"2026-10-17T12:00:00Z","place":` + place + `}`,
		"ForgetLocationHistory": `{"pointCount":10,"visitCount":2,"placeCount":1}`,
		"ListSharedLocations":   `[{"userId":"u-ben","username":"ben","name":"Ben","locationShareLevel":"history"}]`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		data := map[string]json.RawMessage{}
		for operation, answer := range answers {
			if strings.Contains(document.Query, operation+"(") || strings.Contains(document.Query, operation+" ") || strings.Contains(document.Query, operation+"\n") {
				data[operation] = json.RawMessage(answer)
				lock.Lock()
				calls[operation] = document.Variables
				lock.Unlock()
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"data": data})
	}))
	test.Cleanup(server.Close)
	return server, func(operation string) map[string]any {
		lock.Lock()
		defer lock.Unlock()
		return calls[operation]
	}
}

func runLocationCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewLocationCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "location"}, arguments...))
	})
	return output, err
}

// now says where the person is and the stay in progress.
func TestLocationNowSaysWhereAndAt(test *testing.T) {
	server, _ := locationServer(test)
	output, err := runLocationCommand(test, server, "now")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "40.00050, -75.00030") || !strings.Contains(output, "Maple Park") {
		test.Errorf("the position and the place: %q", output)
	}
}

// visits lists stays with the place and says how to read on.
func TestLocationVisitsListsStaysAndFilters(test *testing.T) {
	server, called := locationServer(test)
	output, err := runLocationCommand(test, server, "visits", "--from", "2026-10-17", "--label", "home", "--place", "p1", "--first", "2")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "Maple Park") || !strings.Contains(output, "still there") || !strings.Contains(output, "20 minutes") {
		test.Errorf("the stays: %q", output)
	}
	variables := called("ListLocationVisits")
	if variables["placeLabel"] != "home" || variables["placeId"] != "p1" || variables["limitCount"] != float64(2) || variables["from"] == nil || variables["until"] != nil {
		test.Errorf("the filters were sent: %v", variables)
	}
	if _, err := runLocationCommand(test, server, "visits", "--label", "castle"); err == nil {
		test.Error("a label there is not should be refused")
	}
	if _, err := runLocationCommand(test, server, "visits", "--from", "2026-10-18", "--until", "2026-10-17"); err == nil {
		test.Error("a range that ends before it begins should be refused")
	}
}

// save sends only what was given, and an empty name is sent as empty.
func TestLocationSaveSendsOnlyWhatWasGiven(test *testing.T) {
	server, called := locationServer(test)
	if _, err := runLocationCommand(test, server, "save", "p1", "--label", "none", "--name", ""); err != nil {
		test.Fatal(err)
	}
	variables := called("SavePlace")
	if variables["placeId"] != "p1" || variables["placeLabel"] != "" || variables["placeName"] != "" || variables["radiusMeters"] != nil {
		test.Errorf("an empty name and no label are sent as empty, the rest is left out: %v", variables)
	}
	if _, err := runLocationCommand(test, server, "save"); err == nil {
		test.Error("a new place needs coordinates")
	}
	if _, err := runLocationCommand(test, server, "save", "p1", "--latitude", "40"); err == nil {
		test.Error("coordinates come together")
	}
}

// checkin at a point checks in at the nearest place found, which is one of
// the person's own when they have one there.
func TestLocationCheckInAtAPointUsesTheNearestPlace(test *testing.T) {
	server, called := locationServer(test)
	if _, err := runLocationCommand(test, server, "checkin", "--latitude", "40.0005", "--longitude", "-75.0003"); err != nil {
		test.Fatal(err)
	}
	variables := called("CheckIn")
	if variables["placeName"] != "Corner Cafe" || variables["osmId"] != float64(42) || variables["osmType"] != "node" || variables["placeId"] != nil {
		test.Errorf("the nearest found place, with what the map called it: %v", variables)
	}
	if _, err := runLocationCommand(test, server, "checkin", "--place", "p1"); err != nil {
		test.Fatal(err)
	}
	if variables = called("CheckIn"); variables["placeId"] != "p1" || variables["placeName"] != nil {
		test.Errorf("one of the person's places by id: %v", variables)
	}
	if _, err := runLocationCommand(test, server, "checkin"); err == nil {
		test.Error("a check-in needs a place or a point")
	}
}

// erase and forget refuse without --force when nobody can be asked.
func TestLocationEraseAndForgetNeedForce(test *testing.T) {
	server, called := locationServer(test)
	if _, err := runLocationCommand(test, server, "erase"); err == nil || !strings.Contains(err.Error(), "--force") {
		test.Errorf("erase without confirmation: %v", err)
	}
	if called("ForgetLocationHistory") != nil {
		test.Error("nothing was sent")
	}
	output, err := runLocationCommand(test, server, "erase", "--force", "--until", "2026-10-01")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("ForgetLocationHistory"); variables["until"] == nil || variables["from"] != nil {
		test.Errorf("the range: %v", variables)
	}
	if !strings.Contains(output, "10 positions") || !strings.Contains(output, "1 place") {
		test.Errorf("what was deleted: %q", output)
	}
	if _, err := runLocationCommand(test, server, "forget", "p1"); err == nil {
		test.Error("forget without confirmation")
	}
	if _, err := runLocationCommand(test, server, "forget", "p1", "--force"); err != nil {
		test.Error(err)
	}
}

// --person reads somebody else's, by username, among the people who share.
func TestLocationPersonIsResolvedAmongThoseSharing(test *testing.T) {
	server, called := locationServer(test)
	if _, err := runLocationCommand(test, server, "visits", "--person", "ben"); err != nil {
		test.Fatal(err)
	}
	if called("ListLocationVisits")["userId"] != "u-ben" {
		test.Errorf("ben's account was asked about: %v", called("ListLocationVisits"))
	}
	if _, err := runLocationCommand(test, server, "visits", "--person", "stranger"); err == nil || !strings.Contains(err.Error(), "does not share") {
		test.Errorf("a stranger: %v", err)
	}
}
