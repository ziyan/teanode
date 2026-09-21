package calendar

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

// A window is read in the person's own zone, because a day is a local thing:
// "what is on tomorrow" from a calendar kept in Tokyo means Tokyo's tomorrow.
func TestADayIsALocalThing(t *testing.T) {
	if _, err := time.LoadLocation("Asia/Tokyo"); err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	from, until, where, err := window(windowArguments{From: "2026-09-14", Until: "2026-09-15"}, "Asia/Tokyo")
	if err != nil {
		t.Fatalf("reading the window: %v", err)
	}
	if where.String() != "Asia/Tokyo" {
		t.Fatalf("in their zone: %s", where)
	}
	// Midnight in Tokyo is three in the afternoon of the day before in UTC.
	if got := from.UTC().Format("2006-01-02T15:04Z"); got != "2026-09-13T15:00Z" {
		t.Fatalf("midnight in Tokyo: %s", got)
	}
	if !until.After(from) {
		t.Fatal("and the window runs forwards")
	}
}

// What is refused, and said plainly rather than guessed at.
func TestWhatTheWindowRefuses(t *testing.T) {
	if _, _, _, err := window(windowArguments{From: "next tuesday"}, ""); err == nil {
		t.Fatal("a date this server cannot read should be refused")
	}
	if _, _, _, err := window(windowArguments{From: "2026-09-14", Until: "2026-09-13"}, ""); err == nil {
		t.Fatal("a window that ends before it begins")
	}
	if _, _, _, err := window(windowArguments{From: "2026-01-01", Until: "2030-01-01"}, ""); err == nil {
		t.Fatal("more than a year at once")
	}
	// And nothing given is today, for a week.
	from, until, _, err := window(windowArguments{}, "")
	if err != nil || until.Sub(from) != 7*24*time.Hour {
		t.Fatalf("a week by default: %v %v", until.Sub(from), err)
	}
}

// The times an event may be written with, in the person's own zone.
func TestTheShapesATimeMayBeWrittenIn(t *testing.T) {
	where := time.UTC
	for _, shape := range []string{"2026-09-14T10:00", "2026-09-14 10:00", "2026-09-14T10:00:00"} {
		parsed, err := momentOf(shape, where, false)
		if err != nil || parsed.Format("2006-01-02 15:04") != "2026-09-14 10:00" {
			t.Fatalf("%q: %v %v", shape, parsed, err)
		}
	}
	// A date alone is the start of that day, which is what an all-day
	// event means.
	day, err := momentOf("2026-09-14", where, true)
	if err != nil || day.Format("15:04") != "00:00" {
		t.Fatalf("a date alone: %v %v", day, err)
	}
	if _, err := momentOf("tomorrow at ten", where, false); err == nil {
		t.Fatal("a time this server cannot read should be refused")
	}
	if _, err := momentOf("", where, false); err == nil {
		t.Fatal("an event needs a time")
	}
}

// An hour of the day is read, or defaulted, and nonsense is refused.
func TestTheHoursOfTheWorkingDay(t *testing.T) {
	if got, err := hourOf("", 9); err != nil || got != 9*time.Hour {
		t.Fatalf("nine by default: %v %v", got, err)
	}
	if got, err := hourOf("08:30", 9); err != nil || got != 8*time.Hour+30*time.Minute {
		t.Fatalf("half past eight: %v %v", got, err)
	}
	if _, err := hourOf("the morning", 9); err == nil {
		t.Fatal("an hour this server cannot read should be refused")
	}
}

