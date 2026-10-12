package db_test

import (
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"testing"
	"time"
)

func TestDisabledMailboxJobsKeepTheirAttemptsAndResume(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "queue-owner")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		agent, err := transaction.CreateAgent(&models.Agent{UserID: userId, Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Personal"})
		if err != nil {
			test.Fatal(err)
		}
		paused, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: agent.ID, MailboxID: mailbox.ID, Kind: models.AgentJobTriage, SubjectID: "message"})
		if err != nil {
			test.Fatal(err)
		}
		core, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: agent.ID, Kind: models.AgentJobNoop, SubjectID: "assistant"})
		if err != nil {
			test.Fatal(err)
		}
		jobs, err := transaction.ClaimAgentJobsKeepingRoom("instance", 10, 10, time.Now(), true)
		if err != nil {
			test.Fatal(err)
		}
		if len(jobs) != 1 || jobs[0].ID != core.ID {
			test.Fatalf("disabled mailbox work claimed: %+v", jobs)
		}
		kept, err := transaction.GetAgentJob(paused.ID)
		if err != nil {
			test.Fatal(err)
		}
		if kept.Attempts != 0 || kept.Status != models.AgentJobQueued {
			test.Fatalf("paused work changed: %+v", kept)
		}
		jobs, err = transaction.ClaimAgentJobsKeepingRoom("instance", 10, 10, time.Now(), false)
		if err != nil {
			test.Fatal(err)
		}
		if len(jobs) != 1 || jobs[0].ID != paused.ID || jobs[0].Attempts != 1 {
			test.Fatalf("retained work did not resume: %+v", jobs)
		}
	})
}
