package journal_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

// weatherCalls is a weather fetcher that answers every day the same and
// remembers who asked.
type weatherCalls struct {
	calls      []string
	failOnDays map[string]bool
}

func (self *weatherCalls) fetch(ctx context.Context, latitude, longitude float64, day, timeZone string, isForecast bool) (*models.JournalWeather, error) {
	self.calls = append(self.calls, fmt.Sprintf("%s %.1f,%.1f %s %t", day, latitude, longitude, timeZone, isForecast))
	if self.failOnDays[day] {
		return nil, errors.New("the archive has not got that day yet")
	}
	return &models.JournalWeather{WeatherCode: 3, HighCelsius: 14, LowCelsius: 8, FetchedAt: time.Now()}, nil
}

func grantEverything(t *testing.T, tx db.Transaction, world *world) {
	t.Helper()
	dbtest.GrantPermission(t, tx, world.user.ID, models.PermissionMailRead, models.PermissionPhotosUse, models.PermissionLocationUse,
		models.PermissionCalendarUse, models.PermissionHealthUse, models.PermissionCircleUse, models.PermissionAgentUse, models.PermissionJournalUse)
}

// The hourly refresh stores the numbers of today and yesterday, walks back
// over the days before them as far as anything goes, gives a day the mood
// of Apple Health, and asks for the weather of finished days once.
func TestRefreshStoresFactsMoodsAndWeatherAndWalksBack(t *testing.T) {
	world, release := newWorld(t)
	defer release()

	// Today is the day after the invented day, early in the morning.
	now := later(1, 0)
	weather := &weatherCalls{}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		grantEverything(t, tx, world)

		result, err := journal.Refresh(context.Background(), tx, world.user, world.agent, now, weather.fetch)
		if err != nil {
			t.Fatal(err)
		}
		// Today, yesterday and the one older day there is; then the sources
		// run out.
		if result.DaysComputedCount != 3 || result.OlderDaysComputedCount != 1 || result.OldestDay != "2026-10-21" || !result.IsBackfillComplete {
			t.Errorf("the walk: %+v", result)
		}
		if result.MoodsFilledCount != 1 || result.WeatherFetchedCount != 2 || result.WeatherFailedCount != 0 {
			t.Errorf("mood and weather: %+v", result)
		}

		day, err := tx.GetJournalDay(world.user.ID, worldDay)
		if err != nil {
			t.Fatal(err)
		}
		if day.Facts.PhotoCount != 2 || day.Facts.StepCount != 8200 || day.Facts.MainPlaceName != "Harbor Office" || !day.Facts.HasActivity || day.FactsComputedAt == nil {
			t.Errorf("the numbers of the day: %+v", day.Facts)
		}
		if day.TimeZone != "America/New_York" {
			t.Errorf("the day keeps the zone it was made in: %s", day.TimeZone)
		}
		if day.MoodLevel == nil || *day.MoodLevel != 4 || day.MoodSource != models.JournalMoodFromHealth {
			t.Errorf("the mood from Apple Health: %v %s", day.MoodLevel, day.MoodSource)
		}
		if day.Weather == nil || day.Weather.WeatherCode != 3 || day.Weather.PlaceName != "Harbor Office" {
			t.Errorf("the weather: %+v", day.Weather)
		}
		today, err := tx.GetJournalDay(world.user.ID, "2026-10-23")
		if err != nil || today.Facts.PhotoCount != 1 || today.Facts.ConversationCount != 1 || today.Weather != nil {
			t.Errorf("today has numbers and no weather yet: %+v %v", today, err)
		}
		older, err := tx.GetJournalDay(world.user.ID, "2026-10-21")
		if err != nil || older.Facts.PhotoCount != 1 || older.Facts.MailReceivedCount != 1 || older.Facts.MainPlaceName != "Home" || older.Weather == nil {
			t.Errorf("the day before: %+v %v", older, err)
		}
		if len(weather.calls) != 2 || weather.calls[0] != "2026-10-22 40.7,-74.0 America/New_York false" {
			t.Errorf("the weather was asked for newest first, at the rounded place: %v", weather.calls)
		}

		// The same hour again changes nothing and asks nothing.
		again, err := journal.Refresh(context.Background(), tx, world.user, world.agent, now, weather.fetch)
		if err != nil {
			t.Fatal(err)
		}
		if again.OlderDaysComputedCount != 0 || !again.IsBackfillComplete || again.WeatherFetchedCount != 0 || again.MoodsFilledCount != 0 || len(weather.calls) != 2 {
			t.Errorf("the second refresh: %+v %v", again, weather.calls)
		}

		// A period adds the days up.
		period, err := journal.ReadPeriod(tx, world.user, models.JournalPeriodWeek, worldDay, models.WeekStartsMonday, now)
		if err != nil {
			t.Fatal(err)
		}
		if period.Start != "2026-10-19" || period.End != "2026-10-25" || period.DayCount != 3 || period.Totals.PhotoCount != 4 || period.Totals.StepCount != 8200 ||
			period.MoodCounts != [5]int{0, 0, 0, 1, 0} || period.Totals.PlaceCount != 3 || period.Totals.MainPlaceLatitude != 40.7 || period.Days[0].Day != "2026-10-21" || period.Days[2].Day != "2026-10-23" {
			t.Errorf("the week: %+v totals %+v", period, period.Totals)
		}
		year, err := journal.ReadPeriod(tx, world.user, models.JournalPeriodYear, worldDay, "", now)
		if err != nil {
			t.Fatal(err)
		}
		october := year.Months[9]
		if len(year.Months) != 12 || october.Start != "2026-10-01" || len(october.DayMoods) != 31 || october.DayMoods[21] != 4 || october.DayMoods[20] != 0 || october.PhotoCount != 4 || october.PlaceCount != 3 {
			t.Errorf("the year's months: %+v", october)
		}
		if len(year.Months[0].DayMoods) != 31 || len(year.Months[1].DayMoods) != 28 {
			t.Errorf("the days of January and February: %d %d", len(year.Months[0].DayMoods), len(year.Months[1].DayMoods))
		}
	})
}

