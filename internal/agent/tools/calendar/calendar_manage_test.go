package calendar

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

const ownCalendarKept = `[{"id":"own","name":"Calendar","description":"Things","colour":"#112233","timezone":"Europe/Berlin",
	"weekStart":"monday","agentGranted":true,"calendarKind":"events","shareLevel":"owner"}]`

// Looking a request up reads, stopping one writes, and making a circle's
// calendar or changing the person's own writes: none of them is asked about as
// outward or destructive, and the cards say what they will do in words.
func TestTheRequestAndCalendarActionsAreClassedAndWorded(t *testing.T) {
	calendar := tools.Build().Get("calendar")
	if calendar == nil {
		t.Fatal("calendar is registered")
	}
	for arguments, want := range map[string]tools.Risk{
		`{"action":"request","request_id":"r1"}`:                  tools.RiskRead,
		`{"action":"stop","request_id":"r1"}`:                     tools.RiskWrite,
		`{"action":"set","name":"Work"}`:                          tools.RiskWrite,
		`{"action":"circle","circle":"Home","name":"Home plans"}`: tools.RiskWrite,
	} {
		if got := calendar.RiskFor([]byte(arguments)); got != want {
			t.Errorf("%s is %q, not %q", arguments, got, want)
		}
	}

	operations := &routing{answers: map[string]string{
		"ListCircles": `[{"id":"c1","name":"Home","members":[{"userId":"u-ada"},{"userId":"u-ben"}]}]`,
	}}
	ctx := runWith(operations)
	for arguments, want := range map[string]string{
		`{"action":"stop","request_id":"r1"}`:    `Stop the calendar change "r1" from executing`,
		`{"action":"set","name":"Work"}`:         `Rename your calendar to "Work"`,
		`{"action":"set","week_start":"monday"}`: `Change how your calendar is shown`,
		`{"action":"circle","circle":"c1"}`:      `Make the calendar of the circle "Home" (2 people), which everyone in it reads and writes`,
	} {
		if line := calendar.PreviewLine(ctx, []byte(arguments)); line != want {
			t.Errorf("%s is previewed as %q, want %q", arguments, line, want)
		}
	}
}

// A request that completed says which event it was for and whether that event
// is still there; one that did not is not claimed to be dead, because a request
// still in flight has no receipt yet.
func TestARequestIsLookedUpWithoutRepeatingIt(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars": ownCalendarKept,
		"GetCalendarRequest": `{"requestId":"r1","calendarId":"own","objectId":"e1","operation":"delete",
			"completedAt":"2030-01-02T10:00:00Z","isMissing":true}`,
	}}
	ctx := runWith(operations)
	answer, err := call(t, "calendar_request", ctx, `{"request_id":" r1 "}`)
	if err != nil {
		t.Fatal(err)
	}
	if operations.sent["GetCalendarRequest"]["requestId"] != "r1" {
		t.Errorf("the identifier is sent as given, trimmed: %v", operations.sent["GetCalendarRequest"])
	}
	if answer["completed"] != true || answer["event"] != "e1" || answer["operation"] != "delete" ||
		answer["note"] != "the event no longer exists" {
		t.Errorf("a completed request: %v", answer)
	}

	operations.answers["GetCalendarRequest"] = `null`
	answer, err = call(t, "calendar_request", ctx, `{"request_id":"r2"}`)
	if err != nil {
		t.Fatal(err)
	}
	if answer["completed"] != false || !strings.Contains(answer["note"].(string), "may still be in progress") {
		t.Errorf("no receipt: %v", answer)
	}

	if _, err := call(t, "calendar_request", ctx, `{}`); err == nil || !strings.Contains(err.Error(), "request_id") {
		t.Errorf("a call that names no request: %v", err)
	}
	// Nothing of the diary is read until the person has given it.
	withheld := runWith(&routing{answers: map[string]string{
		"ListCalendars": `[{"id":"own","name":"Calendar","agentGranted":false,"calendarKind":"events","shareLevel":"owner"}]`,
	}})
	if _, err := call(t, "calendar_request", withheld, `{"request_id":"r1"}`); err == nil {
		t.Error("a calendar that was not given is not looked into")
	}
}

