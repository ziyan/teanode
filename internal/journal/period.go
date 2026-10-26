package journal

import (
	"errors"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// daysPageSize is how many day rows one read of a period asks for at a time.
const daysPageSize = 100

// Period is a week, a month or a year of a journal: its own row, the days in
// it that have anything in them, and what they add up to. The days' numbers
// are the ones stored (facts), so reading a year reads no source.
type Period struct {
	Kind models.JournalPeriodKind

	// Start and End are its first and last day, both included.
	Start string
	End   string

	// Today is the person's today, which no day of the period passes.
	Today string

	// Row is what the journal stores for the period, nil when nothing was ever
	// kept for it. Links are the records chosen for it.
	Row   *models.JournalPeriod
	Links []*models.JournalLink

	// Days are the period's days that have anything in them, oldest first,
	// today and before.
	Days []*models.JournalDay

	// Totals are the days' numbers added up. Places are counted once however
	// many days they were visited on; money is added only in the currency most
	// of the days spent in; the main place is the one most days were spent at.
	Totals models.JournalFacts

	// MoodCounts are how many days had each mood, awful first.
	MoodCounts [5]int

	// DayCount is how many days in it have anything in them.
	DayCount int

	// Months are a year's twelve months; nil for a week or a month.
	Months []MonthSummary
}

// MonthSummary is a month as a year shows it.
type MonthSummary struct {
	// Start is the first of the month.
	Start    string
	Headline string

	// DayMoods are the month's days in order, each 0 for no mood or 1 to 5.
	DayMoods []int

	PlaceCount int
	PhotoCount int
}

// ReadPeriod reads the period of a kind that contains a day. weekStart is
// sunday or monday, for a week. now is the moment of the reading, which
// fixes today.
func ReadPeriod(transaction db.Transaction, user *models.User, periodKind models.JournalPeriodKind, day, weekStart string, now time.Time) (*Period, error) {
	if !periodKind.IsValid() {
		return nil, errors.New("journal: a period is a week, a month or a year")
	}
	first, last, err := PeriodContaining(periodKind, day, weekStart)
	if err != nil {
		return nil, err
	}
	period := &Period{Kind: periodKind, Start: first, End: last, Today: Today(user, now)}

	row, err := transaction.GetJournalPeriod(user.ID, periodKind, first)
	switch {
	case err == nil:
		period.Row = row
		links, err := transaction.ListJournalLinks(user.ID, "", row.ID)
		switch {
		case err == nil:
			period.Links = links
		case errors.Is(err, db.ErrNotFound):
		default:
			return nil, err
		}
	case errors.Is(err, db.ErrNotFound):
	default:
		return nil, err
	}

	until := last
	if period.Today < until {
		until = period.Today
	}
	if first <= until {
		days, err := listDays(transaction, user.ID, first, until)
		if err != nil {
			return nil, err
		}
		period.Days = days
	}
	period.DayCount = len(period.Days)
	period.Totals, period.MoodCounts = addDays(period.Days)

	if periodKind == models.JournalPeriodYear {
		months, err := summarizeMonths(transaction, user.ID, first, period.Days)
		if err != nil {
			return nil, err
		}
		period.Months = months
	}
	return period, nil
}

// listDays reads every day row from one day to another, whatever the page
// size of the storage, oldest first.
func listDays(transaction db.Transaction, userId, fromDay, untilDay string) ([]*models.JournalDay, error) {
	var days []*models.JournalDay
	for offset := 0; ; {
		page, total, err := transaction.ListJournalDays(userId, fromDay, untilDay, offset, daysPageSize)
		switch {
		case err == nil:
		case errors.Is(err, db.ErrNotFound):
			return nil, nil
		default:
			return nil, err
		}
		days = append(days, page...)
		offset += len(page)
		if len(page) == 0 || offset >= total {
			break
		}
	}
	sort.SliceStable(days, func(left, right int) bool { return days[left].Day < days[right].Day })
	return days, nil
}

// addDays adds up the numbers of days and counts their moods.
func addDays(days []*models.JournalDay) (models.JournalFacts, [5]int) {
	var totals models.JournalFacts
	var moods [5]int
	places := map[string]bool{}
	mainPlaceDays := map[string]int{}
	mainPlaceNames := map[string]string{}
	currencyDays := map[string]int{}
	for _, day := range days {
		facts := day.Facts
		totals.PhotoCount += facts.PhotoCount
		totals.WalkedMeters += facts.WalkedMeters
		totals.StepCount += facts.StepCount
		totals.SleepMinutes += facts.SleepMinutes
		totals.WorkoutMinutes += facts.WorkoutMinutes
		totals.CodingSessionCount += facts.CodingSessionCount
		totals.CodingMinutes += facts.CodingMinutes
		totals.MeetingCount += facts.MeetingCount
		totals.MeetingMinutes += facts.MeetingMinutes
		totals.RecordingCount += facts.RecordingCount
		totals.ConversationCount += facts.ConversationCount
		totals.ChatMessageCount += facts.ChatMessageCount
		totals.MailReceivedCount += facts.MailReceivedCount
		totals.MailSentCount += facts.MailSentCount
		totals.DocumentCount += facts.DocumentCount
		totals.TransactionCount += facts.TransactionCount
		totals.ShoppingTripCount += facts.ShoppingTripCount
		totals.MemoryPageCount += facts.MemoryPageCount
		for _, placeId := range facts.VisitedPlaceIDs {
			places[placeId] = true
		}
		if facts.MainPlaceID != "" {
			mainPlaceDays[facts.MainPlaceID]++
			mainPlaceNames[facts.MainPlaceID] = facts.MainPlaceName
		}
		if facts.SpentCurrency != "" {
			currencyDays[facts.SpentCurrency]++
		}
		if day.MoodLevel != nil && *day.MoodLevel >= 1 && *day.MoodLevel <= 5 {
			moods[*day.MoodLevel-1]++
		}
	}
	totals.PlaceCount = len(places)
	for placeId := range places {
		totals.VisitedPlaceIDs = append(totals.VisitedPlaceIDs, placeId)
	}
	sort.Strings(totals.VisitedPlaceIDs)
	totals.HasActivity = len(days) > 0

	// The place most days were spent at, the first by identifier on a tie.
	bestPlace := ""
	for placeId, count := range mainPlaceDays {
		if bestPlace == "" || count > mainPlaceDays[bestPlace] || (count == mainPlaceDays[bestPlace] && placeId < bestPlace) {
			bestPlace = placeId
		}
	}
	totals.MainPlaceID, totals.MainPlaceName = bestPlace, mainPlaceNames[bestPlace]

	// Money is added in one currency: the one most days spent in.
	bestCurrency := ""
	for currency, count := range currencyDays {
		if bestCurrency == "" || count > currencyDays[bestCurrency] || (count == currencyDays[bestCurrency] && currency < bestCurrency) {
			bestCurrency = currency
		}
	}
	for _, day := range days {
		if day.Facts.SpentCurrency == bestCurrency {
			totals.SpentMinorUnits += day.Facts.SpentMinorUnits
		}
	}
	totals.SpentCurrency = bestCurrency
	return totals, moods
}

// summarizeMonths is a year's twelve months, from the days of the year and
// the month periods' headlines.
func summarizeMonths(transaction db.Transaction, userId, yearStart string, days []*models.JournalDay) ([]MonthSummary, error) {
	first, err := ParseDay(yearStart)
	if err != nil {
		return nil, err
	}
	headlines := map[string]string{}
	periods, err := transaction.ListJournalPeriods(userId, models.JournalPeriodMonth, yearStart, first.AddDate(0, 11, 0).Format(DayLayout))
	switch {
	case err == nil:
		for _, period := range periods {
			headlines[period.PeriodStart] = period.Headline
		}
	case errors.Is(err, db.ErrNotFound):
	default:
		return nil, err
	}
	months := make([]MonthSummary, 0, 12)
	for month := 0; month < 12; month++ {
		start := first.AddDate(0, month, 0)
		length := start.AddDate(0, 1, 0).Sub(start).Hours() / 24
		summary := MonthSummary{Start: start.Format(DayLayout), Headline: headlines[start.Format(DayLayout)], DayMoods: make([]int, int(length+0.5))}
		places := map[string]bool{}
		for _, day := range days {
			parsed, err := ParseDay(day.Day)
			if err != nil || parsed.Year() != start.Year() || parsed.Month() != start.Month() {
				continue
			}
			if day.MoodLevel != nil {
				summary.DayMoods[parsed.Day()-1] = *day.MoodLevel
			}
			summary.PhotoCount += day.Facts.PhotoCount
			for _, placeId := range day.Facts.VisitedPlaceIDs {
				places[placeId] = true
			}
		}
		summary.PlaceCount = len(places)
		months = append(months, summary)
	}
	return months, nil
}
