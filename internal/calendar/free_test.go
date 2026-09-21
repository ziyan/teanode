package calendar

import (
	"strings"
	"testing"
	"time"
)

// dayAt is a moment on the day these tests are about.
func dayAt(hour, minute int) time.Time {
	return time.Date(2026, 9, 14, hour, minute, 0, 0, time.UTC)
}

// freeAt is what is left of a day, written for a test to read. Through both
// halves, because that is how every caller asks: what somebody is busy with,
// and then what is left.
func freeAt(busy [][2]time.Time, opens, closes time.Time) string {
	periods := make([]Occurrence, 0, len(busy))
	for _, stretch := range busy {
		periods = append(periods, Occurrence{StartsAt: stretch[0], EndsAt: stretch[1]})
	}
	var said []string
	for _, gap := range Free(FreeBusy(periods, opens, closes), opens, closes) {
		said = append(said, gap.StartsAt.Format("15:04")+"-"+gap.EndsAt.Format("15:04"))
	}
	return strings.Join(said, " ")
}

// The free stretches of a day are what is left once the meetings are taken
// out.
func TestWhatIsLeftOfADay(t *testing.T) {
	got := freeAt([][2]time.Time{
		{dayAt(10, 0), dayAt(11, 0)},
		{dayAt(14, 0), dayAt(15, 0)},
	}, dayAt(9, 0), dayAt(17, 0))
	if got != "09:00-10:00 11:00-14:00 15:00-17:00" {
		t.Fatalf("three gaps: %s", got)
	}
}

// Meetings given out of order are still taken out in order. A caller has no
// reason to sort them, and one that did not would otherwise be told it is
// free during a meeting.
func TestMeetingsOutOfOrderAreStillTakenOut(t *testing.T) {
	got := freeAt([][2]time.Time{
		{dayAt(14, 0), dayAt(15, 0)},
		{dayAt(10, 0), dayAt(11, 0)},
	}, dayAt(9, 0), dayAt(17, 0))
	if got != "09:00-10:00 11:00-14:00 15:00-17:00" {
		t.Fatalf("order should not matter: %s", got)
	}
}

// Two meetings that overlap are one stretch. Treating them separately invents
// a gap between them that is not there, which is how somebody gets offered a
// time they are already in a meeting.
func TestOverlappingMeetingsDoNotInventAGap(t *testing.T) {
	got := freeAt([][2]time.Time{
		{dayAt(10, 0), dayAt(12, 0)},
		{dayAt(11, 0), dayAt(13, 0)},
	}, dayAt(9, 0), dayAt(17, 0))
	if got != "09:00-10:00 13:00-17:00" {
		t.Fatalf("one stretch from ten to one: %s", got)
	}
	// And one wholly inside another changes nothing.
	inside := freeAt([][2]time.Time{
		{dayAt(10, 0), dayAt(13, 0)},
		{dayAt(11, 0), dayAt(12, 0)},
	}, dayAt(9, 0), dayAt(17, 0))
	if inside != "09:00-10:00 13:00-17:00" {
		t.Fatalf("the inner one is already covered: %s", inside)
	}
}

// A day with nothing in it is free all through, and a day booked solid has
// nothing to offer.
func TestAnEmptyDayAndAFullOne(t *testing.T) {
	if got := freeAt(nil, dayAt(9, 0), dayAt(17, 0)); got != "09:00-17:00" {
		t.Fatalf("all day: %s", got)
	}
	full := freeAt([][2]time.Time{{dayAt(9, 0), dayAt(17, 0)}}, dayAt(9, 0), dayAt(17, 0))
	if full != "" {
		t.Fatalf("nothing free: %s", full)
	}
}

// A few minutes between two meetings is not a time anybody can meet in, and
// offering it is worse than offering nothing.
func TestASliverIsNotOffered(t *testing.T) {
	got := freeAt([][2]time.Time{
		{dayAt(10, 0), dayAt(11, 0)},
		{dayAt(11, 5), dayAt(12, 0)},
	}, dayAt(10, 0), dayAt(12, 0))
	if got != "" {
		t.Fatalf("five minutes is not a meeting: %s", got)
	}
}

// A meeting that runs past the end of the working day does not go on making
// the evening busy, and one that began before it started does not make the
// morning free.
func TestAMeetingIsClippedToTheDayAskedAbout(t *testing.T) {
	got := freeAt([][2]time.Time{
		{dayAt(8, 0), dayAt(10, 0)},
		{dayAt(16, 0), dayAt(19, 0)},
	}, dayAt(9, 0), dayAt(17, 0))
	if got != "10:00-16:00" {
		t.Fatalf("the middle of the day: %s", got)
	}
}
