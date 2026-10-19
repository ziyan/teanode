package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// How many readings a person has, and when the last arrived, are kept by the
// database as readings come and go, and the counts a page, a report and the
// agent read are those and not a count of the readings.
func TestHealthTotalsFollowEveryInsertAndDelete(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		morning := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)
		for _, person := range []*models.User{world.ada, world.ben} {
			if _, err := tx.InsertHealthSamples(person.ID, []*models.HealthSample{
				stepSample("a", morning, 1), stepSample("b", morning.Add(time.Hour), 2), stepSample("c", morning.Add(2*time.Hour), 3),
				{HealthSampleID: "w", HealthType: "bodyMass", StartedAt: morning, EndedAt: morning, QuantityValue: number(70), QuantityUnit: "kg"},
			}); err != nil {
				t.Fatal(err)
			}
		}
		// A reading sent again is not counted again.
		if _, err := tx.InsertHealthSamples(world.ada.ID, []*models.HealthSample{stepSample("a", morning, 1), stepSample("d", morning.Add(3*time.Hour), 4)}); err != nil {
			t.Fatal(err)
		}
		counts := func(person *models.User) map[string]int {
			listed, err := tx.ListHealthTypes(person.ID)
			if err != nil {
				t.Fatal(err)
			}
			byType := map[string]int{}
			for _, each := range listed {
				byType[each.HealthType] = each.SampleCount
			}
			return byType
		}
		if got := counts(world.ada); got["stepCount"] != 4 || got["bodyMass"] != 1 || len(got) != 2 {
			t.Errorf("ada's readings by kind: %v", got)
		}
		if got := counts(world.ben); got["stepCount"] != 3 || got["bodyMass"] != 1 {
			t.Errorf("ben's readings by kind: %v", got)
		}
		status, err := tx.GetHealthStatus(world.ada.ID)
		if err != nil || status.SampleCount != 5 || status.LastReceivedAt == nil {
			t.Fatalf("the status after inserting: %+v %v", status, err)
		}
		names, err := tx.ListHealthTypeNames(world.ada.ID)
		if err != nil || len(names) != 2 || names[0] != "bodyMass" || names[1] != "stepCount" {
			t.Errorf("the kinds with readings: %v %v", names, err)
		}

		if _, err := tx.DeleteHealthSamples(world.ada.ID, []string{"a", "b", "w", "never-had-this"}); err != nil {
			t.Fatal(err)
		}
		if got := counts(world.ada); got["stepCount"] != 2 || len(got) != 1 {
			t.Errorf("a kind with none left is gone: %v", got)
		}
		if got := counts(world.ben); got["stepCount"] != 3 || got["bodyMass"] != 1 {
			t.Errorf("deleting ada's readings leaves ben's counts: %v", got)
		}
		if status, _ = tx.GetHealthStatus(world.ada.ID); status.SampleCount != 2 {
			t.Errorf("the status after deleting: %+v", status)
		}

		// An erase of one kind and then of everything.
		if _, err := tx.EraseHealthData(world.ben.ID, "stepCount"); err != nil {
			t.Fatal(err)
		}
		if got := counts(world.ben); len(got) != 1 || got["bodyMass"] != 1 {
			t.Errorf("erasing a kind: %v", got)
		}
		if _, err := tx.EraseHealthData(world.ada.ID, ""); err != nil {
			t.Fatal(err)
		}
		if status, _ = tx.GetHealthStatus(world.ada.ID); status.SampleCount != 0 || status.LastReceivedAt != nil {
			t.Errorf("after erasing it all: %+v", status)
		}
		if names, _ = tx.ListHealthTypeNames(world.ada.ID); len(names) != 0 {
			t.Errorf("no kinds are left: %v", names)
		}

		// The page reads the total the database keeps when no range is given,
		// and only counts when it is asked to.
		page, err := tx.ListHealthSamples(world.ben.ID, "bodyMass", nil, nil, 10, 0, true)
		if err != nil || page.TotalCount != 1 || page.IsTotalCountCapped || page.HasMore || len(page.Samples) != 1 {
			t.Errorf("a page with its count: %+v %v", page, err)
		}
		page, err = tx.ListHealthSamples(world.ben.ID, "bodyMass", nil, nil, 10, 0, false)
		if err != nil || page.TotalCount != 0 || len(page.Samples) != 1 {
			t.Errorf("a page without a count: %+v %v", page, err)
		}
	})
}

