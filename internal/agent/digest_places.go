package agent

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The places section of a digest: where the person spent the month, by
// arithmetic over their visits. The figures are rounded and the times of day
// are ranges, so that a month written up again later says the same things
// about a routine instead of changing a number by a minute.

const (
	// digestPlacesMost is how many places without a label are listed, the
	// ones with the most hours.
	digestPlacesMost = 10

	// digestVisitsMost is how many visits one digest reads.
	digestVisitsMost = 5000

	// digestRangeStepMinutes is what the ends of a usual-hours range are
	// rounded outward to.
	digestRangeStepMinutes = 30
)

// listVisitsBetween is the person's visits that touch the stretch of time,
// every page of them up to digestVisitsMost.
func listVisitsBetween(tx db.Transaction, userId string, from, until time.Time) ([]*models.PlaceVisit, error) {
	var visits []*models.PlaceVisit
	for offset := 0; len(visits) < digestVisitsMost; {
		page, nextOffset, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{From: &from, Until: &until, Offset: offset, LimitCount: 200})
		if err != nil {
			return nil, err
		}
		visits = append(visits, page...)
		if nextOffset == 0 {
			break
		}
		offset = nextOffset
	}
	return visits, nil
}

// digestPlaceTotals is what is added up for one place, or for every place
// with one label.
type digestPlaceTotals struct {
	name       string
	placeIds   map[string]bool
	visitCount int
	duration   time.Duration
	startedAt  []time.Time
}

// placesTheyWent is the lines of the section, fenced as record text is because
// the place names in them were written by strangers, or empty when the stretch
// has no visits. A visit is counted in the stretch it began in, and its hours
// are cut to the stretch. A visit still in progress lasts until now, unless a
// later visit began, which ended it. Places with a label come first, one line
// for each label, then the places with no label that took the most hours.
func placesTheyWent(visits []*models.PlaceVisit, from, until, now time.Time, zone *time.Location) string {
	byLabel := map[models.PlaceLabel]*digestPlaceTotals{}
	byPlace := map[string]*digestPlaceTotals{}
	laterVisitStarts := openVisitEnds(visits)
	for _, visit := range visits {
		place := visit.Place
		if place == nil {
			continue
		}
		var startedAt time.Time
		switch {
		case visit.ArrivedAt != nil:
			startedAt = *visit.ArrivedAt
		case visit.DepartedAt != nil:
			startedAt = *visit.DepartedAt
		default:
			continue
		}
		endedAt := startedAt
		laterVisitStart, isFollowed := laterVisitStarts[visit.ID]
		switch {
		case visit.ArrivedAt == nil:
		case visit.DepartedAt != nil:
			endedAt = *visit.DepartedAt
		case isFollowed && laterVisitStart.Before(now):
			endedAt = laterVisitStart
		case now.After(startedAt):
			endedAt = now
		}
		clippedStart, clippedEnd := startedAt, endedAt
		if clippedStart.Before(from) {
			clippedStart = from
		}
		if clippedEnd.After(until) {
			clippedEnd = until
		}
		var totals *digestPlaceTotals
		if place.PlaceLabel != "" {
			if totals = byLabel[place.PlaceLabel]; totals == nil {
				totals = &digestPlaceTotals{placeIds: map[string]bool{}}
				byLabel[place.PlaceLabel] = totals
			}
		} else {
			if totals = byPlace[place.ID]; totals == nil {
				totals = &digestPlaceTotals{placeIds: map[string]bool{}}
				byPlace[place.ID] = totals
			}
		}
		if totals.name == "" || place.PlaceLabel == "" {
			totals.name = placeTextForModel(place.PlaceName)
		}
		totals.placeIds[place.ID] = true
		if !startedAt.Before(from) && startedAt.Before(until) {
			totals.visitCount++
			totals.startedAt = append(totals.startedAt, startedAt.In(zone))
		}
		if clippedEnd.After(clippedStart) {
			totals.duration += clippedEnd.Sub(clippedStart)
		}
	}

	var lines []string
	for _, label := range models.PlaceLabels {
		totals := byLabel[label]
		if totals == nil {
			continue
		}
		subject := string(label)
		switch {
		case len(totals.placeIds) > 1:
			subject += fmt.Sprintf(" (%d places)", len(totals.placeIds))
		case totals.name != "":
			subject += " (" + totals.name + ")"
		}
		lines = append(lines, digestPlaceLine(subject, totals))
	}
	unlabeled := make([]*digestPlaceTotals, 0, len(byPlace))
	for _, totals := range byPlace {
		unlabeled = append(unlabeled, totals)
	}
	sort.SliceStable(unlabeled, func(left, right int) bool {
		if unlabeled[left].duration != unlabeled[right].duration {
			return unlabeled[left].duration > unlabeled[right].duration
		}
		return unlabeled[left].name < unlabeled[right].name
	})
	for index, totals := range unlabeled {
		if index == digestPlacesMost {
			break
		}
		name := totals.name
		if name == "" {
			name = "an unnamed place"
		}
		lines = append(lines, digestPlaceLine(name, totals))
	}
	if len(lines) == 0 {
		return ""
	}
	return fenced(strings.Join(lines, "\n"))
}

