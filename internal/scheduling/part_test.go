package scheduling

import (
	"encoding/base64"
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

// Nearly every real invitation travels encoded: an iCalendar file has long
// lines and names that are not ASCII, so a calendar program writes it as
// base64 and a mail program that folds it writes quoted-printable. Handing
// the parser the bytes as they travelled meant handing it a wall of base64,
// which reads as no calendar at all -- so most real invitations were
// silently not invitations.
func TestAnInvitationIsDecodedBeforeItIsRead(t *testing.T) {
	calendar := "BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nSUMMARY:Planning = strategy\r\nEND:VCALENDAR\r\n"

	base64Body := []byte("--edge\r\nContent-Type: text/plain\r\n\r\nPlease come\r\n" +
		"--edge\r\nContent-Type: text/calendar; method=REQUEST; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString([]byte(calendar)) + "\r\n--edge--\r\n")
	quotedBody := []byte("--edge\r\nContent-Type: text/plain\r\n\r\nPlease come\r\n" +
		"--edge\r\nContent-Type: text/calendar; method=REQUEST; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
		"BEGIN:VCALENDAR=0D=0AMETHOD:REQUEST=0D=0ASUMMARY:Planning =3D strategy=0D=0AEND:VCALENDAR=0D=0A" +
		"\r\n--edge--\r\n")

	headers := headersOf("Content-Type: multipart/alternative; boundary=\"edge\"", "MIME-Version: 1.0")
	for name, body := range map[string][]byte{"base64": base64Body, "quoted-printable": quotedBody} {
		found := string(CalendarPart(headers, body))
		if !strings.Contains(found, "METHOD:REQUEST") || !strings.Contains(found, "Planning = strategy") {
			t.Errorf("%s: %q", name, found)
		}
	}
}

// And the ceiling still holds, on what the part decodes to rather than on
// what arrived: base64 is four bytes for every three.
func TestAHugeEncodedCalendarPartIsStillNotRead(t *testing.T) {
	huge := "BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nDESCRIPTION:" + strings.Repeat("x", maximumPart) + "\r\nEND:VCALENDAR\r\n"
	headers := headersOf("Content-Type: multipart/alternative; boundary=\"edge\"", "MIME-Version: 1.0")
	body := []byte("--edge\r\nContent-Type: text/calendar; method=REQUEST\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString([]byte(huge)) + "\r\n--edge--\r\n")
	if found := CalendarPart(headers, body); found != nil {
		t.Fatalf("a calendar larger than the ceiling is not read: %d bytes", len(found))
	}
}
