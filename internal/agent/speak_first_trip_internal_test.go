package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Speaking about a trip leaves a follow-up that says so, which is what keeps
// the agent from speaking about it again.
func TestATripSpokenAboutIsRememberedAsTold(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "trip-ada"})
		if err != nil {
			t.Fatal(err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		trip := &tripNote{EventID: "event", Summary: "Lake weekend", Place: "Example Lake", FirstOn: "2030-03-02"}
		for attempt := 0; attempt < 2; attempt++ {
			if err := noteTripTold(tx, found.ID, trip, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		followUps, err := tx.ListOpenAgentFollowUps(found.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(followUps) != 1 {
			t.Fatalf("%d follow-ups, want one", len(followUps))
		}
		kept := followUps[0]
		if kept.FollowUpKey != followUpKeyOfTrip("event") || kept.ToldState == "" || kept.FollowUpKind != models.AgentFollowUpEvent || kept.DueOn != "2030-03-02" {
			t.Fatalf("kept %+v", kept)
		}
	})
}
