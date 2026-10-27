package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
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
		"GetLocation":                 {"now", "now"},
		"ListLocationVisits":          {"visits", "visits"},
		"ListLocationPauses":          {"visits", "visits"},
		"PauseLocation":               {"pause", "pause"},
		"ResumeLocation":              {"resume", "resume"},
		"MuteLocationNotifications":   {"mute", "mute"},
		"UnmuteLocationNotifications": {"unmute", "unmute"},
		"ListTrips":                   {"trips", "trips"},
		"SetTripLegTravelMode":        {"travel-mode", "travel_mode"},
		"ListTravelTotals":            {"travel", "travel_totals"},
		"ListPlaces":                  {"places", "places"},
		"ReadPlace":                   {"place", "places"},
		"ListNearbyPlaces":            {"nearby", "nearby"},
		"SearchPlaces":                {"search", "search"},
		"SearchAddresses":             {"addresses", "find_address"},
		"ListLocationPoints":          {"path", ""},
		"ReportLocation":              {"", ""},
		"SavePlace":                   {"save", "save_place"},
		"ForgetPlace":                 {"forget", "forget_place"},
		"ForgetPlaceVisit":            {"forget-visit", "forget_visit"},
		"CheckIn":                     {"checkin", "check_in"},
		"EraseLocationHistory":        {"erase", "erase"},
		"ListSharedLocations":         {"people", "people"},
		"ListLocationShares":          {"shares", "shares"},
		"ShareLocation":               {"share", "share"},
		"UnshareLocation":             {"unshare", "unshare"},
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

	// Every argument of a read operation is a flag of its subcommand and a
	// property of its tool action, under the name that surface gives it, or
	// is exempt with the reason.
	flagNames := map[string]string{
		"userId": "person", "from": "from", "until": "until", "placeId": "place", "placeLabel": "label", "query": "query",
		"offset": "offset", "limitCount": "first", "latitudeDegrees": "latitude", "longitudeDegrees": "longitude", "radiusMeters": "radius",
		"period": "by",
	}
	propertyNames := map[string]string{
		"userId": "person", "from": "from", "until": "until", "placeId": "place_id", "placeLabel": "place_label", "query": "query",
		"offset": "offset", "limitCount": "limit", "latitudeDegrees": "latitude_degrees", "longitudeDegrees": "longitude_degrees", "radiusMeters": "radius_meters",
		"period": "period",
	}
	// "<Operation>.<argument>.<surface>" is exempt for the reason given.
	exempt := map[string]string{
		"SearchPlaces.query.command":       "the words are the arguments of the subcommand",
		"SearchAddresses.query.command":    "the words are the arguments of the subcommand",
		"SearchPlaces.placeId.command":     "the flag is --near-place, since --place would read as a filter",
		"SearchAddresses.placeId.command":  "the flag is --near-place, as it is for search",
		"ReadPlace.placeId.command":        "the place id is the argument of the subcommand",
		"ListLocationPoints.from.command":  "a day is given with --day",
		"ListLocationPoints.until.command": "a day is given with --day",
	}
	queryOperations := reflect.TypeFor[apigraph.LocationQuery]()
	for operation, names := range table {
		method, isQuery := queryOperations.MethodByName(operation)
		if !isQuery || method.Type.NumIn() < 2 || names.subcommand == "" {
			continue
		}
		command := commands[names.subcommand]
		flags := map[string]bool{}
		for _, flag := range command.Flags {
			for _, name := range flag.Names() {
				flags[name] = true
			}
		}
		// The tool's actions share one set of properties, so this holds the
		// action to the tool having the property, not to the action alone.
		properties, _ := tools.Build().Get("location").Parameters["properties"].(map[string]any)
		argumentsType := method.Type.In(1)
		for index := 0; index < argumentsType.NumField(); index++ {
			argument, _, _ := strings.Cut(argumentsType.Field(index).Tag.Get("json"), ",")
			if _, isExempt := exempt[operation+"."+argument+".command"]; !isExempt && !flags[flagNames[argument]] {
				test.Errorf("teanode location %s has no flag for the argument %s of %s", names.subcommand, argument, operation)
			}
			if names.action != "" && properties[propertyNames[argument]] == nil {
				test.Errorf("the location tool action %s has no property for the argument %s of %s", names.action, argument, operation)
			}
		}
	}
	for key, reason := range exempt {
		if strings.TrimSpace(reason) == "" {
			test.Errorf("the exemption %s needs a reason", key)
		}
	}
}

// locationServer answers the documents the location commands send with a small
// world and records the variables of each by operation name.
func locationServer(test *testing.T) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	return locationServerAnswering(test, nil)
}

