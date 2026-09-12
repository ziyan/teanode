package calendar

import (
	"strings"
	"testing"
	"time"
)

func text(value string) *string { return &value }
func flag(value bool) *bool     { return &value }
func moment(year int, month time.Month, day, hour, minute int) *time.Time {
	at := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	return &at
}

func mustBuild(t *testing.T, previous []byte, fields *Fields) *Parsed {
	t.Helper()
	built, err := Build(previous, fields)
	if err != nil {
		t.Fatalf("that should have built: %v", err)
	}
	return built
}

// A new event gets everything a file needs without anybody filling it in.
func TestANewEventIsAWholeFile(t *testing.T) {
	built := mustBuild(t, nil, &Fields{
		Summary:  text("Coffee"),
		StartsAt: moment(2026, time.September, 14, 10, 0),
		EndsAt:   moment(2026, time.September, 14, 11, 0),
	})
	if built.UID == "" || !strings.Contains(built.UID, "@") {
		t.Fatalf("an event names itself: %q", built.UID)
	}
	if built.Summary != "Coffee" {
		t.Fatalf("what it is: %q", built.Summary)
	}
	written := string(Unfold(built.Data))
	for _, want := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:", "BEGIN:VEVENT", "DTSTAMP:"} {
		if !strings.Contains(written, want) {
			t.Fatalf("a whole file needs %q:\n%s", want, written)
		}
	}
	// Two events made at once are two events.
	other := mustBuild(t, nil, &Fields{
		Summary: text("Coffee"), StartsAt: moment(2026, time.September, 14, 10, 0)})
	if other.UID == built.UID {
		t.Fatal("two events made separately are not the same event")
	}
}

// Nothing said about a field leaves it alone; a field emptied is taken away.
// A caller sending only a title must not thereby delete the location.
func TestLeavingABoxOutIsNotTheSameAsEmptyingIt(t *testing.T) {
	first := mustBuild(t, nil, &Fields{
		Summary: text("Standup"), Location: text("The small room"),
		StartsAt: moment(2026, time.September, 14, 10, 0),
		EndsAt:   moment(2026, time.September, 14, 10, 15),
	})
	// Only the title.
	renamed := mustBuild(t, first.Data, &Fields{Summary: text("Stand-up")})
	if renamed.Summary != "Stand-up" || renamed.Location != "The small room" {
		t.Fatalf("the place was not asked about: %q %q", renamed.Summary, renamed.Location)
	}
	if !renamed.StartsAt.Equal(first.StartsAt) {
		t.Fatalf("nor was the time: %v %v", renamed.StartsAt, first.StartsAt)
	}
	if renamed.UID != first.UID {
		t.Fatalf("it is still the same event: %q %q", renamed.UID, first.UID)
	}
	// And now emptied.
	cleared := mustBuild(t, renamed.Data, &Fields{Location: text("")})
	if cleared.Location != "" {
		t.Fatalf("the place was cleared: %q", cleared.Location)
	}
}

// What a phone put on an event and no form here shows survives being edited
// in a browser.
func TestWhatAPhonePutThereSurvivesBeingEditedHere(t *testing.T) {
	fromPhone := crlf(
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Apple Inc.//iOS 26.0//EN",
		"BEGIN:VEVENT", "UID:from-a-phone", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z", "SUMMARY:Lunch",
		"X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC",
		"X-APPLE-STRUCTURED-LOCATION;VALUE=URI:geo:51.5,-0.1",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT15M", "DESCRIPTION:Lunch", "END:VALARM",
		"END:VEVENT", "END:VCALENDAR")
	edited := mustBuild(t, fromPhone, &Fields{Summary: text("Lunch with Ada")})
	written := string(Unfold(edited.Data))
	for _, want := range []string{
		"X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC",
		"X-APPLE-STRUCTURED-LOCATION;VALUE=URI:geo:51.5,-0.1",
		"BEGIN:VALARM", "TRIGGER:-PT15M",
		"SUMMARY:Lunch with Ada",
	} {
		if !strings.Contains(written, want) {
			t.Fatalf("editing the title lost %q:\n%s", want, written)
		}
	}
	if edited.UID != "from-a-phone" {
		t.Fatalf("and it is still the same event: %q", edited.UID)
	}
}

