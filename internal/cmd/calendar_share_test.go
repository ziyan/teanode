package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Every operation on calendars is on the API, the command line and the agent's
// calendar tool under the same names: nothing exists on one that the others
// lack, except that the agent reads the calendars a person reaches and a single
// event through its agenda rather than under actions of their own. The
// dashboard and the iPhone app call the same operations.
func TestCalendarSharingParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.CalendarQuery](), reflect.TypeFor[apigraph.CalendarMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	// Every operation on calendars, then the subcommand of teanode calendar
	// that does it and, where the agent can, the action of its calendar tool.
	// An operation missing from here fails the test, so a new one is on every
	// surface or its absence is written down.
	table := map[string]struct {
		subcommand string
		action     string
	}{
		"ListCalendars":         {"calendars", ""},
		"ListCalendarEvents":    {"list", "agenda"},
		"GetCalendarEvent":      {"show", ""},
		"GetCalendarRequest":    {"request", "request"},
		"CancelCalendarRequest": {"stop", "stop"},
		"SaveCalendarEvent":     {"add", "add"},
		"DeleteCalendarEvent":   {"remove", "remove"},
		"SaveCalendar":          {"set", "set"},
		"ListCalendarShares":    {"shares", "shares"},
		"ShareCalendar":         {"share", "share"},
		"UnshareCalendar":       {"unshare", "unshare"},
		"CreateCircleCalendar":  {"circle", "circle"},
		"ListCalendarFreeBusy":  {"free", "free_together"},
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}

	commands := map[string]*cli.Command{}
	for _, subcommand := range NewCalendarCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	actions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "calendar" {
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
		test.Fatal("the agent has no calendar tool")
	}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if commands[names.subcommand] == nil {
			test.Errorf("teanode calendar has no %s", names.subcommand)
		}
		if names.action != "" && !actions[names.action] {
			test.Errorf("the calendar tool has no action %s", names.action)
		}
	}

	// And the two ways of reading other people's calendars: a flag on the
	// listing and on the free times, with the agent's agenda and free_together.
	hasFlag := func(command *cli.Command, name string) bool {
		for _, flag := range command.Flags {
			for _, each := range flag.Names() {
				if each == name {
					return true
				}
			}
		}
		return false
	}
	if !hasFlag(commands["list"], "owner") {
		test.Error("teanode calendar list has no --owner")
	}
	if !hasFlag(commands["free"], "with") {
		test.Error("teanode calendar free has no --with")
	}
	if !actions["agenda"] || !actions["free"] {
		test.Error("the calendar tool lost agenda or free")
	}
}

// stdoutLock keeps tests that take over standard output from doing so at once.
var stdoutLock sync.Mutex

// captureStandardOutput runs a function and returns what it printed.
func captureStandardOutput(test *testing.T, run func()) string {
	test.Helper()
	stdoutLock.Lock()
	defer stdoutLock.Unlock()
	reader, writer, err := os.Pipe()
	if err != nil {
		test.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string)
	go func() {
		content, _ := io.ReadAll(reader)
		done <- string(content)
	}()
	run()
	os.Stdout = original
	_ = writer.Close()
	return <-done
}

// sharingServer answers the documents the sharing commands send with a small
// world: ada is the caller, ben and cora share a circle with her, and ben and
// ada shared at busy. Each document is recorded by operation name.
func sharingServer(test *testing.T) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	answers := map[string]string{
		"ListCalendars": `[
			{"id":"own","name":"Calendar","timezone":"UTC","calendarKind":"events","shareLevel":"owner","ownerUserId":"u-ada","ownerUsername":"ada","ownerName":"Ada"},
			{"id":"reminders","name":"Reminders","calendarKind":"reminders","shareLevel":"owner","ownerUserId":"u-ada","ownerUsername":"ada"},
			{"id":"ben-cal","name":"Calendar","calendarKind":"events","shareLevel":"titles","ownerUserId":"u-ben","ownerUsername":"ben","ownerName":"Ben"},
			{"id":"family","name":"Family","calendarKind":"events","shareLevel":"edit","circleId":"c1","circleName":"Home","isCircleCalendar":true}]`,
		"ListCirclePeople": `[{"userId":"u-ben","username":"ben","name":"Ben"},{"userId":"u-cora","username":"cora","name":"Cora"}]`,
		"ListCircles":      `[{"id":"c1","name":"Home","circleKind":"family","members":[],"pendingInvitations":[]}]`,
		"ShareCalendar":    `{"id":"s1","calendarId":"own","granteeUserId":"u-ben","granteeUsername":"ben","shareLevel":"titles"}`,
		"ListCalendarShares": `[{"id":"s1","calendarId":"own","granteeUserId":"u-ben","granteeUsername":"ben","shareLevel":"titles"},
			{"id":"s2","calendarId":"own","granteeCircleId":"c1","granteeCircleName":"Home","shareLevel":"busy"}]`,
		"UnshareCalendar":      `true`,
		"CreateCircleCalendar": `{"id":"family","name":"Family","calendarKind":"events","shareLevel":"edit","circleId":"c1","circleName":"Home","isCircleCalendar":true}`,
		"ListCalendarEvents": `[
			{"id":"e1","calendarId":"x","summary":"Dentist","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z"},
			{"id":"","calendarId":"x","summary":"","isBusyOnly":true,"startsAt":"2030-01-02T09:00:00Z","endsAt":"2030-01-02T09:30:00Z"}]`,
		"ListCalendarFreeBusy": `[
			{"userId":"u-ada","username":"ada","name":"Ada","isShared":true,"shareLevel":"owner","periods":[{"startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z"}]},
			{"userId":"u-ben","username":"ben","name":"Ben","isShared":true,"shareLevel":"busy","periods":[{"startsAt":"2030-01-02T10:30:00Z","endsAt":"2030-01-02T12:00:00Z"},{"startsAt":"2030-01-02T15:00:00Z","endsAt":"2030-01-02T16:00:00Z"}]}]`,
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

func runCalendarCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewCalendarCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "calendar"}, arguments...))
	})
	return output, err
}

