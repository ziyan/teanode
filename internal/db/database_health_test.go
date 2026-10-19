package db_test

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/health"
	"github.com/ziyan/teanode/internal/models"
)

func number(value float64) *float64 { return &value }

// listSamples is a page of readings with the count of them made.
func listSamples(tx db.Transaction, userId, healthType string, from, until *time.Time, limitCount, offset int) ([]*models.HealthSample, int, error) {
	page, err := tx.ListHealthSamples(userId, healthType, from, until, limitCount, offset, true)
	if err != nil {
		return nil, 0, err
	}
	return page.Samples, page.TotalCount, nil
}

func stepSample(healthSampleId string, startedAt time.Time, steps float64) *models.HealthSample {
	return &models.HealthSample{
		HealthSampleID: healthSampleId, HealthType: "stepCount", StartedAt: startedAt, EndedAt: startedAt.Add(5 * time.Minute),
		QuantityValue: number(steps), QuantityUnit: "count", SourceName: "Phone",
	}
}

func daySummary(healthType, summaryDay string, change func(*models.HealthDaySummary)) *models.HealthDaySummary {
	summary := &models.HealthDaySummary{HealthType: healthType, SummaryDay: summaryDay, QuantityUnit: "count"}
	change(summary)
	return summary
}

// A reading the phone sends again is stored once, readings of two people with
// the same HealthKit identifier are two readings, and a reading that cannot be
// right is refused with nothing stored.
func TestHealthSamplesAreStoredOnceAndKeptApartPerPerson(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		morning := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

		batch := []*models.HealthSample{
			stepSample("sample-1", morning, 100), stepSample("sample-2", morning.Add(time.Hour), 200),
			// The same reading twice in one batch.
			stepSample("sample-2", morning.Add(time.Hour), 200),
		}
		if newCount, err := tx.InsertHealthSamples(world.ada.ID, batch); err != nil || newCount != 2 {
			t.Fatalf("the first batch: %d new, %v", newCount, err)
		}
		if newCount, err := tx.InsertHealthSamples(world.ada.ID, batch); err != nil || newCount != 0 {
			t.Fatalf("the same batch again adds nothing: %d new, %v", newCount, err)
		}
		if newCount, err := tx.InsertHealthSamples(world.ben.ID, batch[:1]); err != nil || newCount != 1 {
			t.Fatalf("another person's reading with the same identifier is their own: %d new, %v", newCount, err)
		}

		samples, totalCount, err := listSamples(tx, world.ada.ID, "stepCount", nil, nil, 10, 0)
		if err != nil || totalCount != 2 || len(samples) != 2 {
			t.Fatalf("ada's readings: %d of %d, %v", len(samples), totalCount, err)
		}
		if samples[0].HealthSampleID != "sample-2" || samples[1].HealthSampleID != "sample-1" {
			t.Errorf("newest first: %s then %s", samples[0].HealthSampleID, samples[1].HealthSampleID)
		}
		first := samples[1]
		if first.QuantityValue == nil || *first.QuantityValue != 100 || first.QuantityUnit != "count" || first.SourceName != "Phone" ||
			first.SampleMetadataJSON != "{}" || first.CategoryValue != nil || first.HasRoute || first.ReceivedAt.IsZero() ||
			!first.StartedAt.Equal(morning) || !first.EndedAt.Equal(morning.Add(5*time.Minute)) {
			t.Errorf("a reading as stored: %+v", first)
		}

		// Paging and ranges.
		page, totalCount, err := listSamples(tx, world.ada.ID, "stepCount", nil, nil, 1, 1)
		if err != nil || totalCount != 2 || len(page) != 1 || page[0].HealthSampleID != "sample-1" {
			t.Errorf("the second page of one: %v of %d, %v", page, totalCount, err)
		}
		from, until := morning.Add(30*time.Minute), morning.Add(2*time.Hour)
		inRange, totalCount, err := listSamples(tx, world.ada.ID, "stepCount", &from, &until, 10, 0)
		if err != nil || totalCount != 1 || inRange[0].HealthSampleID != "sample-2" {
			t.Errorf("the readings that started in a range: %v of %d, %v", inRange, totalCount, err)
		}
		if other, total, _ := listSamples(tx, world.ada.ID, "heartRate", nil, nil, 10, 0); len(other) != 0 || total != 0 {
			t.Errorf("another kind has none: %v", other)
		}
		if others, total, _ := listSamples(tx, world.cora.ID, "stepCount", nil, nil, 10, 0); len(others) != 0 || total != 0 {
			t.Errorf("a person who reported nothing has none: %v", others)
		}

		// A reading that cannot be right stores nothing, and not the good ones
		// before it.
		before := morning.Add(-time.Hour)
		for name, bad := range map[string]*models.HealthSample{
			"noidentifier":  {HealthType: "stepCount", StartedAt: morning, EndedAt: morning},
			"notype":        {HealthSampleID: "x", HealthType: "step count", StartedAt: morning, EndedAt: morning},
			"endsbefore":    {HealthSampleID: "x", HealthType: "stepCount", StartedAt: morning, EndedAt: before},
			"notime":        {HealthSampleID: "x", HealthType: "stepCount"},
			"notfinite":     {HealthSampleID: "x", HealthType: "stepCount", StartedAt: morning, EndedAt: morning, QuantityValue: number(math.Inf(1))},
			"notanobject":   {HealthSampleID: "x", HealthType: "stepCount", StartedAt: morning, EndedAt: morning, SampleMetadataJSON: "[1]"},
			"nullcharacter": {HealthSampleID: "x", HealthType: "stepCount", StartedAt: morning, EndedAt: morning, SourceName: "a\x00b"},
		} {
			_, err := tx.InsertHealthSamples(world.ada.ID, []*models.HealthSample{stepSample("good", morning, 1), bad})
			if !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("%s: %v", name, err)
			}
		}
		if _, total, _ := listSamples(tx, world.ada.ID, "stepCount", nil, nil, 10, 0); total != 2 {
			t.Errorf("a refused batch stored something: %d", total)
		}
	})
}

