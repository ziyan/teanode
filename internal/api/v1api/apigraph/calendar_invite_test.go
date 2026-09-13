package apigraph

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/calendar"
)

// An event carries its guest list with it, and the list is not always its
// owner's work. Saving one that arrived by mail must not send an invitation
// to everybody on somebody else's list, from this person's address and signed
// by their domain.
func TestOnlyTheOrganizerInvitesAnybody(t *testing.T) {
	t.Parallel()

	invitation := func(organizer string, guests ...string) *calendar.Parsed {
		t.Helper()
		lines := []string{
			"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//EN",
			"BEGIN:VEVENT", "UID:the-meeting", "DTSTAMP:20260912T120000Z",
			"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z", "SUMMARY:A meeting",
		}
		if organizer != "" {
			lines = append(lines, "ORGANIZER:mailto:"+organizer)
		}
		for _, guest := range guests {
			lines = append(lines, "ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:"+guest)
		}
		lines = append(lines, "END:VEVENT", "END:VCALENDAR")
		parsed, err := calendar.Parse([]byte(strings.Join(lines, "\r\n") + "\r\n"))
		if err != nil {
			t.Fatalf("parse: %s", err)
		}
		return parsed
	}

	// Somebody else's meeting, saved by the person it was sent to.
	theirs := invitation("mallory@evil.example", "alice@example.com", "victim@example.org")
	asked, err := guestsToInvite(theirs, nil, "alice@example.com")
	if err != nil || len(asked) != 0 {
		t.Fatalf("saving somebody else's event invites nobody: %v %v", asked, err)
	}

	// Her own, which is what inviting people is for.
	hers := invitation("alice@example.com", "bob@example.com", "carol@example.com")
	asked, err = guestsToInvite(hers, nil, "alice@example.com")
	if err != nil || len(asked) != 2 {
		t.Fatalf("her own event invites the people on it: %v %v", asked, err)
	}

	// An event made here names nobody until it is sent.
	fresh := invitation("", "bob@example.com")
	asked, err = guestsToInvite(fresh, nil, "alice@example.com")
	if err != nil || len(asked) != 1 {
		t.Fatalf("an event naming no organizer is this person's own: %v %v", asked, err)
	}

	// And a guest list has an end.
	guests := make([]string, 0, calendar.MaximumGuests+1)
	for index := 0; index <= calendar.MaximumGuests; index++ {
		guests = append(guests, "guest"+string(rune('a'+index%26))+string(rune('a'+index/26))+"@example.org")
	}
	crowd := invitation("alice@example.com", guests...)
	if _, err := guestsToInvite(crowd, nil, "alice@example.com"); err == nil {
		t.Fatal("more people than this server invites at once is refused")
	}
}
