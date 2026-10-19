package health

import (
	"context"
	"encoding/json"
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
	return &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionHealthUse}}
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

// called says whether an operation was sent, and with which variables.
func (self *routing) called(operation string) (map[string]any, bool) {
	self.lock.Lock()
	defer self.lock.Unlock()
	variables, isCalled := self.sent[operation]
	return variables, isCalled
}

// fakeRun is a run whose operations are these, for a person in UTC.
type fakeRun struct {
	tools.Run
	operations tools.Operations
	shownPath  string
	canShow    bool
}

func (self *fakeRun) CanShowPages() bool   { return self.canShow }
func (self *fakeRun) ShowPage(path string) { self.shownPath = path }

func (self *fakeRun) Operations() tools.Operations         { return self.operations }
func (self *fakeRun) Configuration() *config.Configuration { return config.Default() }
func (self *fakeRun) Owner() *models.User {
	return &models.User{ID: "u-ada", Username: "ada", Timezone: "UTC"}
}

// chattingRun is a run whose words are posted to the people of a chat.
type chattingRun struct{ *fakeRun }

func (self chattingRun) IsAnsweringInChat() bool { return true }

func runOf(operations tools.Operations) *fakeRun { return &fakeRun{operations: operations} }

// act runs one action of the health tool as the model does: through the
// merged tool, naming the action. The answer is the tool's content.
func act(t *testing.T, run tools.Run, action, arguments string) (string, *tools.Result, error) {
	t.Helper()
	merged := tools.Build().Get("health")
	if merged == nil {
		t.Fatal("no health tool")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(arguments), &fields); err != nil {
		t.Fatalf("%s: %s", err, arguments)
	}
	fields["action"] = action
	encoded, _ := json.Marshal(fields)
	result, err := merged.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call", Arguments: encoded})
	if err != nil {
		return "", nil, err
	}
	return result.Content, result, nil
}

// actJSON is act for an action that answers an object.
func actJSON(t *testing.T, run tools.Run, action, arguments string) (map[string]any, *tools.Result) {
	t.Helper()
	content, result, err := act(t, run, action, arguments)
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(content), &answer); err != nil {
		t.Fatalf("%s: %s", err, content)
	}
	return answer, result
}

const (
	switchOn  = `{"canAgentReadHealth":true,"sampleCount":3}`
	switchOff = `{"canAgentReadHealth":false,"sampleCount":3}`
)

func stepsSummary() string {
	return `{"healthType":"stepCount","quantityUnit":"count","summaryKind":"sum","periods":[
		{"startDay":"2026-10-12","endDay":"2026-10-12","sumValue":8412,"sampleCount":96},
		{"startDay":"2026-10-13","endDay":"2026-10-13","sumValue":10250.4,"sampleCount":120}]}`
}

// While the switch is off every action answers in one sentence that says where
// to turn it on, as an answer and not an error, and reads nothing else.
func TestEveryActionRefusesInOneSentenceWhileTheSwitchIsOff(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ReadHealthSettings": switchOff,
		"ListHealthTypes":    `[{"healthType":"stepCount"}]`,
		"ReadHealthSummary":  stepsSummary(),
		"ListHealthSamples":  `{"samples":[],"totalCount":0}`,
	}}
	for action, arguments := range map[string]string{
		"types":   `{}`,
		"summary": `{"health_type":"stepCount"}`,
		"samples": `{"health_type":"stepCount"}`,
	} {
		content, result, err := act(t, runOf(operations), action, arguments)
		if err != nil {
			t.Fatalf("%s should answer, not fail: %s", action, err)
		}
		if !strings.Contains(content, "has not let you read their Health data") ||
			!strings.Contains(content, "Settings > Health") || !strings.Contains(content, "iPhone app") || !strings.Contains(content, "set_up") {
			t.Errorf("%s: %q", action, content)
		}
		if strings.Contains(content, "\n") || strings.HasPrefix(strings.TrimSpace(content), "{") {
			t.Errorf("%s: should be plain words, not data: %q", action, content)
		}
		if result.Untrusted {
			t.Errorf("%s: the refusal is ours, not somebody else's words", action)
		}
	}
	for _, operation := range []string{"ListHealthTypes", "ReadHealthSummary", "ListHealthSamples"} {
		if _, isCalled := operations.called(operation); isCalled {
			t.Errorf("%s was sent while the switch was off", operation)
		}
	}
}