// locationServerAnswering is locationServer with some of its answers replaced.
func locationServerAnswering(test *testing.T, replacements map[string]string) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	place := `{"id":"p1","userId":"u-ada","placeName":"Maple Park","placeLabel":"","radiusMeters":75,"visitCount":2,"isNamedByPerson":false}`
	answers := map[string]string{
		"GetLocation": `{"userId":"u-ada","username":"ada","name":"Ada","shareLevel":"owner",
			"latestPoint":{"latitudeDegrees":40.0005,"longitudeDegrees":-75.0003,"accuracyMeters":12,"measuredAt":"2026-10-17T10:00:00Z","locationSource":"phone"},
			"openVisit":{"id":"v1","arrivedAt":"2026-10-17T09:30:00Z","place":` + place + `}}`,
		"ListLocationVisits": `{"nextOffset":50,"placeVisits":[{"id":"v1","arrivedAt":"2026-10-17T09:30:00Z","departedAt":"2026-10-17T09:50:00Z","durationSeconds":1200,"photoCount":3,"photos":[{"id":"ph1","smallPreviewUrl":"/api/v1/photo/ph1/small","takenAt":"2026-10-17T09:40:00Z"}],"place":` + place + `},
			{"id":"v2","arrivedAt":"2026-10-17T11:00:00Z","place":` + place + `}]}`,
		"ListPlaces": `{"places":[` + place + `]}`,
		"ListNearbyPlaces": `[{"placeName":"Corner Cafe","osmType":"node","osmId":42,"osmCategory":"amenity=cafe","addressText":"1 Elm Street","latitudeDegrees":40.0006,"longitudeDegrees":-75.0004,"distanceMeters":15},
			{"placeName":"Maple Park","latitudeDegrees":40.0001,"longitudeDegrees":-75.0001,"distanceMeters":90,"placeId":"p1"}]`,
		"SearchPlaces": `{"foundPlaces":[{"placeName":"Elm Playground","osmType":"way","osmId":7,"distanceMeters":900}]}`,
		"SearchAddresses": `[{"placeName":"6 Rue Example","addressText":"6 Rue Example, Exampleton, Testland","latitudeDegrees":40.5,"longitudeDegrees":-75.5,"distanceMeters":0},
			{"placeName":"8 Rue Example","addressText":"8 Rue Example, Exampleton, Testland","latitudeDegrees":40.6,"longitudeDegrees":-75.6,"distanceMeters":2500}]`,
		"SavePlace":                   place,
		"ForgetPlace":                 `true`,
		"ForgetPlaceVisit":            `true`,
		"CheckIn":                     `{"id":"v3","arrivedAt":"2026-10-17T12:00:00Z","place":` + place + `}`,
		"EraseLocationHistory":        `{"pointCount":10,"visitCount":2,"placeCount":1}`,
		"ListSharedLocations":         `[{"userId":"u-ben","username":"ben","name":"Ben","shareLevel":"history"},{"userId":"u-cleo","username":"cleo","name":"Cleo","shareLevel":"current"}]`,
		"PauseLocation":               `{"id":"pause1","startedAt":"2026-10-17T12:00:00Z","endedAt":"2026-10-17T13:30:00Z"}`,
		"ResumeLocation":              `true`,
		"MuteLocationNotifications":   `{"mutedUntil":"2026-10-17T13:30:00Z"}`,
		"UnmuteLocationNotifications": `true`,
		"ListLocationPauses":          `[{"id":"pause1","startedAt":"2026-10-17T10:00:00Z","endedAt":"2026-10-17T10:30:00Z"},{"id":"pause2","startedAt":"2026-10-17T09:55:00Z"}]`,
		"ListTrips": `{"nextOffset":null,"trips":[{"id":"t1","startedAt":"2026-10-17T08:05:00Z","endedAt":"2026-10-17T08:40:00Z","durationSeconds":2100,"distanceMeters":12400,
			"fromPlace":` + place + `,"toPlace":null,
			"legs":[{"id":"leg1","startedAt":"2026-10-17T08:05:00Z","endedAt":"2026-10-17T08:15:00Z","durationSeconds":600,"distanceMeters":600,"travelMode":"walk","detectedTravelMode":"walk","isTravelModeCorrected":false,"detectionSource":"motion"},
				{"id":"leg2","startedAt":"2026-10-17T08:15:00Z","endedAt":"2026-10-17T08:40:00Z","durationSeconds":1500,"distanceMeters":11800,"travelMode":"bus","detectedTravelMode":"car","isTravelModeCorrected":true,"detectionSource":"motion"}]}]}`,
		"ListTravelTotals": `[{"periodStartDay":"2026-10-12","travelMode":"walk","distanceMeters":600,"durationSeconds":600,"legCount":1},
			{"periodStartDay":"2026-10-12","travelMode":"bus","distanceMeters":11800,"durationSeconds":1500,"legCount":1},
			{"periodStartDay":"2026-10-05","travelMode":"car","distanceMeters":2000,"durationSeconds":300,"legCount":1}]`,
		"SetTripLegTravelMode": `{"id":"leg2","startedAt":"2026-10-17T08:15:00Z","endedAt":"2026-10-17T08:40:00Z","durationSeconds":1500,"distanceMeters":11800,"travelMode":"bus","detectedTravelMode":"car","isTravelModeCorrected":true,"detectionSource":"motion"}`,
		"GetCurrentUser":       `{"id":"u-ada","username":"ada","measurementSystem":"metric"}`,
		"ListLocationPoints":   `[{"latitudeDegrees":40.0005,"longitudeDegrees":-75.0003,"accuracyMeters":12,"measuredAt":"2026-10-17T10:00:00Z","locationSource":"phone"}]`,
	}
	for operation, answer := range replacements {
		answers[operation] = answer
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

// now says where the person is and the visit in progress.
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

// visits lists visits with the place and says how to read on.
func TestLocationVisitsListsVisitsAndFilters(test *testing.T) {
	server, called := locationServer(test)
	output, err := runLocationCommand(test, server, "visits", "--from", "2026-10-17", "--label", "home", "--place", "p1", "--first", "2")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "Maple Park") || !strings.Contains(output, "still there") || !strings.Contains(output, "20 minutes") {
		test.Errorf("the visits: %q", output)
	}
	if !strings.Contains(output, "photo count") || !regexp.MustCompile(`20 minutes\s+3\s`).MatchString(output) {
		test.Errorf("the photo count of the first visit and none for the second: %q", output)
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

// checkin at a point sends the coordinates alone: the server uses the
// person's own place there or makes one.
func TestLocationCheckInAtAPointSendsOnlyTheCoordinates(test *testing.T) {
	server, called := locationServer(test)
	if _, err := runLocationCommand(test, server, "checkin", "--latitude", "40.0005", "--longitude", "-75.0003"); err != nil {
		test.Fatal(err)
	}
	variables := called("CheckIn")
	if variables["latitudeDegrees"] != 40.0005 || variables["longitudeDegrees"] != -75.0003 ||
		variables["placeName"] != nil || variables["osmId"] != nil || variables["osmType"] != nil || variables["placeId"] != nil {
		test.Errorf("the coordinates alone: %v", variables)
	}
	if variables["accuracyMeters"] != nil {
		test.Errorf("no accuracy was given: %v", variables)
	}
	if _, err := runLocationCommand(test, server, "checkin", "--latitude", "40.0005", "--longitude", "-75.0003", "--accuracy", "850"); err != nil {
		test.Fatal(err)
	}
	if variables = called("CheckIn"); variables["accuracyMeters"] != float64(850) {
		test.Errorf("the accuracy was sent: %v", variables)
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
	if called("EraseLocationHistory") != nil {
		test.Error("nothing was sent")
	}
	output, err := runLocationCommand(test, server, "erase", "--force", "--until", "2026-10-01")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("EraseLocationHistory"); variables["until"] == nil || variables["from"] != nil {
		test.Errorf("the range: %v", variables)
	}
	if !strings.Contains(output, "10 positions") || !strings.Contains(output, "1 place") {
		test.Errorf("what was erased: %q", output)
	}
	if _, err := runLocationCommand(test, server, "forget", "p1"); err == nil {
		test.Error("forget without confirmation")
	}
	if _, err := runLocationCommand(test, server, "forget", "p1", "--force"); err != nil {
		test.Error(err)
	}
	if _, err := runLocationCommand(test, server, "forget-visit", "v1"); err == nil {
		test.Error("forget-visit without confirmation")
	}
	if _, err := runLocationCommand(test, server, "forget-visit", "v1", "--force"); err != nil || called("ForgetPlaceVisit")["placeVisitId"] != "v1" {
		test.Errorf("forget-visit: %v %v", err, called("ForgetPlaceVisit"))
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

// The path of a day is a sharer's too, when they share their history.
func TestLocationPathTakesAPerson(test *testing.T) {
	server, called := locationServer(test)
	if _, err := runLocationCommand(test, server, "path", "--day", "2026-10-17", "--person", "ben"); err != nil {
		test.Fatal(err)
	}
	if called("ListLocationPoints")["userId"] != "u-ben" {
		test.Errorf("ben's account was asked about: %v", called("ListLocationPoints"))
	}
}

// Somebody who shares only where they are now is said to, in those words, for
// what is not shared, and read for what is.
func TestLocationSaysWhenOnlyWhereTheyAreNowIsShared(test *testing.T) {
	server, called := locationServer(test)
	for _, subcommand := range []string{"visits", "places", "path"} {
		if _, err := runLocationCommand(test, server, subcommand, "--person", "cleo"); err == nil || !strings.Contains(err.Error(), "cleo shares only where they are now") {
			test.Errorf("%s: %v", subcommand, err)
		}
	}
	if called("ListLocationVisits") != nil {
		test.Error("the server was not asked for what is not shared")
	}
	if _, err := runLocationCommand(test, server, "now", "--person", "cleo"); err != nil {
		test.Errorf("where they are now is shared: %v", err)
	}
}

// Every section of the settings the API has is read by the command line,
// places among them: dropping one from the selection would make 'settings'
// silently stop showing it.
func TestEverySettingsSectionIsRead(test *testing.T) {
	test.Parallel()

	kind := reflect.TypeFor[apigraph.Settings]()
	for index := 0; index < kind.NumField(); index++ {
		section, _, _ := strings.Cut(kind.Field(index).Tag.Get("json"), ",")
		if section == "" || section == "-" {
			continue
		}
		if !regexp.MustCompile(`(^|\s)` + section + `\s`).MatchString(client.DocumentGetSettings) {
			test.Errorf("the settings section %q is not in what the command line reads", section)
		}
	}
}

// A place is called by the name the person gave it, else by its label, else by
// what the map calls it, else by its address, else by its city.
func TestAPlaceLineNamesThePlaceTheWayThePersonWould(test *testing.T) {
	test.Parallel()

	for expected, found := range map[string]*client.Place{
		"Grandma's [family]": {PlaceName: "Grandma's", IsNamedByPerson: true, PlaceLabel: "family", OSMName: "Elm Court"},
		"Home (Maple Park)":  {PlaceName: "Maple Park", PlaceLabel: "home", OSMName: "Maple Park"},
		"Gym":                {PlaceName: "Exampleton", PlaceLabel: "gym"},
		"Maple Park":         {PlaceName: "Maple Park", OSMName: "Maple Park", AddressText: "1 Elm Street"},
		"1 Elm Street":       {PlaceName: "Exampleton", AddressText: "1 Elm Street"},
		"Exampleton":         {PlaceName: "Exampleton"},
		"(not named yet)":    {},
	} {
		if got := placeLine(found); got != expected {
			test.Errorf("%+v is %q, want %q", found, got, expected)
		}
	}
}

// A found row that is the person's own place is called the way placeLine
// calls a place.
func TestAFoundPlaceLineNamesTheRowTheWayThePersonWould(test *testing.T) {
	test.Parallel()

	for expected, found := range map[string]*client.FoundPlace{
		"Grandma's [family]": {PlaceName: "Grandma's", IsNamedByPerson: true, PlaceLabel: "family"},
		"Home (Maple Park)":  {PlaceName: "Maple Park", PlaceLabel: "home"},
		"Gym (1 Elm Street)": {AddressText: "1 Elm Street", PlaceLabel: "gym"},
		"Maple Park":         {PlaceName: "Maple Park"},
	} {
		if got := foundPlaceLine(found); got != expected {
			test.Errorf("%+v is %q, want %q", found, got, expected)
		}
	}
}

// addresses lists the matches with their positions, and sends the count.
func TestLocationAddressesListsMatches(test *testing.T) {
	server, called := locationServer(test)
	output, err := runLocationCommand(test, server, "addresses", "rue", "example", "--first", "3")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "6 Rue Example, Exampleton, Testland") || !strings.Contains(output, "40.50000") || !strings.Contains(output, "-75.60000") || !strings.Contains(output, "2.5 km") {
		test.Errorf("the matches: %q", output)
	}
	variables := called("SearchAddresses")
	if variables["query"] != "rue example" || variables["limitCount"] != float64(3) {
		test.Errorf("the words and the count were sent: %v", variables)
	}
	if _, err := runLocationCommand(test, server, "addresses"); err == nil {
		test.Error("an address search needs words")
	}
}

// addresses centers on a place or a position, never both and never half a
// position, and sends only what was given.
func TestLocationAddressesCentersOnAPlaceOrAPoint(test *testing.T) {
	server, called := locationServer(test)
	if _, err := runLocationCommand(test, server, "addresses", "rue", "example", "--near-place", "p1"); err != nil {
		test.Fatal(err)
	}
	variables := called("SearchAddresses")
	if variables["placeId"] != "p1" || variables["latitudeDegrees"] != nil || variables["longitudeDegrees"] != nil {
		test.Errorf("the place was sent: %v", variables)
	}
	if _, err := runLocationCommand(test, server, "addresses", "rue", "example", "--latitude", "40.1", "--longitude", "-75.2"); err != nil {
		test.Fatal(err)
	}
	variables = called("SearchAddresses")
	if variables["placeId"] != nil || variables["latitudeDegrees"] != 40.1 || variables["longitudeDegrees"] != -75.2 {
		test.Errorf("the point was sent: %v", variables)
	}
	for _, arguments := range [][]string{
		{"addresses", "rue", "--latitude", "40.1"},
		{"addresses", "rue", "--near-place", "p1", "--latitude", "40.1", "--longitude", "-75.2"},
	} {
		if _, err := runLocationCommand(test, server, arguments...); err == nil {
			test.Errorf("%v should have been refused", arguments)
		}
	}
}

// save --address takes the first match, moves the place there with its
// address, and lists the others so that the first being wrong can be undone.
func TestLocationSaveAddressTakesTheFirstMatch(test *testing.T) {
	server, called := locationServer(test)
	output, err := runLocationCommand(test, server, "save", "p1", "--address", "rue example")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("SavePlace")
	if variables["placeId"] != "p1" || variables["latitudeDegrees"] != 40.5 || variables["longitudeDegrees"] != -75.5 ||
		variables["addressText"] != "6 Rue Example, Exampleton, Testland" || variables["placeName"] != nil {
		test.Errorf("the first match was sent: %v", variables)
	}
	if !strings.Contains(output, "address: ") || !strings.Contains(output, "8 Rue Example, Exampleton, Testland") || !strings.Contains(output, "--address-text") {
		test.Errorf("the address chosen and the other matches: %q", output)
	}
	if lookup := called("SearchAddresses"); lookup["placeId"] != "p1" || lookup["latitudeDegrees"] != nil {
		test.Errorf("the lookup centered on the place being saved: %v", lookup)
	}

	// The exact form sends what it was given and looks nothing up.
	if _, err := runLocationCommand(test, server, "save", "p1", "--latitude", "40.6", "--longitude", "-75.6", "--address-text", "8 Rue Example, Exampleton, Testland"); err != nil {
		test.Fatal(err)
	}
	variables = called("SavePlace")
	if variables["latitudeDegrees"] != 40.6 || variables["addressText"] != "8 Rue Example, Exampleton, Testland" {
		test.Errorf("the exact form: %v", variables)
	}
	// An address the map does not know is sent as typed to a place that stays.
	if _, err := runLocationCommand(test, server, "save", "p1", "--address-text", "12 Lane Example"); err != nil {
		test.Fatal(err)
	}
	variables = called("SavePlace")
	if variables["placeId"] != "p1" || variables["addressText"] != "12 Lane Example" || variables["latitudeDegrees"] != nil || variables["longitudeDegrees"] != nil {
		test.Errorf("the address alone: %v", variables)
	}
	// An empty address takes the typed one away from a place.
	if _, err := runLocationCommand(test, server, "save", "p1", "--address-text", ""); err != nil {
		test.Fatal(err)
	}
	if variables = called("SavePlace"); variables["addressText"] != "" || variables["placeId"] != "p1" {
		test.Errorf("the empty address: %v", variables)
	}
	for _, arguments := range [][]string{
		{"save", "--address-text", ""},
		{"save", "--address-text", "6 Rue Example"},
		{"save", "p1", "--address", "   "},
		{"save", "p1", "--address", "rue example", "--latitude", "40", "--longitude", "-75"},
		{"save", "p1", "--address", "rue example", "--address-text", "6 Rue Example"},
	} {
		if _, err := runLocationCommand(test, server, arguments...); err == nil {
			test.Errorf("%v should be refused", arguments)
		}
	}
}

// A typed address shows after save, also when it was given with --address-text
// alone, and in the places table.
func TestLocationShowsATypedAddress(test *testing.T) {
	typedPlace := `{"id":"p2","userId":"u-ada","placeName":"12 Lane Example","placeLabel":"home","addressText":"12 Lane Example, Exampleton","isAddressTyped":true,"radiusMeters":75,"visitCount":0}`
	server, _ := locationServerAnswering(test, map[string]string{"SavePlace": typedPlace, "ListPlaces": `{"places":[` + typedPlace + `]}`})
	output, err := runLocationCommand(test, server, "save", "p2", "--address-text", "12 Lane Example, Exampleton")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "address: 12 Lane Example, Exampleton (typed)") {
		test.Errorf("save did not say the address was typed: %q", output)
	}
	output, err = runLocationCommand(test, server, "places")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "12 Lane Example, Exampleton (typed)") {
		test.Errorf("places did not mark the typed address: %q", output)
	}
	// An address the map gave is not marked.
	server, _ = locationServer(test)
	output, err = runLocationCommand(test, server, "save", "p1", "--label", "none")
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(output, "(typed)") {
		test.Errorf("a place without a typed address: %q", output)
	}
}

// pause sends the end that --for or --until names, none for a pause until the
// person resumes, and refuses the two together and an end in the past.
func TestLocationPauseSendsTheEndItWasGiven(test *testing.T) {
	server, called := locationServer(test)
	before := time.Now()
	output, err := runLocationCommand(test, server, "pause", "--for", "1h30m")
	if err != nil {
		test.Fatal(err)
	}
	endedAt, parseErr := time.Parse(time.RFC3339, fmt.Sprint(called("PauseLocation")["endedAt"]))
	if parseErr != nil || endedAt.Before(before.Add(89*time.Minute)) || endedAt.After(time.Now().Add(91*time.Minute)) {
		test.Errorf("an hour and a half from now was sent: %v", called("PauseLocation"))
	}
	if !strings.Contains(output, "paused until") {
		test.Errorf("it says when the pause ends: %q", output)
	}

	if _, err := runLocationCommand(test, server, "pause", "--for", "2d"); err != nil {
		test.Errorf("days are a length: %v", err)
	}
	next := time.Now().Add(26 * time.Hour).Truncate(time.Minute)
	if _, err := runLocationCommand(test, server, "pause", "--until", next.Format("2006-01-02T15:04")); err != nil {
		test.Fatal(err)
	}
	if sent := fmt.Sprint(called("PauseLocation")["endedAt"]); !strings.HasPrefix(sent, next.UTC().Format("2006-01-02T15:04")) {
		test.Errorf("the moment typed was sent: %q, want %s", sent, next.UTC().Format(time.RFC3339))
	}

	output, err = runLocationCommand(test, server, "pause")
	if err != nil {
		test.Fatal(err)
	}
	if called("PauseLocation")["endedAt"] != nil {
		test.Errorf("no end was given, none is sent: %v", called("PauseLocation"))
	}
	if !strings.Contains(output, "paused") {
		test.Errorf("it says it paused: %q", output)
	}

	for name, arguments := range map[string][]string{
		"both":             {"pause", "--for", "1h", "--until", "2099-01-01"},
		"a length of zero": {"pause", "--for", "0m"},
		"not a length":     {"pause", "--for", "soon"},
		"the past":         {"pause", "--until", "2020-01-01"},
		"not a moment":     {"pause", "--until", "tomorrow"},
	} {
		if _, err := runLocationCommand(test, server, arguments...); err == nil {
			test.Errorf("%s should be refused", name)
		}
	}
}

// resume says whether there was a pause to end.
func TestLocationResumeSaysWhetherThereWasAPause(test *testing.T) {
	server, _ := locationServer(test)
	output, err := runLocationCommand(test, server, "resume")
	if err != nil || !strings.Contains(output, "recording again") {
		test.Errorf("resuming: %q %v", output, err)
	}
	notPaused, _ := locationServerAnswering(test, map[string]string{"ResumeLocation": `false`})
	if output, err := runLocationCommand(test, notPaused, "resume"); err != nil || !strings.Contains(output, "was not paused") {
		test.Errorf("resuming when not paused: %q %v", output, err)
	}
}

// mute names the person by username, sends the end that --for or --until
// names, none for a mute until unmuted, and refuses what pause refuses.
func TestLocationMuteSendsThePersonAndTheEndItWasGiven(test *testing.T) {
	server, called := locationServer(test)
	before := time.Now()
	output, err := runLocationCommand(test, server, "mute", "ben", "--for", "1h30m")
	if err != nil {
		test.Fatal(err)
	}
	if called("MuteLocationNotifications")["userId"] != "u-ben" {
		test.Errorf("the person was found by username: %v", called("MuteLocationNotifications"))
	}
	mutedUntil, parseErr := time.Parse(time.RFC3339, fmt.Sprint(called("MuteLocationNotifications")["mutedUntil"]))
	if parseErr != nil || mutedUntil.Before(before.Add(89*time.Minute)) || mutedUntil.After(time.Now().Add(91*time.Minute)) {
		test.Errorf("an hour and a half from now was sent: %v", called("MuteLocationNotifications"))
	}
	if !strings.Contains(output, "notifications muted until") {
		test.Errorf("it says when the mute ends: %q", output)
	}

	next := time.Now().Add(26 * time.Hour).Truncate(time.Minute)
	if _, err := runLocationCommand(test, server, "mute", "ben", "--until", next.Format("2006-01-02T15:04")); err != nil {
		test.Fatal(err)
	}
	if sent := fmt.Sprint(called("MuteLocationNotifications")["mutedUntil"]); !strings.HasPrefix(sent, next.UTC().Format("2006-01-02T15:04")) {
		test.Errorf("the moment typed was sent: %q, want %s", sent, next.UTC().Format(time.RFC3339))
	}

	untilUnmuted, calledAgain := locationServerAnswering(test, map[string]string{"MuteLocationNotifications": `{"mutedUntil":null}`})
	output, err = runLocationCommand(test, untilUnmuted, "mute", "ben")
	if err != nil || calledAgain("MuteLocationNotifications")["mutedUntil"] != nil || strings.TrimSpace(output) != "notifications muted" {
		test.Errorf("no end was given, none is sent: %q %v %v", output, calledAgain("MuteLocationNotifications"), err)
	}

	for name, arguments := range map[string][]string{
		"both":                        {"mute", "ben", "--for", "1h", "--until", "2099-01-01"},
		"a length of zero":            {"mute", "ben", "--for", "0m"},
		"the past":                    {"mute", "ben", "--until", "2020-01-01"},
		"no person":                   {"mute"},
		"somebody who does not share": {"mute", "nobody"},
	} {
		if _, err := runLocationCommand(test, server, arguments...); err == nil {
			test.Errorf("%s should be refused", name)
		}
	}
}

// unmute says whether there was a mute to end.
func TestLocationUnmuteSaysWhetherThereWasAMute(test *testing.T) {
	server, called := locationServer(test)
	output, err := runLocationCommand(test, server, "unmute", "ben")
	if err != nil || !strings.Contains(output, "notifications on again") || called("UnmuteLocationNotifications")["userId"] != "u-ben" {
		test.Errorf("unmuting: %q %v %v", output, err, called("UnmuteLocationNotifications"))
	}
	notMuted, _ := locationServerAnswering(test, map[string]string{"UnmuteLocationNotifications": `false`})
	if output, err := runLocationCommand(test, notMuted, "unmute", "ben"); err != nil || !strings.Contains(output, "were not muted") {
		test.Errorf("unmuting when not muted: %q %v", output, err)
	}
	if _, err := runLocationCommand(test, server, "unmute"); err == nil {
		test.Error("unmute needs a person")
	}
}

// The people listing says which person's notifications are muted, and until
// when.
func TestLocationPeopleShowsAMute(test *testing.T) {
	until := time.Now().Add(time.Hour)
	server, _ := locationServerAnswering(test, map[string]string{"ListSharedLocations": `[
		{"userId":"u-ben","username":"ben","name":"Ben","shareLevel":"current","notificationMute":{"mutedUntil":"` + until.UTC().Format(time.RFC3339) + `"}},
		{"userId":"u-cleo","username":"cleo","name":"Cleo","shareLevel":"current","notificationMute":{"mutedUntil":null}},
		{"userId":"u-dana","username":"dana","name":"Dana","shareLevel":"current","notificationMute":null}]`})
	output, err := runLocationCommand(test, server, "people")
	if err != nil || !regexp.MustCompile(`ben .*notifications muted until (\d{4}-\d{2}-\d{2} )?`+until.In(time.Local).Format("15:04")).MatchString(output) ||
		!regexp.MustCompile(`cleo .*notifications muted\s*\n`).MatchString(output) || strings.Count(output, "notifications muted") != 2 {
		test.Errorf("the people who share: %q %v", output, err)
	}
}

// The person's own location says that recording is paused and until when, and
// another person who paused reads "Location paused" with no position.
func TestLocationShowsAPauseOnTheirOwnAndOnAnotherPersons(test *testing.T) {
	endedAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	own, _ := locationServerAnswering(test, map[string]string{"GetLocation": `{"userId":"u-ada","username":"ada","name":"Ada","shareLevel":"owner",
		"latestPoint":{"latitudeDegrees":40.0005,"longitudeDegrees":-75.0003,"accuracyMeters":12,"measuredAt":"2026-10-17T10:00:00Z","locationSource":"phone"},
		"pause":{"id":"pause1","startedAt":"2026-10-17T12:00:00Z","endedAt":"` + endedAt + `"}}`})
	output, err := runLocationCommand(test, own, "now")
	if err != nil || !strings.Contains(output, "Paused until") || !strings.Contains(output, "40.00050, -75.00030") {
		test.Errorf("their own: %q %v", output, err)
	}
	until, err := time.Parse(time.RFC3339, endedAt)
	if err != nil || !strings.Contains(output, until.In(time.Local).Format("15:04")) {
		test.Errorf("until when: %q", output)
	}

	until = time.Now().Add(time.Hour)
	open, _ := locationServerAnswering(test, map[string]string{"GetLocation": `{"userId":"u-ben","username":"ben","name":"Ben","shareLevel":"current","pause":{"id":"pause2","startedAt":"2026-10-17T12:00:00Z"}}`,
		"ListSharedLocations": `[{"userId":"u-ben","username":"ben","name":"Ben","shareLevel":"current","pause":{"id":"pause2","startedAt":"2026-10-17T12:00:00Z"}},
			{"userId":"u-cleo","username":"cleo","name":"Cleo","shareLevel":"current","pause":{"id":"pause3","startedAt":"2026-10-17T12:00:00Z","endedAt":"` + until.UTC().Format(time.RFC3339) + `"}}]`})
	if output, err = runLocationCommand(test, open, "now", "--person", "ben"); err != nil || strings.TrimSpace(output) != "Location paused until you resume" {
		test.Errorf("another person's: %q %v", output, err)
	}
	// The end an hour ahead carries its date as well when it falls tomorrow.
	if output, err = runLocationCommand(test, open, "people"); err != nil || !strings.Contains(output, "Location paused until you resume") ||
		!regexp.MustCompile(`cleo +current +Location paused until (\d{4}-\d{2}-\d{2} )?`+until.In(time.Local).Format("15:04")).MatchString(output) {
		test.Errorf("the people who share: %q %v", output, err)
	}
}

// visits lists the pauses of the range among the visits, newest first, and
// leaves them out of a listing of the visits to one place.
func TestLocationVisitsListsThePausesAmongThem(test *testing.T) {
	server, called := locationServer(test)
	output, err := runLocationCommand(test, server, "visits", "--from", "2026-10-17")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("ListLocationPauses")
	if variables["from"] == nil || variables["until"] == nil || variables["userId"] != nil {
		test.Errorf("the range was sent: %v", variables)
	}
	first := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC).In(time.Local)
	second := first.Add(30 * time.Minute)
	want := fmt.Sprintf("paused %s to %s", first.Format("15:04"), second.Format("15:04"))
	if !strings.Contains(output, want) || !strings.Contains(output, "paused from ") || !strings.Contains(output, "still paused") || !strings.Contains(output, "30 minutes") {
		test.Errorf("the pauses are among the visits (%s): %q", want, output)
	}
	lines := strings.Split(output, "\n")
	positionOf := func(words string) int {
		for index, line := range lines {
			if strings.Contains(line, words) {
				return index
			}
		}
		return -1
	}
	if positionOf("still there") > positionOf(want) || positionOf(want) > positionOf("20 minutes") {
		test.Errorf("newest first: the open visit, then the pause, then the earlier visit:\n%s", output)
	}
	if output, err = runLocationCommand(test, server, "visits", "--json", "--from", "2026-10-17"); err != nil || !strings.Contains(output, `"pauses"`) || !strings.Contains(output, "pause1") {
		test.Errorf("the pauses are in the JSON: %q %v", output, err)
	}

	forOnePlace, calledAgain := locationServer(test)
	if _, err := runLocationCommand(test, forOnePlace, "visits", "--place", "p1"); err != nil || calledAgain("ListLocationPauses") != nil {
		test.Errorf("a pause belongs to no place: %v %v", err, calledAgain("ListLocationPauses"))
	}
	if _, err := runLocationCommand(test, server, "visits", "--person", "ben", "--from", "2026-10-17"); err != nil || called("ListLocationPauses")["userId"] != "u-ben" {
		test.Errorf("another person's pauses are asked for by their account: %v %v", err, called("ListLocationPauses"))
	}
}

// trips lists each trip with its places, distance in kilometers, its legs and
// the leg ids that travel-mode takes; a day is a range of that day.
func TestLocationTripsListsTripsWithTheirLegs(test *testing.T) {
	server, called := locationServer(test)
	output, err := runLocationCommand(test, server, "trips", "--day", "2026-10-17", "--person", "ben")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"12.4 km", "walk 0.6 km, bus 11.8 km (corrected)", "walk leg1, bus leg2", "Maple Park to somewhere"} {
		if !strings.Contains(output, want) {
			test.Errorf("the table lacks %q: %q", want, output)
		}
	}
	sent := called("ListTrips")
	from, fromErr := time.Parse(time.RFC3339, fmt.Sprint(sent["from"]))
	until, untilErr := time.Parse(time.RFC3339, fmt.Sprint(sent["until"]))
	if fromErr != nil || untilErr != nil || until.Sub(from) < 23*time.Hour || until.Sub(from) > 25*time.Hour || sent["userId"] != "u-ben" {
		test.Errorf("one day of ben's was asked for: %v", sent)
	}

	empty, _ := locationServerAnswering(test, map[string]string{"ListTrips": `{"nextOffset":null,"trips":[]}`})
	if output, err := runLocationCommand(test, empty, "trips"); err != nil || !strings.Contains(output, "no trips") {
		test.Errorf("no trips: %q %v", output, err)
	}
	for name, arguments := range map[string][]string{
		"a day and a range": {"trips", "--day", "2026-10-17", "--from", "2026-10-16"},
		"not a day":         {"trips", "--day", "today"},
		"backwards":         {"trips", "--from", "2026-10-17", "--until", "2026-10-16"},
	} {
		if _, err := runLocationCommand(test, server, arguments...); err == nil {
			test.Errorf("%s should be refused", name)
		}
	}
}

