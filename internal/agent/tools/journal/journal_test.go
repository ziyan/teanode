package journal

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
// it names, and keeps every document and the variables it was sent.
type routing struct {
	lock      sync.Mutex
	answers   map[string]string
	sent      map[string]map[string]any
	operation []string
}

func (self *routing) Permissions() *models.EffectivePermissions {
	return &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionJournalUse}}
}

func (self *routing) Execute(_ context.Context, document string, variables map[string]any, result any) error {
	self.lock.Lock()
	defer self.lock.Unlock()
	data := map[string]json.RawMessage{}
	for operation, answer := range self.answers {
		if strings.Contains(document, operation+"(") || strings.Contains(document, operation+" ") {
			data[operation] = json.RawMessage(answer)
			if self.sent == nil {
				self.sent = map[string]map[string]any{}
			}
			self.sent[operation] = variables
			self.operation = append(self.operation, operation)
		}
	}
	if result == nil {
		return nil
	}
	encoded, _ := json.Marshal(data)
	return json.Unmarshal(encoded, result)
}

func (self *routing) called(operation string) (map[string]any, bool) {
	self.lock.Lock()
	defer self.lock.Unlock()
	variables, isCalled := self.sent[operation]
	return variables, isCalled
}

// fakeRun is a run for Ada, who lives in UTC.
type fakeRun struct {
	tools.Run
	operations tools.Operations

	// isStartedByPerson is a turn Ada typed; isHeadless a run nobody is
	// watching; isInChat one whose words go to a room.
	isStartedByPerson bool
	isHeadless        bool
	isInChat          bool
}

func (self *fakeRun) Headless() bool                       { return self.isHeadless }
func (self *fakeRun) CanAsk() bool                         { return !self.isHeadless }
func (self *fakeRun) Surface() string                      { return "" }
func (self *fakeRun) IsStartedByPerson() bool              { return self.isStartedByPerson }
func (self *fakeRun) IsAnsweringInChat() bool              { return self.isInChat }
func (self *fakeRun) Operations() tools.Operations         { return self.operations }
func (self *fakeRun) Configuration() *config.Configuration { return config.Default() }
func (self *fakeRun) Owner() *models.User {
	return &models.User{ID: "u-ada", Username: "ada", Timezone: "UTC"}
}

// aTurnOfAda is a conversation Ada started.
func aTurnOfAda(operations tools.Operations) *fakeRun {
	return &fakeRun{operations: operations, isStartedByPerson: true}
}

