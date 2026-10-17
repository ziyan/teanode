package commands

import (
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
)

// OwnerAddresses are every address of the person whose calendar it is,
// lowercased, so that "which of these attendees is me" can be answered and an
// event they declined does not make them look busy.
//
// Every address of theirs, not just the mailbox a client signed in with: a
// calendar belongs to the account, and somebody with two mailboxes who
// declined a meeting under the other one would be reported busy for a meeting
// they had said no to.
func OwnerAddresses(transaction db.Transaction, ownerUserId string) (map[string]bool, error) {
	theirs := make(map[string]bool)
	if ownerUserId == "" {
		return theirs, nil
	}
	mailboxes, err := transaction.ListMailboxes(ownerUserId)
	if err != nil {
		return theirs, err
	}
	for _, mailbox := range mailboxes {
		for _, address := range mailbox.Addresses {
			theirs[strings.ToLower(strings.TrimSpace(address.Address))] = true
		}
	}
	return theirs, nil
}

// BusyOccurrences are the times in a window that make the owner of an events
// calendar busy: the occurrences of the events that do, by the four
// judgements of calendar.MakesBusy. They are not merged; calendar.FreeBusy
// does that.
//
// The one place that decides it, for the owner's own free-busy over CalDAV, for
// somebody a calendar is shared with at busy, and for working out when several
// people are free together, so that all of them give the same answer for the
// same calendar. Nothing about what any of it is leaves this function.
func BusyOccurrences(transaction db.Transaction, calendarId string, theirs map[string]bool, from, until time.Time) ([]calendar.Occurrence, error) {
	occurrences, err := transaction.ListOccurrences(calendarId, from, until)
	if err != nil {
		return nil, err
	}
	// Whether an event makes its owner busy is a property of the event, not of
	// each time it happens, so it is decided once per file however many
	// occurrences that file has in the window.
	counts := make(map[string]bool, len(occurrences))
	busy := make([]calendar.Occurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		decided, already := counts[occurrence.ObjectID]
		if !already {
			object, err := transaction.GetCalendarObject(calendarId, occurrence.ObjectID)
			if err != nil {
				return nil, err
			}
			if object == nil {
				counts[occurrence.ObjectID] = false
				continue
			}
			parsed, err := calendar.Parse([]byte(object.Data))
			if err != nil {
				// Kept text that cannot be read is this server's problem.
				// Counted as busy, because the safe mistake here is to say
				// somebody is unavailable when they are free rather than the
				// other way round: the first wastes a slot, the second books
				// over something real.
				decided = true
			} else {
				decided = calendar.MakesBusy(parsed, theirs)
			}
			counts[occurrence.ObjectID] = decided
		}
		if !decided {
			continue
		}
		busy = append(busy, calendar.Occurrence{
			StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, AllDay: occurrence.AllDay,
		})
	}
	return busy, nil
}