// Everything the agent can do to a calendar, and what it is asked about
// first: putting something in is a write, and anything that sends mail in the
// person's name is outward, which is the class that stops and asks.
func TestWhatTheAgentIsAskedAboutBeforeItActs(t *testing.T) {
	catalog := tools.Build()
	// One tool with five actions, where there were five tools. Each action
	// keeps the class it had: the merge is in the name, not in the risk.
	calendar := catalog.Get("calendar")
	if calendar == nil {
		t.Fatal("calendar is registered")
	}
	for _, action := range []string{"agenda", "free", "add", "edit", "remove"} {
		if !strings.Contains(calendar.Description, action+" — ") {
			t.Fatalf("%s is one of its actions:\n%s", action, calendar.Description)
		}
	}

	if got := calendar.RiskFor([]byte(`{"action":"agenda"}`)); got != tools.RiskRead {
		t.Fatalf("reading the diary is a read: %q", got)
	}
	if got := calendar.RiskFor([]byte(`{"action":"add","summary":"Dentist","starts":"2026-09-14T09:00"}`)); got != tools.RiskWrite {
		t.Fatalf("putting something in one's own diary is a write: %q", got)
	}
	if got := calendar.RiskFor([]byte(`{"action":"add","summary":"Meeting","invite":["ada@example.com"]}`)); got != tools.RiskOutward {
		t.Fatalf("inviting anybody is outward: %q", got)
	}

	if got := calendar.RiskFor([]byte(`{"action":"edit","event":"e1","summary":"Dentist, later"}`)); got != tools.RiskWrite {
		t.Fatalf("changing one's own appointment is a write: %q", got)
	}
	for _, arguments := range []string{
		`{"action":"edit","event":"e1","starts":"2026-09-15T09:00","tell_guests":true}`,
		`{"action":"edit","event":"e1","invite":["ada@example.com"]}`,
	} {
		if got := calendar.RiskFor([]byte(arguments)); got != tools.RiskOutward {
			t.Fatalf("telling anybody is outward (%s): %q", arguments, got)
		}
		if !tools.NeedsConfirmation(calendar, []byte(arguments), nil, nil) {
			t.Fatalf("and outward is asked about first: %s", arguments)
		}
	}

	// Taking something out of a calendar is destructive whoever else hears
	// about it, and destructive is asked about too.
	if got := calendar.RiskFor([]byte(`{"action":"remove","event":"e1"}`)); got != tools.RiskDestructive {
		t.Fatalf("removing an event is destructive: %q", got)
	}
	if got := calendar.RiskFor([]byte(`{"action":"remove","event":"e1","tell_guests":true}`)); got != tools.RiskOutward {
		t.Fatalf("and outward when everybody is told: %q", got)
	}
	if !tools.NeedsConfirmation(calendar, []byte(`{"action":"remove","event":"e1"}`), nil, nil) {
		t.Fatal("removing anything is asked about first")
	}
}

// An event with people on it is not changed or removed until the call says
// they are to be told, because that is mail going out in the person's name
// and the risk of a call is read off its arguments alone.
func TestAnEventWithGuestsIsNotTouchedQuietly(t *testing.T) {
	alone := &keptEvent{Summary: "Dentist"}
	if err := guestsAreTold(alone, false, "changing"); err != nil {
		t.Fatalf("nobody to tell: %s", err)
	}
	meeting := &keptEvent{Summary: "Planning"}
	meeting.Attendees = append(meeting.Attendees, struct {
		Address string `json:"address"`
	}{Address: "ada@example.com"})

	err := guestsAreTold(meeting, false, "changing")
	if err == nil {
		t.Fatal("an event with somebody on it is not changed quietly")
	}
	if !strings.Contains(err.Error(), "tell_guests") {
		t.Fatalf("and the refusal says how to ask properly: %s", err)
	}
	if err := guestsAreTold(meeting, true, "changing"); err != nil {
		t.Fatalf("asked properly, it goes ahead: %s", err)
	}
}

// answering is an Operations that answers one document with one thing.
type answering struct {
	answer string
	asked  []string
}

func (self *answering) Permissions() *models.EffectivePermissions { return nil }

func (self *answering) Execute(_ context.Context, document string, _ map[string]any, result any) error {
	self.asked = append(self.asked, document)
	if result == nil {
		return nil
	}
	return json.Unmarshal([]byte(self.answer), result)
}

// A calendar is a source, and a source the person has not granted is not the
// agent's to read.
//
// The rule the whole agent is built on is that nothing from an ungranted
// source reaches a model. A mailbox has had that switch since the agent did;
// the calendar did not, and was reachable on the person's own permission
// alone -- so an agent granted one mailbox could read every appointment in
// the diary, which is not what granting a mailbox means.
func TestTheDiaryIsOnlyReadWhenItHasBeenGiven(t *testing.T) {
	t.Parallel()

	withheld := &answering{answer: `{"ListCalendars":[{"id":"c1","name":"Calendar","timezone":"Europe/London","agentGranted":false}]}`}
	if _, _, err := theCalendar(context.Background(), withheld); err == nil {
		t.Fatal("a calendar nobody granted is not readable")
	} else if !strings.Contains(err.Error(), "not given you their calendar") {
		// The refusal is worded for the model to pass on: "there is no
		// calendar" would be a lie, because there is one.
		t.Fatalf("and the refusal says why: %s", err)
	}

	granted := &answering{answer: `{"ListCalendars":[{"id":"c1","name":"Calendar","timezone":"","agentGranted":false},{"id":"c2","name":"Family","timezone":"Europe/London","agentGranted":true}]}`}
	id, zone, err := theCalendar(context.Background(), granted)
	if err != nil || id != "c2" || zone != "Europe/London" {
		t.Fatalf("the one they did grant: %q %q %v", id, zone, err)
	}
	if len(granted.asked) != 1 || !strings.Contains(granted.asked[0], "agentGranted") {
		t.Fatalf("and the switch is what it asked for: %v", granted.asked)
	}
}