// Sharing names a person by username or a circle by name, and sends the
// identifier the server wants.
func TestShareResolvesWhoItIsFor(test *testing.T) {
	server, called := sharingServer(test)

	output, err := runCalendarCommand(test, server, "share", "ben", "--level", "titles")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("ShareCalendar")
	if variables["calendarId"] != "own" || variables["granteeUserId"] != "u-ben" || variables["granteeCircleId"] != nil || variables["shareLevel"] != "titles" {
		test.Errorf("a person is shared with by their account: %v", variables)
	}
	if !strings.Contains(output, "ben") || !strings.Contains(output, "titles") {
		test.Errorf("it says what it did: %q", output)
	}

	output, err = runCalendarCommand(test, server, "share", "Home", "--level", "busy")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, `the circle "Home" (`) || strings.Contains(output, "everyone in Home") {
		test.Errorf("a circle is named as one, with its size: %q", output)
	}
	if variables = called("ShareCalendar"); variables["granteeCircleId"] != "c1" || variables["granteeUserId"] != nil || variables["shareLevel"] != "busy" {
		test.Errorf("a circle is shared with by name: %v", variables)
	}

	// Nobody the caller has no circle with, and no level, are refused here, in
	// words, before anything is sent.
	if _, err = runCalendarCommand(test, server, "share", "stranger", "--level", "busy"); err == nil || !strings.Contains(err.Error(), "circle") {
		test.Errorf("a stranger: %v", err)
	}
	if _, err = runCalendarCommand(test, server, "share", "ben"); err == nil || !strings.Contains(err.Error(), "--level") {
		test.Errorf("no level: %v", err)
	}
	if _, err = runCalendarCommand(test, server, "share", "ben", "--level", "everything"); err == nil {
		test.Errorf("a level there is not")
	}
}

// Circle names are not unique: a name two circles have is refused with the
// ids and sizes of the circles, and nothing is shared; an id still names one
// circle. A username wins over a circle of the same name, which is reached by
// its id.
func TestShareRefusesACircleNameThatIsNotUnique(test *testing.T) {
	var lock sync.Mutex
	shared := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(string(body), "ShareCalendar"):
			lock.Lock()
			shared = true
			lock.Unlock()
			_, _ = response.Write([]byte(`{"data":{"ShareCalendar":{"id":"s1","shareLevel":"busy"}}}`))
		case strings.Contains(string(body), "ListCirclePeople"):
			_, _ = response.Write([]byte(`{"data":{"ListCirclePeople":[{"userId":"u-ben","username":"ben","name":"Ben"}]}}`))
		case strings.Contains(string(body), "ListCircles"):
			_, _ = response.Write([]byte(`{"data":{"ListCircles":[
				{"id":"c1","name":"Family","circleKind":"family","members":[{"userId":"a"},{"userId":"b"}],"pendingInvitations":[]},
				{"id":"c2","name":"Family","circleKind":"family","members":[{"userId":"a"}],"pendingInvitations":[]},
				{"id":"c3","name":"Ben","circleKind":"friends","members":[{"userId":"a"},{"userId":"b"},{"userId":"c"}],"pendingInvitations":[]}]}}`))
		default:
			_, _ = response.Write([]byte(`{"data":{"ListCalendars":[{"id":"own","name":"Calendar","calendarKind":"events","shareLevel":"owner","ownerUserId":"u-ada"}]}}`))
		}
	}))
	defer server.Close()

	_, err := runCalendarCommand(test, server, "share", "family", "--level", "busy")
	if err == nil || !strings.Contains(err.Error(), "c1 (2 people)") || !strings.Contains(err.Error(), "c2 (1 person)") {
		test.Errorf("two circles with one name are listed with their sizes: %v", err)
	}
	lock.Lock()
	wasShared := shared
	lock.Unlock()
	if wasShared {
		test.Fatal("nothing was shared while the name was ambiguous")
	}
	output, err := runCalendarCommand(test, server, "share", "ben", "--level", "busy")
	if err != nil || !strings.Contains(output, "with ben") {
		test.Errorf("a username wins over a circle of that name: %q %v", output, err)
	}
	if _, err = runCalendarCommand(test, server, "share", "c3", "--level", "busy"); err != nil {
		test.Errorf("the circle named like a person is reached by its id: %v", err)
	}
	if _, err = runCalendarCommand(test, server, "share", "c2", "--level", "busy"); err != nil {
		test.Errorf("an id names one circle: %v", err)
	}
}

