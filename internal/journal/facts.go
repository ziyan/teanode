package journal

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// mainPlaceRounding makes the coordinates of the day's main place whole
// tenths of a degree, about eleven kilometers. They go to the weather
// service, which needs a city and not a street, and the person is told so on
// the page.
const mainPlaceRounding = 10.0

// ComputeFacts works out a day's numbers from what was read of it, without a
// model. Every field of models.JournalFacts is filled: a source the caller
// could not read, or the person does not keep, is zero.
func ComputeFacts(day *Day) models.JournalFacts {
	facts := models.JournalFacts{
		PhotoCount:         day.PhotoTotal,
		PhotoCandidateIDs:  append([]string(nil), day.PhotoCandidateIDs...),
		CodingSessionCount: len(day.CodingSessions),
		CodingMinutes:      codingMinutes(day.CodingSessions),
		RecordingCount:     len(day.Recordings),
		ConversationCount:  len(day.Conversations),
		MailReceivedCount:  day.MailReceivedTotal,
		MailSentCount:      day.MailSentTotal,
		DocumentCount:      day.DocumentTotal,
		TransactionCount:   len(day.Transactions),
		MemoryPageCount:    len(day.MemoryPages),
	}
	for _, chat := range day.Chats {
		facts.ChatMessageCount += chat.MessageCount
	}
	facts.MeetingCount, facts.MeetingMinutes = meetings(day.Events)
	facts.SpentMinorUnits, facts.SpentCurrency = spent(day.Transactions)
	facts.VisitedPlaceIDs = visitedPlaces(day.Visits)
	facts.MainPlaceID, facts.MainPlaceName, facts.MainPlaceLatitude, facts.MainPlaceLongitude = mainPlace(day.Visits, day.From, day.Until, day.Now)
	facts.PlaceCount = len(facts.VisitedPlaceIDs)

	if health := day.Health; health != nil {
		facts.StepCount = health.StepCount
		facts.SleepMinutes = health.SleepMinutes
		facts.WalkedMeters = health.WalkedMeters
		facts.HealthMoodLevel = health.MoodLevel
		for _, workout := range health.Workouts {
			facts.WorkoutMinutes += workout.Minutes
		}
	}
	facts.HasActivity = hasActivity(day)
	return facts
}

// hasActivity says there is anything at all on the day: a record of any
// source, or something the person kept on it.
func hasActivity(day *Day) bool {
	if len(day.CodingSessions) > 0 || day.DocumentTotal > 0 || len(day.JournalNotes) > 0 ||
		len(day.Conversations) > 0 || len(day.Chats) > 0 || day.MailReceivedTotal > 0 || day.MailSentTotal > 0 ||
		len(day.Recordings) > 0 || day.PhotoTotal > 0 || len(day.Visits) > 0 || len(day.Events) > 0 ||
		len(day.Reminders) > 0 || len(day.Transactions) > 0 || day.Health != nil || len(day.Notes) > 0 ||
		len(day.MemoryPages) > 0 {
		return true
	}
	if row := day.Row; row != nil {
		return strings.TrimSpace(row.Words) != "" || row.MoodLevel != nil
	}
	return false
}

// minutesOfUnion is the whole minutes of the spans' union, so that two
// stretches that overlap are counted once.
func minutesOfUnion(spans [][2]time.Time) int {
	sort.SliceStable(spans, func(left, right int) bool { return spans[left][0].Before(spans[right][0]) })
	total := time.Duration(0)
	var coveredUntil time.Time
	for _, span := range spans {
		start, end := span[0], span[1]
		if !end.After(start) {
			continue
		}
		if start.Before(coveredUntil) {
			start = coveredUntil
		}
		if end.After(start) {
			total += end.Sub(start)
			coveredUntil = end
		}
	}
	return int(math.Round(total.Minutes()))
}

func codingMinutes(sessions []CodingSession) int {
	spans := make([][2]time.Time, 0, len(sessions))
	for _, session := range sessions {
		spans = append(spans, [2]time.Time{session.StartedAt, session.EndedAt})
	}
	return minutesOfUnion(spans)
}

// meetings counts the events with a time that were not called off, and
// their minutes. An all-day event is a date and not a meeting.
func meetings(events []Event) (count, minutes int) {
	var spans [][2]time.Time
	for _, event := range events {
		if event.IsAllDay {
			continue
		}
		count++
		spans = append(spans, [2]time.Time{event.StartsAt, event.EndsAt})
	}
	return count, minutesOfUnion(spans)
}

// spent is the money spent in the currency most of it was spent in, and
// that currency; money received is not subtracted.
func spent(transactions []Transaction) (int64, string) {
	byCurrency := map[string]int64{}
	for _, transaction := range transactions {
		if transaction.AmountMinorUnits < 0 {
			byCurrency[transaction.Currency] += -transaction.AmountMinorUnits
		}
	}
	currencies := make([]string, 0, len(byCurrency))
	for currency := range byCurrency {
		currencies = append(currencies, currency)
	}
	// The one with the most spent, the first by name on a tie, so that the
	// same day gives the same answer every time.
	sort.Strings(currencies)
	best := ""
	for _, currency := range currencies {
		if best == "" || byCurrency[currency] > byCurrency[best] {
			best = currency
		}
	}
	return byCurrency[best], best
}

// visitedPlaces are the places stayed at, in the order they were first
// reached.
func visitedPlaces(visits []Visit) []string {
	seen := map[string]bool{}
	var identifiers []string
	for _, visit := range visits {
		if visit.PlaceID == "" || seen[visit.PlaceID] {
			continue
		}
		seen[visit.PlaceID] = true
		identifiers = append(identifiers, visit.PlaceID)
	}
	return identifiers
}

// mainPlace is where most of the day was spent: the place with the longest
// stay inside the day, with its coordinates rounded to a tenth of a degree. A
// stay that began before the day or goes on after it counts only for the part
// inside; a visit still going counts up to now. A visit the phone reported
// only the departure of, with no arrival, counts for nothing, since there is
// no telling how long it was. The first of two equal stays wins.
func mainPlace(visits []Visit, from, until, now time.Time) (placeId, name string, latitude, longitude float64) {
	stays := map[string]time.Duration{}
	var order []string
	byPlace := map[string]Visit{}
	for _, visit := range visits {
		if visit.PlaceID == "" {
			continue
		}
		if _, isKnown := byPlace[visit.PlaceID]; !isKnown {
			byPlace[visit.PlaceID] = visit
			order = append(order, visit.PlaceID)
		}
		if visit.ArrivedAt == nil {
			continue
		}
		start := from
		if visit.ArrivedAt.After(from) {
			start = *visit.ArrivedAt
		}
		end := until
		switch {
		case visit.DepartedAt != nil && visit.DepartedAt.Before(until):
			end = *visit.DepartedAt
		case visit.DepartedAt == nil && now.Before(until):
			end = now
		}
		if end.After(start) {
			stays[visit.PlaceID] += end.Sub(start)
		}
	}
	best := ""
	for _, candidate := range order {
		if stays[candidate] > 0 && (best == "" || stays[candidate] > stays[best]) {
			best = candidate
		}
	}
	if best == "" {
		return "", "", 0, 0
	}
	visit := byPlace[best]
	return best, visit.PlaceName, roundDegrees(visit.Latitude), roundDegrees(visit.Longitude)
}

func roundDegrees(degrees float64) float64 {
	return math.Round(degrees*mainPlaceRounding) / mainPlaceRounding
}