// travel lists a row per period, newest first, with a column for each way that
// appears, in kilometers, and its total.
func TestLocationTravelListsDistancePerWay(test *testing.T) {
	server, called := locationServer(test)
	output, err := runLocationCommand(test, server, "travel", "--by", "month")
	if err != nil {
		test.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "2026-10-12") || !strings.Contains(lines[1], "12.4 km") || !strings.HasPrefix(lines[2], "2026-10-05") {
		test.Errorf("newest period first with its total: %q", output)
	}
	for _, header := range []string{"month starting", "walk", "car", "bus", "total"} {
		if !strings.Contains(lines[0], header) {
			test.Errorf("the header lacks %q: %q", header, lines[0])
		}
	}
	if strings.Contains(lines[0], "plane") {
		test.Errorf("a way nobody used has no column: %q", lines[0])
	}
	sent := called("ListTravelTotals")
	from, _ := time.Parse(time.RFC3339, fmt.Sprint(sent["from"]))
	if sent["period"] != "month" || from.IsZero() || time.Since(from) < 10*30*24*time.Hour || time.Since(from) > 12*31*24*time.Hour {
		test.Errorf("the last 12 months were asked for: %v", sent)
	}
	if _, err := runLocationCommand(test, server, "travel", "--by", "fortnight"); err == nil {
		test.Error("a period nobody has should be refused")
	}
	if _, err := runLocationCommand(test, server, "travel", "--person", "ben", "--json"); err != nil || called("ListTravelTotals")["period"] != "week" || called("ListTravelTotals")["userId"] != "u-ben" {
		test.Errorf("week is the default: %v %v", called("ListTravelTotals"), err)
	}
}

