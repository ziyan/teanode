package scheduling

import (
	"strings"
	"testing"
)

func headersOf(lines ...string) []string { return lines }

// A message with the calendar as its own part, which is what a calendar
// program sends.
func TestACalendarPartIsFound(t *testing.T) {
	headers := headersOf(
		"Content-Type: multipart/alternative; boundary=\"edge\"",
		"MIME-Version: 1.0")
	body := []byte("--edge\r\nContent-Type: text/plain\r\n\r\nPlease come\r\n" +
		"--edge\r\nContent-Type: text/calendar; method=REQUEST; charset=utf-8\r\n\r\n" +
		"BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nEND:VCALENDAR\r\n--edge--\r\n")
	found := CalendarPart(headers, body)
	if !strings.Contains(string(found), "METHOD:REQUEST") {
		t.Fatalf("the calendar part: %q", found)
	}
}

// And as an attachment named something.ics, which is what a program that does
// not really speak the protocol sends. Somebody invited by one of those still
// wants their invitation.
func TestACalendarAttachmentIsFoundToo(t *testing.T) {
	headers := headersOf(
		"Content-Type: multipart/mixed; boundary=\"edge\"",
		"MIME-Version: 1.0")
	body := []byte("--edge\r\nContent-Type: text/plain\r\n\r\nPlease come\r\n" +
		"--edge\r\nContent-Type: application/octet-stream; name=\"meeting.ics\"\r\n" +
		"Content-Disposition: attachment; filename=\"meeting.ics\"\r\n\r\n" +
		"BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nEND:VCALENDAR\r\n--edge--\r\n")
	found := CalendarPart(headers, body)
	if !strings.Contains(string(found), "METHOD:REQUEST") {
		t.Fatalf("the attachment: %q", found)
	}
}

// An ordinary message carries nothing, and saying so is how most messages are
// dismissed without reading any further.
func TestAnOrdinaryMessageCarriesNothing(t *testing.T) {
	headers := headersOf("Content-Type: text/plain", "MIME-Version: 1.0")
	if found := CalendarPart(headers, []byte("hello\r\n")); found != nil {
		t.Fatalf("nothing to find: %q", found)
	}
	// And neither does an attachment that is not one.
	headers = headersOf("Content-Type: multipart/mixed; boundary=\"edge\"", "MIME-Version: 1.0")
	body := []byte("--edge\r\nContent-Type: text/plain\r\n\r\nhello\r\n" +
		"--edge\r\nContent-Type: application/pdf; name=\"notes.pdf\"\r\n\r\nnot a calendar\r\n--edge--\r\n")
	if found := CalendarPart(headers, body); found != nil {
		t.Fatalf("a PDF is not a calendar: %q", found)
	}
}

// A part claiming to be a calendar is not a reason to read a gigabyte into
// memory.
func TestAHugeCalendarPartIsNotRead(t *testing.T) {
	headers := headersOf("Content-Type: multipart/mixed; boundary=\"edge\"", "MIME-Version: 1.0")
	body := []byte("--edge\r\nContent-Type: text/calendar\r\n\r\n" +
		strings.Repeat("x", maximumPart+10) + "\r\n--edge--\r\n")
	if found := CalendarPart(headers, body); found != nil {
		t.Fatalf("something larger than this server keeps: %d octets", len(found))
	}
}
