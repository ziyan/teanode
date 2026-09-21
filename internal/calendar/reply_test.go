package calendar

import (
	"strings"
	"testing"
)

const invited = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nMETHOD:REQUEST\r\n" +
	"BEGIN:VEVENT\r\nUID:the-meeting\r\nDTSTAMP:20260912T120000Z\r\n" +
	"DTSTART:20260914T100000Z\r\nDTEND:20260914T110000Z\r\nSEQUENCE:3\r\n" +
	"SUMMARY:Planning\r\nLOCATION:The small room\r\n" +
	"ORGANIZER;CN=Grace Hopper:mailto:grace@example.com\r\n" +
	"ATTENDEE;CN=Ada Lovelace;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:ada@example.com\r\n" +
	"ATTENDEE;CN=Alan Turing;PARTSTAT=ACCEPTED:mailto:alan@example.com\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

// A reply carries who is answering, what they said, and enough for the
// organizer to know which event -- and nothing else.
func TestAReplyCarriesTheAnswerAndNotTheEvent(t *testing.T) {
	written, err := Reply([]byte(invited), "ada@example.com", Accepted)
	if err != nil {
		t.Fatalf("answering: %v", err)
	}
	text := string(Unfold(written))
	for _, want := range []string{
		"METHOD:REPLY",
		"UID:the-meeting",
		"SEQUENCE:3",
		"ORGANIZER;CN=Grace Hopper:mailto:grace@example.com",
		"PARTSTAT=ACCEPTED",
		"mailto:ada@example.com",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("a reply needs %q:\n%s", want, text)
		}
	}
	// Exactly one attendee. Sending the whole guest list back tells
	// everybody who accepted to everybody who answers.
	if count := strings.Count(text, "ATTENDEE"); count != 1 {
		t.Fatalf("one attendee, not %d:\n%s", count, text)
	}
	if strings.Contains(text, "alan@example.com") {
		t.Fatalf("and not the other guests:\n%s", text)
	}
	// The answer settles it, so the organizer is not still being asked to
	// remind them.
	if strings.Contains(text, "RSVP") {
		t.Fatalf("no longer waiting on an answer:\n%s", text)
	}
}

// Answering as somebody the event does not invite is refused. Otherwise
// anybody who learns an event's identifier can write to the organizer with a
// calendar file and be added to the meeting.
func TestOnlySomebodyInvitedMayAnswer(t *testing.T) {
	if _, err := Reply([]byte(invited), "stranger@example.com", Accepted); err == nil {
		t.Fatal("a stranger should not be able to answer")
	}
	if _, err := Reply([]byte(invited), "ada@example.com", "MAYBE"); err == nil {
		t.Fatal("an answer is accepted, declined or tentative")
	}
	if _, err := Reply(nil, "ada@example.com", Accepted); err == nil {
		t.Fatal("there has to be an event to answer")
	}
}

// An answer arriving changes exactly one thing on the event already held: what
// that person said. Everything the organizer wrote stays as it was, because a
// reply carries the organizer's own words echoed back and a program that
// trusted them would let an invitee rewrite the meeting by answering it.
func TestAnAnswerChangesOnlyWhatWasSaid(t *testing.T) {
	updated, err := Answer([]byte(invited), []Attendee{
		{Address: "ada@example.com", Participation: Declined},
	})
	if err != nil {
		t.Fatalf("applying an answer: %v", err)
	}
	text := string(Unfold(updated.Data))
	if !strings.Contains(text, "PARTSTAT=DECLINED") {
		t.Fatalf("she said no:\n%s", text)
	}
	if updated.Summary != "Planning" || updated.Location != "The small room" {
		t.Fatalf("the organizer's words are untouched: %q %q", updated.Summary, updated.Location)
	}
	// And the other guest's answer is not disturbed.
	if !strings.Contains(text, "CN=Alan Turing;PARTSTAT=ACCEPTED") &&
		!strings.Contains(text, "PARTSTAT=ACCEPTED;CN=Alan Turing") {
		t.Fatalf("Alan still accepted:\n%s", text)
	}
}

// An answer from somebody who was never invited is refused rather than added
// to the guest list.
func TestAnAnswerFromAStrangerIsRefused(t *testing.T) {
	if _, err := Answer([]byte(invited), []Attendee{
		{Address: "stranger@example.com", Participation: Accepted},
	}); err == nil {
		t.Fatal("a stranger cannot answer their way onto the guest list")
	}
}

// A reply says which occurrence it is about, when it is about one of them.
func TestAReplyToOneOccurrenceSaysWhichOne(t *testing.T) {
	series := strings.Replace(invited, "SEQUENCE:3\r\n",
		"SEQUENCE:3\r\nRECURRENCE-ID:20260921T100000Z\r\n", 1)
	written, err := Reply([]byte(series), "ada@example.com", Tentative)
	if err != nil {
		t.Fatalf("answering one occurrence: %v", err)
	}
	if !strings.Contains(string(Unfold(written)), "RECURRENCE-ID:20260921T100000Z") {
		t.Fatalf("which one:\n%s", written)
	}
}