// Deleting readings answers the kind and times of each, takes the route of a
// workout with it, leaves the day summaries alone, and touches nobody else's.
func TestDeletingHealthSamplesAnswersTheirTimesAndLeavesTheDays(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		morning := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)
		workout := &models.HealthSample{
			HealthSampleID: "run-1", HealthType: "workout", StartedAt: morning, EndedAt: morning.Add(40 * time.Minute),
			WorkoutActivityType: "running", SampleMetadataJSON: `{"totalDistanceMeters": 5000}`,
		}
		for _, person := range []*models.User{world.ada, world.ben} {
			if _, err := tx.InsertHealthSamples(person.ID, []*models.HealthSample{
				stepSample("sample-1", morning.Add(time.Hour), 100), stepSample("sample-2", morning.Add(2*time.Hour), 200), workout,
			}); err != nil {
				t.Fatalf("InsertHealthSamples: %s", err)
			}
			if kept, _, err := tx.UpsertHealthRoutes(person.ID, []*models.HealthRoute{{HealthSampleID: "run-1", RoutePoints: [][]float64{{0, 10.5, 20.5, 3}, {60, 10.51, 20.51, 4}}}}); err != nil || kept != 1 {
				t.Fatalf("UpsertHealthRoutes: %d, %v", kept, err)
			}
			if _, err := tx.UpsertHealthDaySummaries(person.ID, []*models.HealthDaySummary{
				daySummary("stepCount", "2026-03-02", func(summary *models.HealthDaySummary) { summary.SumValue = number(300) }),
			}); err != nil {
				t.Fatalf("UpsertHealthDaySummaries: %s", err)
			}
		}

		listed, _, _ := listSamples(tx, world.ada.ID, "workout", nil, nil, 10, 0)
		if len(listed) != 1 || !listed[0].HasRoute || listed[0].SampleMetadataJSON == "" {
			t.Fatalf("the workout has its route: %+v", listed)
		}

		deleted, err := tx.DeleteHealthSamples(world.ada.ID, []string{"sample-2", "run-1", "never-had-this", "sample-2"})
		if err != nil {
			t.Fatalf("DeleteHealthSamples: %s", err)
		}
		if len(deleted) != 2 || deleted[0].HealthType != "workout" || !deleted[0].StartedAt.Equal(morning) || !deleted[0].EndedAt.Equal(morning.Add(40*time.Minute)) ||
			deleted[1].HealthType != "stepCount" || !deleted[1].StartedAt.Equal(morning.Add(2*time.Hour)) || !deleted[1].EndedAt.Equal(morning.Add(2*time.Hour+5*time.Minute)) {
			t.Fatalf("what was deleted, oldest first: %+v %+v", deleted[0], deleted[len(deleted)-1])
		}
		// A phone that never got the answer sends the same identifiers again,
		// and gets the same answer, although the readings are gone.
		again, err := tx.DeleteHealthSamples(world.ada.ID, []string{"sample-2", "run-1", "never-had-this"})
		if err != nil || len(again) != 2 || again[0].HealthType != "workout" || !again[0].StartedAt.Equal(morning) ||
			again[1].HealthType != "stepCount" || !again[1].StartedAt.Equal(morning.Add(2*time.Hour)) {
			t.Errorf("deleting what is gone answers what it was: %+v %v", again, err)
		}
		if other, err := tx.DeleteHealthSamples(world.ben.ID, []string{"never-had-this"}); err != nil || len(other) != 0 {
			t.Errorf("what is known of a deleted reading is its person's: %v %v", other, err)
		}
		if none, err := tx.DeleteHealthSamples(world.ada.ID, nil); err != nil || len(none) != 0 {
			t.Errorf("deleting nothing: %v %v", none, err)
		}

		if _, err := tx.GetHealthRoute(world.ada.ID, "run-1"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("the route went with the workout: %v", err)
		}
		if _, err := tx.GetHealthRoute(world.ben.ID, "run-1"); err != nil {
			t.Errorf("ben's route stays: %v", err)
		}
		if _, total, _ := listSamples(tx, world.ben.ID, "stepCount", nil, nil, 10, 0); total != 2 {
			t.Errorf("ben's readings stay: %d", total)
		}
		// The phone sends the days again; deleting readings does not change them.
		periods, _, err := tx.ListHealthPeriods(world.ada.ID, "stepCount", "2026-03-01", "2026-03-03", health.PeriodDay)
		if err != nil || len(periods) != 1 || periods[0].SumValue == nil || *periods[0].SumValue != 300 {
			t.Errorf("the day stays as it was sent: %+v %v", periods, err)
		}
	})
}

