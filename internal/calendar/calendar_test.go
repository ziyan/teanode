package calendar

import (
	"strings"
	"testing"
	"time"
)

// crlf writes a test file the way the format wants it, so that a test can be
// read as the lines it is about rather than as escapes.
func crlf(lines ...string) []byte {
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

func wrap(lines ...string) []byte {
	all := append([]string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//TeaNode//EN", "BEGIN:VEVENT"}, lines...)
	return crlf(append(all, "END:VEVENT", "END:VCALENDAR")...)
}

func mustParse(t *testing.T, data []byte) *Parsed {
	t.Helper()
	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("that file should have parsed: %v", err)
	}
	return parsed
}

func days(occurrences []Occurrence) []string {
	when := make([]string, 0, len(occurrences))
	for _, occurrence := range occurrences {
		when = append(when, occurrence.StartsAt.Format("2006-01-02"))
	}
	return when
}

// A realistic event -- a zone, a rule, an exception, people, and a property
// this server has never heard of -- survives being read and written, and the
// fields pulled out of it say what the file says.
func TestARealEventSurvivesBeingKept(t *testing.T) {
	data := crlf(
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//EN", "CALSCALE:GREGORIAN",
		"BEGIN:VTIMEZONE", "TZID:Europe/London",
		"BEGIN:DAYLIGHT", "TZOFFSETFROM:+0000", "TZOFFSETTO:+0100", "TZNAME:BST",
		"DTSTART:19700329T010000", "RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU", "END:DAYLIGHT",
		"BEGIN:STANDARD", "TZOFFSETFROM:+0100", "TZOFFSETTO:+0000", "TZNAME:GMT",
		"DTSTART:19701025T020000", "RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU", "END:STANDARD",
		"END:VTIMEZONE",
		"BEGIN:VEVENT", "UID:9C1F2A3B-4D5E-6789-ABCD-EF0123456789",
		"DTSTAMP:20260912T120000Z",
		"DTSTART;TZID=Europe/London:20260914T100000",
		"DTEND;TZID=Europe/London:20260914T110000",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=10",
		"EXDATE;TZID=Europe/London:20260921T100000",
		"SUMMARY:Weekly sync", "LOCATION:The small room",
		"STATUS:CONFIRMED", "SEQUENCE:2", "TRANSP:OPAQUE",
		`ORGANIZER;CN="Hopper, Grace":mailto:grace@example.com`,
		"ATTENDEE;CN=Ada Lovelace;PARTSTAT=ACCEPTED;ROLE=REQ-PARTICIPANT:mailto:ada@example.com",
		"X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC",
		"END:VEVENT", "END:VCALENDAR")

	parsed := mustParse(t, data)
	if parsed.UID != "9C1F2A3B-4D5E-6789-ABCD-EF0123456789" {
		t.Fatalf("the identifier is the card's own: %q", parsed.UID)
	}
	if parsed.Summary != "Weekly sync" || parsed.Location != "The small room" {
		t.Fatalf("what and where: %q %q", parsed.Summary, parsed.Location)
	}
	if parsed.Status != "CONFIRMED" || parsed.Sequence != 2 || parsed.Transparent {
		t.Fatalf("status, sequence, transparency: %q %d %v", parsed.Status, parsed.Sequence, parsed.Transparent)
	}
	if !parsed.Recurring || parsed.AllDay {
		t.Fatalf("it recurs and is not all day: %v %v", parsed.Recurring, parsed.AllDay)
	}
	// Ten o'clock in London in September is nine o'clock UTC.
	if got := parsed.StartsAt.Format(time.RFC3339); got != "2026-09-14T09:00:00Z" {
		t.Fatalf("the zone was honoured: %s", got)
	}
	if got := parsed.EndsAt.Sub(parsed.StartsAt); got != time.Hour {
		t.Fatalf("an hour long: %v", got)
	}
	if parsed.Organizer != "grace@example.com" {
		t.Fatalf("the organizer, without the scheme: %q", parsed.Organizer)
	}
	if len(parsed.Attendees) != 1 || parsed.Attendees[0].Address != "ada@example.com" ||
		parsed.Attendees[0].Name != "Ada Lovelace" || parsed.Attendees[0].Participation != "ACCEPTED" {
		t.Fatalf("the attendee: %+v", parsed.Attendees)
	}

	// The property nobody here has heard of is still there, and so is the
	// organizer's quoted name -- the shape that destroyed an address in a
	// vCard, which is why this server writes its own vCards and not its own
	// calendars.
	kept := string(Unfold(parsed.Data))
	for _, want := range []string{
		"X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC",
		`ORGANIZER;CN="Hopper, Grace":mailto:grace@example.com`,
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=10",
		"EXDATE;TZID=Europe/London:20260921T100000",
		"BEGIN:VTIMEZONE",
	} {
		if !strings.Contains(kept, want) {
			t.Fatalf("what was kept lost %q:\n%s", want, kept)
		}
	}
}

// Writing what was read changes nothing further. The first pass normalizes,
// and everything after it is stable -- which is what lets the stored bytes be
// what the ETag is taken over and what a device is given back.
func TestKeepingItTwiceChangesNothing(t *testing.T) {
	once := mustParse(t, wrap(
		"UID:once", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z", "SUMMARY:Once"))
	twice := mustParse(t, once.Data)
	if string(once.Data) != string(twice.Data) {
		t.Fatalf("a second pass changed it:\n%s\n---\n%s", once.Data, twice.Data)
	}
	if ETag(once.Data) != ETag(twice.Data) {
		t.Fatal("and so the version would have changed under a device that had not")
	}
}

// A line too long for the format is folded, and folding can be undone.
func TestALongLineIsFolded(t *testing.T) {
	long := strings.Repeat("a description that runs on and on ", 12)
	parsed := mustParse(t, wrap(
		"UID:long", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z",
		"SUMMARY:Long", "DESCRIPTION:"+long))
	for _, line := range strings.Split(string(parsed.Data), "\r\n") {
		if len(line) > foldAt {
			t.Fatalf("a line of %d octets was written: %q", len(line), line)
		}
	}
	if !strings.Contains(string(Unfold(parsed.Data)), "DESCRIPTION:"+long) {
		t.Fatalf("unfolding did not give the description back:\n%s", Unfold(parsed.Data))
	}
}

// Folding breaks between characters. A line of emoji folded by counting
// octets alone would cut one in half, and half a character is not text.
func TestFoldingDoesNotCutACharacterInHalf(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:emoji", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z",
		"SUMMARY:"+strings.Repeat("\U0001F642", 30)))
	if !strings.Contains(string(Unfold(parsed.Data)), "SUMMARY:"+strings.Repeat("\U0001F642", 30)) {
		t.Fatalf("the faces did not survive:\n%s", Unfold(parsed.Data))
	}
	for _, line := range strings.Split(string(parsed.Data), "\r\n") {
		if len(line) > foldAt {
			t.Fatalf("a line of %d octets: %q", len(line), line)
		}
	}
}

// An event that does not recur happens once, at its own time.
func TestSomethingThatHappensOnce(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:once", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z", "SUMMARY:Once"))
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	occurrences, err := Occurrences(parsed, from, until)
	if err != nil || len(occurrences) != 1 {
		t.Fatalf("once: %v %v", days(occurrences), err)
	}
	if occurrences[0].EndsAt.Sub(occurrences[0].StartsAt) != time.Hour {
		t.Fatalf("an hour: %v", occurrences[0])
	}
	// And not at all, in a window it is not in.
	away, err := Occurrences(parsed,
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(away) != 0 {
		t.Fatalf("not in November: %v %v", days(away), err)
	}
}