// Stopping a request that had not committed says it can no longer execute; one
// that had is reported with what it did, and is not undone.
func TestStoppingARequestNeverUndoesACompletedChange(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars":         ownCalendarKept,
		"CancelCalendarRequest": `null`,
	}}
	ctx := runWith(operations)
	answer, err := call(t, "calendar_stop", ctx, `{"request_id":"r1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if operations.sent["CancelCalendarRequest"]["requestId"] != "r1" {
		t.Errorf("the request stopped: %v", operations.sent["CancelCalendarRequest"])
	}
	if answer["stopped"] != true || answer["note"] != "stopped; this request can no longer execute" {
		t.Errorf("a request that had not committed: %v", answer)
	}

	operations.answers["CancelCalendarRequest"] = `{"requestId":"r1","calendarId":"own","objectId":"e1","operation":"save","isMissing":false}`
	answer, err = call(t, "calendar_stop", ctx, `{"request_id":"r1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if answer["stopped"] != false || answer["already_completed"] != true || answer["event"] != "e1" ||
		!strings.Contains(answer["note"].(string), "was not undone") {
		t.Errorf("a request that had committed: %v", answer)
	}
}

// The server replaces every field it is sent, so a call that gives only a name
// sends the rest back as the calendar holds it, as the command line does, and a
// call that gives nothing is refused.
func TestSettingACalendarKeepsWhatTheCallLeavesOut(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars": ownCalendarKept,
		"SaveCalendar":  `{"id":"own","name":"Work","timezone":"Europe/Berlin","weekStart":"monday"}`,
	}}
	ctx := runWith(operations)
	answer, err := call(t, "calendar_set", ctx, `{"name":" Work "}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := operations.sent["SaveCalendar"]
	want := map[string]any{"id": "own", "name": "Work", "description": "Things", "colour": "#112233",
		"timezone": "Europe/Berlin", "weekStart": "monday"}
	for name, value := range want {
		if sent[name] != value {
			t.Errorf("%s is sent as %v, want %v: %v", name, sent[name], value, sent)
		}
	}
	if answer["calendar"] != "Work" || answer["week_start"] != "monday" {
		t.Errorf("what is said: %v", answer)
	}

	if _, err := call(t, "calendar_set", ctx, `{"week_start":"sunday","color":"","description":""}`); err != nil {
		t.Fatal(err)
	}
	sent = operations.sent["SaveCalendar"]
	if sent["weekStart"] != "sunday" || sent["colour"] != "" || sent["description"] != "" || sent["name"] != "Calendar" {
		t.Errorf("an empty value clears it, and the name stays: %v", sent)
	}

	if _, err := call(t, "calendar_set", ctx, `{}`); err == nil || !strings.Contains(err.Error(), "say what to change") {
		t.Errorf("a call that changes nothing: %v", err)
	}
}

// A circle's calendar is made for the circle named by its name or its id; a
// name two circles have is refused with their ids, and nobody's circle is
// guessed at.
func TestACircleCalendarIsMadeForTheCircleNamed(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCalendars": ownCalendarKept,
		"ListCircles": `[{"id":"c1","name":"Home","members":[{"userId":"u-ada"}]},
			{"id":"c2","name":"Club","members":[{"userId":"u-ada"}]},{"id":"c3","name":"club","members":[{"userId":"u-ada"},{"userId":"u-ben"}]}]`,
		"CreateCircleCalendar": `{"id":"family","name":"Home plans","circleName":"Home"}`,
	}}
	ctx := runWith(operations)
	answer, err := call(t, "calendar_circle", ctx, `{"circle":"home","name":" Home plans "}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := operations.sent["CreateCircleCalendar"]
	if sent["circleId"] != "c1" || sent["name"] != "Home plans" {
		t.Errorf("the circle by its name: %v", sent)
	}
	if answer["made"] != "Home plans" || answer["circle"] != `the circle "Home"` {
		t.Errorf("what is said: %v", answer)
	}

	if _, err := call(t, "calendar_circle", ctx, `{"circle":"c1"}`); err != nil {
		t.Fatal(err)
	}
	if sent := operations.sent["CreateCircleCalendar"]; sent["circleId"] != "c1" || sent["name"] != nil {
		t.Errorf("by its id, with the circle's name by default: %v", sent)
	}

	operations.sent = nil
	if _, err := call(t, "calendar_circle", ctx, `{"circle":"CLUB"}`); err == nil || !strings.Contains(err.Error(), "give the id") {
		t.Errorf("a name two circles have: %v", err)
	}
	if _, err := call(t, "calendar_circle", ctx, `{"circle":"Nobody"}`); err == nil || !strings.Contains(err.Error(), "neither the name nor the id") {
		t.Errorf("a circle the person is not in: %v", err)
	}
	if _, err := call(t, "calendar_circle", ctx, `{}`); err == nil {
		t.Error("a call that names no circle")
	}
	if operations.sent["CreateCircleCalendar"] != nil {
		t.Errorf("nothing was made: %v", operations.sent)
	}
}
