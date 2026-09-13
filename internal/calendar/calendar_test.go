package calendar

import (
	"fmt"
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

// A zone named the way Microsoft names them is read from the description the
// file carries, not from this machine's table.
//
// Exchange and Outlook write "W. Europe Standard Time" rather than
// "Europe/Berlin", and the library resolves a TZID with LoadLocation and
// ignores the VTIMEZONE beside it. That failed quietly: the moment came back
// as the zero time, the event was stored starting in the year one, and it
// then appeared in no window anybody ever asked about -- not the calendar,
// not free-busy, not a phone. It is the commonest inbound invitation there is.
func TestAZoneThisMachineDoesNotKnowIsReadFromTheFile(t *testing.T) {
	written := exchangeEvent("W. Europe Standard Time", "20260310T100000", nil)
	parsed := mustParse(t, written)
	if parsed.StartsAt.IsZero() {
		t.Fatal("the start has to be readable")
	}
	// The tenth of March is the winter side of the European change, so the
	// offset is one hour: ten o'clock local is nine o'clock UTC.
	if got := parsed.StartsAt.Format(time.RFC3339); got != "2026-03-10T09:00:00Z" {
		t.Fatalf("ten o'clock in that zone: %s", got)
	}
	if got := parsed.EndsAt.Sub(parsed.StartsAt); got != time.Hour {
		t.Fatalf("an hour long: %v", got)
	}
}

// And a repeat in such a zone still keeps its hour when the clocks change.
func TestARepeatInAZoneFromTheFileKeepsItsHour(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	written := exchangeEvent("W. Europe Standard Time", "20260316T100000",
		[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=4"})
	parsed := mustParse(t, written)
	occurrences, err := Occurrences(parsed,
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(occurrences) != 4 {
		t.Fatalf("four Mondays: %d %v", len(occurrences), err)
	}
	offsets := map[string]bool{}
	for _, occurrence := range occurrences {
		local := occurrence.StartsAt.In(berlin)
		if got := local.Format("15:04"); got != "10:00" {
			t.Fatalf("%s was at %s in Berlin", local.Format("2006-01-02"), got)
		}
		offsets[local.Format("-0700")] = true
	}
	// The clocks changed inside that span, so this tested something.
	if len(offsets) != 2 {
		t.Fatalf("the offset never changed, so this proved nothing: %v", offsets)
	}
}

// exchangeEvent is a file shaped the way Exchange writes one: a zone named its
// way, described in full beside the event.
func exchangeEvent(tzid, start string, extra []string) []byte {
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Microsoft Exchange Server 2019//EN",
		"BEGIN:VTIMEZONE", "TZID:" + tzid,
		"BEGIN:STANDARD", "DTSTART:16011028T030000", "TZOFFSETFROM:+0200", "TZOFFSETTO:+0100",
		"RRULE:FREQ=YEARLY;BYDAY=-1SU;BYMONTH=10", "END:STANDARD",
		"BEGIN:DAYLIGHT", "DTSTART:16010325T020000", "TZOFFSETFROM:+0100", "TZOFFSETTO:+0200",
		"RRULE:FREQ=YEARLY;BYDAY=-1SU;BYMONTH=3", "END:DAYLIGHT",
		"END:VTIMEZONE",
		"BEGIN:VEVENT", "UID:from-exchange", "DTSTAMP:20260912T120000Z",
		"DTSTART;TZID=" + tzid + ":" + start,
		"DTEND;TZID=" + tzid + ":" + start[:9] + "110000",
		"SUMMARY:From Exchange",
	}
	lines = append(lines, extra...)
	return crlf(append(lines, "END:VEVENT", "END:VCALENDAR")...)
}

// A rule that repeats for ever is bounded in the work it costs, not only in
// what it returns.
//
// Between builds the whole list before handing it back, so a cap applied to
// the result limited the answer and not the work: "every second" over the
// stretch this server indexes is ninety million moments built in memory. One
// message carrying that rule was enough to take the server down, and it
// needed no account -- an invitation from any sender reaches this.
func TestARuleThatRepeatsForEverIsBoundedInWorkNotJustInAnswer(t *testing.T) {
	parsed := mustParse(t, wrap(
		"UID:spin", "DTSTAMP:20260912T120000Z",
		"DTSTART:20260101T000000Z", "DTEND:20260101T000100Z",
		"RRULE:FREQ=SECONDLY", "SUMMARY:Every second, for ever"))

	// A window years wide, asked for the way the index asks for it. If the
	// work were proportional to the rule rather than to the bound this
	// would allocate gigabytes; the deadline is what says it does not.
	done := make(chan int, 1)
	go func() {
		occurrences, err := Occurrences(parsed,
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			done <- -1
			return
		}
		done <- len(occurrences)
	}()
	select {
	case got := <-done:
		if got != MaximumOccurrences {
			t.Fatalf("bounded at %d, got %d", MaximumOccurrences, got)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("expanding a rule that repeats every second did not finish")
	}
}

// A whole-day event given the same date at both ends lasts the day.
//
// The format writes the end of a date range as the day after, so equal dates
// mean no time at all: the event fell out of every window that asked for it,
// including the day view of the day it was on. A form that offers one date
// sends exactly this.
func TestAWholeDayEventWithOneDateLastsTheDay(t *testing.T) {
	built, err := Build(nil, &Fields{
		Summary:  text("Moving day"),
		StartsAt: moment(2026, time.March, 15, 0, 0),
		EndsAt:   moment(2026, time.March, 15, 0, 0),
		AllDay:   flag(true),
	})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if got := built.EndsAt.Sub(built.StartsAt); got != 24*time.Hour {
		t.Fatalf("a day: %v", got)
	}
	// And it is found by a window covering that day.
	occurrences, err := Occurrences(built,
		time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC))
	if err != nil || len(occurrences) != 1 {
		t.Fatalf("on the day it is on: %d %v", len(occurrences), err)
	}
}

// A zone written the way Windows writes it becomes the zone everybody else
// means, so everything after it is the ordinary path.
//
// The first attempt worked around the library at each place a time was read,
// expanding recurrences in wall clock and putting the offset back per
// occurrence. That left EXDATE and UNTIL -- both real instants -- comparing
// against wall clock, so a cancelled occurrence came back and the last of a
// series went missing, and it rescanned the zone on every step of every rule.
// Renaming the zone once, up front, is what makes those disappear.
func TestAWindowsZoneBecomesTheZoneItMeans(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Berlin"); err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	// The same file twice, differing only in what the zone is called. They
	// have to behave identically.
	for _, shape := range []struct {
		what  string
		extra []string
		want  string
	}{
		{"a cancelled occurrence stays cancelled",
			[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=4", "EXDATE:20260323T090000Z"},
			"2026-03-16T09:00:00Z 2026-03-30T08:00:00Z 2026-04-06T08:00:00Z"},
		{"the last of a series survives",
			[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO;UNTIL=20260420T083000Z"},
			"2026-03-16T09:00:00Z 2026-03-23T09:00:00Z 2026-03-30T08:00:00Z " +
				"2026-04-06T08:00:00Z 2026-04-13T08:00:00Z 2026-04-20T08:00:00Z"},
	} {
		for _, tzid := range []string{"Europe/Berlin", "W. Europe Standard Time"} {
			parsed := mustParse(t, exchangeEvent(tzid, "20260316T100000", shape.extra))
			occurrences, err := Occurrences(parsed,
				time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
				time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("%s (%s): %v", shape.what, tzid, err)
			}
			var when []string
			for _, occurrence := range occurrences {
				when = append(when, occurrence.StartsAt.Format(time.RFC3339))
			}
			if got := strings.Join(when, " "); got != shape.want {
				t.Errorf("%s (%s):\n got  %s\n want %s", shape.what, tzid, got, shape.want)
			}
		}
	}
}

// And the offset is right on the days the literal dates in a VTIMEZONE
// disagree with the rules beside them.
//
// Reading the offset out of the file took the observance's own 1601 date and
// shifted it to the event's year, which is several days out from the rule
// that actually governs: the European change is the last Sunday in March, and
// the literal date is the 25th. An event on the 26th was stored an hour early.
func TestTheOffsetIsRightNearAChangeOfClocks(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Berlin"); err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	// The 26th of March 2026 is before the real change on the 29th, so ten
	// o'clock in Berlin is nine o'clock UTC.
	parsed := mustParse(t, exchangeEvent("W. Europe Standard Time", "20260326T100000", nil))
	if got := parsed.StartsAt.Format(time.RFC3339); got != "2026-03-26T09:00:00Z" {
		t.Fatalf("ten o'clock in Berlin on the 26th: %s", got)
	}
}

// A repeat too fine to reach the window says so rather than looking empty.
//
// The step bound stops the walk, and returning what had been gathered meant a
// rule fine enough and old enough -- every hour, six years back -- spent the
// whole bound getting to today and then reported that nothing was happening.
// The event vanished from every view while a fetch of it still worked.
func TestARepeatTooFineToReachTheWindowSaysSo(t *testing.T) {
	old := time.Now().AddDate(-6, 0, 0).UTC().Format("20060102T150405Z")
	parsed := mustParse(t, wrap(
		"UID:fine", "DTSTAMP:20260912T120000Z",
		"DTSTART:"+old, "DTEND:"+old,
		"RRULE:FREQ=HOURLY", "SUMMARY:Every hour, for years"))
	_, _, err := Indexed(parsed)
	if err == nil {
		t.Fatal("a repeat that cannot be reached is not the same as one with nothing in it")
	}
}

// Several excluded dates on one line are honoured.
//
// The format allows a list, comma separated, and plenty of programs write one.
// The library reads a property's value as a single moment, so one such line
// made the whole recurrence unreadable -- in any zone -- and the event
// appeared nowhere while a fetch of it still worked.
func TestSeveralExcludedDatesOnOneLineAreHonoured(t *testing.T) {
	for _, tzid := range []string{"", "W. Europe Standard Time"} {
		what := "no zone"
		listed := wrap(
			"UID:listed", "DTSTAMP:20260912T120000Z",
			"DTSTART:20260601T080000Z", "DTEND:20260601T090000Z",
			"RRULE:FREQ=WEEKLY;COUNT=4",
			"EXDATE:20260608T080000Z,20260615T080000Z", "SUMMARY:Weekly")
		if tzid != "" {
			what = tzid
			listed = exchangeEvent(tzid, "20260601T100000", []string{
				"RRULE:FREQ=WEEKLY;COUNT=4",
				"EXDATE;TZID=" + tzid + ":20260608T100000,20260615T100000",
			})
		}
		parsed := mustParse(t, listed)
		occurrences, err := Occurrences(parsed,
			time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		// Four weekly, less the two excluded.
		if len(occurrences) != 2 {
			var when []string
			for _, occurrence := range occurrences {
				when = append(when, occurrence.StartsAt.Format("01-02"))
			}
			t.Errorf("%s: two left, got %d (%s)", what, len(occurrences), strings.Join(when, " "))
		}
	}
}

// A zone whose name this machine maps to the wrong place is not renamed.
//
// The table maps names to zones, and a government moving its clocks makes an
// entry wrong years after it was written. Once renamed the file's own
// description is never consulted again, so a stale entry would silently move
// every occurrence with nothing to show for it.
func TestAZoneIsNotRenamedWhenTheFileDisagrees(t *testing.T) {
	// A file claiming six hours ahead under a name this table maps to a
	// zone that keeps a different offset.
	written := crlf(
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Microsoft Exchange//EN",
		"BEGIN:VTIMEZONE", "TZID:Not A Real Zone Name",
		"BEGIN:STANDARD", "DTSTART:16010101T000000", "TZOFFSETFROM:+0600", "TZOFFSETTO:+0600",
		"END:STANDARD", "END:VTIMEZONE",
		"BEGIN:VEVENT", "UID:disagrees", "DTSTAMP:20260912T120000Z",
		"DTSTART;TZID=Not A Real Zone Name:20260701T090000",
		"DTEND;TZID=Not A Real Zone Name:20260701T100000", "SUMMARY:Somewhere",
		"END:VEVENT", "END:VCALENDAR")
	parsed := mustParse(t, written)
	// Nine in the morning, six hours ahead, is three o'clock UTC -- taken
	// from what the file says rather than from a guess at the name.
	if got := parsed.StartsAt.Format("15:04Z"); got != "03:00Z" {
		t.Fatalf("the file's own offset: %s", got)
	}
}

// Renaming never produces two zones under one name.
//
// A file may carry the same zone twice under both spellings. Renaming one onto
// the other leaves two components claiming one identifier, which the format
// forbids and strict clients refuse -- and this file is stored and served back
// to phones.
func TestRenamingDoesNotCollideWithAZoneAlreadyThere(t *testing.T) {
	if _, err := time.LoadLocation("America/New_York"); err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	eastern := []string{
		"BEGIN:STANDARD", "DTSTART:16011101T020000", "TZOFFSETFROM:-0400", "TZOFFSETTO:-0500", "END:STANDARD",
		"BEGIN:DAYLIGHT", "DTSTART:16010308T020000", "TZOFFSETFROM:-0500", "TZOFFSETTO:-0400", "END:DAYLIGHT",
	}
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//x//EN",
		"BEGIN:VTIMEZONE", "TZID:Eastern Standard Time"}
	lines = append(lines, eastern...)
	lines = append(lines, "END:VTIMEZONE", "BEGIN:VTIMEZONE", "TZID:America/New_York")
	lines = append(lines, eastern...)
	lines = append(lines, "END:VTIMEZONE",
		"BEGIN:VEVENT", "UID:twice", "DTSTAMP:20260912T120000Z",
		"DTSTART;TZID=America/New_York:20260701T090000",
		"DTEND;TZID=America/New_York:20260701T100000", "SUMMARY:Twice",
		"END:VEVENT", "END:VCALENDAR")
	parsed := mustParse(t, crlf(lines...))
	if got := strings.Count(string(Unfold(parsed.Data)), "TZID:America/New_York"); got != 1 {
		t.Fatalf("one zone under that name, not %d:\n%s", got, parsed.Data)
	}

	// And the event that names the taken-over zone keeps its own clock.
	// Refusing the rename outright dropped it into fixed offsets, so a
	// weekly nine o'clock in New York became eight o'clock the moment the
	// clocks went back -- out of a file that described the zone correctly,
	// twice over.
	lines = lines[:len(lines)-8]
	lines = append(lines,
		"BEGIN:VEVENT", "UID:weekly", "DTSTAMP:20261001T120000Z",
		"DTSTART;TZID=Eastern Standard Time:20261026T090000",
		"DTEND;TZID=Eastern Standard Time:20261026T100000",
		"RRULE:FREQ=WEEKLY;COUNT=4", "SUMMARY:Nine in New York",
		"END:VEVENT", "END:VCALENDAR")
	parsed = mustParse(t, crlf(lines...))
	occurrences, err := Occurrences(parsed,
		time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("expand: %s", err)
	}
	where, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("this machine has no zone database: %v", err)
	}
	if len(occurrences) != 4 {
		t.Fatalf("four of them: %d", len(occurrences))
	}
	for _, occurrence := range occurrences {
		if hour := occurrence.StartsAt.In(where).Hour(); hour != 9 {
			t.Fatalf("nine o'clock stays nine o'clock, not %d: %s",
				hour, occurrence.StartsAt)
		}
	}
}

// TestTheHorizonIsWhereTheIndexActuallyStops is the repeat too fine to work
// out over the whole stretch. Saying it was indexed to the horizon when the
// list filled up five months in meant nothing ever extended it, so the event
// stopped appearing while every fetch of it still worked -- which is the exact
// failure the horizon column was added to abolish.
func TestTheHorizonIsWhereTheIndexActuallyStops(t *testing.T) {
	start := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Hour)
	text := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nBEGIN:VEVENT\r\n" +
		"UID:hourly\r\nDTSTAMP:20260912T120000Z\r\n" +
		"DTSTART:" + start.Format("20060102T150405Z") + "\r\n" +
		"DTEND:" + start.Add(30*time.Minute).Format("20060102T150405Z") + "\r\n" +
		"SUMMARY:On the hour\r\nRRULE:FREQ=HOURLY\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	parsed, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	occurrences, until, err := Indexed(parsed)
	if err != nil {
		t.Fatalf("index: %s", err)
	}
	if len(occurrences) != MaximumOccurrences {
		t.Fatalf("this repeat fills the list: %d", len(occurrences))
	}
	last := occurrences[len(occurrences)-1].StartsAt
	if !until.Equal(last) {
		t.Fatalf("the horizon is the last moment written down, not %s past it: %s vs %s",
			until.Sub(last), until, last)
	}
	// And the cap is spent on what is ahead. Worked out from a year back it
	// filled up with last spring, so the event was nowhere in today's
	// calendar.
	if !occurrences[0].StartsAt.After(time.Now().UTC().Add(-time.Hour)) {
		t.Fatalf("a repeat that cannot be indexed in full is indexed forwards: %s", occurrences[0].StartsAt)
	}
}

// A series that ends exactly on the cap has not been cut short, and its past
// is not thrown away.
//
// Told otherwise, a finished series lost every occurrence before today from
// the index -- so free-busy for last month and the agenda behind it went
// blank -- and recorded a horizon it had already passed, which put it at the
// head of the re-index queue for ever.
func TestAFinishedSeriesIsNotCutShort(t *testing.T) {
	start := time.Now().UTC().Add(-100 * 24 * time.Hour).Truncate(time.Hour)
	text := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nBEGIN:VEVENT\r\n" +
		"UID:finite\r\nDTSTAMP:20260912T120000Z\r\n" +
		"DTSTART:" + start.Format("20060102T150405Z") + "\r\n" +
		"DTEND:" + start.Add(30*time.Minute).Format("20060102T150405Z") + "\r\n" +
		"SUMMARY:Hourly, and then done\r\nRRULE:FREQ=HOURLY;COUNT=" +
		fmt.Sprint(MaximumOccurrences) + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	parsed, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	occurrences, until, err := Indexed(parsed)
	if err != nil {
		t.Fatalf("index: %s", err)
	}
	if len(occurrences) != MaximumOccurrences {
		t.Fatalf("every one of them: %d", len(occurrences))
	}
	if !occurrences[0].StartsAt.Before(time.Now().UTC()) {
		t.Fatalf("including the ones that have already happened: %s", occurrences[0].StartsAt)
	}
	if !until.After(time.Now().UTC().Add(HorizonAhead - time.Hour)) {
		t.Fatalf("worked out to the horizon, not to %s", until)
	}
}

// A repeat too fine to walk as far as today says so, rather than handing back
// a list of last spring with a horizon already behind it -- which nothing
// could ever extend, so it was walked again, twice, every hour for ever.
func TestARepeatThatCannotReachTodaySaysSo(t *testing.T) {
	start := time.Now().UTC().AddDate(-5, 0, 0).Truncate(time.Hour)
	text := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nBEGIN:VEVENT\r\n" +
		"UID:ancient\r\nDTSTAMP:20260912T120000Z\r\n" +
		"DTSTART:" + start.Format("20060102T150405Z") + "\r\n" +
		"DTEND:" + start.Add(time.Minute).Format("20060102T150405Z") + "\r\n" +
		"SUMMARY:Every minute since 2021\r\nRRULE:FREQ=MINUTELY\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	parsed, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	occurrences, until, err := Indexed(parsed)
	if err == nil {
		t.Fatalf("this cannot be indexed and should say so: %d occurrences, horizon %s, first %s",
			len(occurrences), until, occurrences[0].StartsAt)
	}
}