// A route needs the workout it is for, belongs to one person, and a second one
// for the same workout replaces the first.
func TestAHealthRouteNeedsItsWorkout(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		morning := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)
		if _, err := tx.InsertHealthSamples(world.ada.ID, []*models.HealthSample{
			{HealthSampleID: "run-1", HealthType: "workout", StartedAt: morning, EndedAt: morning.Add(time.Hour)},
			stepSample("steps-1", morning, 5),
		}); err != nil {
			t.Fatal(err)
		}
		path := [][]float64{{0, 10.5, 20.5, 3}}
		kept, unmatched, err := tx.UpsertHealthRoutes(world.ada.ID, []*models.HealthRoute{
			{HealthSampleID: "run-1", RoutePoints: path},
			{HealthSampleID: "steps-1", RoutePoints: path},
			{HealthSampleID: "never-had-this", RoutePoints: path},
		})
		if err != nil || kept != 1 {
			t.Fatalf("only the workout's route is kept: %d, %v", kept, err)
		}
		if len(unmatched) != 2 || unmatched[0] != "steps-1" || unmatched[1] != "never-had-this" {
			t.Errorf("the routes left out are named, so the phone sends them again after their workout: %v", unmatched)
		}
		if _, _, err := tx.UpsertHealthRoutes(world.ben.ID, []*models.HealthRoute{{HealthSampleID: "run-1", RoutePoints: path}}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.GetHealthRoute(world.ben.ID, "run-1"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("ben has no such workout, so no route: %v", err)
		}
		if _, _, err := tx.UpsertHealthRoutes(world.ada.ID, []*models.HealthRoute{{HealthSampleID: "run-1", RoutePoints: [][]float64{{0, 11, 21, 3}, {5, 11.1, 21.1, 3}}}}); err != nil {
			t.Fatal(err)
		}
		route, err := tx.GetHealthRoute(world.ada.ID, "run-1")
		if err != nil || len(route.RoutePoints) != 2 || route.RoutePoints[1][1] != 11.1 {
			t.Errorf("a route replaces the one before: %+v %v", route, err)
		}
		if _, _, err := tx.UpsertHealthRoutes(world.ada.ID, []*models.HealthRoute{
			{HealthSampleID: "run-1", RoutePoints: [][]float64{{0, 12, 22, 3}}},
			{HealthSampleID: "run-1", RoutePoints: [][]float64{{math.NaN(), 1, 1, 1}}},
		}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a route that is not finite: %v", err)
		}
		if route, _ := tx.GetHealthRoute(world.ada.ID, "run-1"); route == nil || route.RoutePoints[0][1] != 11 {
			t.Errorf("every route is checked before the first is stored: %+v", route)
		}
	})
}

