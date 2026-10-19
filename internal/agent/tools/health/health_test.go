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

	// isUnattended makes it a run nobody is watching, and isHeadless a
	// headless one (a dream, a schedule, a goal).
	isUnattended bool
	isHeadless   bool
}

func (self *fakeRun) Headless() bool { return self.isHeadless }
func (self *fakeRun) CanAsk() bool   { return !self.isUnattended && !self.isHeadless }

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
			!strings.Contains(content, "the Settings tab of the Health page on the dashboard, or in Health in the iPhone app's Settings") || !strings.Contains(content, "set_up") {
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
// the Settings tab of the Health page, where the switch is, where there is a
// dashboard, and says where Health is otherwise. With the switch on it opens
// the Health page itself.
func TestSetUpWorksWhileTheSwitchIsOff(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadHealthSettings": switchOff}}
	run := runOf(operations)
	run.canShow = true
	answer, _ := actJSON(t, run, "set_up", `{}`)
	if run.shownPath != "/health?tab=settings" || answer["is_shown"] != true || answer["page_path"] != "/health?tab=settings" ||
		!strings.Contains(answer["setup_instructions"].(string), "iPhone") {
		t.Errorf("the dashboard was taken to where the switch is: %v %q", answer, run.shownPath)
	}
	operations = &routing{answers: map[string]string{"ReadHealthSettings": switchOn}}
	run = runOf(operations)
	run.canShow = true
	if answer, _ = actJSON(t, run, "set_up", `{}`); run.shownPath != "/health" || answer["page_path"] != "/health" {
		t.Errorf("with the switch on, the Health page: %v %q", answer, run.shownPath)
	}

	operations = &routing{answers: map[string]string{"ReadHealthSettings": switchOff}}
	answer, _ = actJSON(t, runOf(operations), "set_up", `{}`)
	words, _ := answer["setup_instructions"].(string)
	if answer["is_shown"] != false || !strings.Contains(words, "side menu") || !strings.Contains(words, "Apple's Health permissions screen") ||
		!strings.Contains(words, "the Settings tab of the Health page on the dashboard, or in Health in the iPhone app's Settings") ||
		strings.Contains(words, "Settings > Health") || strings.Contains(words, "Apple's sheet") {
		t.Errorf("where Health is: %v", answer)
	}
	if _, isCalled := operations.called("ReadHealthSettings"); isCalled {
		t.Error("where there is no dashboard to take, set_up asks nothing of the switch")
	}
}

// types lists the kinds the person has with the latest day on one line each.
func TestTypesListsWhatThePersonHas(t *testing.T) {
	born := time.Now().UTC().AddDate(-40, 0, -1).Format(time.DateOnly)
	operations := &routing{answers: map[string]string{
		"ReadHealthSettings": `{"canAgentReadHealth":true,"characteristics":[
			{"healthType":"dateOfBirth","characteristicValue":"` + born + `"},{"healthType":"bloodType","characteristicValue":"oNegative"},
			{"healthType":"biologicalSex","characteristicValue":"female"}]}`,
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
	if steps["health_type"] != "stepCount" || steps["unit_text"] != "steps" || steps["sample_count"] != float64(4000) || steps["latest_day"] != "2026-10-17: total 9,120 steps; 40 readings" {
		t.Errorf("steps: %v", steps)
	}
	if sleep["latest_day"] != "2026-10-17: asleep 7 h 15 min; 12 readings" {
		t.Errorf("sleep: %v", sleep)
	}
	// What the Health app keeps once is here too, a date of birth as an age.
	characteristics, _ := answer["characteristics"].([]any)
	if len(characteristics) != 3 {
		t.Fatalf("the characteristics: %v", answer["characteristics"])
	}
	age, bloodType := characteristics[0].(map[string]any), characteristics[1].(map[string]any)
	if age["characteristic_name"] != "Age" || age["characteristic_text"] != "40 years" || bloodType["characteristic_text"] != "O-" ||
		characteristics[2].(map[string]any)["characteristic_text"] != "Female" {
		t.Errorf("the characteristics: %v", characteristics)
	}
	if strings.Contains(string(mustMarshal(t, answer)), born) {
		t.Error("a date of birth is told as an age, not as a date")
	}
	if variables, _ := operations.called("ListHealthTypes"); variables["ownerUserId"] != nil {
		t.Errorf("the person's own types name no owner: %v", variables)
	}
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
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
	if len(periods) != 2 || periods[0] != "2026-10-12: total 8,412 steps; 96 readings" || periods[1] != "2026-10-13: total 10,250 steps; 120 readings" {
		t.Errorf("the periods: %v", periods)
	}
	if answer["unit_text"] != "steps" || answer["period"] != "day" {
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
	if line := answer["periods"].([]any)[0]; line != "2026-10-12 to 2026-10-18: average 71 bpm (lowest 48 bpm, highest 152 bpm); 900 readings" {
		t.Errorf("the week: %v", line)
	}

	operations.answers["ReadHealthSummary"] = `{"healthType":"sleepAnalysis","quantityUnit":"","summaryKind":"duration","periods":[
		{"startDay":"2026-10-17","endDay":"2026-10-17","durationSeconds":26100,"sampleCount":14,
		 "categorySeconds":{"asleepCore":12600,"asleepDeep":3000,"asleepREM":5400,"awake":600,"inBed":27000}}]}`
	answer, _ = actJSON(t, runOf(operations), "summary", `{"health_type":"sleepAnalysis"}`)
	want := "2026-10-17: asleep 7 h 15 min (Deep 50 min, Core 3 h 30 min, REM 1 h 30 min, Awake 10 min, In bed 7 h 30 min); 14 readings"
	if line := answer["periods"].([]any)[0]; line != want {
		t.Errorf("sleep: %v", line)
	}

	// The server may send the stages as a string holding the object.
	operations.answers["ReadHealthSummary"] = `{"healthType":"sleepAnalysis","summaryKind":"duration","periods":[
		{"startDay":"2026-10-17","endDay":"2026-10-17","durationSeconds":3600,"sampleCount":1,"categorySeconds":"{\"asleepCore\":3600}"}]}`
	answer, _ = actJSON(t, runOf(operations), "summary", `{"health_type":"sleepAnalysis"}`)
	if line := answer["periods"].([]any)[0]; line != "2026-10-17: asleep 1 h (Core 1 h); 1 reading" {
		t.Errorf("stages as a string: %v", line)
	}

	// A week of sleep is the average night with the total, as the dashboard
	// shows it, and the stages are those of an average night.
	operations.answers["ReadHealthSummary"] = `{"healthType":"sleepAnalysis","summaryKind":"duration","periods":[
		{"startDay":"2026-10-12","endDay":"2026-10-18","durationSeconds":151200,"sampleCount":80,"dayCount":6,
		 "categorySeconds":{"asleep":151200,"asleepCore":75600,"asleepDeep":21600,"asleepREM":32400,"awake":7200}}]}`
	answer, _ = actJSON(t, runOf(operations), "summary", `{"health_type":"sleepAnalysis","period":"week"}`)
	want = "2026-10-12 to 2026-10-18: asleep average 7 h a night over 6 nights, total 42 h (a night: Deep 1 h, Core 3 h 30 min, REM 1 h 30 min, Awake 20 min); 80 readings"
	if line := answer["periods"].([]any)[0]; line != want {
		t.Errorf("a week of sleep:\n got %v\nwant %v", line, want)
	}

	// A month of steps is the total and the average day over the days that
	// have data.
	operations.answers["ReadHealthSummary"] = `{"healthType":"stepCount","quantityUnit":"count","summaryKind":"sum","periods":[
		{"startDay":"2026-09-01","endDay":"2026-09-30","sumValue":240000,"sampleCount":900,"dayCount":24}]}`
	answer, _ = actJSON(t, runOf(operations), "summary", `{"health_type":"stepCount","period":"month","from":"2026-09-01","to":"2026-09-30"}`)
	want = "2026-09-01 to 2026-09-30: total 240,000 steps; average 10,000 steps a day over 24 days; 900 readings"
	if line := answer["periods"].([]any)[0]; line != want {
		t.Errorf("a month of steps:\n got %v\nwant %v", line, want)
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

// The tool reads the person's own Health and nothing else: it has no username
// to name a sharer, never asks for the shares made to the person, and never
// sends an owner. A person who shares Health shares it with people, not with
// their agents' model providers.
func TestTheToolReadsOnlyThePersonsOwnHealth(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ReadHealthSettings": switchOn,
		"ReadHealthSummary":  stepsSummary(),
		"ListHealthTypes":    `[]`,
		"ListHealthShares":   `[{"id":"s2","ownerUserId":"u-cleo","ownerUsername":"cleo","granteeCircleId":"c1","healthCategories":["activity"]}]`,
	}}
	for _, run := range []tools.Run{runOf(operations), chattingRun{runOf(operations)}} {
		if _, _, err := act(t, run, "summary", `{"health_type":"stepCount"}`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := act(t, run, "types", `{}`); err != nil {
			t.Fatal(err)
		}
	}
	for _, operation := range []string{"ReadHealthSummary", "ListHealthTypes"} {
		if variables, _ := operations.called(operation); variables["ownerUserId"] != nil {
			t.Errorf("%s asked as somebody else: %v", operation, variables)
		}
	}
	if _, isCalled := operations.called("ListHealthShares"); isCalled {
		t.Error("the shares made to the person are not the tool's business")
	}
	// Naming somebody does not reach their data either.
	if _, _, err := act(t, runOf(operations), "summary", `{"health_type":"stepCount","username":"cleo"}`); err != nil {
		t.Logf("a username is refused or ignored: %s", err)
	}
	if variables, _ := operations.called("ReadHealthSummary"); variables["ownerUserId"] != nil {
		t.Errorf("a username must not reach somebody else's data: %v", variables)
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
	if answer["has_more"] != true || answer["next_offset"] != float64(20) || answer["total_sample_count"] != float64(45) {
		t.Errorf("the first page: %v", answer)
	}
	if !result.Untrusted {
		t.Error("sources and details are written by other apps")
	}
	rows := answer["samples"].([]any)
	first, second := rows[0].(map[string]any), rows[1].(map[string]any)
	if first["reading_text"] != "72 bpm" || first["started_local"] != "2026-10-17 07:12" || first["source_name"] != "Watch" {
		t.Errorf("a reading: %v", first)
	}
	if second["workout_activity_type"] != "running" || second["duration_text"] != "35 min" || second["has_route"] != true ||
		second["sample_metadata"].(map[string]any)["totalDistanceMeters"] != float64(5200) {
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
	answer, _ = actJSON(t, runOf(operations), "samples", `{"health_type":"heartRate","limit_count":50,"offset":20}`)
	if answer["has_more"] != false || answer["next_offset"] != nil {
		t.Errorf("the last page: %v", answer)
	}
	variables, _ = operations.called("ListHealthSamples")
	if (variables["limitCount"] != float64(50) && variables["limitCount"] != 50) || (variables["offset"] != float64(20) && variables["offset"] != 20) {
		t.Errorf("the page asked for: %v", variables)
	}
}

// Sleep readings are named by stage, as the dashboard names them, and the
// limit is held.
func TestSamplesNameSleepStagesAndHoldTheLimits(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ReadHealthSettings": switchOn,
		"ListHealthSamples": `{"totalCount":2,"samples":[
			{"healthSampleId":"s1","healthType":"sleepAnalysis","startedAt":"2026-10-16T23:10:00Z","endedAt":"2026-10-17T02:40:00Z","categoryValue":3,"sampleMetadataJson":"{}"},
			{"healthSampleId":"s2","healthType":"sleepAnalysis","startedAt":"2026-10-17T02:40:00Z","endedAt":"2026-10-17T03:30:00Z","categoryValue":4,"sampleMetadataJson":"{}"}]}`,
	}}
	answer, _ := actJSON(t, runOf(operations), "samples", `{"health_type":"sleepAnalysis"}`)
	rows := answer["samples"].([]any)
	if rows[0].(map[string]any)["reading_text"] != "Core" || rows[0].(map[string]any)["duration_text"] != "3 h 30 min" || rows[1].(map[string]any)["reading_text"] != "Deep" {
		t.Errorf("the stages: %v", rows)
	}
	for _, arguments := range []string{`{"health_type":"heartRate","limit_count":0}`, `{"health_type":"heartRate","limit_count":500}`, `{}`} {
		if _, _, err := act(t, runOf(operations), "samples", arguments); err == nil {
			t.Errorf("%s should be refused", arguments)
		}
	}
}

// A run nobody is watching never reads Health: a dream, a background subagent
// and a headless run (a schedule, a goal, a research run) get one plain
// sentence, ask nothing of the server, and the person's own conversation still
// reads. The docs say Health never goes into memory and the dreams do not read
// it; this is what makes that true.
func TestARunNobodyIsWatchingNeverReadsHealth(t *testing.T) {
	for name, run := range map[string]*fakeRun{
		"a background run": {isUnattended: true},
		"a headless run":   {isHeadless: true},
		"both":             {isUnattended: true, isHeadless: true},
	} {
		operations := &routing{answers: map[string]string{
			"ReadHealthSettings": switchOn,
			"ListHealthTypes":    `[{"healthType":"stepCount"}]`,
			"ReadHealthSummary":  stepsSummary(),
			"ListHealthSamples":  `{"samples":[],"totalCount":0}`,
		}}
		run.operations = operations
		for action, arguments := range map[string]string{"types": `{}`, "summary": `{"health_type":"stepCount"}`, "samples": `{"health_type":"stepCount"}`} {
			content, result, err := act(t, run, action, arguments)
			if err != nil {
				t.Fatalf("%s %s should answer, not fail: %s", name, action, err)
			}
			if !strings.Contains(content, "not in a dream, a background task or any other run nobody is watching") || strings.Contains(content, "\n") ||
				strings.HasPrefix(strings.TrimSpace(content), "{") || result.Untrusted {
				t.Errorf("%s %s: %q", name, action, content)
			}
		}
		for _, operation := range []string{"ReadHealthSettings", "ListHealthTypes", "ReadHealthSummary", "ListHealthSamples"} {
			if _, isCalled := operations.called(operation); isCalled {
				t.Errorf("%s: %s was sent in a run nobody is watching", name, operation)
			}
		}
	}

	operations := &routing{answers: map[string]string{"ReadHealthSettings": switchOn, "ReadHealthSummary": stepsSummary()}}
	content, _, err := act(t, runOf(operations), "summary", `{"health_type":"stepCount"}`)
	if err != nil || strings.Contains(content, "nobody is watching") || !strings.Contains(content, "8,412 steps") {
		t.Errorf("the person's own conversation reads: %q %v", content, err)
	}
}

// A range over the clock change in spring is counted in calendar days.
func TestPeriodCountCountsCalendarDays(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no time zone database")
	}
	first := time.Date(2026, 3, 1, 0, 0, 0, 0, zone)
	last := time.Date(2026, 3, 31, 0, 0, 0, 0, zone)
	if got := periodCount(first, last, "day"); got != 31 {
		t.Errorf("March has 31 days, counted %d", got)
	}
	first = time.Date(2025, 3, 1, 0, 0, 0, 0, zone)
	last = time.Date(2026, 4, 5, 0, 0, 0, 0, zone)
	if got := periodCount(first, last, "day"); got != 401 {
		t.Errorf("401 calendar days over a clock change, counted %d", got)
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
	for _, words := range []string{"Prefer summary", "samples only for particular readings", "workout", "sleepAnalysis", "Deep, Core, REM, Awake", "unit",
		"never what other people share with them", "not with their agents' model providers",
		"What others share with the person is on the Health page under Showing, on the dashboard and in the iPhone app; you cannot read it",
		"never in a dream, a background task or any other run nobody is watching",
		"the Settings tab of the Health page on the dashboard, or Health in the iPhone app's Settings"} {
		if !strings.Contains(tool.Description, words) {
			t.Errorf("the description should say %q", words)
		}
	}
	properties, _ := tool.Parameters["properties"].(map[string]any)
	for _, name := range []string{"action", "health_type", "from", "to", "period", "limit_count", "offset"} {
		if properties[name] == nil {
			t.Errorf("no parameter %s", name)
		}
	}
	if properties["username"] != nil {
		t.Error("the tool has no way to name another person")
	}
}