// A person whose account reads US units sees trips, travel and places found
// in miles, and in feet for a short way.
func TestLocationDistancesAreInMilesForAPersonWhoReadsThem(test *testing.T) {
	server, _ := locationServerAnswering(test, map[string]string{"GetCurrentUser": `{"id":"u-ada","username":"ada","measurementSystem":"us"}`})
	output, err := runLocationCommand(test, server, "trips", "--day", "2026-10-17")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "7.7 mi") || !strings.Contains(output, "walk 0.4 mi, bus 7.3 mi (corrected)") || strings.Contains(output, "km") {
		test.Errorf("the trip in miles: %q", output)
	}
	if output, err = runLocationCommand(test, server, "travel"); err != nil || !strings.Contains(output, "7.7 mi") || !strings.Contains(output, "1.2 mi") {
		test.Errorf("the totals in miles: %q %v", output, err)
	}
	if output, err = runLocationCommand(test, server, "nearby"); err != nil || !strings.Contains(output, "49 ft") || !strings.Contains(output, "295 ft") {
		test.Errorf("a short way in feet: %q %v", output, err)
	}
}

// distanceWords writes meters under a tenth of a kilometer and feet under a
// tenth of a mile, else kilometers or miles with one decimal.
func TestDistanceWordsFollowTheUnits(test *testing.T) {
	for _, scenario := range []struct {
		distanceMeters float64
		isUSUnits      bool
		want           string
	}{
		{40, false, "40 m"},
		{640, false, "0.6 km"},
		{12440, false, "12.4 km"},
		{40, true, "131 ft"},
		{160, true, "525 ft"},
		{170, true, "0.1 mi"},
		{12440, true, "7.7 mi"},
	} {
		if got := distanceWords(scenario.distanceMeters, scenario.isUSUnits); got != scenario.want {
			test.Errorf("%v meters, US units %v: %q, not %q", scenario.distanceMeters, scenario.isUSUnits, got, scenario.want)
		}
	}
}

