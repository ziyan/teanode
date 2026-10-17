package calendar

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// eventObject is one stored event: a dentist visit with everything an owner puts on
// one, so that each level has something to leave out.
func eventObject(class string) models.CalendarObject {
	data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nX-WR-CALNAME:Private name\r\n" +
		"BEGIN:VEVENT\r\nUID:visit-1\r\nDTSTAMP:20260912T120000Z\r\nSEQUENCE:2\r\n" +
		"DTSTART:20260914T100000Z\r\nDTEND:20260914T110000Z\r\n" +
		"RRULE:FREQ=WEEKLY;COUNT=3\r\nEXDATE:20260921T100000Z\r\nTRANSP:OPAQUE\r\nSTATUS:CONFIRMED\r\n" +
		"SUMMARY:Dentist\r\nLOCATION:12 Elm Street\r\nDESCRIPTION:Bring the insurance form\r\n" +
		"ORGANIZER:mailto:owner@example.com\r\nATTENDEE;CN=Guest:mailto:guest@example.com\r\n" +
		"URL:https://example.com/visit\r\nX-PRIVATE-NOTE:secret\r\n"
	if class != "" {
		data += "CLASS:" + class + "\r\n"
	}
	data += "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nDESCRIPTION:Dentist\r\nEND:VALARM\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	return models.CalendarObject{
		ID: "visit-1.ics", CalendarID: "calendar-1", UID: "visit-1", ETag: "stored", Data: data,
		Summary: "Dentist", Location: "12 Elm Street",
		StartsAt: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC),
		Recurring: true,
	}
}

func parseRedacted(t *testing.T, data string) *Parsed {
	t.Helper()
	parsed, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("what Redact wrote cannot be read back: %s\n%s", err, data)
	}
	return parsed
}

// Each level shows exactly what it promises, and what it gives can be read
// back as an event with the same times and repeats.
func TestRedactShowsWhatTheLevelAllows(t *testing.T) {
	t.Parallel()
	stored := eventObject("")

	cases := []struct {
		level     models.ShareLevel
		kept      []string
		left      []string
		summary   string
		isChanged bool
	}{
		{models.ShareLevelOwner, []string{"Bring the insurance form", "VALARM", "X-PRIVATE-NOTE", "guest@example.com"}, nil, "Dentist", false},
		{models.ShareLevelEdit, []string{"Bring the insurance form", "VALARM", "X-PRIVATE-NOTE", "guest@example.com"}, nil, "Dentist", false},
		{models.ShareLevelDetails, []string{"Bring the insurance form", "guest@example.com", "X-PRIVATE-NOTE", "owner@example.com"}, []string{"VALARM"}, "Dentist", true},
		{models.ShareLevelTitles, []string{"SUMMARY:Dentist", "LOCATION:12 Elm Street", "RRULE:FREQ=WEEKLY;COUNT=3", "EXDATE", "DTSTART:20260914T100000Z"},
			[]string{"Bring the insurance form", "guest@example.com", "owner@example.com", "VALARM", "X-PRIVATE-NOTE", "URL", "Private name"}, "Dentist", true},
		{models.ShareLevelBusy, []string{"SUMMARY:Busy", "RRULE:FREQ=WEEKLY;COUNT=3", "EXDATE", "DTSTART:20260914T100000Z", "DTEND:20260914T110000Z", "TRANSP:OPAQUE"},
			[]string{"Dentist", "12 Elm Street", "Bring the insurance form", "guest@example.com", "owner@example.com", "VALARM", "X-PRIVATE-NOTE", "Private name"}, "Busy", true},
	}
	for _, each := range cases {
		redacted := Redact(stored, each.level)
		for _, wanted := range each.kept {
			if !strings.Contains(redacted.Data, wanted) {
				t.Errorf("%s keeps %q:\n%s", each.level, wanted, redacted.Data)
			}
		}
		for _, unwanted := range each.left {
			if strings.Contains(redacted.Data, unwanted) {
				t.Errorf("%s leaves out %q:\n%s", each.level, unwanted, redacted.Data)
			}
		}
		if each.level != models.ShareLevelBusy && !strings.Contains(redacted.Data, "SUMMARY:Dentist") {
			t.Errorf("%s: the title in the file is the event's own:\n%s", each.level, redacted.Data)
		}
		if redacted.Summary != each.summary {
			t.Errorf("%s: the title column is %q", each.level, redacted.Summary)
		}
		if each.level == models.ShareLevelBusy && redacted.Location != "" {
			t.Errorf("busy: the location column is %q", redacted.Location)
		}
		if each.isChanged == (redacted.ETag == stored.ETag) {
			t.Errorf("%s: the version names the bytes given (%q against %q)", each.level, redacted.ETag, stored.ETag)
		}
		if redacted.ETag != stored.ETag && redacted.ETag != ETag([]byte(redacted.Data)) {
			t.Errorf("%s: the version is taken over what is given", each.level)
		}
		parsed := parseRedacted(t, redacted.Data)
		// Only a reduced event says it is one, so that "Busy" the label is
		// not mistaken for an event somebody called Busy.
		if parsed.IsBusyOnly != (each.level == models.ShareLevelBusy) {
			t.Errorf("%s: marked as busy only is %v", each.level, parsed.IsBusyOnly)
		}
		if !parsed.StartsAt.Equal(stored.StartsAt) || !parsed.EndsAt.Equal(stored.EndsAt) || !parsed.Recurring {
			t.Errorf("%s: the times and the repeat survive: %+v", each.level, parsed)
		}
		occurrences, err := Occurrences(parsed, stored.StartsAt.Add(-time.Hour), stored.StartsAt.AddDate(0, 0, 30))
		if err != nil || len(occurrences) != 2 {
			t.Errorf("%s: the repeat still works, less the excluded day: %d %v", each.level, len(occurrences), err)
		}
	}
	if stored.Data != eventObject("").Data {
		t.Errorf("the stored event is not changed by redacting it")
	}
}

