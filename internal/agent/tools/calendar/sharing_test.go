package calendar

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

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
	return &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionCalendarUse}}
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

// sharedRun is a run whose operations are these.
type sharedRun struct {
	tools.Run
	operations tools.Operations
}

func (self *sharedRun) Operations() tools.Operations { return self.operations }

func (self *sharedRun) Configuration() *config.Configuration { return config.Default() }

const sharedCalendars = `[
	{"id":"ben-cal","name":"Calendar","calendarKind":"events","shareLevel":"titles","ownerUserId":"u-ben","ownerUsername":"ben","ownerName":"Ben"},
	{"id":"own","name":"Calendar","timezone":"UTC","agentGranted":true,"calendarKind":"events","shareLevel":"owner","ownerUserId":"u-ada","ownerUsername":"ada","ownerName":"Ada"},
	{"id":"cora-cal","name":"Calendar","calendarKind":"events","shareLevel":"edit","ownerUserId":"u-cora","ownerUsername":"cora","ownerName":"Cora"},
	{"id":"family","name":"Family","timezone":"UTC","calendarKind":"events","shareLevel":"edit","circleId":"c1","circleName":"Home","isCircleCalendar":true}]`

func runWith(operations tools.Operations) context.Context {
	return tools.WithRun(context.Background(), &sharedRun{operations: operations})
}

// call runs one action of the calendar tool, as the model does: through the
// merged tool, naming the action.
func call(t *testing.T, name string, ctx context.Context, arguments string) (map[string]any, error) {
	t.Helper()
	merged := tools.Build().Get("calendar")
	if merged == nil {
		t.Fatal("no calendar tool")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(arguments), &fields); err != nil {
		t.Fatalf("%s: %s", err, arguments)
	}
	fields["action"] = strings.TrimPrefix(name, "calendar_")
	encoded, _ := json.Marshal(fields)
	result, err := merged.Run(ctx, &tools.Call{Arguments: encoded})
	if err != nil {
		return nil, err
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		t.Fatalf("%s: %s", err, result.Content)
	}
	return answer, nil
}

// The agent's own calendar is the one it reads, whatever else the person
// reaches and whatever order the server lists them in.
func TestTheOwnCalendarIsNeverAnotherOnesShared(t *testing.T) {
	operations := &routing{answers: map[string]string{"ListCalendars": sharedCalendars}}
	id, _, err := theCalendar(context.Background(), operations)
	if err != nil || id != "own" {
		t.Fatalf("the person's own: %q %v", id, err)
	}
	// Nothing of theirs is granted: somebody's shared calendar cannot stand in.
	withheld := &routing{answers: map[string]string{"ListCalendars": `[
		{"id":"ben-cal","name":"Calendar","agentGranted":true,"calendarKind":"events","shareLevel":"titles"},
		{"id":"own","name":"Calendar","agentGranted":false,"calendarKind":"events","shareLevel":"owner"}]`}}
	if _, _, err := theCalendar(context.Background(), withheld); err == nil {
		t.Fatal("a shared calendar does not count as the person's own, granted or not")
	}
}