func TestSharesAndUnshareAndCircleCalendar(test *testing.T) {
	server, called := sharingServer(test)

	output, err := runCalendarCommand(test, server, "shares")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"ben", "titles", "s1", `the circle "Home"`, "busy", "s2"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("shares lacks %q:\n%s", wanted, output)
		}
	}
	if _, err = runCalendarCommand(test, server, "unshare", "s2"); err != nil {
		test.Fatal(err)
	}
	if called("UnshareCalendar")["shareId"] != "s2" {
		test.Errorf("unshare sends the share: %v", called("UnshareCalendar"))
	}
	output, err = runCalendarCommand(test, server, "circle", "c1", "--name", "The household")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("CreateCircleCalendar"); variables["circleId"] != "c1" || variables["name"] != "The household" {
		test.Errorf("the circle calendar: %v", variables)
	}
	if !strings.Contains(output, "Family") {
		test.Errorf("it names what it made: %q", output)
	}
}

// Listing another person's calendar says whose each entry is and shows busy
// time as busy; "calendars" says whose each calendar is and the level.
func TestListOwnerSaysWhoseEachEntryIs(test *testing.T) {
	server, called := sharingServer(test)

	output, err := runCalendarCommand(test, server, "list", "--owner", "ben", "--from", "2030-01-02", "--until", "2030-01-03")
	if err != nil {
		test.Fatal(err)
	}
	if called("ListCalendarEvents")["calendarId"] != "ben-cal" {
		test.Errorf("ben's calendar was read: %v", called("ListCalendarEvents"))
	}
	for _, wanted := range []string{"Dentist", "Ben", "titles", "busy"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("the listing lacks %q:\n%s", wanted, output)
		}
	}
	if _, err = runCalendarCommand(test, server, "list", "--owner", "cora"); err == nil || !strings.Contains(err.Error(), "has not shared") {
		test.Errorf("somebody who shared nothing: %v", err)
	}
	// Everybody's includes the circle's calendar, and not the caller's own.
	output, err = runCalendarCommand(test, server, "list", "--owner", "everyone", "--json", "--from", "2030-01-02", "--until", "2030-01-03")
	if err != nil {
		test.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(output), &entries); err != nil {
		test.Fatalf("%s: %s", err, output)
	}
	whose := map[any]bool{}
	for _, entry := range entries {
		whose[entry["owner"]] = true
	}
	if !whose["Ben"] || !whose["Home"] || whose["Ada"] {
		test.Errorf("everyone's is the shared calendars and the circle's: %v", whose)
	}

	output, err = runCalendarCommand(test, server, "calendars")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"Ben", "titles", "Home", "edit", "you", "owner"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("calendars lacks %q:\n%s", wanted, output)
		}
	}
}

