package calendar

import (
	"sort"
	"strings"
	"time"
)

// Free-busy is the question "when is this person busy", answered without
// saying what they are doing. It is what a calendar shows somebody who is
// arranging a meeting, and it is the one thing in this package that is
// deliberately less informative than what it is computed from.
//
// Two things decide whether an event makes its owner busy, and neither is
// optional. An event marked transparent does not: that is what the mark
// means, and it is how a birthday or a "holiday in France" reminder stays out
// of the way. And an event the person has declined does not: they said they
// are not going.
//
// Getting either wrong is worse than having no free-busy at all, because
// somebody arranging a meeting will believe it and pick another time for
// nothing.

// Period is a stretch of time somebody is busy.
type Period struct {
	StartsAt time.Time
	EndsAt   time.Time
}

// MakesBusy reports whether an event puts its owner's time out of reach.
//
// theirs is the set of addresses belonging to the person whose calendar this
// is, lowercased, so that "which of these attendees is me" can be answered.
// An event with no attendee of theirs is one they put in their own calendar,
// and it makes them busy unless it says otherwise.
func MakesBusy(parsed *Parsed, theirs map[string]bool) bool {
	if parsed == nil {
		return false
	}
	if parsed.Transparent {
		return false
	}
	if parsed.Status == "CANCELLED" {
		return false
	}
	// An all-day event does not block the day. A birthday, a public
	// holiday or "Ada is in Berlin this week" are things people keep in a
	// calendar to know about, not appointments, and treating them as busy
	// makes a week look unbookable.
	if parsed.AllDay {
		return false
	}
	for _, attendee := range parsed.Attendees {
		if !theirs[strings.ToLower(strings.TrimSpace(attendee.Address))] {
			continue
		}
		switch attendee.Participation {
		case "DECLINED":
			return false
		default:
			return true
		}
	}
	return true
}

// FreeBusy is the stretches somebody is busy, merged.
//
// Merged, because two meetings that touch or overlap are one stretch of
// unavailability and a client showing them separately draws a gap that is not
// there. Clipped to the window, because a client asked about a window and a
// period reaching outside it says more about the person's day than they
// agreed to tell.
func FreeBusy(occurrences []Occurrence, from, until time.Time) []Period {
	periods := make([]Period, 0, len(occurrences))
	for _, occurrence := range occurrences {
		starts, ends := occurrence.StartsAt, occurrence.EndsAt
		if starts.Before(from) {
			starts = from
		}
		if ends.After(until) {
			ends = until
		}
		if !ends.After(starts) {
			continue
		}
		periods = append(periods, Period{StartsAt: starts, EndsAt: ends})
	}
	if len(periods) == 0 {
		return nil
	}
	sort.Slice(periods, func(first, second int) bool {
		if periods[first].StartsAt.Equal(periods[second].StartsAt) {
			return periods[first].EndsAt.Before(periods[second].EndsAt)
		}
		return periods[first].StartsAt.Before(periods[second].StartsAt)
	})
	merged := []Period{periods[0]}
	for _, period := range periods[1:] {
		last := &merged[len(merged)-1]
		// Touching counts as overlapping: an hour ending at ten and one
		// beginning at ten are two hours with nobody free in between.
		if !period.StartsAt.After(last.EndsAt) {
			if period.EndsAt.After(last.EndsAt) {
				last.EndsAt = period.EndsAt
			}
			continue
		}
		merged = append(merged, period)
	}
	return merged
}

// WriteFreeBusy is the answer as the format writes it: a calendar holding one
// VFREEBUSY, with a line per stretch and nothing about what any of it is.
func WriteFreeBusy(periods []Period, from, until time.Time) []byte {
	var written strings.Builder
	written.WriteString("BEGIN:VCALENDAR\r\n")
	written.WriteString("VERSION:2.0\r\n")
	written.WriteString("PRODID:" + productID + "\r\n")
	written.WriteString("BEGIN:VFREEBUSY\r\n")
	written.WriteString("DTSTAMP:" + momentText(time.Now()) + "\r\n")
	written.WriteString("DTSTART:" + momentText(from) + "\r\n")
	written.WriteString("DTEND:" + momentText(until) + "\r\n")
	for _, period := range periods {
		// BUSY, and nothing else: the format allows saying "busy
		// tentatively" or "out of office", and either would leak what
		// kind of thing is in the calendar to somebody who was told only
		// that the time is taken.
		written.WriteString("FREEBUSY;FBTYPE=BUSY:" +
			momentText(period.StartsAt) + "/" + momentText(period.EndsAt) + "\r\n")
	}
	written.WriteString("END:VFREEBUSY\r\n")
	written.WriteString("END:VCALENDAR\r\n")
	return fold([]byte(written.String()))
}

// momentText writes a time the way the format wants it, in UTC. Named like
// offsetText beside it, and not "moment", which reads as a time rather than
// as the writing of one.
func momentText(at time.Time) string {
	return at.UTC().Format("20060102T150405Z")
}