// An event anchored to a zone stays at the same wall-clock time when the
// clocks change, which is the entire reason a zone is written rather than an
// offset. Ten o'clock every Monday is ten o'clock in November too.
func TestARecurringEventKeepsItsHourWhenTheClocksChange(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	built := mustBuild(t, nil, &Fields{
		Summary:    text("Weekly sync"),
		StartsAt:   moment(2026, time.September, 14, 9, 0), // 10:00 in London, which is BST
		EndsAt:     moment(2026, time.September, 14, 10, 0),
		Timezone:   "Europe/London",
		Recurrence: text("FREQ=WEEKLY;BYDAY=MO;COUNT=12"),
	})
	written := string(Unfold(built.Data))
	if !strings.Contains(written, "BEGIN:VTIMEZONE") || !strings.Contains(written, "TZID:Europe/London") {
		t.Fatalf("the zone must be described, not just named:\n%s", written)
	}
	if !strings.Contains(written, "DTSTART;TZID=Europe/London:20260914T100000") {
		t.Fatalf("ten o'clock, in London:\n%s", written)
	}
	occurrences, err := Occurrences(built,
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(occurrences) != 12 {
		t.Fatalf("twelve Mondays: %d %v", len(occurrences), err)
	}
	var offsets = map[string]bool{}
	for _, occurrence := range occurrences {
		local := occurrence.StartsAt.In(london)
		if got := local.Format("15:04"); got != "10:00" {
			t.Fatalf("%s was at %s in London", local.Format("2006-01-02"), got)
		}
		offsets[local.Format("-0700")] = true
	}
	// And the clocks really did change inside that span, so this tested
	// something: both offsets were seen.
	if len(offsets) != 2 {
		t.Fatalf("the clocks did not change in that span, so this proved nothing: %v", offsets)
	}
}

// The zone written describes both sides of the change, the right way round.
func TestTheWrittenZoneSaysWhatTheOffsetsAre(t *testing.T) {
	if _, err := time.LoadLocation("Europe/London"); err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	built := mustBuild(t, nil, &Fields{
		Summary:    text("Weekly"),
		StartsAt:   moment(2026, time.September, 14, 9, 0),
		EndsAt:     moment(2026, time.September, 14, 10, 0),
		Timezone:   "Europe/London",
		Recurrence: text("FREQ=WEEKLY;BYDAY=MO;COUNT=12"),
	})
	written := string(Unfold(built.Data))
	// Clocks back in October: British Summer Time gives way to Greenwich.
	if !strings.Contains(written, "BEGIN:STANDARD") {
		t.Fatalf("the change back to standard time:\n%s", written)
	}
	if !strings.Contains(written, "TZOFFSETFROM:+0100") || !strings.Contains(written, "TZOFFSETTO:+0000") {
		t.Fatalf("an hour back:\n%s", written)
	}
}

// A zone that does not change is written as one stretch rather than as
// nothing at all, so that a client is never left guessing.
func TestAZoneThatNeverChangesIsStillWritten(t *testing.T) {
	if _, err := time.LoadLocation("Asia/Tokyo"); err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	built := mustBuild(t, nil, &Fields{
		Summary:  text("Morning call"),
		StartsAt: moment(2026, time.September, 14, 1, 0), // 10:00 in Tokyo
		EndsAt:   moment(2026, time.September, 14, 2, 0),
		Timezone: "Asia/Tokyo",
	})
	written := string(Unfold(built.Data))
	if !strings.Contains(written, "TZID:Asia/Tokyo") || !strings.Contains(written, "TZOFFSETTO:+0900") {
		t.Fatalf("nine hours ahead, all year:\n%s", written)
	}
	if !strings.Contains(written, "DTSTART;TZID=Asia/Tokyo:20260914T100000") {
		t.Fatalf("ten o'clock, in Tokyo:\n%s", written)
	}
}

// Moving an event from one zone to another leaves one zone behind it, not
// two. Two components claiming the same identifier is not something a client
// should have to choose between.
func TestChangingTheZoneDoesNotLeaveTheOldOneBehind(t *testing.T) {
	for _, name := range []string{"Europe/London", "Asia/Tokyo"} {
		if _, err := time.LoadLocation(name); err != nil {
			t.Skipf("this machine has no zone database: %v", err)
		}
	}
	first := mustBuild(t, nil, &Fields{
		Summary: text("Call"), StartsAt: moment(2026, time.September, 14, 9, 0),
		EndsAt: moment(2026, time.September, 14, 10, 0), Timezone: "Europe/London",
	})
	moved := mustBuild(t, first.Data, &Fields{
		StartsAt: moment(2026, time.September, 14, 9, 0),
		EndsAt:   moment(2026, time.September, 14, 10, 0), Timezone: "Asia/Tokyo",
	})
	written := string(Unfold(moved.Data))
	if strings.Contains(written, "TZID:Europe/London") {
		t.Fatalf("the old zone is gone:\n%s", written)
	}
	if strings.Count(written, "BEGIN:VTIMEZONE") != 1 {
		t.Fatalf("exactly one zone:\n%s", written)
	}
}

// An all-day event is written as a date, with no zone, so that it does not
// move a day for somebody reading it from further west.
func TestAnAllDayEventIsWrittenAsADate(t *testing.T) {
	built := mustBuild(t, nil, &Fields{
		Summary:  text("Ada's birthday"),
		StartsAt: moment(2026, time.December, 10, 0, 0),
		AllDay:   flag(true),
		Timezone: "Asia/Tokyo",
	})
	written := string(Unfold(built.Data))
	if !strings.Contains(written, "DTSTART;VALUE=DATE:20261210") {
		t.Fatalf("a date, not a moment:\n%s", written)
	}
	if strings.Contains(written, "TZID") {
		t.Fatalf("and no zone at all:\n%s", written)
	}
	if !built.AllDay {
		t.Fatal("and it reads back as all day")
	}
}

// Turning an all-day event into one with a time, and back, leaves the file
// describing only one of them.
func TestAnEventCanStopBeingAllDay(t *testing.T) {
	allDay := mustBuild(t, nil, &Fields{
		Summary: text("Moving day"), StartsAt: moment(2026, time.December, 10, 0, 0),
		AllDay: flag(true),
	})
	timed := mustBuild(t, allDay.Data, &Fields{
		StartsAt: moment(2026, time.December, 10, 9, 0),
		EndsAt:   moment(2026, time.December, 10, 17, 0),
		AllDay:   flag(false),
	})
	if timed.AllDay {
		t.Fatal("it has a time now")
	}
	written := string(Unfold(timed.Data))
	if strings.Contains(written, "VALUE=DATE:") {
		t.Fatalf("and is not still a date:\n%s", written)
	}
	if got := timed.EndsAt.Sub(timed.StartsAt); got != 8*time.Hour {
		t.Fatalf("nine to five: %v", got)
	}
}

// A length given instead of an end is replaced when an end is written, rather
// than left behind to argue with it.
func TestALengthIsReplacedByAnEnd(t *testing.T) {
	withDuration := crlf(
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//EN",
		"BEGIN:VEVENT", "UID:lengthy", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DURATION:PT3H", "SUMMARY:Long one",
		"END:VEVENT", "END:VCALENDAR")
	built := mustBuild(t, withDuration, &Fields{
		StartsAt: moment(2026, time.September, 14, 10, 0),
		EndsAt:   moment(2026, time.September, 14, 11, 0),
	})
	if strings.Contains(string(Unfold(built.Data)), "DURATION:") {
		t.Fatalf("the length is gone:\n%s", built.Data)
	}
	if got := built.EndsAt.Sub(built.StartsAt); got != time.Hour {
		t.Fatalf("an hour: %v", got)
	}
}

// An event with no end is an hour, because that is what a calendar means by
// an appointment; an all-day one with no end is a day.
func TestSomethingWithNoEndIsGivenOne(t *testing.T) {
	hour := mustBuild(t, nil, &Fields{
		Summary: text("Chat"), StartsAt: moment(2026, time.September, 14, 10, 0)})
	if got := hour.EndsAt.Sub(hour.StartsAt); got != time.Hour {
		t.Fatalf("an hour: %v", got)
	}
	day := mustBuild(t, nil, &Fields{
		Summary: text("Away"), StartsAt: moment(2026, time.December, 10, 0, 0), AllDay: flag(true)})
	if got := day.EndsAt.Sub(day.StartsAt); got != 24*time.Hour {
		t.Fatalf("a day: %v", got)
	}
}

// What is refused, and said plainly rather than stored and silently wrong.
func TestWhatBuildingRefuses(t *testing.T) {
	if _, err := Build(nil, &Fields{Summary: text("Nowhen")}); err == nil {
		t.Fatal("an event with no start should be refused")
	}
	if _, err := Build(nil, &Fields{
		Summary: text("Bad rule"), StartsAt: moment(2026, time.September, 14, 10, 0),
		Recurrence: text("FREQ=NEVERLY;NONSENSE"),
	}); err == nil {
		t.Fatal("a repeat that cannot be read should be refused while somebody is typing")
	}
	if _, err := Build(nil, &Fields{
		Summary: text("Nowhere"), StartsAt: moment(2026, time.September, 14, 10, 0),
		Timezone: "Mars/Olympus_Mons",
	}); err == nil {
		t.Fatal("a zone this server does not know should be refused")
	}
	if _, err := Build(nil, &Fields{
		Summary: text("Odd"), StartsAt: moment(2026, time.September, 14, 10, 0),
		Status: text("MAYBE"),
	}); err == nil {
		t.Fatal("an event is confirmed, tentative or cancelled")
	}
	if _, err := Build([]byte("this is not a calendar"), &Fields{Summary: text("x")}); err == nil {
		t.Fatal("what cannot be read cannot be edited")
	}
}

// An event stops recurring when the repeat is emptied.
func TestARepeatCanBeTakenAway(t *testing.T) {
	repeating := mustBuild(t, nil, &Fields{
		Summary: text("Weekly"), StartsAt: moment(2026, time.September, 14, 10, 0),
		EndsAt:     moment(2026, time.September, 14, 11, 0),
		Recurrence: text("FREQ=WEEKLY;COUNT=5"),
	})
	if !repeating.Recurring {
		t.Fatal("it repeats")
	}
	once := mustBuild(t, repeating.Data, &Fields{Recurrence: text("")})
	if once.Recurring {
		t.Fatal("and now it does not")
	}
	occurrences, err := Occurrences(once,
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(occurrences) != 1 {
		t.Fatalf("once: %d %v", len(occurrences), err)
	}
}

// Writing an event twice gives a file that is stable but for the moment it
// records having been written, which is meant to change.
func TestBuildingIsStableExceptForWhenItWasWritten(t *testing.T) {
	first := mustBuild(t, nil, &Fields{
		Summary: text("Stable"), StartsAt: moment(2026, time.September, 14, 10, 0),
		EndsAt: moment(2026, time.September, 14, 11, 0),
	})
	second := mustBuild(t, first.Data, &Fields{})
	strip := func(data []byte) string {
		var kept []string
		for _, line := range strings.Split(string(data), "\r\n") {
			if !strings.HasPrefix(line, "DTSTAMP:") {
				kept = append(kept, line)
			}
		}
		return strings.Join(kept, "\r\n")
	}
	if strip(first.Data) != strip(second.Data) {
		t.Fatalf("nothing else should have moved:\n%s\n---\n%s", first.Data, second.Data)
	}
}

// A zone described past the end of what this machine's zone database knows
// stops rather than spinning.
//
// This is the test for a real hang. Go's table for Europe/London runs out at
// the end of 2040; past the last change, ZoneBounds returns the moment asked
// about rather than the zero time, which reads as "there is a change, and it
// is now". A loop that takes that at face value never advances, and a bound
// that counted only the observances it wrote never noticed, because the step
// that writes nothing is exactly the step it was stuck on. An event that
// repeats for ever is the ordinary way to reach it.
func TestAZoneIsNotDescribedPastTheEndOfWhatIsKnown(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	done := make(chan int, 1)
	go func() {
		component, err := zoneComponent(london,
			time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC),
			time.Date(2400, 1, 1, 0, 0, 0, 0, time.UTC))
		if err != nil || component == nil {
			done <- -1
			return
		}
		done <- len(component.Children)
	}()
	select {
	case written := <-done:
		if written <= 0 {
			t.Fatalf("a zone should have been described: %d", written)
		}
		if written > maximumObservances {
			t.Fatalf("and bounded: %d", written)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("describing a zone did not finish")
	}
}

// An event that repeats with no end still gets a zone, and getting it does
// not take a noticeable amount of time.
func TestAnEventThatRepeatsForEverIsWrittenQuickly(t *testing.T) {
	if _, err := time.LoadLocation("Europe/London"); err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	started := time.Now()
	built := mustBuild(t, nil, &Fields{
		Summary:    text("Every Monday, indefinitely"),
		StartsAt:   moment(2026, time.September, 14, 9, 0),
		EndsAt:     moment(2026, time.September, 14, 10, 0),
		Timezone:   "Europe/London",
		Recurrence: text("FREQ=WEEKLY;BYDAY=MO"),
	})
	if taken := time.Since(started); taken > 5*time.Second {
		t.Fatalf("writing one event took %v", taken)
	}
	if !strings.Contains(string(Unfold(built.Data)), "BEGIN:VTIMEZONE") {
		t.Fatal("and it still carries its zone")
	}
}