// An event its owner marked private is busy to everybody else at every level,
// and is left alone for the owner and for an editor of the owner's own.
func TestAPrivateEventIsBusyAtEveryLevel(t *testing.T) {
	t.Parallel()
	for _, class := range []string{"PRIVATE", "CONFIDENTIAL", "private"} {
		stored := eventObject(class)
		for _, level := range []models.ShareLevel{models.ShareLevelBusy, models.ShareLevelTitles, models.ShareLevelDetails} {
			redacted := Redact(stored, level)
			if redacted.Summary != "Busy" || strings.Contains(redacted.Data, "Dentist") ||
				strings.Contains(redacted.Data, "Elm Street") || strings.Contains(redacted.Data, "guest@example.com") ||
				strings.Contains(redacted.Data, "CLASS") {
				t.Errorf("%s at %s is busy and nothing else:\n%s", class, level, redacted.Data)
			}
			if !strings.Contains(redacted.Data, "DTSTART:20260914T100000Z") {
				t.Errorf("%s at %s still says when", class, level)
			}
		}
		for _, level := range []models.ShareLevel{models.ShareLevelOwner, models.ShareLevelEdit} {
			if redacted := Redact(stored, level); redacted.Data != stored.Data {
				t.Errorf("%s at %s is the event as it is", class, level)
			}
		}
	}
	// And a public one is not made private by the rule.
	if redacted := Redact(eventObject("PUBLIC"), models.ShareLevelTitles); redacted.Summary != "Dentist" {
		t.Errorf("a public event keeps its title: %q", redacted.Summary)
	}
}