// The person's own mood is theirs, weather can be turned off, and a day the
// archive has not got yet is asked about again at the next refresh.
func TestRefreshLeavesThePersonsMoodAndHonorsTheWeatherSwitch(t *testing.T) {
	world, release := newWorld(t)
	defer release()

	now := later(1, 0)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		grantEverything(t, tx, world)
		low := models.JournalMoodLow
		if _, err := tx.SetJournalMood(world.user.ID, worldDay, journal.ZoneName(world.user), &low, "Tired.", models.JournalMoodFromPerson, now); err != nil {
			t.Fatal(err)
		}
		failing := &weatherCalls{failOnDays: map[string]bool{worldDay: true}}
		result, err := journal.Refresh(context.Background(), tx, world.user, world.agent, now, failing.fetch)
		if err != nil {
			t.Fatal(err)
		}
		day, err := tx.GetJournalDay(world.user.ID, worldDay)
		if err != nil {
			t.Fatal(err)
		}
		if day.MoodLevel == nil || *day.MoodLevel != 2 || day.MoodSource != models.JournalMoodFromPerson || result.MoodsFilledCount != 0 {
			t.Errorf("the person's mood stays: %v %s %d", day.MoodLevel, day.MoodSource, result.MoodsFilledCount)
		}
		// Yesterday's weather failed; it is asked for again, by the forecast
		// too since it is recent, and the day before's was kept.
		if result.WeatherFailedCount != 1 || result.WeatherFetchedCount != 1 || day.Weather != nil {
			t.Errorf("one day failed: %+v", result)
		}
		forecastAsked := false
		for _, call := range failing.calls {
			if call == "2026-10-22 40.7,-74.0 America/New_York true" {
				forecastAsked = true
			}
		}
		if !forecastAsked {
			t.Errorf("a recent day is asked of the forecast when the archive has not got it: %v", failing.calls)
		}

		// With weather off nothing is asked.
		if err := tx.SetJournalSettings(world.user.ID, models.JournalSettings{IsWeatherOn: false, EveningQuestionTime: "21:00"}); err != nil {
			t.Fatal(err)
		}
		quiet := &weatherCalls{}
		result, err = journal.Refresh(context.Background(), tx, world.user, world.agent, now, quiet.fetch)
		if err != nil || len(quiet.calls) != 0 || result.WeatherFetchedCount != 0 {
			t.Errorf("weather is off: %+v %v %v", result, quiet.calls, err)
		}
		// And a nil fetcher is no weather at all.
		if _, err := journal.Refresh(context.Background(), tx, world.user, world.agent, now, nil); err != nil {
			t.Error(err)
		}
	})
}

func TestRefreshStopsWhenTheContextIsOver(t *testing.T) {
	world, release := newWorld(t)
	defer release()
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		grantEverything(t, tx, world)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := journal.Refresh(ctx, tx, world.user, world.agent, later(1, 0), nil); !errors.Is(err, context.Canceled) {
			t.Errorf("a refresh that is told to stop does: %v", err)
		}
	})
}