// Days are replaced when sent again, and add up into weeks that start on
// Monday and into months: sums are added, averages are weighted by how many
// readings each day had, the lowest minimum and highest maximum are kept,
// durations and the seconds of each category word are added.
func TestHealthDaysAddUpIntoWeeksAndMonths(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)

		// 2026-03-01 is a Sunday: the last day of the week that began on
		// 2026-02-23. 2026-03-02 is the Monday after it.
		if updated, err := tx.UpsertHealthDaySummaries(world.ada.ID, []*models.HealthDaySummary{
			daySummary("stepCount", "2026-02-28", func(summary *models.HealthDaySummary) { summary.SumValue = number(1000); summary.SampleCount = 10 }),
			daySummary("stepCount", "2026-03-01", func(summary *models.HealthDaySummary) { summary.SumValue = number(2000); summary.SampleCount = 20 }),
			daySummary("stepCount", "2026-03-02", func(summary *models.HealthDaySummary) { summary.SumValue = number(4000); summary.SampleCount = 40 }),
			daySummary("stepCount", "2026-03-04", func(summary *models.HealthDaySummary) { summary.SumValue = number(8000); summary.SampleCount = 80 }),
			// The same day twice in one batch: the later one is kept.
			daySummary("stepCount", "2026-03-04", func(summary *models.HealthDaySummary) { summary.SumValue = number(8500); summary.SampleCount = 85 }),
			daySummary("heartRate", "2026-03-02", func(summary *models.HealthDaySummary) {
				summary.QuantityUnit = "count/min"
				summary.AverageValue, summary.MinimumValue, summary.MaximumValue, summary.SampleCount = number(60), number(50), number(90), 100
			}),
			daySummary("heartRate", "2026-03-03", func(summary *models.HealthDaySummary) {
				summary.QuantityUnit = "count/min"
				summary.AverageValue, summary.MinimumValue, summary.MaximumValue, summary.SampleCount = number(80), number(45), number(120), 300
			}),
			daySummary("heartRate", "2026-04-01", func(summary *models.HealthDaySummary) {
				summary.QuantityUnit = "count/min"
				summary.AverageValue, summary.SampleCount = number(70), 0
			}),
			daySummary("sleepAnalysis", "2026-03-02", func(summary *models.HealthDaySummary) {
				summary.QuantityUnit = ""
				summary.DurationSeconds = number(27000)
				summary.CategorySecondsJSON = `{"asleepCore": 12600, "asleepDeep": 3000, "awake": 600}`
				summary.SampleCount = 6
			}),
			daySummary("sleepAnalysis", "2026-03-03", func(summary *models.HealthDaySummary) {
				summary.QuantityUnit = ""
				summary.DurationSeconds = number(25200)
				summary.CategorySecondsJSON = `{"asleepCore": 10000, "asleepREM": 5400}`
				summary.SampleCount = 4
			}),
		}); err != nil || updated != 9 {
			t.Fatalf("UpsertHealthDaySummaries: %d, %v", updated, err)
		}
		// Another person's days never mix in.
		if _, err := tx.UpsertHealthDaySummaries(world.ben.ID, []*models.HealthDaySummary{
			daySummary("stepCount", "2026-03-02", func(summary *models.HealthDaySummary) { summary.SumValue = number(999999) }),
		}); err != nil {
			t.Fatal(err)
		}

		days, unit, err := tx.ListHealthPeriods(world.ada.ID, "stepCount", "2026-02-01", "2026-03-31", health.PeriodDay)
		if err != nil || len(days) != 4 || unit != "count" {
			t.Fatalf("the days: %d, %q, %v", len(days), unit, err)
		}
		if days[3].StartDay != "2026-03-04" || days[3].EndDay != "2026-03-04" || *days[3].SumValue != 8500 || days[3].SampleCount != 85 {
			t.Errorf("a day sent twice is the later one: %+v", days[3])
		}

		weeks, _, err := tx.ListHealthPeriods(world.ada.ID, "stepCount", "2026-02-01", "2026-03-31", health.PeriodWeek)
		if err != nil || len(weeks) != 2 {
			t.Fatalf("the weeks: %+v %v", weeks, err)
		}
		if weeks[0].StartDay != "2026-02-23" || weeks[0].EndDay != "2026-03-01" || *weeks[0].SumValue != 3000 || weeks[0].SampleCount != 30 {
			t.Errorf("Saturday and Sunday are the end of the week before: %+v", weeks[0])
		}
		if weeks[1].StartDay != "2026-03-02" || weeks[1].EndDay != "2026-03-08" || *weeks[1].SumValue != 12500 || weeks[1].SampleCount != 125 {
			t.Errorf("Monday starts a week: %+v", weeks[1])
		}
		if weeks[1].AverageValue != nil || weeks[1].DurationSeconds != nil || len(weeks[1].CategorySeconds) != 0 {
			t.Errorf("a sum has no average or duration: %+v", weeks[1])
		}
		// A per-day average divides by the days that have a summary, not by
		// the days of the week: two days in the first week, two in the second.
		if weeks[0].DayCount != 2 || weeks[1].DayCount != 2 || days[0].DayCount != 1 {
			t.Errorf("the days a period holds: %d, %d, %d", weeks[0].DayCount, weeks[1].DayCount, days[0].DayCount)
		}

		months, _, err := tx.ListHealthPeriods(world.ada.ID, "stepCount", "2026-02-01", "2026-03-31", health.PeriodMonth)
		if err != nil || len(months) != 2 || months[0].StartDay != "2026-02-01" || months[0].EndDay != "2026-02-28" || *months[0].SumValue != 1000 ||
			months[1].StartDay != "2026-03-01" || months[1].EndDay != "2026-03-31" || *months[1].SumValue != 14500 {
			t.Errorf("the months: %+v %v", months, err)
		}
		if months[0].DayCount != 1 || months[1].DayCount != 3 {
			t.Errorf("a month of three days has three days: %d and %d", months[0].DayCount, months[1].DayCount)
		}

		// A range inside one week adds only the days asked for.
		narrow, _, _ := tx.ListHealthPeriods(world.ada.ID, "stepCount", "2026-03-03", "2026-03-05", health.PeriodWeek)
		if len(narrow) != 1 || *narrow[0].SumValue != 8500 || narrow[0].StartDay != "2026-03-02" {
			t.Errorf("only the days in the range: %+v", narrow)
		}

		// An average is weighted by readings; a day with an average and no
		// readings counts as one reading, so it is never left out.
		heart, unit, err := tx.ListHealthPeriods(world.ada.ID, "heartRate", "2026-03-01", "2026-04-30", health.PeriodMonth)
		if err != nil || len(heart) != 2 || unit != "count/min" {
			t.Fatalf("heart rate by month: %+v %q %v", heart, unit, err)
		}
		if want := (60.0*100 + 80.0*300) / 400; math.Abs(*heart[0].AverageValue-want) > 1e-9 || *heart[0].MinimumValue != 45 || *heart[0].MaximumValue != 120 || heart[0].SampleCount != 400 {
			t.Errorf("a month of heart rate: %+v (want average %v)", heart[0], want)
		}
		if *heart[1].AverageValue != 70 || heart[1].MinimumValue != nil {
			t.Errorf("a day with an average and no count: %+v", heart[1])
		}

		if _, err := tx.UpsertHealthDaySummaries(world.ada.ID, []*models.HealthDaySummary{
			daySummary("bodyMass", "2026-03-02", func(summary *models.HealthDaySummary) {
				summary.QuantityUnit = "kg"
				summary.AverageValue, summary.SampleCount = number(70), 10
			}),
			daySummary("bodyMass", "2026-03-03", func(summary *models.HealthDaySummary) {
				summary.QuantityUnit = "kg"
				summary.AverageValue, summary.SampleCount = number(81), 0
			}),
		}); err != nil {
			t.Fatal(err)
		}
		mass, _, err := tx.ListHealthPeriods(world.ada.ID, "bodyMass", "2026-03-01", "2026-03-31", health.PeriodMonth)
		if want := (70.0*10 + 81.0*1) / 11; err != nil || len(mass) != 1 || math.Abs(*mass[0].AverageValue-want) > 1e-9 || mass[0].DayCount != 2 {
			t.Errorf("a day with no readings is not left out of the average: %+v %v (want %v)", mass, err, want)
		}

		sleep, _, err := tx.ListHealthPeriods(world.ada.ID, "sleepAnalysis", "2026-03-01", "2026-03-31", health.PeriodWeek)
		if err != nil || len(sleep) != 1 || *sleep[0].DurationSeconds != 52200 || sleep[0].SampleCount != 10 || sleep[0].DayCount != 2 {
			t.Fatalf("a week of sleep: %+v %v", sleep, err)
		}
		if want := map[string]float64{"asleepCore": 22600, "asleepDeep": 3000, "asleepREM": 5400, "awake": 600}; len(sleep[0].CategorySeconds) != len(want) {
			t.Errorf("the seconds of each word are added: %v", sleep[0].CategorySeconds)
		} else {
			for word, seconds := range want {
				if sleep[0].CategorySeconds[word] != seconds {
					t.Errorf("%s: %v, want %v", word, sleep[0].CategorySeconds[word], seconds)
				}
			}
		}

		if empty, _, err := tx.ListHealthPeriods(world.cora.ID, "stepCount", "2026-02-01", "2026-03-31", health.PeriodDay); err != nil || len(empty) != 0 {
			t.Errorf("a person with no days: %v %v", empty, err)
		}

		// A day that cannot be right stores nothing.
		for name, bad := range map[string]*models.HealthDaySummary{
			"notaday":       daySummary("stepCount", "2026-02-30", func(*models.HealthDaySummary) {}),
			"notype":        daySummary("", "2026-03-02", func(*models.HealthDaySummary) {}),
			"negativecount": daySummary("stepCount", "2026-03-02", func(summary *models.HealthDaySummary) { summary.SampleCount = -1 }),
			"notfinite":     daySummary("stepCount", "2026-03-02", func(summary *models.HealthDaySummary) { summary.SumValue = number(math.NaN()) }),
			"notanobject":   daySummary("stepCount", "2026-03-02", func(summary *models.HealthDaySummary) { summary.CategorySecondsJSON = `"asleep"` }),
			"notnumbers":    daySummary("stepCount", "2026-03-02", func(summary *models.HealthDaySummary) { summary.CategorySecondsJSON = `{"asleep": "long"}` }),
		} {
			if _, err := tx.UpsertHealthDaySummaries(world.ada.ID, []*models.HealthDaySummary{bad}); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("%s: %v", name, err)
			}
		}
		if _, _, err := tx.ListHealthPeriods(world.ada.ID, "stepCount", "yesterday", "2026-03-31", health.PeriodDay); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a range that is not days: %v", err)
		}
	})
}