// digestPlaceLine is one line: "- work (Harbor Office): 18 visits, 142 h,
// Monday to Friday, mornings 08:30 to 09:30".
func digestPlaceLine(subject string, totals *digestPlaceTotals) string {
	parts := []string{
		"- " + subject + ": " + plural(totals.visitCount, "1 visit", "%d visits"),
		digestHoursText(totals.duration),
	}
	if weekdays := digestWeekdaysText(totals.startedAt); weekdays != "" {
		parts = append(parts, weekdays)
	}
	if ranges := digestUsualHoursText(totals.startedAt); ranges != "" {
		parts = append(parts, ranges)
	}
	return strings.Join(parts, ", ")
}

// digestHoursText is a total of hours: whole hours from ten up, otherwise one
// decimal.
func digestHoursText(duration time.Duration) string {
	hours := duration.Hours()
	if hours >= 10 {
		return fmt.Sprintf("%d h", int(math.Round(hours)))
	}
	return fmt.Sprintf("%.1f h", hours)
}

// digestWeekdaysText names the days of the week the visits began on, those
// with at least a tenth of the visits so that one odd day is not part of the
// routine, from Monday: "Tuesday, Thursday". Every day is said as "every
// day".
func digestWeekdaysText(startedAt []time.Time) string {
	if len(startedAt) == 0 {
		return ""
	}
	var countByWeekday [7]int
	for _, moment := range startedAt {
		countByWeekday[moment.Weekday()]++
	}
	leastCount := max(1, (len(startedAt)+9)/10)
	var names []string
	for offset := 0; offset < 7; offset++ {
		weekday := time.Weekday((offset + 1) % 7)
		if countByWeekday[weekday] >= leastCount {
			names = append(names, weekday.String())
		}
	}
	if len(names) == 7 {
		return "every day"
	}
	return strings.Join(names, ", ")
}

// digestPeriods are the parts of the day the usual hours are said in, by the
// hour a visit began.
var digestPeriods = []struct {
	name                string
	fromHour, untilHour int
}{
	{"mornings", 5, 12}, {"afternoons", 12, 17}, {"evenings", 17, 22}, {"nights", 22, 29},
}

// digestUsualHoursText says when in the day the visits began, as one range
// for each part of the day that holds at least a quarter of them: from the
// tenth to the ninetieth percentile of the time of day, rounded outward to
// the half hour. "mornings 07:00 to 08:30, evenings 17:30 to 18:30".
func digestUsualHoursText(startedAt []time.Time) string {
	if len(startedAt) == 0 {
		return ""
	}
	minutesByPeriod := make([][]int, len(digestPeriods))
	for _, moment := range startedAt {
		minute := moment.Hour()*60 + moment.Minute()
		// The small hours belong to the night before.
		if moment.Hour() < digestPeriods[0].fromHour {
			minute += 24 * 60
		}
		for index, period := range digestPeriods {
			if minute >= period.fromHour*60 && minute < period.untilHour*60 {
				minutesByPeriod[index] = append(minutesByPeriod[index], minute)
				break
			}
		}
	}
	leastCount := max(1, (len(startedAt)+3)/4)
	var ranges []string
	for index, minutes := range minutesByPeriod {
		if len(minutes) < leastCount {
			continue
		}
		sort.Ints(minutes)
		low := minutes[int(math.Floor(0.1*float64(len(minutes)-1)))]
		high := minutes[int(math.Ceil(0.9*float64(len(minutes)-1)))]
		low = low / digestRangeStepMinutes * digestRangeStepMinutes
		high = (high + digestRangeStepMinutes - 1) / digestRangeStepMinutes * digestRangeStepMinutes
		// One usual time is still a range, a step wide.
		if high == low {
			high += digestRangeStepMinutes
		}
		ranges = append(ranges, fmt.Sprintf("%s %s to %s", digestPeriods[index].name, digestClockText(low), digestClockText(high)))
	}
	return strings.Join(ranges, ", ")
}

// digestClockText is minutes since midnight as 07:30, wrapping past 24:00.
func digestClockText(minute int) string {
	minute %= 24 * 60
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}
