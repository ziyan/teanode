package journal

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

const (
	// olderDaysMost is how many past days one refresh works the facts of out,
	// walking back from the oldest it has done.
	olderDaysMost = 31

	// weatherDaysMost is how many days one refresh asks the weather service
	// about. A day the archive does not have yet is asked again the next time.
	weatherDaysMost = 10

	// weatherForecastDays is how recent a finished day is for the archive,
	// which trails by a few days, to be backed up by the forecast service,
	// which holds the last weeks.
	weatherForecastDays = 7

	// weatherListMost is how many day rows are looked through for ones
	// without weather.
	weatherListMost = 400
)

// RefreshResult says what one refresh did.
type RefreshResult struct {
	// DaysComputedCount is the days whose facts were worked out and stored:
	// today, yesterday and the older ones. OlderDaysComputedCount is the part
	// of them that were older, and OldestDay the oldest day looked at.
	DaysComputedCount      int
	OlderDaysComputedCount int
	OldestDay              string

	// IsBackfillComplete says no source has anything earlier than OldestDay.
	IsBackfillComplete bool

	// MoodsFilledCount is the days given a mood from Apple Health.
	MoodsFilledCount int

	// WeatherFetchedCount and WeatherFailedCount are the days the weather
	// service was asked about and answered or did not.
	WeatherFetchedCount int
	WeatherFailedCount  int
}