// Free together takes everybody's busy times, the caller's among them, and
// offers what none of them covers.
func TestFreeWithOffersWhenEveryoneIsFree(test *testing.T) {
	server, called := sharingServer(test)

	output, err := runCalendarCommand(test, server, "free", "--with", "ben", "--from", "2030-01-02", "--until", "2030-01-03", "--json")
	if err != nil {
		test.Fatal(err)
	}
	ids, _ := called("ListCalendarFreeBusy")["userIds"].([]any)
	if len(ids) != 2 || ids[0] != "u-ada" || ids[1] != "u-ben" {
		test.Errorf("the caller and the person asked about: %v", ids)
	}
	var free []map[string]string
	if err := json.Unmarshal([]byte(output), &free); err != nil {
		test.Fatalf("%s: %s", err, output)
	}
	got := make([]string, 0, len(free))
	for _, gap := range free {
		got = append(got, gap["from"]+"-"+gap["until"])
	}
	// ada 10:00-11:00 and ben 10:30-12:00 and 15:00-16:00 in a 09:00-17:00 day.
	if want := "09:00-10:00 12:00-15:00 16:00-17:00"; strings.Join(got, " ") != want {
		test.Errorf("free together:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	// Somebody the caller shares a circle with who has shared nothing is
	// said, and nothing is offered.
	failing := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(string(body), "ListCalendarFreeBusy"):
			_, _ = response.Write([]byte(`{"data":{"ListCalendarFreeBusy":[
				{"userId":"u-ada","isShared":true,"periods":[]},{"userId":"u-cora","isShared":false,"periods":[]}]}}`))
		case strings.Contains(string(body), "ListCirclePeople"):
			_, _ = response.Write([]byte(`{"data":{"ListCirclePeople":[{"userId":"u-cora","username":"cora","name":"Cora"}]}}`))
		default:
			_, _ = response.Write([]byte(`{"data":{"ListCalendars":[{"id":"own","name":"Calendar","calendarKind":"events","shareLevel":"owner","ownerUserId":"u-ada"}]}}`))
		}
	}))
	defer failing.Close()
	if _, err = runCalendarCommand(test, failing, "free", "--with", "cora", "--from", "2030-01-02", "--until", "2030-01-03"); err == nil ||
		!strings.Contains(err.Error(), "cora has not shared") {
		test.Errorf("somebody who shared nothing: %v", err)
	}
	if _, err = runCalendarCommand(test, server, "free", "--with", "stranger"); err == nil || !strings.Contains(err.Error(), "circle") {
		test.Errorf("somebody not in a circle: %v", err)
	}
}

// --private on adding and editing sends isPrivate, --private=false takes the
// mark off, saying nothing sends nothing, and the listing and show say which
// events are private.
func TestCalendarPrivateFlagParity(test *testing.T) {
	var lock sync.Mutex
	var saved map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		answer := `{}`
		switch {
		case strings.Contains(document.Query, "ListCalendars"):
			answer = `{"ListCalendars":[{"id":"own","timezone":"UTC","calendarKind":"events","shareLevel":"owner"}]}`
		case strings.Contains(document.Query, "SaveCalendarEvent"):
			lock.Lock()
			saved = document.Variables
			lock.Unlock()
			answer = `{"SaveCalendarEvent":{"id":"e1","summary":"Checkup"}}`
		case strings.Contains(document.Query, "ListCalendarEvents"):
			answer = `{"ListCalendarEvents":[{"id":"e1","summary":"Checkup","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","isPrivate":true},{"id":"e2","summary":"Lunch","startsAt":"2030-01-02T12:00:00Z","endsAt":"2030-01-02T13:00:00Z"}]}`
		case strings.Contains(document.Query, "GetCalendarEvent"):
			answer = `{"GetCalendarEvent":{"id":"e1","summary":"Checkup","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","isPrivate":true,"attendees":[]}}`
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":` + answer + `}`))
	}))
	test.Cleanup(server.Close)
	sentIsPrivate := func() (any, bool) {
		lock.Lock()
		defer lock.Unlock()
		value, said := saved["isPrivate"]
		return value, said && value != nil
	}

	if _, err := runCalendarCommand(test, server, "add", "--summary", "Checkup", "--starts", "2030-01-02T10:00", "--private"); err != nil {
		test.Fatal(err)
	}
	if value, said := sentIsPrivate(); !said || value != true {
		test.Errorf("--private sends isPrivate: %v", saved)
	}
	if _, err := runCalendarCommand(test, server, "edit", "e1", "--private=false"); err != nil {
		test.Fatal(err)
	}
	if value, said := sentIsPrivate(); !said || value != false {
		test.Errorf("--private=false takes the mark off: %v", saved)
	}
	if _, err := runCalendarCommand(test, server, "edit", "e1", "--summary", "Check-up"); err != nil {
		test.Fatal(err)
	}
	if _, said := sentIsPrivate(); said {
		test.Errorf("saying nothing sends nothing: %v", saved)
	}

	output, err := runCalendarCommand(test, server, "list", "--from", "2030-01-02", "--until", "2030-01-03")
	if err != nil {
		test.Fatal(err)
	}
	var markedLines int
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "private") {
			markedLines++
			if !strings.Contains(line, "Checkup") {
				test.Errorf("the mark is on the wrong line: %s", line)
			}
		}
	}
	if markedLines != 1 {
		test.Errorf("one event is marked private:\n%s", output)
	}
	output, err = runCalendarCommand(test, server, "show", "e1")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "private") {
		test.Errorf("show says it is private:\n%s", output)
	}
}