// One private occurrence moved out of a series makes the whole file busy.
func TestOnePrivateEventInAFileMakesTheFileBusy(t *testing.T) {
	t.Parallel()
	stored := eventObject("")
	stored.Data = strings.Replace(stored.Data, "END:VCALENDAR\r\n",
		"BEGIN:VEVENT\r\nUID:visit-1\r\nDTSTAMP:20260912T120000Z\r\nRECURRENCE-ID:20260928T100000Z\r\n"+
			"DTSTART:20260929T100000Z\r\nDTEND:20260929T110000Z\r\nSUMMARY:Surprise party\r\nCLASS:PRIVATE\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n", 1)
	redacted := Redact(stored, models.ShareLevelDetails)
	if strings.Contains(redacted.Data, "Surprise") || strings.Contains(redacted.Data, "Dentist") || redacted.Summary != "Busy" {
		t.Errorf("the file is busy:\n%s", redacted.Data)
	}
	if !strings.Contains(redacted.Data, "RECURRENCE-ID:20260928T100000Z") || !strings.Contains(redacted.Data, "DTSTART:20260929T100000Z") {
		t.Errorf("the moved occurrence still says when:\n%s", redacted.Data)
	}
}

// A file this server cannot read is busy, from the columns beside it.
func TestAnUnreadableFileIsBusyFromItsColumns(t *testing.T) {
	t.Parallel()
	stored := eventObject("")
	stored.Data = "this is not a calendar, but Dentist is in it"
	redacted := Redact(stored, models.ShareLevelDetails)
	if strings.Contains(redacted.Data, "Dentist") || !strings.Contains(redacted.Data, "DTSTART:20260914T100000Z") {
		t.Fatalf("busy from the columns:\n%s", redacted.Data)
	}
	if parseRedacted(t, redacted.Data).UID != "visit-1" {
		t.Errorf("it keeps the identifier")
	}
}

// The private rule is beside the four judgements and changes nothing about
// whether somebody is busy: their own free-busy is the same with or without it.
func TestPrivateDoesNotChangeWhoIsBusy(t *testing.T) {
	t.Parallel()
	for _, class := range []string{"", "PRIVATE"} {
		parsed := parseRedacted(t, eventObject(class).Data)
		if IsPrivate(parsed) != (class != "") {
			t.Errorf("%q: IsPrivate is %v", class, IsPrivate(parsed))
		}
		if !MakesBusy(parsed, map[string]bool{}) {
			t.Errorf("%q: an opaque private event still makes its owner busy", class)
		}
	}
	if IsPrivateClass("PUBLIC") || IsPrivateClass("") || !IsPrivateClass(" confidential ") {
		t.Errorf("IsPrivateClass reads PRIVATE and CONFIDENTIAL in any case")
	}
}

// Two people's busy times are taken out of the day together, and one person
// asking alone gets the answer the single-person path always gave.
func TestFreeByDayUsesEveryonesBusyTimes(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	ada := []Occurrence{{StartsAt: dayAt(10, 0), EndsAt: dayAt(11, 0)}}
	ben := []Occurrence{{StartsAt: dayAt(10, 30), EndsAt: dayAt(12, 0)}, {StartsAt: dayAt(15, 0), EndsAt: dayAt(16, 0)}}

	describe := func(stretches []FreeStretch) string {
		said := make([]string, 0, len(stretches))
		for _, stretch := range stretches {
			said = append(said, stretch.Day.Format("02")+" "+stretch.StartsAt.Format("15:04")+"-"+stretch.EndsAt.Format("15:04"))
		}
		return strings.Join(said, ", ")
	}
	together := describe(FreeByDay(append(append([]Occurrence{}, ada...), ben...), day, day.AddDate(0, 0, 2), 9*time.Hour, 17*time.Hour))
	if want := "14 09:00-10:00, 14 12:00-15:00, 14 16:00-17:00, 15 09:00-17:00"; together != want {
		t.Errorf("together:\n got %s\nwant %s", together, want)
	}
	alone := describe(FreeByDay(ada, day, day.AddDate(0, 0, 1), 9*time.Hour, 17*time.Hour))
	if want := "14 09:00-10:00, 14 11:00-17:00"; alone != want {
		t.Errorf("alone:\n got %s\nwant %s", alone, want)
	}
}