// Refresh brings a person's stored numbers up to date, without a model. The
// agent's hourly tick calls it. Each call:
//
//   - works out and stores the facts of today and of yesterday;
//   - works out up to 31 older days, walking back from the oldest day it has
//     done toward the earliest day any source has, jumping over days that
//     have nothing;
//   - gives a day with no mood the mood of Apple Health's State of Mind for
//     it, marked as coming from Health, and brings one that came from Health
//     up to date; the person's own mood is never touched;
//   - asks Open-Meteo for the weather of up to ten finished days that have a
//     main place and no weather, when the person has weather on and weather
//     is not nil.
//
// The cost: reading a day is about thirty queries (more with mail, photos and
// Health), so a call is 2 days when the walk back is done, up to 33 while it
// is under way, which spreads a person's whole past over a few hours; one query
// to see whether the walk is done; and at most ten requests to the weather
// service, each given the context. Facts of a day are written over every time,
// so the call can run any number of times.
//
// Only today and yesterday are refreshed once the walk back is done: a photo
// uploaded a week late is counted when its day is next computed, which a
// dream does by reading the day.
func Refresh(ctx context.Context, transaction db.Transaction, user *models.User, agent *models.Agent, now time.Time, weather WeatherFetcher) (RefreshResult, error) {
	var result RefreshResult
	permissions, err := transaction.EffectivePermissions(user.ID)
	if err != nil {
		return result, err
	}
	// The numbers are for the person's own page, which shows Health whatever
	// the agent may read of it.
	options := OptionsFor(permissions, true)
	refresher := &refresher{transaction: transaction, user: user, agent: agent, now: now, options: options}

	today := Today(user, now)
	yesterday, err := AddDays(today, -1)
	if err != nil {
		return result, err
	}
	for _, day := range []string{today, yesterday} {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := refresher.computeDay(day, &result); err != nil {
			return result, err
		}
	}
	result.OldestDay = yesterday

	if err := refresher.walkBack(ctx, yesterday, &result); err != nil {
		return result, err
	}
	if weather != nil {
		if err := refresher.fetchWeather(ctx, today, weather, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

type refresher struct {
	transaction db.Transaction
	user        *models.User
	agent       *models.Agent
	now         time.Time
	options     ReadOptions
}

// computeDay reads a day, stores its facts, and fills its mood from Health.
func (self *refresher) computeDay(day string, result *RefreshResult) error {
	read, err := ReadDay(self.transaction, self.user, self.agent, day, self.now, self.options)
	if err != nil {
		return err
	}
	facts := ComputeFacts(read)
	if err := self.transaction.PutJournalFacts(self.user.ID, day, read.TimeZone, facts, self.now); err != nil {
		return err
	}
	result.DaysComputedCount++

	level := facts.HealthMoodLevel
	if level == 0 || !facts.HasActivity {
		return nil
	}
	if row := read.Row; row != nil && row.MoodLevel != nil && (row.MoodSource != models.JournalMoodFromHealth || *row.MoodLevel == level) {
		// The person's own mood stays, and a Health mood that says the same
		// is already there.
		return nil
	}
	if _, err := self.transaction.SetJournalMood(self.user.ID, day, read.TimeZone, &level, "", models.JournalMoodFromHealth, self.now); err != nil {
		return err
	}
	result.MoodsFilledCount++
	return nil
}

// walkBack works out the days before the oldest one that has a row, newest
// first, as far as the sources go.
func (self *refresher) walkBack(ctx context.Context, yesterday string, result *RefreshResult) error {
	boundary := yesterday
	first, err := self.transaction.FirstJournalDay(self.user.ID)
	if err != nil {
		return err
	}
	if first != "" && first < boundary {
		boundary = first
	}
	chatNames := reading.ChatNamesOf(self.user)
	agentId := ""
	if self.agent != nil {
		agentId = self.agent.ID
	}
	location := Zone(self.user)
	for tried := 0; tried < olderDaysMost; tried++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		boundaryStart, _, err := DayRange(boundary, location)
		if err != nil {
			return err
		}
		latest, err := self.transaction.LatestJournalSourceTimeBefore(self.user.ID, agentId, chatNames, boundaryStart)
		if err != nil {
			return err
		}
		if latest == nil {
			result.IsBackfillComplete = true
			return nil
		}
		day := latest.In(location).Format(DayLayout)
		if day >= boundary {
			// A date placed at noon UTC can fall on the day it was found
			// before in a zone far from Greenwich; the day before is next.
			if day, err = AddDays(boundary, -1); err != nil {
				return err
			}
		}
		if err := self.computeDay(day, result); err != nil {
			return err
		}
		result.OlderDaysComputedCount++
		result.OldestDay = day
		boundary = day
	}
	return nil
}

// fetchWeather asks for the weather of finished days that have a main place
// and none yet, newest first.
func (self *refresher) fetchWeather(ctx context.Context, today string, weather WeatherFetcher, result *RefreshResult) error {
	settings, err := self.transaction.GetJournalSettings(self.user.ID)
	switch {
	case err == nil:
		if !settings.IsWeatherOn {
			return nil
		}
	case errors.Is(err, db.ErrNotFound):
		// Weather is on until the person turns it off.
	default:
		return err
	}
	yesterday, err := AddDays(today, -1)
	if err != nil {
		return err
	}
	days, _, err := self.transaction.ListJournalDays(self.user.ID, "", yesterday, 0, weatherListMost)
	switch {
	case err == nil:
	case errors.Is(err, db.ErrNotFound):
		return nil
	default:
		return err
	}
	asked := 0
	for _, day := range days {
		if asked == weatherDaysMost {
			break
		}
		facts := day.Facts
		if day.Weather != nil || facts.MainPlaceID == "" || day.Day > yesterday {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		asked++
		fetched, err := weather(ctx, facts.MainPlaceLatitude, facts.MainPlaceLongitude, day.Day, day.TimeZone, false)
		if err != nil && day.Day >= weatherRecent(today) {
			// The archive trails the present by a few days; the forecast
			// service has them.
			fetched, err = weather(ctx, facts.MainPlaceLatitude, facts.MainPlaceLongitude, day.Day, day.TimeZone, true)
		}
		if err != nil {
			result.WeatherFailedCount++
			continue
		}
		fetched.PlaceName = facts.MainPlaceName
		if err := self.transaction.PutJournalWeather(self.user.ID, day.Day, fetched); err != nil {
			return err
		}
		result.WeatherFetchedCount++
	}
	return nil
}

// weatherRecent is the oldest day the forecast service is tried for.
func weatherRecent(today string) string {
	recent, err := AddDays(today, -weatherForecastDays)
	if err != nil {
		return today
	}
	return recent
}