// The settings of the account are read without the readings.
func TestHealthChoicesAreReadFromTheAccount(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		choices, err := tx.GetHealthChoices(world.ada.ID)
		if err != nil || choices.CanAgentReadHealth || choices.HealthErasedAt != nil {
			t.Fatalf("a new account: %+v %v", choices, err)
		}
		if err := tx.SetCanAgentReadHealth(world.ada.ID, true); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.EraseHealthData(world.ada.ID, ""); err != nil {
			t.Fatal(err)
		}
		if choices, err = tx.GetHealthChoices(world.ada.ID); err != nil || !choices.CanAgentReadHealth || choices.HealthErasedAt == nil {
			t.Errorf("the switch and the erase: %+v %v", choices, err)
		}
		if _, err := tx.GetHealthChoices("nobody"); err == nil {
			t.Error("a person who does not exist has no choices")
		}
	})
}

// A phone that deleted a reading and never got the answer is answered the
// same when it asks again, for 30 days, and no longer.
func TestADeletedReadingIsRememberedFor30Days(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	userId := dbtest.CreateUser(t, database, "forgetful")
	morning := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.InsertHealthSamples(userId, []*models.HealthSample{
			stepSample("old", morning, 1), stepSample("recent", morning.Add(time.Hour), 2), stepSample("other", morning.Add(2*time.Hour), 3),
		}); err != nil {
			t.Fatal(err)
		}
		if deleted, err := tx.DeleteHealthSamples(userId, []string{"old", "recent"}); err != nil || len(deleted) != 2 {
			t.Fatalf("the first delete: %v %v", deleted, err)
		}
	})
	dbtest.Exec(t, database, `UPDATE "health_deleted_sample" SET "deleted_at" = now() - interval '31 days' WHERE "health_sample_id" = 'old'`)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		// Deleting something else forgets what is older than 30 days.
		deleted, err := tx.DeleteHealthSamples(userId, []string{"other", "old", "recent"})
		if err != nil || len(deleted) != 2 || !deleted[0].StartedAt.Equal(morning.Add(time.Hour)) || !deleted[1].StartedAt.Equal(morning.Add(2*time.Hour)) {
			t.Errorf("the 31 days old reading is forgotten, the recent one is not: %+v %v", deleted, err)
		}
	})
	// Erasing a kind forgets what was deleted of it.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EraseHealthData(userId, "stepCount"); err != nil {
			t.Fatal(err)
		}
		if deleted, err := tx.DeleteHealthSamples(userId, []string{"recent", "other"}); err != nil || len(deleted) != 0 {
			t.Errorf("an erased kind leaves nothing behind: %v %v", deleted, err)
		}
	})
}

// A count of a range stops at a limit, so that a page of millions of readings
// does not wait for all of them, and says so; a count of every reading of a
// kind is the total the database keeps and is exact.
func TestACountOfARangeOfHealthSamplesStopsAtALimit(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	userId := dbtest.CreateUser(t, database, "manyreadings")
	extra := 500
	dbtest.Exec(t, database, `INSERT INTO "health_sample" ("id", "user_id", "health_sample_id", "health_type", "started_at", "ended_at", "received_at")
		SELECT 'many-' || n, '`+userId+`', 'reading-' || n, 'heartRate', timestamptz '2026-01-01 00:00:00+00' + n * interval '1 second',
			timestamptz '2026-01-01 00:00:00+00' + n * interval '1 second', now()
		FROM generate_series(1, `+itoa(models.HealthSampleCountMost+extra)+`) AS n`)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		from := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
		page, err := tx.ListHealthSamples(userId, "heartRate", &from, nil, 50, 0, true)
		if err != nil || len(page.Samples) != 50 || !page.HasMore || page.TotalCount != models.HealthSampleCountMost || !page.IsTotalCountCapped {
			t.Errorf("a range of more than the limit: %d samples, more %v, total %d, capped %v, %v",
				len(page.Samples), page.HasMore, page.TotalCount, page.IsTotalCountCapped, err)
		}
		until := time.Date(2026, 1, 1, 0, 1, 40, 0, time.UTC)
		page, err = tx.ListHealthSamples(userId, "heartRate", &from, &until, 50, 0, true)
		if err != nil || page.TotalCount != 99 || page.IsTotalCountCapped {
			t.Errorf("a small range is counted exactly: %+v %v", page, err)
		}
		page, err = tx.ListHealthSamples(userId, "heartRate", nil, nil, 50, 0, true)
		if err != nil || page.TotalCount != models.HealthSampleCountMost+extra || page.IsTotalCountCapped {
			t.Errorf("every reading of the kind is the exact total: %+v %v", page.TotalCount, err)
		}
		status, err := tx.GetHealthStatus(userId)
		if err != nil || status.SampleCount != models.HealthSampleCountMost+extra {
			t.Errorf("the readings kept: %+v %v", status, err)
		}
	})
}
