package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The general put-back of jobs an instance never finished leaves the
// night and the ingest alone: both may legitimately run past fifteen
// minutes, and each is put back on its own, longer bound.
//
// An ingest page of a large source read through a script took
// twenty-four minutes, was put back at fifteen while still running, and
// ran a second time beside the first; with both holding a slot the
// night could not claim one, and the reading stopped.
func TestTheGeneralPutBackLeavesTheNightAndTheIngestAlone(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	claimed := time.Now().Add(-20 * time.Minute)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		for _, kind := range []models.AgentJobKind{models.AgentJobTriage, models.AgentJobDream, models.AgentJobIngest} {
			if _, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: agent.ID, Kind: kind, SubjectID: string(kind)}); err != nil {
				t.Fatalf("EnqueueAgentJob(%s): %s", kind, err)
			}
		}
		jobs, err := tx.ClaimAgentJobs("instance-a", 10, claimed)
		if err != nil || len(jobs) != 3 {
			t.Fatalf("ClaimAgentJobs: %d jobs, %v", len(jobs), err)
		}

		released, err := tx.ReleaseStaleAgentJobs(claimed.Add(15 * time.Minute))
		if err != nil || released != 1 {
			t.Fatalf("the general put-back releases the triage job and nothing else: %d, %v", released, err)
		}
		released, err = tx.ReleaseStaleAgentJobsOfKind(models.AgentJobIngest, claimed.Add(-time.Minute))
		if err != nil || released != 0 {
			t.Fatalf("an ingest inside its own bound stays claimed: %d, %v", released, err)
		}
		released, err = tx.ReleaseStaleAgentJobsOfKind(models.AgentJobIngest, claimed.Add(time.Minute))
		if err != nil || released != 1 {
			t.Fatalf("an ingest past its own bound is put back: %d, %v", released, err)
		}
	})
}