// act runs one action of the journal tool as the model does: through the
// merged tool, naming the action.
func act(t *testing.T, run tools.Run, action, arguments string) (string, *tools.Result, error) {
	t.Helper()
	merged := tools.Build().Get("journal")
	if merged == nil {
		t.Fatal("no journal tool")
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

func actJSON(t *testing.T, run tools.Run, action, arguments string) map[string]any {
	t.Helper()
	content, _, err := act(t, run, action, arguments)
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(content), &answer); err != nil {
		t.Fatalf("%s: %s", err, content)
	}
	return answer
}

const (
	switchOn  = `{"canAgentReadHealth":true}`
	switchOff = `{"canAgentReadHealth":false}`
)

// harborDay is Ada's invented Thursday: her words, a mood that came from
// Apple Health, the numbers with Health in them, a narrative, a mail TeaNode
// chose, and three moments one of which is a workout.
func harborDay() string {
	return `{
	"day":"2026-10-22","timeZone":"UTC","isToday":false,"weekday":"thursday","weekStart":"2026-10-19",
	"words":{"original":"Lunch with Ben at the harbor.","modifiedAt":"2026-10-22T20:00:00Z","isTouchUpOn":true,"touchedUp":"I had lunch with Ben at the harbor.","touchUpHow":"fix the grammar","isTouchedUpStale":false,"touchUpPending":false,"journalNotes":[]},
	"mood":{"moodLevel":4,"moodWords":"","moodSource":"health"},
	"weather":{"weatherCode":3,"weatherKind":"cloudy","highCelsius":14,"lowCelsius":8,"precipitationMM":0,"placeName":"Harbor Office"},
	"facts":{"photoCount":2,"stepCount":8200,"sleepMinutes":430,"workoutMinutes":35,"walkedMeters":5400,"mailReceivedCount":3,"mainPlaceName":"Harbor Office"},
	"narrative":{"headline":"A harbor lunch","narrative":"You had lunch with Ben, then read [[mail:m1]].","references":[{"recordKind":"mail","recordId":"m1","reference":"mail:m1","title":"Budget for the dock","isUnavailable":false}],"isStale":false,"narrativePending":false,"failure":"","corrections":[{"correctionText":"It was Ben, not Sam"}]},
	"choices":[{"linkId":"l1","record":{"recordKind":"mail","recordId":"m1","reference":"mail:m1","title":"Budget for the dock"},"linkSource":"agent","linkReason":"you were asked to decide","isHidden":false}],
	"pins":[],
	"photoTotal":2,"documentTotal":0,"mailTotal":3,"unavailableSections":[],
	"health":{"stepCount":8200,"sleepMinutes":430,"restingHeartRate":58,"workouts":[{"activityType":"running","minutes":35,"meters":5400}]},
	"moments":[
		{"at":"2026-10-22T12:00:00Z","recordKind":"calendar_occurrence","recordId":"e1@2026-10-22T12:00:00Z","title":"Lunch with Ben","detail":"Harbor Cafe"},
		{"at":"2026-10-22T13:30:00Z","recordKind":"mail","recordId":"m1","title":"Budget for the dock","detail":"from Ben Quay"},
		{"at":"2026-10-22T17:00:00Z","recordKind":"health_day","recordId":"workout@2026-10-22","title":"Running","detail":"35 min"}]
	}`
}

// Reading a day gives the person's original words and the touched-up version
// marked as TeaNode's, the narrative with the records it mentions, the
// choices with their reasons and the moments in order, and marks the answer as
// data from outside.
func TestDayReadsTheDay(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadJournalDay": harborDay(), "ReadHealthSettings": switchOn}}
	content, result, err := act(t, aTurnOfAda(operations), "day", `{"day":"2026-10-22"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Untrusted {
		t.Errorf("the journal holds words from outside and must be marked as data")
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(content), &answer); err != nil {
		t.Fatalf("%s: %s", err, content)
	}
	words := answer["words"].(map[string]any)
	if words["original"] != "Lunch with Ben at the harbor." || words["touched_up_by_teanode"] != "I had lunch with Ben at the harbor." {
		t.Errorf("the words: %v", words)
	}
	narrative := answer["narrative"].(map[string]any)
	if narrative["headline"] != "A harbor lunch" || narrative["written_by"] != "TeaNode" || len(narrative["mentioned_records"].([]any)) != 1 || len(narrative["corrections"].([]any)) != 1 {
		t.Errorf("the narrative: %v", narrative)
	}
	chosen := answer["chosen_records"].([]any)[0].(map[string]any)
	if chosen["reference"] != "mail:m1" || chosen["reason"] != "you were asked to decide" {
		t.Errorf("the chosen record: %v", chosen)
	}
	moments := answer["moments"].([]any)
	first := moments[0].(map[string]any)
	if len(moments) != 3 || first["time"] != "12:00" || first["reference"] != "calendar_occurrence:e1@2026-10-22T12:00:00Z" || answer["has_more"] != nil {
		t.Errorf("the moments: %v", moments)
	}
	if answer["mood"].(map[string]any)["mood"] != "good" {
		t.Errorf("with the switch on a mood from Apple Health is the day's mood: %v", answer["mood"])
	}
}

// Yesterday is the day when none is asked for, and "yesterday" and "today"
// are days.
func TestDayDefaultsToYesterday(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadJournalDay": harborDay(), "ReadHealthSettings": switchOff}}
	run := aTurnOfAda(operations)
	for _, arguments := range []string{`{}`, `{"day":"yesterday"}`} {
		operations.sent = nil
		actJSON(t, run, "day", arguments)
		variables, _ := operations.called("ReadJournalDay")
		yesterday := operationsDay(-1)
		if variables["day"] != yesterday {
			t.Errorf("%s asked for %v, not %s", arguments, variables["day"], yesterday)
		}
	}
	if _, _, err := act(t, run, "day", `{"day":"last thursday"}`); err == nil {
		t.Errorf("a day that is not a day was taken")
	}
}

// A long day is read in pages: the first has everything and the first moments,
// and says where the next starts; the next has moments only.
func TestDayPagesItsMoments(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadJournalDay": harborDay(), "ReadHealthSettings": switchOn}}
	run := aTurnOfAda(operations)
	first := actJSON(t, run, "day", `{"day":"2026-10-22","limit_count":2}`)
	if first["has_more"] != true || first["next_offset"] != float64(2) || len(first["moments"].([]any)) != 2 || first["words"] == nil {
		t.Errorf("the first page: %v", first)
	}
	second := actJSON(t, run, "day", `{"day":"2026-10-22","limit_count":2,"offset":2}`)
	if len(second["moments"].([]any)) != 1 || second["has_more"] != nil || second["words"] != nil {
		t.Errorf("the second page has only the rest of the moments: %v", second)
	}
}

// With the switch off no Health reaches the model: not the Health section,
// not the Health numbers among the day's facts, not the workout among its
// moments, and not a mood that came from Apple Health. The rest of the day is
// there. With the switch on all of it is.
func TestDayHasNoHealthWhileTheSwitchIsOff(t *testing.T) {
	off := &routing{answers: map[string]string{"ReadJournalDay": harborDay(), "ReadHealthSettings": switchOff}}
	content, _, err := act(t, aTurnOfAda(off), "day", `{"day":"2026-10-22"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, health := range []string{"step_count", "sleep_minutes", "workout_minutes", "walked_meters", "resting_heart_rate", "8200", "430", "workout@", "Running", `"health"`, "mood_level"} {
		if strings.Contains(content, health) {
			t.Errorf("Health reached the agent with the switch off (%q): %s", health, content)
		}
	}
	for _, kept := range []string{"Lunch with Ben at the harbor.", "photo_count", "mail_received_count", "A harbor lunch", "Budget for the dock"} {
		if !strings.Contains(content, kept) {
			t.Errorf("%q was left out with the switch off", kept)
		}
	}

	on := &routing{answers: map[string]string{"ReadJournalDay": harborDay(), "ReadHealthSettings": switchOn}}
	content, _, err = act(t, aTurnOfAda(on), "day", `{"day":"2026-10-22"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, health := range []string{"step_count", "sleep_minutes", "workout_minutes", "resting_heart_rate", "workout@2026-10-22", "mood_level"} {
		if !strings.Contains(content, health) {
			t.Errorf("%q is missing with the switch on: %s", health, content)
		}
	}

	// A mood the person chose is theirs whatever the switch says.
	chosen := strings.Replace(harborDay(), `"moodSource":"health"`, `"moodSource":"person"`, 1)
	off = &routing{answers: map[string]string{"ReadJournalDay": chosen, "ReadHealthSettings": switchOff}}
	if mood := actJSON(t, aTurnOfAda(off), "day", `{"day":"2026-10-22"}`)["mood"]; mood == nil {
		t.Errorf("the mood the person chose was dropped with the switch off")
	}
}

func weekWithAHealthMood() string {
	return `{
	"periodKind":"week","periodStart":"2026-10-19","periodEnd":"2026-10-25","isCurrent":false,
	"lookingBack":{"original":"A week of docks.","isTouchUpOn":false},
	"narrative":{"headline":"Docks and lunches","narrative":"A week at the harbor.","references":[],"corrections":[]},
	"choices":[],
	"days":[
		{"day":"2026-10-21","mood":{"moodLevel":2,"moodWords":"","moodSource":"person"},"headline":"Rain","hasNarrative":true,"facts":{"photoCount":1,"stepCount":3000,"sleepMinutes":400}},
		{"day":"2026-10-22","mood":{"moodLevel":5,"moodWords":"","moodSource":"health"},"headline":"A harbor lunch","hasNarrative":true,"facts":{"photoCount":2,"stepCount":8200,"workoutMinutes":35}}],
	"months":[],
	"totals":{"photoCount":3,"stepCount":11200,"sleepMinutes":830,"workoutMinutes":35,"walkedMeters":9000,"mailReceivedCount":4},
	"moodCounts":[0,1,0,0,1],"dayCount":2,"chapters":[]}`
}

// A week without the switch has no Health among its days' or its sums'
// numbers, and its mood counts leave out the day whose mood came from Apple
// Health; with the switch on it has them all.
func TestPeriodHasNoHealthWhileTheSwitchIsOff(t *testing.T) {
	off := &routing{answers: map[string]string{"ReadJournalPeriod": weekWithAHealthMood(), "ReadHealthSettings": switchOff}}
	content, _, err := act(t, aTurnOfAda(off), "period", `{"period_kind":"week","day":"2026-10-22"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, health := range []string{"step_count", "sleep_minutes", "workout_minutes", "walked_meters", "11200", "8200", `"great"`} {
		if strings.Contains(content, health) {
			t.Errorf("Health reached the agent with the switch off (%q): %s", health, content)
		}
	}
	var answer map[string]any
	_ = json.Unmarshal([]byte(content), &answer)
	counts := answer["mood_counts"].(map[string]any)
	if len(counts) != 1 || counts["low"] != float64(1) || answer["totals"].(map[string]any)["photo_count"] != float64(3) {
		t.Errorf("the mood counts and sums: %v %v", counts, answer["totals"])
	}

	on := &routing{answers: map[string]string{"ReadJournalPeriod": weekWithAHealthMood(), "ReadHealthSettings": switchOn}}
	content, _, err = act(t, aTurnOfAda(on), "period", `{"period_kind":"week","day":"2026-10-22"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, health := range []string{"step_count", "11200", "workout_minutes", `"great"`} {
		if !strings.Contains(content, health) {
			t.Errorf("%q is missing with the switch on: %s", health, content)
		}
	}
}

// A year's months give their mood per day only with the switch on, since the
// view does not say which of them came from Apple Health.
func TestYearHasNoDayMoodsWhileTheSwitchIsOff(t *testing.T) {
	year := `{"periodKind":"year","periodStart":"2026-01-01","periodEnd":"2026-12-31","narrative":{},"days":[],
		"months":[{"periodStart":"2026-10-01","headline":"October","dayMoods":[0,4,5],"placeCount":2,"photoCount":9}],
		"totals":{"stepCount":900000},"moodCounts":[0,0,0,1,1],"dayCount":2,"chapters":[]}`
	off := &routing{answers: map[string]string{"ReadJournalPeriod": year, "ReadHealthSettings": switchOff}}
	content, _, err := act(t, aTurnOfAda(off), "period", `{"period_kind":"year"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, health := range []string{"day_moods", "mood_counts", "900000"} {
		if strings.Contains(content, health) {
			t.Errorf("%q reached the agent with the switch off: %s", health, content)
		}
	}
}

// The timeline of days and the search give pages with next_offset and no
// Health with the switch off.
func TestDaysAndSearchPage(t *testing.T) {
	list := `{"totalCount":3,"today":"2026-10-26","firstDay":"2026-10-20","days":[
		{"day":"2026-10-22","mood":{"moodLevel":5,"moodSource":"health"},"headline":"A harbor lunch","hasNarrative":true,"facts":{"photoCount":2,"stepCount":8200}}]}`
	operations := &routing{answers: map[string]string{"ListJournalDays": list, "ReadHealthSettings": switchOff,
		"SearchJournal": `{"totalCount":2,"hits":[{"day":"2026-10-22","periodKind":"","headline":"A harbor lunch","snippet":"lunch with Ben"}]}`}}
	run := aTurnOfAda(operations)
	answer := actJSON(t, run, "days", `{"from":"2026-10-20","to":"2026-10-26","limit_count":1}`)
	if answer["has_more"] != true || answer["next_offset"] != float64(1) {
		t.Errorf("the page of days: %v", answer)
	}
	if variables, _ := operations.called("ListJournalDays"); variables["fromDay"] != "2026-10-20" || variables["untilDay"] != "2026-10-26" || variables["first"] != 1 {
		t.Errorf("the range asked for: %v", variables)
	}
	content, _, _ := act(t, run, "days", `{"to":"2026-10-26"}`)
	if strings.Contains(content, "8200") || strings.Contains(content, "mood") {
		t.Errorf("Health in the days: %s", content)
	}
	hits := actJSON(t, run, "search", `{"query":"harbor","limit_count":1}`)
	if hits["has_more"] != true || hits["next_offset"] != float64(1) {
		t.Errorf("the page of hits: %v", hits)
	}
}

// A change is made, and the journal read, only in a conversation the person
// started. A run that a mail, a schedule, a dream or a background task began
// is told so and sends nothing; nor does a chat with other people.
func TestTheJournalIsRefusedInARunThePersonDidNotStart(t *testing.T) {
	for action, arguments := range map[string]string{
		"add_words": `{"text":"Went swimming."}`, "set_mood": `{"mood":"good"}`, "touch_up": `{"how":"shorter"}`,
		"correct": `{"correction_text":"It was Ben."}`, "pin": `{"record":"mail:m1"}`,
	} {
		operations := &routing{answers: map[string]string{"ReadJournalDay": harborDay()}}
		for name, run := range map[string]*fakeRun{
			"a background run": {operations: operations, isHeadless: true},
			"a mail's run":     {operations: operations},
			"a chat room":      {operations: operations, isStartedByPerson: true, isInChat: true},
		} {
			content, _, err := act(t, run, action, arguments)
			if err != nil {
				t.Fatalf("%s in %s should answer, not fail: %s", action, name, err)
			}
			if !strings.Contains(content, "journal") || strings.HasPrefix(strings.TrimSpace(content), "{") {
				t.Errorf("%s in %s: %q", action, name, content)
			}
		}
		if len(operations.operation) != 0 {
			t.Errorf("%s sent %v from a run the person did not start", action, operations.operation)
		}
	}
	// Nor is the journal read there: a mail or a schedule could steer the
	// run into telling somebody else what the person wrote for themselves.
	operations := &routing{answers: map[string]string{"ReadJournalDay": harborDay(), "ReadHealthSettings": switchOff}}
	for name, run := range map[string]*fakeRun{
		"a background run": {operations: operations, isHeadless: true},
		"a mail's run":     {operations: operations},
	} {
		content, _, err := act(t, run, "day", `{}`)
		if err != nil || !strings.Contains(content, "conversation the person started") {
			t.Errorf("%s read the journal: %q %v", name, content, err)
		}
	}
	content, _, _ := act(t, &fakeRun{operations: operations, isStartedByPerson: true, isInChat: true}, "day", `{}`)
	if !strings.Contains(content, "own conversation") {
		t.Errorf("a chat room was given the journal: %q", content)
	}
	if len(operations.operation) != 0 {
		t.Errorf("a refused read sent %v", operations.operation)
	}
	// The person's own turn reads it.
	if _, _, err := act(t, aTurnOfAda(operations), "day", `{}`); err != nil || len(operations.operation) == 0 {
		t.Errorf("the person's own turn could not read: %v %v", err, operations.operation)
	}
}

// Adding words keeps the original exactly as it is, adds the new words after
// a blank line, sends the time the words were last saved so that a save made
// elsewhere is not overwritten, and says nothing else changed.
func TestAddWordsKeepsTheOriginal(t *testing.T) {
	operations := &routing{answers: map[string]string{"ReadJournalDay": harborDay(), "SetJournalWords": `{"day":"2026-10-22"}`}}
	run := aTurnOfAda(operations)
	answer := actJSON(t, run, "add_words", `{"day":"2026-10-22","text":"  Then a swim.  "}`)
	variables, isCalled := operations.called("SetJournalWords")
	if !isCalled {
		t.Fatalf("nothing was saved: %v", answer)
	}
	if variables["words"] != "Lunch with Ben at the harbor.\n\nThen a swim." || variables["baseModifiedAt"] != "2026-10-22T20:00:00Z" || variables["day"] != "2026-10-22" {
		t.Errorf("the words saved: %v", variables)
	}
	if answer["is_added"] != true || !strings.Contains(answer["note"].(string), "touched-up") {
		t.Errorf("the answer: %v", answer)
	}

	// A day with nothing yet gets the words as they are, and the looking back
	// of a period is added to in the same way.
	empty := strings.Replace(harborDay(), `"original":"Lunch with Ben at the harbor."`, `"original":""`, 1)
	operations = &routing{answers: map[string]string{"ReadJournalDay": empty, "SetJournalWords": `{}`}}
	actJSON(t, aTurnOfAda(operations), "add_words", `{"day":"2026-10-22","text":"A first line."}`)
	if variables, _ := operations.called("SetJournalWords"); variables["words"] != "A first line." {
		t.Errorf("the first words: %v", variables)
	}
	operations = &routing{answers: map[string]string{"ReadJournalPeriod": weekWithAHealthMood(), "SetJournalLookingBack": `{}`}}
	actJSON(t, aTurnOfAda(operations), "add_words", `{"day":"2026-10-22","period_kind":"week","text":"Busy."}`)
	if variables, _ := operations.called("SetJournalLookingBack"); variables["lookingBack"] != "A week of docks.\n\nBusy." || variables["periodKind"] != "week" {
		t.Errorf("the looking back: %v", variables)
	}
}

// What is already written is never touched, not even its trailing line
// breaks, and words with nothing in them add nothing.
func TestAppendedOnlyEverAdds(t *testing.T) {
	for _, original := range []string{"", "one line", "one line\n", "one line\n\n", "one line  \n \n", "  indented\n\nsecond paragraph"} {
		combined, err := appended(original, "more")
		if err != nil {
			t.Fatalf("%q: %s", original, err)
		}
		if !strings.HasPrefix(combined, original) || !strings.HasSuffix(combined, "more") || len(combined) <= len(original) {
			t.Errorf("%q became %q", original, combined)
		}
		if original != "" && strings.TrimSpace(original) != "" && !strings.HasSuffix(strings.TrimSuffix(combined, "more"), "\n\n") {
			t.Errorf("no blank line before the new words in %q", combined)
		}
	}
	if _, err := appended("something", " \n "); err == nil {
		t.Errorf("empty words were added")
	}
}

// The mood is the person's to name, in the words or the number.
func TestSetMoodTakesANameOrANumber(t *testing.T) {
	operations := &routing{answers: map[string]string{"SetJournalMood": `{}`}}
	run := aTurnOfAda(operations)
	for mood, level := range map[string]int{"good": 4, "Awful": 1, "3": 3} {
		actJSON(t, run, "set_mood", `{"day":"2026-10-22","mood":"`+mood+`","mood_words":"  a calm one "}`)
		variables, _ := operations.called("SetJournalMood")
		if variables["moodLevel"] != level || variables["moodWords"] != "a calm one" {
			t.Errorf("%s: %v", mood, variables)
		}
	}
	if _, _, err := act(t, run, "set_mood", `{"mood":"fantastic"}`); err == nil {
		t.Errorf("a mood that is not one was taken")
	}
}

// The other writes pass what the person said to the same operations the page
// uses.
func TestTouchUpCorrectAndPin(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"TouchUpJournalWords":     `{"day":"2026-10-22","words":{"touchUpHow":"shorter","touchUpPending":true}}`,
		"CorrectJournalNarrative": `{"headline":"x"}`,
		"PinJournalRecord":        `{"linkId":"l1","record":{"reference":"mail:m1","title":"Budget"}}`,
	}}
	run := aTurnOfAda(operations)
	actJSON(t, run, "touch_up", `{"day":"2026-10-22","how":" shorter "}`)
	if variables, _ := operations.called("TouchUpJournalWords"); variables["how"] != "shorter" || variables["day"] != "2026-10-22" {
		t.Errorf("touch up: %v", variables)
	}
	actJSON(t, run, "correct", `{"day":"2026-10-22","correction_text":"It was Ben, not Sam."}`)
	if variables, _ := operations.called("CorrectJournalNarrative"); variables["correctionText"] != "It was Ben, not Sam." {
		t.Errorf("correct: %v", variables)
	}
	actJSON(t, run, "pin", `{"day":"2026-10-22","record":"calendar_occurrence:e1@2026-10-22T12:00:00Z","reason":"the lunch"}`)
	if variables, _ := operations.called("PinJournalRecord"); variables["recordKind"] != "calendar_occurrence" || variables["recordId"] != "e1@2026-10-22T12:00:00Z" || variables["linkReason"] != "the lunch" {
		t.Errorf("pin: %v", variables)
	}
	if _, _, err := act(t, run, "pin", `{"record":"nothing"}`); err == nil {
		t.Errorf("a record that is not kind:id was pinned")
	}
}

// operationsDay is the day count days from today in Ada's zone (UTC).
func operationsDay(count int) string {
	return time.Now().UTC().AddDate(0, 0, count).Format(time.DateOnly)
}
