package calendar

import "time"

// FreeStretch is a stretch of one day nobody is busy in.
type FreeStretch struct {
	// Day is midnight of the day it is on, in the zone the days were counted
	// in.
	Day      time.Time
	StartsAt time.Time
	EndsAt   time.Time
}

// FreeByDay is when everybody whose busy times are given is free, day by day,
// between the earliest and the latest hour of each day.
//
// Day by day, because "when am I free" means free during a day rather than
// free at three in the morning. The busy times may be one person's or the
// union of several people's: they are merged here, so two people's meetings
// that overlap are one stretch, and what is left is when all of them could
// meet. The command line and the agent both answer through this, so that the
// time one offers is the time the other offers, from FreeBusy and Free, the
// same two functions a phone's free-busy request is answered with.
//
// The first day is the one containing from, counted in from's own zone, and
// days run until the one before until. Callers have already left out what does
// not make anybody busy (a whole day, something called off); MakesBusy says
// which those are.
func FreeByDay(busy []Occurrence, from, until time.Time, earliest, latest time.Duration) []FreeStretch {
	var free []FreeStretch
	for day := from; day.Before(until); day = day.AddDate(0, 0, 1) {
		opens, closes := day.Add(earliest), day.Add(latest)
		for _, gap := range Free(FreeBusy(busy, opens, closes), opens, closes) {
			free = append(free, FreeStretch{Day: day, StartsAt: gap.StartsAt, EndsAt: gap.EndsAt})
		}
	}
	return free
}