// An excluded occurrence does not happen.
//
// The dates here are chosen so the rule really does generate the one being
// excluded. An exception for a date the rule never reaches proves nothing at
// all, which is how the first version of this test passed while testing
// nothing: it excluded a Tuesday from a rule that only ever named Mondays.
func TestAnExcludedOccurrenceDoesNotHappen(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:weekly", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=5",
		"EXDATE:20260921T100000Z", "SUMMARY:Weekly"))
	occurrences, err := Occurrences(parsed,
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	got := strings.Join(days(occurrences), " ")
	if got != "2026-09-14 2026-09-28 2026-10-05 2026-10-12" {
		t.Fatalf("five weeks less the cancelled one: %s", got)
	}
}

// A date added by hand happens, even though no rule generates it.
//
// This is the test that earns its keep. The version of the library this
// server first reached for -- the one the WebDAV library asks for, and so the
// one that arrives unless a version is named -- read the exception dates a
// second time where it meant to read the added ones, so RDATE was ignored
// altogether and an occurrence somebody had put in by hand simply was not
// there. Removing the fix from the vendored copy makes this test fail and
// every other test here still pass, which is why the version is pinned.
func TestADateAddedByHandHappens(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:added", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=2",
		"RDATE:20260916T100000Z", "SUMMARY:Weekly and one more"))
	occurrences, err := Occurrences(parsed,
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	got := strings.Join(days(occurrences), " ")
	if got != "2026-09-14 2026-09-16 2026-09-21" {
		t.Fatalf("the two Mondays and the Wednesday: %s", got)
	}
}

