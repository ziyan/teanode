package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
)

// healthServer answers the documents the health commands send with a small
// world and records the variables of each by operation name: ada is the
// caller, cleo shares her activity with the circle Home, and ben and cleo
// share a circle with ada.
func healthServer(test *testing.T) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	answers := map[string]string{
		"ListHealthTypes": `[
			{"healthType":"stepCount","healthCategory":"activity","typeName":"Steps","valueKind":"quantity","quantityUnit":"count","summaryKind":"sum",
			 "firstDay":"2026-01-02","lastDay":"2026-10-17","sampleCount":4000,"latestDaySummary":{"summaryDay":"2026-10-17","sumValue":9120,"sampleCount":40}},
			{"healthType":"sleepAnalysis","healthCategory":"sleep","typeName":"Sleep","valueKind":"category","summaryKind":"duration",
			 "firstDay":"2026-09-01","lastDay":"2026-10-17","sampleCount":900,"latestDaySummary":{"summaryDay":"2026-10-17","durationSeconds":26100,"sampleCount":12}}]`,
		"ReadHealthSummary": `{"healthType":"stepCount","quantityUnit":"count","summaryKind":"sum","periods":[
			{"startDay":"2026-10-12","endDay":"2026-10-18","sumValue":61250.6,"sampleCount":700},
			{"startDay":"2026-10-19","endDay":"2026-10-19","sumValue":8000,"sampleCount":90}]}`,
		"ListHealthSamples": `{"totalCount":45,"nextOffset":20,"samples":[
			{"healthSampleId":"h1","healthType":"heartRate","startedAt":"2026-10-17T07:12:00Z","endedAt":"2026-10-17T07:12:00Z","quantityValue":72,"quantityUnit":"count/min","sourceName":"Watch","hasRoute":false},
			{"healthSampleId":"h2","healthType":"workout","startedAt":"2026-10-17T06:00:00Z","endedAt":"2026-10-17T06:35:00Z","workoutActivityType":"running","sourceName":"Watch","hasRoute":true}]}`,
		"ReadHealthRoute":      `{"healthSampleId":"h2","routePoints":[[0,40.0,-75.0,10],[1800,40.01,-75.02,12]]}`,
		"ReadHealthSettings":   `{"canAgentReadHealth":false,"sampleCount":4900,"lastReceivedAt":"2026-10-17T08:00:00Z","modelProviderName":"Example AI","characteristics":[{"healthType":"bloodType","characteristicValue":"aPositive"}]}`,
		"UpdateHealthSettings": `{"canAgentReadHealth":true,"sampleCount":4900,"modelProviderName":"Example AI","characteristics":[]}`,
		"EraseHealthData":      `{"erasedSampleCount":4000}`,
		"ListHealthShares": `[
			{"id":"s1","ownerUserId":"u-ada","ownerUsername":"ada","granteeCircleId":"c1","granteeCircleName":"Home","healthCategories":["activity","sleep"]},
			{"id":"s2","ownerUserId":"u-cleo","ownerUsername":"cleo","granteeUserId":"u-ada","granteeUsername":"ada","healthCategories":["activity"]}]`,
		"SetHealthShare":   `{"id":"s1","ownerUserId":"u-ada","ownerUsername":"ada","granteeCircleId":"c1","granteeCircleName":"Home","healthCategories":["activity","sleep"]}`,
		"ListCirclePeople": `[{"userId":"u-ben","username":"ben","name":"Ben"},{"userId":"u-cleo","username":"cleo","name":"Cleo"}]`,
		"ListCircles":      `[{"id":"c1","name":"Home","circleKind":"family","members":[],"pendingInvitations":[]}]`,
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

func runHealthCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewHealthCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "health"}, arguments...))
	})
	return output, err
}

// types is a table with the unit, the days, and the latest day in words.
func TestHealthTypesPrintsATable(test *testing.T) {
	server, _ := healthServer(test)
	output, err := runHealthCommand(test, server, "types")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"stepCount", "activity", "Steps", "count", "2026-01-02 to 2026-10-17", "4000", "2026-10-17: 9,120 count", "2026-10-17: 7h 15m"} {
		if !strings.Contains(output, want) {
			test.Errorf("the table should hold %q: %q", want, output)
		}
	}
}