// The list of kinds has each kind a person has once, from readings or from
// days, with the first and last day and the latest day's summary.
func TestListingTheHealthTypesAPersonHas(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		morning := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)
		if _, err := tx.InsertHealthSamples(world.ada.ID, []*models.HealthSample{
			stepSample("a", morning, 1), stepSample("b", morning.Add(time.Hour), 2),
			// Readings of a kind whose days have not arrived yet.
			{HealthSampleID: "c", HealthType: "heartRate", StartedAt: morning, EndedAt: morning, QuantityValue: number(61), QuantityUnit: "count/min"},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.UpsertHealthDaySummaries(world.ada.ID, []*models.HealthDaySummary{
			daySummary("stepCount", "2026-03-01", func(summary *models.HealthDaySummary) { summary.SumValue = number(10) }),
			daySummary("stepCount", "2026-03-02", func(summary *models.HealthDaySummary) { summary.SumValue = number(3); summary.SampleCount = 2 }),
			// Days of a kind whose readings were never sent.
			daySummary("bodyMass", "2026-02-01", func(summary *models.HealthDaySummary) {
				summary.QuantityUnit = "kg"
				summary.AverageValue = number(70.5)
			}),
		}); err != nil {
			t.Fatal(err)
		}
		if err := tx.UpsertHealthCharacteristics(world.ada.ID, []*models.HealthCharacteristic{{HealthType: "bloodType", CharacteristicValue: "aPositive"}}); err != nil {
			t.Fatal(err)
		}

		listed, err := tx.ListHealthTypes(world.ada.ID)
		if err != nil || len(listed) != 3 {
			t.Fatalf("the kinds: %+v %v", listed, err)
		}
		byType := map[string]*models.HealthTypeSummary{}
		for _, each := range listed {
			byType[each.HealthType] = each
		}
		steps := byType["stepCount"]
		if steps == nil || steps.SampleCount != 2 || steps.FirstDay != "2026-03-01" || steps.LastDay != "2026-03-02" ||
			steps.LatestDaySummary == nil || *steps.LatestDaySummary.SumValue != 3 || steps.LatestDaySummary.StartDay != "2026-03-02" || steps.QuantityUnit != "count" {
			t.Errorf("steps: %+v", steps)
		}
		if heart := byType["heartRate"]; heart == nil || heart.SampleCount != 1 || heart.FirstDay != "" || heart.LatestDaySummary != nil {
			t.Errorf("readings and no days: %+v", heart)
		}
		if mass := byType["bodyMass"]; mass == nil || mass.SampleCount != 0 || mass.FirstDay != "2026-02-01" || mass.QuantityUnit != "kg" || *mass.LatestDaySummary.AverageValue != 70.5 {
			t.Errorf("days and no readings: %+v", mass)
		}
		if none, err := tx.ListHealthTypes(world.ben.ID); err != nil || len(none) != 0 {
			t.Errorf("ben has none: %v %v", none, err)
		}

		status, err := tx.GetHealthStatus(world.ada.ID)
		if err != nil || status.SampleCount != 3 || status.LastReceivedAt == nil || status.CanAgentReadHealth || status.HealthErasedAt != nil ||
			len(status.Characteristics) != 1 || status.Characteristics[0].CharacteristicValue != "aPositive" {
			t.Errorf("the status: %+v %v", status, err)
		}
		if err := tx.UpsertHealthCharacteristics(world.ada.ID, []*models.HealthCharacteristic{{HealthType: "bloodType", CharacteristicValue: "oNegative"}}); err != nil {
			t.Fatal(err)
		}
		if status, _ = tx.GetHealthStatus(world.ada.ID); status.Characteristics[0].CharacteristicValue != "oNegative" || len(status.Characteristics) != 1 {
			t.Errorf("a characteristic is replaced: %+v", status.Characteristics)
		}
		if err := tx.SetCanAgentReadHealth(world.ada.ID, true); err != nil {
			t.Fatal(err)
		}
		if status, _ = tx.GetHealthStatus(world.ada.ID); !status.CanAgentReadHealth {
			t.Error("the switch is on")
		}
		if status, _ = tx.GetHealthStatus(world.ben.ID); status.CanAgentReadHealth {
			t.Error("ben's switch is his own")
		}
		if err := tx.SetCanAgentReadHealth("nobody", true); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a person who does not exist: %v", err)
		}
		if _, err := tx.GetHealthStatus("nobody"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a person who does not exist: %v", err)
		}
	})
}