// A way this command does not know still gets a column, so the total of a row
// is the sum of its columns.
func TestLocationTravelShowsAnUnknownWayAsOther(test *testing.T) {
	server, _ := locationServerAnswering(test, map[string]string{"ListTravelTotals": `[{"periodStartDay":"2026-10-12","travelMode":"walk","distanceMeters":1000,"durationSeconds":600,"legCount":1},
{"periodStartDay":"2026-10-12","travelMode":"hoverboard","distanceMeters":2000,"durationSeconds":600,"legCount":1}]`})
	output, err := runLocationCommand(test, server, "travel")
	if err != nil {
		test.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "other") || !strings.Contains(lines[1], "1.0 km") || !strings.Contains(lines[1], "2.0 km") || !strings.Contains(lines[1], "3.0 km") {
		test.Errorf("walk, other and the total of both: %q", output)
	}
}

// travel-mode sends the way, and "detected" sends none to take the correction
// off; a way nobody has is refused before anything is sent.
func TestLocationTravelModeSetsAndClearsAWay(test *testing.T) {
	server, called := locationServer(test)
	output, err := runLocationCommand(test, server, "travel-mode", "leg2", "Bus")
	if err != nil || called("SetTripLegTravelMode")["travelMode"] != "bus" || called("SetTripLegTravelMode")["tripLegId"] != "leg2" || !strings.Contains(output, "bus") {
		test.Errorf("setting: %q %v %v", output, called("SetTripLegTravelMode"), err)
	}
	if _, err := runLocationCommand(test, server, "travel-mode", "leg2", "detected"); err != nil || called("SetTripLegTravelMode")["travelMode"] != nil {
		test.Errorf("clearing sends no way: %v %v", called("SetTripLegTravelMode"), err)
	}
	refused, calledAgain := locationServer(test)
	if _, err := runLocationCommand(test, refused, "travel-mode", "leg2", "rocket"); err == nil || calledAgain("SetTripLegTravelMode") != nil {
		test.Errorf("a way nobody has: %v %v", err, calledAgain("SetTripLegTravelMode"))
	}
	if _, err := runLocationCommand(test, refused, "travel-mode", "leg2"); err == nil {
		test.Error("a way is needed")
	}
}