// set_up reads no Health data, so it works while the switch is off: it opens
// the Health page where there is a dashboard, and says where Health is
// otherwise.
func TestSetUpWorksWhileTheSwitchIsOff(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadHealthSettings": switchOff}}
	run := runOf(operations)
	run.canShow = true
	answer, _ := actJSON(t, run, "set_up", `{}`)
	if run.shownPath != "/health" || answer["is_shown"] != true || answer["page_path"] != "/health" ||
		!strings.Contains(answer["setup_instructions"].(string), "iPhone") {
		t.Errorf("the dashboard was taken to Health: %v %q", answer, run.shownPath)
	}
	answer, _ = actJSON(t, runOf(operations), "set_up", `{}`)
	if answer["is_shown"] != false || !strings.Contains(answer["setup_instructions"].(string), "side menu") ||
		!strings.Contains(answer["setup_instructions"].(string), "Apple's sheet") {
		t.Errorf("where Health is: %v", answer)
	}
	if _, isCalled := operations.called("ReadHealthSettings"); isCalled {
		t.Error("set_up asks nothing of the switch")
	}
}

// types lists the kinds the person has with the latest day on one line each.
func TestTypesListsWhatThePersonHas(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ReadHealthSettings": switchOn,
		"ListHealthTypes": `[
			{"healthType":"stepCount","healthCategory":"activity","typeName":"Steps","valueKind":"quantity","quantityUnit":"count","summaryKind":"sum",
			 "firstDay":"2026-01-02","lastDay":"2026-10-17","sampleCount":4000,"latestDaySummary":{"summaryDay":"2026-10-17","sumValue":9120,"sampleCount":40}},
			{"healthType":"sleepAnalysis","healthCategory":"sleep","typeName":"Sleep","valueKind":"category","summaryKind":"duration",
			 "firstDay":"2026-09-01","lastDay":"2026-10-17","sampleCount":900,"latestDaySummary":{"summaryDay":"2026-10-17","durationSeconds":26100,"sampleCount":12}}]`,
	}}
	answer, _ := actJSON(t, runOf(operations), "types", `{}`)
	rows := answer["types"].([]any)
	if len(rows) != 2 {
		t.Fatalf("the types: %v", answer)
	}
	steps, sleep := rows[0].(map[string]any), rows[1].(map[string]any)
	if steps["health_type"] != "stepCount" || steps["unit"] != "count" || steps["latest_day"] != "2026-10-17: total 9,120 count; 40 readings" {
		t.Errorf("steps: %v", steps)
	}
	if sleep["latest_day"] != "2026-10-17: time 7h 15m; 12 readings" {
		t.Errorf("sleep: %v", sleep)
	}
	if variables, _ := operations.called("ListHealthTypes"); variables["ownerUserId"] != nil {
		t.Errorf("the person's own types name no owner: %v", variables)
	}
}

// Nothing kept yet says how to get Health going.
func TestTypesWithNothingKeptSaysHowToStart(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadHealthSettings": switchOn, "ListHealthTypes": `[]`}}
	answer, _ := actJSON(t, runOf(operations), "types", `{}`)
	if note, _ := answer["note"].(string); !strings.Contains(note, "iPhone") {
		t.Errorf("the note: %v", answer)
	}
}

// A summary is one line a period with the unit, over the last 30 days unless
// a range is given, by day unless a period is.
func TestSummaryIsOneLinePerPeriodWithUnits(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadHealthSettings": switchOn, "ReadHealthSummary": stepsSummary()}}
	answer, result := actJSON(t, runOf(operations), "summary", `{"health_type":"stepCount"}`)
	periods := answer["periods"].([]any)
	if len(periods) != 2 || periods[0] != "2026-10-12: total 8,412 count; 96 readings" || periods[1] != "2026-10-13: total 10,250 count; 120 readings" {
		t.Errorf("the periods: %v", periods)
	}
	if answer["unit"] != "count" || answer["period"] != "day" {
		t.Errorf("the unit and period: %v", answer)
	}
	if result.Untrusted {
		t.Error("a summary is numbers from the person's own phone")
	}
	variables, _ := operations.called("ReadHealthSummary")
	today := time.Now().UTC().Format(time.DateOnly)
	thirtyDaysBack := time.Now().UTC().AddDate(0, 0, -29).Format(time.DateOnly)
	if variables["toDay"] != today || variables["fromDay"] != thirtyDaysBack || variables["period"] != "day" || variables["healthType"] != "stepCount" {
		t.Errorf("the default range: %v", variables)
	}

	if _, _, err := act(t, runOf(operations), "summary", `{"health_type":"stepCount","from":"2026-09-01","to":"2026-09-30","period":"week"}`); err != nil {
		t.Fatal(err)
	}
	variables, _ = operations.called("ReadHealthSummary")
	if variables["fromDay"] != "2026-09-01" || variables["toDay"] != "2026-09-30" || variables["period"] != "week" {
		t.Errorf("the range asked for: %v", variables)
	}
}

