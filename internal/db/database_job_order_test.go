package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// What a person is waiting for goes before the reading.
//
// The queue was the order things were made in and nothing else. A server
// works three jobs at once, the nightly reading holds one of them for as
// long as it runs, and one pass over a large source holds another for up
// to forty minutes. So during an ingest of a hundred and fifty thousand
// documents -- days, not minutes -- a message arriving found four ingest
// jobs ahead of it and waited an hour to be triaged.
func TestAPersonsWorkIsClaimedBeforeTheReading(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agent *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if agent, err = tx.CreateAgent(&models.Agent{UserID: user.ID, Name: "Bertie", Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})

	// Four long background jobs, then the message. Distinct subjects,
	// because a job for the same subject coalesces with the one waiting.
	var wanted string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, kind := range []models.AgentJobKind{
			models.AgentJobIngest, models.AgentJobIngest, models.AgentJobDream, models.AgentJobBackfill,
		} {
			if _, err := tx.EnqueueAgentJob(&models.AgentJob{
				AgentID: agent.ID, Kind: kind, SubjectID: string(kind) + time.Now().Format(".000000000"),
			}); err != nil {
				t.Fatalf("EnqueueAgentJob %s: %s", kind, err)
			}
		}
		triage, err := tx.EnqueueAgentJob(&models.AgentJob{
			AgentID: agent.ID, Kind: models.AgentJobTriage, SubjectID: "a message that just arrived",
		})
		if err != nil {
			t.Fatalf("EnqueueAgentJob triage: %s", err)
		}
		wanted = triage.ID
	})

	// Three slots, which is what the server works with.
	var claimed []*models.AgentJob
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if claimed, err = tx.ClaimAgentJobs("instance-a", 3, time.Now()); err != nil {
			t.Fatalf("ClaimAgentJobs: %s", err)
		}
	})
	if len(claimed) != 3 {
		t.Fatalf("three slots, three jobs: %d", len(claimed))
	}
	if claimed[0].ID != wanted {
		t.Fatalf("the message waits behind the reading: first claimed was %s, not the triage", claimed[0].Kind)
	}
	// And the reading is not pushed aside, only put second: the other two
	// slots are still filled, in the order they were made.
	for _, job := range claimed[1:] {
		if job.Kind == models.AgentJobTriage {
			t.Fatalf("only one job was the person's: %+v", claimed)
		}
	}
}
