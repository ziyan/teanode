package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A recording belongs to the day it says it was made, and to the day it
// arrived when it does not say; one that is deleted belongs to no day.
func TestRecordingsBetweenPlacesARecordingWhereItWasMade(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	zone := time.FixedZone("test", 9*3600)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	var userId string
	ids := map[string]string{}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner := newCircleTestUser(t, tx, "recording-ada")
		userId = owner.ID
		for _, name := range []string{"standup", "walk", "no-date", "yesterday", "deleted"} {
			recording, err := tx.CreateRecording(&models.Recording{
				UserID: owner.ID, ClientRecordingID: name, RecordingTitle: name, RecordingOrigin: models.RecordingOriginAudioImport,
			})
			if err != nil {
				t.Fatal(err)
			}
			ids[name] = recording.ID
		}
	})
	setRecordedAt := func(name string, when time.Time) {
		dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "recording" SET "recorded_at" = '%s' WHERE "id" = '%s'`, when.UTC().Format(time.RFC3339), ids[name]))
	}
	setRecordedAt("standup", at(22, 9, 30))
	setRecordedAt("walk", at(22, 23, 15))
	setRecordedAt("yesterday", at(21, 23, 59))
	setRecordedAt("deleted", at(22, 12, 0))
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "recording" SET "created_at" = '%s' WHERE "id" = '%s'`, at(22, 7, 0).UTC().Format(time.RFC3339), ids["no-date"]))
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.DeleteRecording(userId, ids["deleted"]); err != nil {
			t.Fatal(err)
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := tx.ListRecordingsBetween(userId, at(22, 0, 0), at(23, 0, 0), 100)
		if err != nil {
			t.Fatal(err)
		}
		titles := []string{}
		for _, recording := range found {
			titles = append(titles, recording.RecordingTitle)
		}
		want := []string{"no-date", "standup", "walk"}
		if fmt.Sprint(titles) != fmt.Sprint(want) {
			t.Fatalf("recordings of the day, earliest first: %v, want %v", titles, want)
		}
		if _, err := tx.ListRecordingsBetween(userId, at(22, 0, 0), at(23, 0, 0), 0); err == nil {
			t.Fatal("a limit of none is refused")
		}
	})
}