// The agenda of what others shared says whose each entry is, at what level,
// and shows busy time as busy; it is read only through the person's own grant.
func TestTheAgendaOfWhatOthersSharedSaysWhoseItIs(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars": sharedCalendars,
		"ListCalendarEvents": `[
			{"id":"e1","summary":"Dentist","location":"Elm Street","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z"},
			{"id":"","summary":"","isBusyOnly":true,"startsAt":"2030-01-02T09:00:00Z","endsAt":"2030-01-02T09:30:00Z"}]`,
	}}
	ctx := runWith(operations)

	answer, err := call(t, "calendar_agenda", ctx, `{"whose":"ben","from":"2030-01-02","until":"2030-01-03"}`)
	if err != nil {
		t.Fatal(err)
	}
	events := answer["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("two entries: %v", answer)
	}
	first, second := events[0].(map[string]any), events[1].(map[string]any)
	if first["what"] != "busy" || first["whose"] != "Ben" || first["level"] != "titles" || first["event"] != nil {
		t.Errorf("busy time is busy and has nothing to act on: %v", first)
	}
	if second["what"] != "Dentist" || second["where"] != "Elm Street" || second["whose"] != "Ben" {
		t.Errorf("a title at titles: %v", second)
	}
	if second["event"] != nil {
		t.Errorf("an event shared below edit cannot be acted on, so it has no identifier to: %v", second)
	}
	if operations.sent["ListCalendarEvents"]["calendarId"] != "ben-cal" {
		t.Errorf("ben's calendar was read: %v", operations.sent["ListCalendarEvents"])
	}

	// Everyone: every calendar shared with the person and the circle's own,
	// and an identifier for what the person may change.
	answer, err = call(t, "calendar_agenda", ctx, `{"whose":"everyone","from":"2030-01-02","until":"2030-01-03"}`)
	if err != nil {
		t.Fatal(err)
	}
	whose := map[string]bool{}
	var acted bool
	for _, each := range answer["events"].([]any) {
		row := each.(map[string]any)
		whose[row["whose"].(string)] = true
		if row["whose"] == "Home" && row["event"] == "e1" {
			acted = true
		}
	}
	if !whose["Ben"] || !whose["Cora"] || !whose["Home"] || whose["Ada"] {
		t.Errorf("everybody's, not the person's own: %v", whose)
	}
	if !acted {
		t.Errorf("an event at edit has its identifier: %v", answer)
	}

	if _, err := call(t, "calendar_agenda", ctx, `{"whose":"dave"}`); err == nil || !strings.Contains(err.Error(), "has not shared") {
		t.Errorf("somebody who shared nothing: %v", err)
	}
	// And the person's own grant is what opens any of it.
	ungranted := &routing{answers: map[string]string{"ListCalendars": `[
		{"id":"own","name":"Calendar","agentGranted":false,"calendarKind":"events","shareLevel":"owner"},
		{"id":"ben-cal","name":"Calendar","calendarKind":"events","shareLevel":"titles","ownerUsername":"ben"}]`}}
	if _, err := call(t, "calendar_agenda", runWith(ungranted), `{"whose":"ben"}`); err == nil || !strings.Contains(err.Error(), "not given you their calendar") {
		t.Errorf("without the grant: %v", err)
	}
}