// An event whose start does not satisfy its own rule does not happen on its
// start. Prepending it would put a meeting in the calendar that the rule
// never described.
func TestItDoesNotHappenOnADayItsRuleDoesNotName(t *testing.T) {
	// The 15th of September 2026 is a Tuesday; the rule says Mondays.
	parsed := mustParse(t, wrap(
		"UID:tuesday", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260915T100000Z", "DTEND:20260915T110000Z",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=3", "SUMMARY:Mondays"))
	occurrences, err := Occurrences(parsed,
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	got := strings.Join(days(occurrences), " ")
	if got != "2026-09-21 2026-09-28 2026-10-05" {
		t.Fatalf("Mondays only: %s", got)
	}
}

// The wall-clock time holds across a change of offset, which is what a person
// means by "every Monday at ten".
func TestTenOClockStaysTenOClockAcrossTheClocksChanging(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	parsed := mustParse(t, crlf(
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//EN",
		"BEGIN:VTIMEZONE", "TZID:Europe/London",
		"BEGIN:DAYLIGHT", "TZOFFSETFROM:+0000", "TZOFFSETTO:+0100", "TZNAME:BST",
		"DTSTART:19700329T010000", "RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU", "END:DAYLIGHT",
		"BEGIN:STANDARD", "TZOFFSETFROM:+0100", "TZOFFSETTO:+0000", "TZNAME:GMT",
		"DTSTART:19701025T020000", "RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU", "END:STANDARD",
		"END:VTIMEZONE",
		"BEGIN:VEVENT", "UID:clocks", "DTSTAMP:20260912T120000Z",
		"DTSTART;TZID=Europe/London:20260914T100000",
		"DTEND;TZID=Europe/London:20260914T110000",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=12", "SUMMARY:Weekly",
		"END:VEVENT", "END:VCALENDAR"))
	occurrences, err := Occurrences(parsed,
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(occurrences) != 12 {
		t.Fatalf("twelve Mondays: %d %v", len(occurrences), err)
	}
	for _, occurrence := range occurrences {
		if got := occurrence.StartsAt.In(london).Format("15:04"); got != "10:00" {
			t.Fatalf("%s was at %s in London", occurrence.StartsAt.Format("2006-01-02"), got)
		}
	}
	// And the clocks really did change inside that window, so the test is
	// about something: the last Sunday of October 2026 is the 25th.
	first := occurrences[0].StartsAt.Format("15:04")
	last := occurrences[len(occurrences)-1].StartsAt.Format("15:04")
	if first == last {
		t.Fatalf("the offset never changed, so this proved nothing: %s %s", first, last)
	}
}

// A birthday belongs to the day, everywhere. Written as a date rather than a
// date and a time, it must not be shifted into the day before by a zone.
func TestAnAllDayEventStaysOnItsDay(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:birthday", "DTSTAMP:20260912T120000Z",
		"DTSTART;VALUE=DATE:20261210", "DTEND;VALUE=DATE:20261211",
		"SUMMARY:Ada's birthday"))
	if !parsed.AllDay {
		t.Fatal("it is an all-day event")
	}
	if got := parsed.StartsAt.Format("2006-01-02"); got != "2026-12-10" {
		t.Fatalf("the tenth: %s", got)
	}
	occurrences, err := Occurrences(parsed,
		time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(occurrences) != 1 || !occurrences[0].AllDay {
		t.Fatalf("once, all day: %v %v", occurrences, err)
	}
	if got := occurrences[0].StartsAt.Format("2006-01-02"); got != "2026-12-10" {
		t.Fatalf("still the tenth: %s", got)
	}
}

// An event that began before the window and has not finished is still on: a
// person looking at Tuesday wants to see the conference that started Monday.
func TestSomethingStillRunningIsStillOn(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:conference", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T090000Z", "DTEND:20260918T170000Z", "SUMMARY:Conference"))
	occurrences, err := Occurrences(parsed,
		time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC))
	if err != nil || len(occurrences) != 1 {
		t.Fatalf("the conference is on on Wednesday: %v %v", occurrences, err)
	}
}