// Averages say their lowest and highest, and sleep says the time in each stage.
func TestSummaryLinesForAveragesAndSleep(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ReadHealthSettings": switchOn,
		"ReadHealthSummary": `{"healthType":"heartRate","quantityUnit":"count/min","summaryKind":"average","periods":[
			{"startDay":"2026-10-12","endDay":"2026-10-18","averageValue":71.46,"minimumValue":48,"maximumValue":152,"sampleCount":900}]}`,
	}}
	answer, _ := actJSON(t, runOf(operations), "summary", `{"health_type":"heartRate","period":"week"}`)
	if line := answer["periods"].([]any)[0]; line != "2026-10-12 to 2026-10-18: average 71.46 count/min (lowest 48, highest 152); 900 readings" {
		t.Errorf("the week: %v", line)
	}

	operations.answers["ReadHealthSummary"] = `{"healthType":"sleepAnalysis","quantityUnit":"","summaryKind":"duration","periods":[
		{"startDay":"2026-10-17","endDay":"2026-10-17","durationSeconds":26100,"sampleCount":14,
		 "categorySeconds":{"asleepCore":12600,"asleepDeep":3000,"asleepREM":5400,"awake":600,"inBed":27000}}]}`
	answer, _ = actJSON(t, runOf(operations), "summary", `{"health_type":"sleepAnalysis"}`)
	want := "2026-10-17: time 7h 15m (inBed 7h 30m, asleepCore 3h 30m, asleepREM 1h 30m, asleepDeep 50m, awake 10m); 14 readings"
	if line := answer["periods"].([]any)[0]; line != want {
		t.Errorf("sleep: %v", line)
	}

	// The server may send the stages as a string holding the object.
	operations.answers["ReadHealthSummary"] = `{"healthType":"sleepAnalysis","summaryKind":"duration","periods":[
		{"startDay":"2026-10-17","endDay":"2026-10-17","durationSeconds":3600,"sampleCount":1,"categorySeconds":"{\"asleepCore\":3600}"}]}`
	answer, _ = actJSON(t, runOf(operations), "summary", `{"health_type":"sleepAnalysis"}`)
	if line := answer["periods"].([]any)[0]; line != "2026-10-17: time 1h (asleepCore 1h); 1 reading" {
		t.Errorf("stages as a string: %v", line)
	}
}

// An empty range says so, and a range of too many periods is refused in words
// before it is asked.
func TestSummaryEmptyAndTooLong(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ReadHealthSettings": switchOn,
		"ReadHealthSummary":  `{"healthType":"stepCount","quantityUnit":"count","summaryKind":"sum","periods":[]}`,
	}}
	answer, _ := actJSON(t, runOf(operations), "summary", `{"health_type":"stepCount"}`)
	if note, _ := answer["note"].(string); note != "no readings in this range" {
		t.Errorf("the note: %v", answer)
	}
	if _, _, err := act(t, runOf(operations), "summary", `{"health_type":"stepCount","from":"2024-01-01","to":"2026-01-01"}`); err == nil || !strings.Contains(err.Error(), "week or month") {
		t.Errorf("two years by day: %v", err)
	}
	if _, _, err := act(t, runOf(operations), "summary", `{"health_type":"stepCount","from":"2024-01-01","to":"2026-01-01","period":"month"}`); err != nil {
		t.Errorf("two years by month is fine: %v", err)
	}
	for _, arguments := range []string{`{}`, `{"health_type":"stepCount","from":"last week"}`, `{"health_type":"stepCount","period":"year"}`, `{"health_type":"stepCount","from":"2026-10-05","to":"2026-10-01"}`} {
		if _, _, err := act(t, runOf(operations), "summary", arguments); err == nil {
			t.Errorf("%s should be refused", arguments)
		}
	}
}