// summary asks for the last 30 days by day, and prints a column for each
// figure the type has, with the unit.
func TestHealthSummaryDefaultsAndColumns(test *testing.T) {
	server, called := healthServer(test)
	output, err := runHealthCommand(test, server, "summary", "stepCount")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("ReadHealthSummary")
	today := time.Now().Format(time.DateOnly)
	thirtyDaysBack := time.Now().AddDate(0, 0, -29).Format(time.DateOnly)
	if variables["healthType"] != "stepCount" || variables["period"] != "day" || variables["toDay"] != today || variables["fromDay"] != thirtyDaysBack || variables["ownerUserId"] != nil {
		test.Errorf("the defaults: %v", variables)
	}
	if !strings.Contains(output, "2026-10-12 to 2026-10-18") || !strings.Contains(output, "61,251 count") || !strings.Contains(output, "total") || strings.Contains(output, "average") {
		test.Errorf("the table: %q", output)
	}

	if _, err := runHealthCommand(test, server, "summary", "stepCount", "--since", "7d", "--to", "2026-10-19", "--period", "week"); err != nil {
		test.Fatal(err)
	}
	variables = called("ReadHealthSummary")
	if variables["period"] != "week" || variables["toDay"] != "2026-10-19" || variables["fromDay"] != time.Now().AddDate(0, 0, -7).Format(time.DateOnly) {
		test.Errorf("the range asked for: %v", variables)
	}
}

func TestHealthSummaryRefusesWhatIsNotARange(test *testing.T) {
	server, called := healthServer(test)
	for _, arguments := range [][]string{
		{"summary"},
		{"summary", "stepCount", "--period", "year"},
		{"summary", "stepCount", "--from", "2026-10-05", "--to", "2026-10-01"},
		{"summary", "stepCount", "--from", "yesterday"},
		{"summary", "stepCount", "--from", "2026-10-01", "--since", "7d"},
	} {
		if _, err := runHealthCommand(test, server, arguments...); err == nil {
			test.Errorf("%v should be refused", arguments)
		}
	}
	if called("ReadHealthSummary") != nil {
		test.Error("nothing was sent")
	}
}

// --user finds the sharer among the shares made to the caller.
func TestHealthSummaryOfSomebodyWhoShares(test *testing.T) {
	server, called := healthServer(test)
	if _, err := runHealthCommand(test, server, "summary", "stepCount", "--user", "Cleo"); err != nil {
		test.Fatal(err)
	}
	if called("ReadHealthSummary")["ownerUserId"] != "u-cleo" {
		test.Errorf("cleo's account was asked about: %v", called("ReadHealthSummary"))
	}
	if _, err := runHealthCommand(test, server, "types", "--user", "cleo"); err != nil {
		test.Fatal(err)
	}
	if called("ListHealthTypes")["ownerUserId"] != "u-cleo" {
		test.Errorf("cleo's types: %v", called("ListHealthTypes"))
	}
	if _, err := runHealthCommand(test, server, "summary", "stepCount", "--user", "stranger"); err == nil || !strings.Contains(err.Error(), "does not share Health") {
		test.Errorf("a stranger: %v", err)
	}
}

// samples pages, and says on standard error how to read on.
func TestHealthSamplesPage(test *testing.T) {
	server, called := healthServer(test)
	output, err := runHealthCommand(test, server, "samples", "heartRate", "--from", "2026-10-17", "--to", "2026-10-17", "--limit", "2", "--offset", "10")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("ListHealthSamples")
	if variables["healthType"] != "heartRate" || variables["limitCount"] != float64(2) || variables["offset"] != float64(10) {
		test.Errorf("the page: %v", variables)
	}
	from, _ := time.Parse(time.RFC3339, variables["from"].(string))
	to, _ := time.Parse(time.RFC3339, variables["to"].(string))
	if to.Sub(from) < 23*time.Hour || to.Sub(from) > 25*time.Hour {
		test.Errorf("a whole day: %v to %v", from, to)
	}
	for _, want := range []string{"72 count/min", "Watch", "running", "35m", "h2"} {
		if !strings.Contains(output, want) {
			test.Errorf("the table should hold %q: %q", want, output)
		}
	}
	if _, err := runHealthCommand(test, server, "samples", "heartRate", "--limit", "500"); err == nil {
		test.Error("more than 200 should be refused")
	}
}

func TestHealthRouteSaysWhereItWent(test *testing.T) {
	server, called := healthServer(test)
	output, err := runHealthCommand(test, server, "route", "h2")
	if err != nil {
		test.Fatal(err)
	}
	if called("ReadHealthRoute")["healthSampleId"] != "h2" || !strings.Contains(output, "30m") || !strings.Contains(output, "40.01000, -75.02000") {
		test.Errorf("the route: %q", output)
	}
	if _, err := runHealthCommand(test, server, "route"); err == nil {
		test.Error("a route needs a workout")
	}
}