// Free together offers what none of them covers, the same times
// "teanode calendar free --with" offers for the same busy times, and says who
// has not shared rather than leave them out.
func TestFreeTogetherMatchesTheCommandLine(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars":    sharedCalendars,
		"ListCirclePeople": `[{"userId":"u-ben","username":"ben","name":"Ben"},{"userId":"u-cora","username":"cora","name":"Cora"}]`,
		"ListCalendarFreeBusy": `[
			{"userId":"u-ada","username":"ada","isShared":true,"periods":[{"startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z"}]},
			{"userId":"u-ben","username":"ben","isShared":true,"periods":[{"startsAt":"2030-01-02T10:30:00Z","endsAt":"2030-01-02T12:00:00Z"},{"startsAt":"2030-01-02T15:00:00Z","endsAt":"2030-01-02T16:00:00Z"}]}]`,
	}}
	ctx := runWith(operations)
	answer, err := call(t, "calendar_free_together", ctx, `{"usernames":["ben"],"from":"2030-01-02","until":"2030-01-03"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, each := range answer["free"].([]any) {
		gap := each.(map[string]any)
		got = append(got, gap["from"].(string)+"-"+gap["until"].(string))
	}
	// The same busy times, the same day and the same hours as the command
	// line's test, and the same answer.
	if want := "09:00-10:00 12:00-15:00 16:00-17:00"; strings.Join(got, " ") != want {
		t.Errorf("free together:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	ids, _ := operations.sent["ListCalendarFreeBusy"]["userIds"].([]string)
	if len(ids) != 2 || ids[0] != "u-ada" || ids[1] != "u-ben" {
		t.Errorf("the person and the person asked about: %v", operations.sent["ListCalendarFreeBusy"])
	}

	// Hours of the day are the person's to narrow.
	answer, err = call(t, "calendar_free_together", ctx, `{"usernames":["ben"],"from":"2030-01-02","until":"2030-01-03","earliest":"12:00","latest":"14:00"}`)
	if err != nil || len(answer["free"].([]any)) != 1 {
		t.Errorf("a narrower day: %v %v", answer, err)
	}

	// Somebody who shared nothing, and somebody who is not in a circle.
	operations.answers["ListCalendarFreeBusy"] = `[
		{"userId":"u-ada","isShared":true,"periods":[]},{"userId":"u-cora","isShared":false,"periods":[]}]`
	if _, err := call(t, "calendar_free_together", ctx, `{"usernames":["cora"]}`); err == nil || !strings.Contains(err.Error(), "cora has not shared") {
		t.Errorf("who has not shared is said: %v", err)
	}
	if _, err := call(t, "calendar_free_together", ctx, `{"usernames":["stranger"]}`); err == nil || !strings.Contains(err.Error(), "circle") {
		t.Errorf("a stranger: %v", err)
	}
	if _, err := call(t, "calendar_free_together", ctx, `{"usernames":[]}`); err == nil {
		t.Errorf("nobody to compare with")
	}
}

// Sharing is a grant and asks first; taking a share back is a write; writing
// to a circle's calendar is the person's own change and to another person's is
// outward.
func TestWhatTheAgentIsAskedAboutBeforeSharingOrWritingToOthersCalendars(t *testing.T) {
	calendar := tools.Build().Get("calendar")
	if calendar == nil {
		t.Fatal("calendar is registered")
	}
	for _, action := range []string{"free_together", "shares", "share", "unshare"} {
		if !strings.Contains(calendar.Description, action+" — ") {
			t.Errorf("%s is one of its actions:\n%s", action, calendar.Description)
		}
	}
	risks := map[string]tools.Risk{
		`{"action":"shares"}`:                                                   tools.RiskRead,
		`{"action":"free_together","usernames":["ben"]}`:                        tools.RiskRead,
		`{"action":"agenda","whose":"everyone"}`:                                tools.RiskRead,
		`{"action":"share","person":"ben","level":"titles"}`:                    tools.RiskGranting,
		`{"action":"share","circle":"Home","level":"busy"}`:                     tools.RiskGranting,
		`{"action":"unshare","share":"s1"}`:                                     tools.RiskWrite,
		`{"action":"add","summary":"School play","circle":"Home"}`:              tools.RiskWrite,
		`{"action":"add","summary":"Lunch","person":"cora"}`:                    tools.RiskOutward,
		`{"action":"edit","event":"e1","summary":"Moved","circle":"Home"}`:      tools.RiskWrite,
		`{"action":"edit","event":"e1","summary":"Moved","person":"cora"}`:      tools.RiskOutward,
		`{"action":"remove","event":"e1","circle":"Home"}`:                      tools.RiskDestructive,
		`{"action":"remove","event":"e1","person":"cora"}`:                      tools.RiskOutward,
		`{"action":"add","summary":"Lunch","person":"cora","invite":["a@b.c"]}`: tools.RiskOutward,
	}
	for arguments, want := range risks {
		if got := calendar.RiskFor([]byte(arguments)); got != want {
			t.Errorf("%s is %q, not %q", arguments, got, want)
		}
	}
	if !tools.NeedsConfirmation(calendar, []byte(`{"action":"share","person":"ben","level":"titles"}`), nil, nil) {
		t.Error("sharing is asked about first")
	}
}

// Writing to a circle's calendar or to one shared at edit goes to that
// calendar, and to nobody else's.
func TestWritingToOthersCalendarsGoesWhereItWasAskedAndNowhereElse(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars":     sharedCalendars,
		"SaveCalendarEvent": `{"id":"new","summary":"School play"}`,
	}}
	ctx := runWith(operations)
	if _, err := call(t, "calendar_add", ctx, `{"summary":"School play","starts":"2030-01-05T18:00","circle":"home"}`); err != nil {
		t.Fatal(err)
	}
	if operations.sent["SaveCalendarEvent"]["calendarId"] != "family" {
		t.Errorf("the circle's calendar: %v", operations.sent["SaveCalendarEvent"])
	}
	if _, err := call(t, "calendar_add", ctx, `{"summary":"Lunch","starts":"2030-01-05T12:00","person":"cora"}`); err != nil {
		t.Fatal(err)
	}
	if operations.sent["SaveCalendarEvent"]["calendarId"] != "cora-cal" {
		t.Errorf("cora's calendar, shared at edit: %v", operations.sent["SaveCalendarEvent"])
	}
	// Ben shared at titles only: said in words, not tried and refused.
	if _, err := call(t, "calendar_add", ctx, `{"summary":"Lunch","starts":"2030-01-05T12:00","person":"ben"}`); err == nil ||
		!strings.Contains(err.Error(), "only be changed when it is shared at edit") {
		t.Errorf("a calendar shared below edit: %v", err)
	}
	// A circle with no calendar, and a person who shared nothing.
	if _, err := call(t, "calendar_add", ctx, `{"summary":"x","starts":"2030-01-05T12:00","circle":"Club"}`); err == nil || !strings.Contains(err.Error(), "has a calendar") {
		t.Errorf("a circle with none: %v", err)
	}
	if _, err := call(t, "calendar_add", ctx, `{"summary":"x","starts":"2030-01-05T12:00","person":"dave"}`); err == nil || !strings.Contains(err.Error(), "has not shared") {
		t.Errorf("a person who shared nothing: %v", err)
	}
	// With neither, it is the person's own calendar, as always.
	operations.sent = nil
	if _, err := call(t, "calendar_add", ctx, `{"summary":"Mine","starts":"2030-01-05T12:00"}`); err != nil {
		t.Fatal(err)
	}
	if operations.sent["SaveCalendarEvent"]["calendarId"] != "own" {
		t.Errorf("the person's own: %v", operations.sent["SaveCalendarEvent"])
	}
}

func TestSharingAndUnsharingSendWhatWasAskedFor(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars":      sharedCalendars,
		"ListCirclePeople":   `[{"userId":"u-ben","username":"ben","name":"Ben"}]`,
		"ListCircles":        `[{"id":"c1","name":"Home"}]`,
		"ShareCalendar":      `{"id":"s1","shareLevel":"titles"}`,
		"UnshareCalendar":    `true`,
		"ListCalendarShares": `[{"id":"s1","granteeUsername":"ben","granteeName":"Ben","shareLevel":"titles"},{"id":"s2","granteeCircleName":"Home","shareLevel":"busy"}]`,
	}}
	ctx := runWith(operations)

	answer, err := call(t, "calendar_share", ctx, `{"person":"ben","level":"Titles"}`)
	if err != nil || answer["shared_with"] != "ben" || answer["level"] != "titles" {
		t.Fatalf("sharing with a person: %v %v", answer, err)
	}
	if sent := operations.sent["ShareCalendar"]; sent["calendarId"] != "own" || sent["granteeUserId"] != "u-ben" || sent["shareLevel"] != "titles" {
		t.Errorf("what was sent: %v", sent)
	}
	if _, err := call(t, "calendar_share", ctx, `{"circle":"home","level":"busy"}`); err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["ShareCalendar"]; sent["granteeCircleId"] != "c1" || sent["granteeUserId"] != nil {
		t.Errorf("a circle by name: %v", sent)
	}
	for _, arguments := range []string{`{"level":"busy"}`, `{"person":"ben","circle":"Home","level":"busy"}`, `{"person":"stranger","level":"busy"}`, `{"circle":"Club","level":"busy"}`} {
		if _, err := call(t, "calendar_share", ctx, arguments); err == nil {
			t.Errorf("%s is refused", arguments)
		}
	}
	shares, err := call(t, "calendar_shares", ctx, `{}`)
	if err != nil || len(shares["shares"].([]any)) != 2 {
		t.Fatalf("the shares: %v %v", shares, err)
	}
	if _, err := call(t, "calendar_unshare", ctx, `{"share":"s2"}`); err != nil {
		t.Fatal(err)
	}
	if operations.sent["UnshareCalendar"]["shareId"] != "s2" {
		t.Errorf("unshare sends the share: %v", operations.sent["UnshareCalendar"])
	}
}

// is_private on adding and editing is sent as isPrivate, an edit that does not
// say leaves it out, and the person's own agenda says which events are private.
func TestAnEventIsMarkedPrivateThroughTheTool(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars":      sharedCalendars,
		"SaveCalendarEvent":  `{"id":"e1","summary":"Checkup","startsAt":"2030-01-02T10:00:00Z","allDay":false,"attendees":[]}`,
		"GetCalendarEvent":   `{"id":"e1","summary":"Checkup","allDay":false,"attendees":[]}`,
		"ListCalendarEvents": `[{"id":"e1","summary":"Checkup","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","isPrivate":true},{"id":"e2","summary":"Lunch","startsAt":"2030-01-02T12:00:00Z","endsAt":"2030-01-02T13:00:00Z"}]`,
	}}
	ctx := runWith(operations)

	if _, err := call(t, "calendar_add", ctx, `{"summary":"Checkup","starts":"2030-01-02T10:00","is_private":true}`); err != nil {
		t.Fatal(err)
	}
	if operations.sent["SaveCalendarEvent"]["isPrivate"] != true {
		t.Errorf("adding sends isPrivate: %v", operations.sent["SaveCalendarEvent"])
	}
	if _, err := call(t, "calendar_edit", ctx, `{"event":"e1","summary":"Check-up"}`); err != nil {
		t.Fatal(err)
	}
	if _, said := operations.sent["SaveCalendarEvent"]["isPrivate"]; said {
		t.Errorf("an edit that does not mention it leaves it alone: %v", operations.sent["SaveCalendarEvent"])
	}
	if _, err := call(t, "calendar_edit", ctx, `{"event":"e1","is_private":false}`); err != nil {
		t.Fatal(err)
	}
	if operations.sent["SaveCalendarEvent"]["isPrivate"] != false {
		t.Errorf("editing can take the mark off: %v", operations.sent["SaveCalendarEvent"])
	}

	answer, err := call(t, "calendar_agenda", ctx, `{"from":"2030-01-02","until":"2030-01-03"}`)
	if err != nil {
		t.Fatal(err)
	}
	events := answer["events"].([]any)
	first, second := events[0].(map[string]any), events[1].(map[string]any)
	if first["private"] != true || second["private"] != nil {
		t.Errorf("the agenda says which is private: %v", events)
	}
}

// The card for a write into a circle's calendar or another person's says
// whose calendar it is, and does not say "your calendar" for it.
func TestTheCardNamesTheCalendarAWriteGoesTo(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars":      sharedCalendars,
		"ListCalendarEvents": `[{"id":"e1","summary":"School play","startsAt":"2030-01-05T18:00:00Z","endsAt":"2030-01-05T19:00:00Z"}]`,
		"GetCalendarEvent":   `{"id":"e1","summary":"School play"}`,
	}}
	ctx := runWith(operations)
	calendar := tools.Build().Get("calendar")
	if calendar == nil {
		t.Fatal("calendar is registered")
	}
	cases := map[string]string{
		`{"action":"add","summary":"Lunch","starts":"2030-01-05T12:00"}`: "in your calendar",
		`{"action":"add","summary":"School play","circle":"Home"}`:       `in the circle calendar "Home"`,
		`{"action":"add","summary":"Lunch","person":"cora"}`:             `in "cora"'s calendar, which they will see`,
		`{"action":"remove","event":"e1","circle":"Home"}`:               `out of the circle calendar "Home"`,
		`{"action":"remove","event":"e1","person":"cora"}`:               `out of "cora"'s calendar, which they will see`,
		`{"action":"remove","event":"e1"}`:                               "out of your calendar",
	}
	for arguments, want := range cases {
		line := calendar.PreviewLine(ctx, []byte(arguments))
		if !strings.Contains(line, want) {
			t.Errorf("%s is previewed as %q, want it to say %q", arguments, line, want)
		}
		if want != "in your calendar" && want != "out of your calendar" && strings.Contains(line, "your calendar") {
			t.Errorf("%s: another calendar is not called yours: %q", arguments, line)
		}
	}
}

// The agenda of what others shared gives, on each row, the value the edit and
// remove tools take to reach that calendar.
func TestTheAgendaRowsGiveWhatEditAndRemoveTake(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars":      sharedCalendars,
		"ListCalendarEvents": `[{"id":"e1","summary":"Dentist","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z"}]`,
	}}
	answer, err := call(t, "calendar_agenda", runWith(operations), `{"whose":"everyone","from":"2030-01-02","until":"2030-01-03"}`)
	if err != nil {
		t.Fatal(err)
	}
	var circles, people int
	for _, each := range answer["events"].([]any) {
		row := each.(map[string]any)
		if row["calendar"] != nil {
			t.Errorf("a calendar identifier no tool takes is not given: %v", row)
		}
		switch {
		case row["whose"] == "Home" && row["circle"] == "Home" && row["person"] == nil:
			circles++
		case row["whose"] == "Cora" && row["person"] == "cora" && row["circle"] == nil:
			people++
		}
	}
	if circles != 1 || people != 1 {
		t.Errorf("one circle row and one person row: %v", answer)
	}
}
