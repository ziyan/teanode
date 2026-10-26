package journal_test

import (
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// memoryJournal keeps a person's journal rows in memory over a real
// transaction, so that the journal's reading and refreshing are tried against
// real sources while the storage of the journal itself is somebody else's.
// Only the methods the journal package calls are answered.
type memoryJournal struct {
	db.Transaction

	days     map[string]*models.JournalDay
	periods  map[string]*models.JournalPeriod
	links    map[string][]*models.JournalLink
	settings *models.JournalSettings

	// moodPuts and weatherPuts count what was stored, to say that something
	// was left alone.
	moodPuts    int
	weatherPuts int
}

func newMemoryJournal(transaction db.Transaction) *memoryJournal {
	return &memoryJournal{
		Transaction: transaction, days: map[string]*models.JournalDay{}, periods: map[string]*models.JournalPeriod{},
		links: map[string][]*models.JournalLink{},
	}
}

func hasSomething(day *models.JournalDay) bool {
	return day.Facts.HasActivity || strings.TrimSpace(day.Words) != "" || day.MoodLevel != nil
}

func (self *memoryJournal) GetJournalDay(userId, day string) (*models.JournalDay, error) {
	found, isFound := self.days[day]
	if !isFound {
		return nil, db.ErrNotFound
	}
	copied := *found
	return &copied, nil
}

func (self *memoryJournal) ListJournalDays(userId, fromDay, untilDay string, offset, limit int) ([]*models.JournalDay, int, error) {
	var found []*models.JournalDay
	for _, day := range self.days {
		if hasSomething(day) && (fromDay == "" || day.Day >= fromDay) && (untilDay == "" || day.Day <= untilDay) {
			copied := *day
			found = append(found, &copied)
		}
	}
	sort.SliceStable(found, func(left, right int) bool { return found[left].Day > found[right].Day })
	total := len(found)
	if offset > len(found) {
		offset = len(found)
	}
	found = found[offset:]
	if limit > 0 && len(found) > limit {
		found = found[:limit]
	}
	return found, total, nil
}

func (self *memoryJournal) FirstJournalDay(userId string) (string, error) {
	first := ""
	for _, day := range self.days {
		if hasSomething(day) && (first == "" || day.Day < first) {
			first = day.Day
		}
	}
	return first, nil
}

func (self *memoryJournal) row(day, timeZone string, at time.Time) *models.JournalDay {
	found, isFound := self.days[day]
	if !isFound {
		found = &models.JournalDay{ID: "journal-day-" + day, Day: day, TimeZone: timeZone, CreatedAt: at, ModifiedAt: at, NarrativeCorrections: []models.JournalCorrection{}}
		self.days[day] = found
	}
	return found
}

func (self *memoryJournal) PutJournalFacts(userId, day, timeZone string, facts models.JournalFacts, at time.Time) error {
	if _, isFound := self.days[day]; !isFound && !facts.HasActivity {
		return nil
	}
	found := self.row(day, timeZone, at)
	found.Facts = facts
	found.FactsComputedAt = &at
	return nil
}

func (self *memoryJournal) SetJournalMood(userId, day, timeZone string, moodLevel *int, moodWords string, moodSource models.JournalMoodSource, at time.Time) (*models.JournalDay, error) {
	found := self.row(day, timeZone, at)
	if moodSource == models.JournalMoodFromHealth && found.MoodLevel != nil && found.MoodSource == models.JournalMoodFromPerson {
		return found, nil
	}
	self.moodPuts++
	found.MoodLevel, found.MoodWords, found.MoodSource = moodLevel, moodWords, moodSource
	return found, nil
}

func (self *memoryJournal) PutJournalWeather(userId, day string, weather *models.JournalWeather) error {
	self.weatherPuts++
	self.days[day].Weather = weather
	return nil
}

func (self *memoryJournal) GetJournalSettings(userId string) (*models.JournalSettings, error) {
	if self.settings == nil {
		return &models.JournalSettings{IsWeatherOn: true, EveningQuestionTime: "21:00"}, nil
	}
	return self.settings, nil
}

func (self *memoryJournal) ListJournalLinks(userId, journalDayId, journalPeriodId string) ([]*models.JournalLink, error) {
	return self.links[journalDayId+"|"+journalPeriodId], nil
}

func (self *memoryJournal) GetJournalPeriod(userId string, periodKind models.JournalPeriodKind, periodStart string) (*models.JournalPeriod, error) {
	found, isFound := self.periods[string(periodKind)+"|"+periodStart]
	if !isFound {
		return nil, db.ErrNotFound
	}
	return found, nil
}

func (self *memoryJournal) ListJournalPeriods(userId string, periodKind models.JournalPeriodKind, fromStart, untilStart string) ([]*models.JournalPeriod, error) {
	var found []*models.JournalPeriod
	for key, period := range self.periods {
		if strings.HasPrefix(key, string(periodKind)+"|") && period.PeriodStart >= fromStart && period.PeriodStart <= untilStart {
			found = append(found, period)
		}
	}
	sort.SliceStable(found, func(left, right int) bool { return found[left].PeriodStart < found[right].PeriodStart })
	return found, nil
}