// Erasing one kind takes its readings, days, routes and characteristic and
// nothing else; erasing everything also takes every characteristic and marks
// the time. Another person's Health and the person's own sharing and switch
// are left.
func TestErasingHealthDataOfOneKindAndOfAll(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		morning := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)
		fill := func(person *models.User) {
			if _, err := tx.InsertHealthSamples(person.ID, []*models.HealthSample{
				stepSample("steps-1", morning, 1), stepSample("steps-2", morning.Add(time.Hour), 2),
				{HealthSampleID: "run-1", HealthType: "workout", StartedAt: morning, EndedAt: morning.Add(time.Hour)},
				{HealthSampleID: "weight-1", HealthType: "bodyMass", StartedAt: morning, EndedAt: morning, QuantityValue: number(70), QuantityUnit: "kg"},
			}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := tx.UpsertHealthRoutes(person.ID, []*models.HealthRoute{{HealthSampleID: "run-1", RoutePoints: [][]float64{{0, 1, 1, 1}}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.UpsertHealthDaySummaries(person.ID, []*models.HealthDaySummary{
				daySummary("stepCount", "2026-03-02", func(summary *models.HealthDaySummary) { summary.SumValue = number(3) }),
				daySummary("workout", "2026-03-02", func(summary *models.HealthDaySummary) { summary.SampleCount = 1 }),
				daySummary("bodyMass", "2026-03-02", func(summary *models.HealthDaySummary) { summary.AverageValue = number(70) }),
			}); err != nil {
				t.Fatal(err)
			}
			if err := tx.UpsertHealthCharacteristics(person.ID, []*models.HealthCharacteristic{
				{HealthType: "bloodType", CharacteristicValue: "aPositive"}, {HealthType: "wheelchairUse", CharacteristicValue: "no"},
			}); err != nil {
				t.Fatal(err)
			}
		}
		fill(world.ada)
		fill(world.ben)
		if err := tx.SetCanAgentReadHealth(world.ada.ID, true); err != nil {
			t.Fatal(err)
		}

		erased, err := tx.EraseHealthData(world.ada.ID, "workout")
		if err != nil || erased != 1 {
			t.Fatalf("erasing the workouts: %d, %v", erased, err)
		}
		if _, err := tx.GetHealthRoute(world.ada.ID, "run-1"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("the route went with the workouts: %v", err)
		}
		listed, _ := tx.ListHealthTypes(world.ada.ID)
		types := []string{}
		for _, each := range listed {
			types = append(types, each.HealthType)
		}
		slices.Sort(types)
		if !slices.Equal(types, []string{"bodyMass", "stepCount"}) {
			t.Errorf("only the workouts went: %v", types)
		}
		if status, _ := tx.GetHealthStatus(world.ada.ID); status.HealthErasedAt != nil || len(status.Characteristics) != 2 {
			t.Errorf("erasing a kind is not erasing it all: %+v", status)
		}

		if erased, err = tx.EraseHealthData(world.ada.ID, "stepCount"); err != nil || erased != 2 {
			t.Fatalf("erasing the steps: %d, %v", erased, err)
		}
		if days, _, _ := tx.ListHealthPeriods(world.ada.ID, "stepCount", "2026-03-01", "2026-03-31", health.PeriodDay); len(days) != 0 {
			t.Errorf("the days of the steps went: %v", days)
		}
		// Erasing the kind a characteristic is takes the characteristic.
		if _, err := tx.EraseHealthData(world.ada.ID, "bloodType"); err != nil {
			t.Fatal(err)
		}
		if status, _ := tx.GetHealthStatus(world.ada.ID); len(status.Characteristics) != 1 || status.Characteristics[0].HealthType != "wheelchairUse" {
			t.Errorf("the characteristic went: %+v", status.Characteristics)
		}

		before := time.Now().Add(-time.Second)
		if erased, err = tx.EraseHealthData(world.ada.ID, ""); err != nil || erased != 1 {
			t.Fatalf("erasing it all: %d, %v", erased, err)
		}
		status, _ := tx.GetHealthStatus(world.ada.ID)
		if status.SampleCount != 0 || len(status.Characteristics) != 0 || status.LastReceivedAt != nil ||
			status.HealthErasedAt == nil || status.HealthErasedAt.Before(before) || !status.CanAgentReadHealth {
			t.Errorf("after erasing it all: %+v", status)
		}
		if listed, _ := tx.ListHealthTypes(world.ada.ID); len(listed) != 0 {
			t.Errorf("no kinds are left: %v", listed)
		}

		// Ben has all of his.
		other, _ := tx.GetHealthStatus(world.ben.ID)
		if other.SampleCount != 4 || len(other.Characteristics) != 2 || other.HealthErasedAt != nil {
			t.Errorf("ben's Health is his own: %+v", other)
		}
		if route, err := tx.GetHealthRoute(world.ben.ID, "run-1"); err != nil || len(route.RoutePoints) != 1 {
			t.Errorf("ben's route: %v", err)
		}

		if _, err := tx.EraseHealthData("nobody", ""); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a person who does not exist: %v", err)
		}
		if _, err := tx.EraseHealthData(world.ada.ID, "step count"); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a kind that cannot be one: %v", err)
		}
	})
}