// settings reads by default, and changes the agent's switch only when told.
func TestHealthSettingsReadAndSwitch(test *testing.T) {
	server, called := healthServer(test)
	output, err := runHealthCommand(test, server, "settings")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "off: your agent cannot read") || !strings.Contains(output, "4900") || !strings.Contains(output, "aPositive") {
		test.Errorf("the settings: %q", output)
	}
	if called("UpdateHealthSettings") != nil {
		test.Error("reading changes nothing")
	}
	output, err = runHealthCommand(test, server, "settings", "--agent-can-read", "on")
	if err != nil {
		test.Fatal(err)
	}
	if called("UpdateHealthSettings")["canAgentReadHealth"] != true || !strings.Contains(output, "which goes to Example AI") {
		test.Errorf("turning it on: %v %q", called("UpdateHealthSettings"), output)
	}
	if _, err := runHealthCommand(test, server, "settings", "--agent-can-read", "off"); err != nil {
		test.Fatal(err)
	}
	if called("UpdateHealthSettings")["canAgentReadHealth"] != false {
		test.Errorf("turning it off: %v", called("UpdateHealthSettings"))
	}
	if _, err := runHealthCommand(test, server, "settings", "--agent-can-read", "maybe"); err == nil {
		test.Error("on or off")
	}
}

// erase refuses without --force when nobody can be asked, and names the type
// only when one was given.
func TestHealthEraseNeedsForce(test *testing.T) {
	server, called := healthServer(test)
	if _, err := runHealthCommand(test, server, "erase"); err == nil || !strings.Contains(err.Error(), "--force") {
		test.Errorf("erase without confirmation: %v", err)
	}
	if _, err := runHealthCommand(test, server, "erase", "stepCount"); err == nil || !strings.Contains(err.Error(), "stepCount") {
		test.Errorf("a type without confirmation: %v", err)
	}
	if called("EraseHealthData") != nil {
		test.Error("nothing was sent")
	}
	output, err := runHealthCommand(test, server, "erase", "--force")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("EraseHealthData"); variables["healthType"] != nil || !strings.Contains(output, "4,000 readings") && !strings.Contains(output, "4000 readings") {
		test.Errorf("erasing everything: %v %q", variables, output)
	}
	if _, err := runHealthCommand(test, server, "erase", "stepCount", "--force"); err != nil {
		test.Fatal(err)
	}
	if called("EraseHealthData")["healthType"] != "stepCount" {
		test.Errorf("erasing one type: %v", called("EraseHealthData"))
	}
	if _, err := runHealthCommand(test, server, "erase", "a", "b", "--force"); err == nil {
		test.Error("one type at most")
	}
}

// shares lists both directions, and share resolves a circle or a person and
// sends the categories; none takes the share away.
func TestHealthSharesAndShare(test *testing.T) {
	server, called := healthServer(test)
	output, err := runHealthCommand(test, server, "shares")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"ada", "the circle \"Home\"", "activity, sleep", "cleo"} {
		if !strings.Contains(output, want) {
			test.Errorf("the shares should hold %q: %q", want, output)
		}
	}

	output, err = runHealthCommand(test, server, "share", "Home", "activity", "sleep")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("SetHealthShare")
	categories, _ := variables["healthCategories"].([]any)
	if variables["granteeCircleId"] != "c1" || variables["granteeUserId"] != nil || len(categories) != 2 || categories[0] != "activity" || categories[1] != "sleep" {
		test.Errorf("sharing with a circle: %v", variables)
	}
	if !strings.Contains(output, "sharing activity, sleep with") {
		test.Errorf("what was said: %q", output)
	}

	if _, err := runHealthCommand(test, server, "share", "ben", "heart"); err != nil {
		test.Fatal(err)
	}
	if variables = called("SetHealthShare"); variables["granteeUserId"] != "u-ben" || variables["granteeCircleId"] != nil {
		test.Errorf("sharing with a person: %v", variables)
	}

	output, err = runHealthCommand(test, server, "share", "ben")
	if err != nil {
		test.Fatal(err)
	}
	if categories, _ = called("SetHealthShare")["healthCategories"].([]any); categories == nil || len(categories) != 0 || !strings.Contains(output, "no longer sharing Health with") {
		test.Errorf("no category takes the share away: %v %q", called("SetHealthShare"), output)
	}
	if _, err := runHealthCommand(test, server, "share"); err == nil {
		test.Error("a share needs somebody")
	}
	if _, err := runHealthCommand(test, server, "share", "nobody", "sleep"); err == nil {
		test.Error("somebody who is neither a person nor a circle")
	}
}

// --json prints the view as the API names it.
func TestHealthJSON(test *testing.T) {
	server, _ := healthServer(test)
	output, err := runHealthCommand(test, server, "summary", "stepCount", "--json")
	if err != nil {
		test.Fatal(err)
	}
	var summary struct {
		HealthType string `json:"healthType"`
		Periods    []struct {
			StartDay string  `json:"startDay"`
			SumValue float64 `json:"sumValue"`
		} `json:"periods"`
	}
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		test.Fatalf("%s: %s", err, output)
	}
	if summary.HealthType != "stepCount" || len(summary.Periods) != 2 || summary.Periods[0].SumValue != 61250.6 {
		test.Errorf("the summary: %+v", summary)
	}
}