// A username finds the sharer among the shares made to the person, and the
// summary is asked as that sharer; somebody who shares nothing is refused.
func TestSummaryOfSomebodyWhoSharesHealth(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ReadHealthSettings": switchOn,
		"ReadHealthSummary":  stepsSummary(),
		"ListHealthTypes":    `[]`,
		"ListHealthShares": `[
			{"id":"s1","ownerUserId":"u-ada","ownerUsername":"ada","granteeUserId":"u-ben","granteeUsername":"ben","healthCategories":["sleep"]},
			{"id":"s2","ownerUserId":"u-cleo","ownerUsername":"cleo","granteeCircleId":"c1","granteeCircleName":"Family","healthCategories":["activity"]}]`,
	}}
	if _, _, err := act(t, runOf(operations), "summary", `{"health_type":"stepCount","username":"Cleo"}`); err != nil {
		t.Fatal(err)
	}
	if variables, _ := operations.called("ReadHealthSummary"); variables["ownerUserId"] != "u-cleo" {
		t.Errorf("asked as the sharer: %v", variables)
	}
	if _, _, err := act(t, runOf(operations), "types", `{"username":"cleo"}`); err != nil {
		t.Fatal(err)
	}
	if variables, _ := operations.called("ListHealthTypes"); variables["ownerUserId"] != "u-cleo" {
		t.Errorf("types as the sharer: %v", variables)
	}

	// ben is shared with, not sharing; the person's own name is their own data.
	if _, _, err := act(t, runOf(operations), "summary", `{"health_type":"stepCount","username":"ben"}`); err == nil || !strings.Contains(err.Error(), "does not share Health") {
		t.Errorf("ben shares nothing: %v", err)
	}
	if _, _, err := act(t, runOf(operations), "summary", `{"health_type":"stepCount","username":"ada"}`); err != nil {
		t.Fatal(err)
	}
	if variables, _ := operations.called("ReadHealthSummary"); variables["ownerUserId"] != nil {
		t.Errorf("the person's own name reads their own data: %v", variables)
	}

	// Nobody else's Health is repeated into a chat.
	if _, _, err := act(t, chattingRun{runOf(operations)}, "summary", `{"health_type":"stepCount","username":"cleo"}`); err == nil || !strings.Contains(err.Error(), "chat") {
		t.Errorf("in a chat: %v", err)
	}
	if _, _, err := act(t, chattingRun{runOf(operations)}, "summary", `{"health_type":"stepCount"}`); err != nil {
		t.Errorf("the person's own in a chat: %v", err)
	}
}

func heartRateSamples(nextOffset string) string {
	return `{"totalCount":45,"nextOffset":` + nextOffset + `,"samples":[
		{"healthSampleId":"h1","healthType":"heartRate","startedAt":"2026-10-17T07:12:00Z","endedAt":"2026-10-17T07:12:00Z","quantityValue":72,"quantityUnit":"count/min",
		 "sourceName":"Watch","deviceName":"Watch","sampleMetadataJson":"{}","hasRoute":false},
		{"healthSampleId":"h2","healthType":"workout","startedAt":"2026-10-17T06:00:00Z","endedAt":"2026-10-17T06:35:00Z","workoutActivityType":"running",
		 "sourceName":"Watch","sampleMetadataJson":"{\"totalDistanceMeters\":5200}","hasRoute":true}]}`
}

