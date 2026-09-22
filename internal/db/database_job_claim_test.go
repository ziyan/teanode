package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

func TestOldJobClaimCannotFinishAReplacement(test *testing.T) {
	for _, nextInstance := range []string{"first-instance", "other-instance"} {
		test.Run(nextInstance, func(test *testing.T) {
			database, closeDatabase := dbtest.AcquireDatabase(test)
			defer closeDatabase()
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				user, err := transaction.CreateUser(&models.User{Username: "claim-fixture"})
				if err != nil {
					test.Fatal(err)
				}
				owner, err := transaction.CreateAgent(&models.Agent{UserID: user.ID, Enabled: true})
				if err != nil {
					test.Fatal(err)
				}
				if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: owner.ID, Kind: models.AgentJobNoop}); err != nil {
					test.Fatal(err)
				}
				now := time.Now()
				first, err := transaction.ClaimAgentJobs("first-instance", 1, now)
				if err != nil || len(first) != 1 {
					test.Fatalf("first claim: %v", err)
				}
				if _, err := transaction.ReleaseStaleAgentJobs(now.Add(time.Second)); err != nil {
					test.Fatal(err)
				}
				second, err := transaction.ClaimAgentJobs(nextInstance, 1, now.Add(time.Minute))
				if err != nil || len(second) != 1 {
					test.Fatalf("second claim: %v", err)
				}
				if first[0].ClaimID == "" || first[0].ClaimID == second[0].ClaimID {
					test.Fatal("claim identity was reused")
				}
				outcome := &db.AgentJobOutcome{JobStatus: models.AgentJobDone, FinishedAt: now.Add(time.Minute)}
				if hasFinished, err := transaction.FinishAgentJob(first[0].ID, first[0].ClaimID, outcome); err != nil || hasFinished {
					test.Fatalf("old claim overwrote replacement: %v", err)
				}
				if hasRetried, err := transaction.RetryAgentJob(second[0].ID); err != nil || hasRetried {
					test.Fatalf("manual retry changed a running job: %v", err)
				}
				if _, err := transaction.FinishAgentJob(second[0].ID, "", outcome); err == nil {
					test.Fatal("empty claim identity was accepted")
				}
				if hasFinished, err := transaction.FinishAgentJob(second[0].ID, second[0].ClaimID, outcome); err != nil || !hasFinished {
					test.Fatalf("current claim did not finish: %v", err)
				}
			})
		})
	}
}

func TestJobClaimMigrationCanBeReversedAndReapplied(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var migration *migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0090_agent_job_claims" {
			migration = &candidate
			break
		}
	}
	if migration == nil {
		test.Fatal("job claim migration is missing")
	}
	var jobId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		user, err := transaction.CreateUser(&models.User{Username: "migration-fixture"})
		if err != nil {
			test.Fatal(err)
		}
		owner, err := transaction.CreateAgent(&models.Agent{UserID: user.ID, Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		job, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: owner.ID, Kind: models.AgentJobNoop})
		if err != nil {
			test.Fatal(err)
		}
		jobId = job.ID
		if _, err := transaction.ClaimAgentJobs("fixture", 1, time.Now()); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.Exec(test, database, migration.ReverseSQL)
	if jobStatus := dbtest.QueryString(test, database, `SELECT status FROM agent_job`); jobStatus != "queued" {
		test.Fatalf("downgrade stranded work: %s", jobStatus)
	}
	dbtest.Exec(test, database, migration.SQL)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		claimed, err := transaction.ClaimAgentJobs("fixture", 1, time.Now())
		if err != nil || len(claimed) != 1 || claimed[0].ID != jobId || claimed[0].FailureCount != 0 || claimed[0].ClaimID == "" {
			test.Fatalf("reapplied migration lost job: %v %+v", err, claimed)
		}
	})
}