// An end before the start describes nothing, and is treated as an instant
// rather than refused: some clients do send it, and refusing loses the event.
func TestAnEndBeforeItsStartIsAnInstant(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:backwards", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T110000Z", "DTEND:20260914T100000Z", "SUMMARY:Backwards"))
	if !parsed.EndsAt.Equal(parsed.StartsAt) {
		t.Fatalf("an instant: %v to %v", parsed.StartsAt, parsed.EndsAt)
	}
}

// An event with a length rather than an end is as long as it says.
func TestALengthInsteadOfAnEnd(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:duration", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DURATION:PT90M", "SUMMARY:Ninety minutes"))
	if got := parsed.EndsAt.Sub(parsed.StartsAt); got != 90*time.Minute {
		t.Fatalf("ninety minutes: %v", got)
	}
}

// A file that is not a calendar, is empty, has no event, has no identifier,
// or is larger than this server keeps, is refused rather than stored.
func TestWhatIsRefused(t *testing.T) {
	for name, data := range map[string][]byte{
		"nothing at all": []byte("   \r\n"),
		"not a calendar": []byte("hello, this is not a calendar\r\n"),
		"a calendar with no event": crlf("BEGIN:VCALENDAR", "VERSION:2.0",
			"PRODID:-//Example//EN", "END:VCALENDAR"),
		"an event with no identifier": wrap("DTSTAMP:20260912T120000Z",
			"DTSTART:20260914T100000Z", "SUMMARY:Nameless"),
	} {
		if _, err := Parse(data); err == nil {
			t.Fatalf("%s should have been refused", name)
		}
	}
	huge := append(wrap("UID:huge", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "SUMMARY:Huge"), make([]byte, MaximumObject)...)
	if _, err := Parse(huge); err != ErrTooLarge {
		t.Fatalf("too large should say so by name: %v", err)
	}
}

// The event a file is about is the one without a recurrence identifier: the
// others are occurrences of it that somebody moved.
func TestTheEventIsTheOneThatIsNotAnOverride(t *testing.T) {
	parsed := mustParse(t, crlf(
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//EN",
		"BEGIN:VEVENT", "UID:series", "DTSTAMP:20260912T120000Z",
		"RECURRENCE-ID:20260921T100000Z",
		"DTSTART:20260921T140000Z", "DTEND:20260921T150000Z", "SUMMARY:Moved",
		"END:VEVENT",
		"BEGIN:VEVENT", "UID:series", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=3", "SUMMARY:Weekly",
		"END:VEVENT", "END:VCALENDAR"))
	if parsed.Summary != "Weekly" {
		t.Fatalf("the series, not the occurrence that was moved: %q", parsed.Summary)
	}
}

// An invitation says what it is, so that what arrives by mail can be told
// apart from what somebody typed.
func TestAnInvitationSaysSo(t *testing.T) {
	parsed := mustParse(t, crlf(
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//EN", "METHOD:REQUEST",
		"BEGIN:VEVENT", "UID:invited", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z", "SUMMARY:Please come",
		"ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:ada@example.com",
		"END:VEVENT", "END:VCALENDAR"))
	if parsed.Method != "REQUEST" {
		t.Fatalf("a request: %q", parsed.Method)
	}
	if len(parsed.Attendees) != 1 || parsed.Attendees[0].Participation != "NEEDS-ACTION" {
		t.Fatalf("waiting on an answer: %+v", parsed.Attendees)
	}
}

// Something that is not a mail address is left alone rather than guessed at.
// A room booking system may name a resource by some other scheme, and turning
// that into an address would be inventing one.
func TestSomethingThatIsNotAnAddressIsNotMadeIntoOne(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:room", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z", "SUMMARY:In a room",
		"ORGANIZER:urn:x-rooms:small", "ATTENDEE:mailto:ada@example.com"))
	if parsed.Organizer != "" {
		t.Fatalf("not an address: %q", parsed.Organizer)
	}
	if len(parsed.Attendees) != 1 {
		t.Fatalf("the one who is: %+v", parsed.Attendees)
	}
}

// A rule that repeats for ever does not expand for ever.
func TestARuleThatNeverEndsIsBounded(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:forever", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T100100Z",
		"RRULE:FREQ=MINUTELY", "SUMMARY:Every minute, for ever"))
	occurrences, err := Occurrences(parsed,
		time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
		time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	if len(occurrences) != MaximumOccurrences {
		t.Fatalf("bounded at %d, got %d", MaximumOccurrences, len(occurrences))
	}
}