// samples pages: the first page says there is more and where to read on, the
// last says there is not; the range is whole days; readings are untrusted.
func TestSamplesPageAndSayWhenThereIsMore(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadHealthSettings": switchOn, "ListHealthSamples": heartRateSamples("20")}}
	answer, result := actJSON(t, runOf(operations), "samples", `{"health_type":"heartRate","from":"2026-10-17","to":"2026-10-17"}`)
	if answer["has_more"] != true || answer["next_offset"] != float64(20) || answer["total_count"] != float64(45) {
		t.Errorf("the first page: %v", answer)
	}
	if !result.Untrusted {
		t.Error("sources and details are written by other apps")
	}
	rows := answer["samples"].([]any)
	first, second := rows[0].(map[string]any), rows[1].(map[string]any)
	if first["reading"] != "72 count/min" || first["started"] != "2026-10-17 07:12" || first["source_name"] != "Watch" {
		t.Errorf("a reading: %v", first)
	}
	if second["workout_activity_type"] != "running" || second["lasted"] != "35m" || second["has_route"] != true ||
		second["details"].(map[string]any)["totalDistanceMeters"] != float64(5200) {
		t.Errorf("a workout: %v", second)
	}
	variables, _ := operations.called("ListHealthSamples")
	if variables["limitCount"] != float64(20) && variables["limitCount"] != 20 {
		t.Errorf("the default page: %v", variables)
	}
	if variables["from"] != "2026-10-17T00:00:00Z" || variables["to"] != "2026-10-18T00:00:00Z" || variables["offset"] != nil {
		t.Errorf("whole days: %v", variables)
	}

	operations.answers["ListHealthSamples"] = heartRateSamples("null")
	answer, _ = actJSON(t, runOf(operations), "samples", `{"health_type":"heartRate","limit":50,"offset":20}`)
	if answer["has_more"] != false || answer["next_offset"] != nil {
		t.Errorf("the last page: %v", answer)
	}
	variables, _ = operations.called("ListHealthSamples")
	if (variables["limitCount"] != float64(50) && variables["limitCount"] != 50) || (variables["offset"] != float64(20) && variables["offset"] != 20) {
		t.Errorf("the page asked for: %v", variables)
	}
}

// Sleep readings are named by stage, and the limit and the owner are held.
func TestSamplesNameSleepStagesAndHoldTheLimits(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ReadHealthSettings": switchOn,
		"ListHealthSamples": `{"totalCount":2,"samples":[
			{"healthSampleId":"s1","healthType":"sleepAnalysis","startedAt":"2026-10-16T23:10:00Z","endedAt":"2026-10-17T02:40:00Z","categoryValue":3,"sampleMetadataJson":"{}"},
			{"healthSampleId":"s2","healthType":"sleepAnalysis","startedAt":"2026-10-17T02:40:00Z","endedAt":"2026-10-17T03:30:00Z","categoryValue":4,"sampleMetadataJson":"{}"}]}`,
	}}
	answer, _ := actJSON(t, runOf(operations), "samples", `{"health_type":"sleepAnalysis"}`)
	rows := answer["samples"].([]any)
	if rows[0].(map[string]any)["reading"] != "asleepCore" || rows[0].(map[string]any)["lasted"] != "3h 30m" || rows[1].(map[string]any)["reading"] != "asleepDeep" {
		t.Errorf("the stages: %v", rows)
	}
	for _, arguments := range []string{`{"health_type":"heartRate","limit":0}`, `{"health_type":"heartRate","limit":500}`, `{}`, `{"health_type":"heartRate","username":"cleo"}`} {
		if _, _, err := act(t, runOf(operations), "samples", arguments); err == nil {
			t.Errorf("%s should be refused", arguments)
		}
	}
	if _, _, err := act(t, runOf(operations), "samples", `{"health_type":"heartRate","username":"ada"}`); err != nil {
		t.Errorf("the person's own name is their own data: %v", err)
	}
}

// The tool only reads, needs health:use, and tells the model what to prefer.
func TestTheToolIsReadOnlyAndSaysWhatToPrefer(t *testing.T) {
	tool := tools.Build().Get("health")
	if tool == nil {
		t.Fatal("no health tool")
	}
	if tool.Risk != tools.RiskRead {
		t.Errorf("risk %v", tool.Risk)
	}
	hasPermission := false
	for _, permission := range tool.Permissions {
		hasPermission = hasPermission || permission == models.PermissionHealthUse
	}
	if !hasPermission {
		t.Errorf("permissions %v", tool.Permissions)
	}
	for _, words := range []string{"Prefer summary", "samples only for particular readings", "workout", "sleepAnalysis", "category seconds", "unit"} {
		if !strings.Contains(tool.Description, words) {
			t.Errorf("the description should say %q", words)
		}
	}
	properties, _ := tool.Parameters["properties"].(map[string]any)
	for _, name := range []string{"action", "health_type", "from", "to", "period", "username", "limit", "offset"} {
		if properties[name] == nil {
			t.Errorf("no parameter %s", name)
		}
	}
}
