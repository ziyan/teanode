package calendar

import (
	"strings"
	"testing"
	"time"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 9, 14, hour, minute, 0, 0, time.UTC)
}

func written(gaps [][2]time.Time) string {
	var said []string
	for _, gap := range gaps {
		said = append(said, gap[0].Format("15:04")+"-"+gap[1].Format("15:04"))
	}
	return strings.Join(said, " ")
}

// The free stretches of a day are what is left once the meetings are taken
// out.
func TestWhatIsLeftOfADay(t *testing.T) {
	gaps := gapsBetween([][2]time.Time{
		{at(10, 0), at(11, 0)},
		{at(14, 0), at(15, 0)},
	}, at(9, 0), at(17, 0))
	if got := written(gaps); got != "09:00-10:00 11:00-14:00 15:00-17:00" {
		t.Fatalf("three gaps: %s", got)
	}
}

// Meetings given out of order are still taken out in order. A caller has no
// reason to sort them, and one that did not would otherwise be told it is
// free during a meeting.
func TestMeetingsOutOfOrderAreStillTakenOut(t *testing.T) {
	gaps := gapsBetween([][2]time.Time{
		{at(14, 0), at(15, 0)},
		{at(10, 0), at(11, 0)},
	}, at(9, 0), at(17, 0))
	if got := written(gaps); got != "09:00-10:00 11:00-14:00 15:00-17:00" {
		t.Fatalf("order should not matter: %s", got)
	}
}

// Two meetings that overlap are one stretch. Treating them separately invents
// a gap between them that is not there, which is how somebody gets offered a
// time they are already in a meeting.
func TestOverlappingMeetingsDoNotInventAGap(t *testing.T) {
	gaps := gapsBetween([][2]time.Time{
		{at(10, 0), at(12, 0)},
		{at(11, 0), at(13, 0)},
	}, at(9, 0), at(17, 0))
	if got := written(gaps); got != "09:00-10:00 13:00-17:00" {
		t.Fatalf("one stretch from ten to one: %s", got)
	}
	// And one wholly inside another changes nothing.
	inside := gapsBetween([][2]time.Time{
		{at(10, 0), at(13, 0)},
		{at(11, 0), at(12, 0)},
	}, at(9, 0), at(17, 0))
	if got := written(inside); got != "09:00-10:00 13:00-17:00" {
		t.Fatalf("the inner one is already covered: %s", got)
	}
}

// A day with nothing in it is free all through, and a day booked solid has
// nothing to offer.
func TestAnEmptyDayAndAFullOne(t *testing.T) {
	if got := written(gapsBetween(nil, at(9, 0), at(17, 0))); got != "09:00-17:00" {
		t.Fatalf("all day: %s", got)
	}
	full := gapsBetween([][2]time.Time{{at(9, 0), at(17, 0)}}, at(9, 0), at(17, 0))
	if got := written(full); got != "" {
		t.Fatalf("nothing free: %s", got)
	}
}

// A few minutes between two meetings is not a time anybody can meet in, and
// offering it is worse than offering nothing.
func TestASliverIsNotOffered(t *testing.T) {
	gaps := gapsBetween([][2]time.Time{
		{at(10, 0), at(11, 0)},
		{at(11, 5), at(12, 0)},
	}, at(10, 0), at(12, 0))
	if got := written(gaps); got != "" {
		t.Fatalf("five minutes is not a meeting: %s", got)
	}
}

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
