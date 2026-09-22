package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

type jobWorkerFixture struct {
	database db.Database
	worker   *agent.Agent
	jobId    string
}

func newJobWorkerFixture(test *testing.T) *jobWorkerFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	isDreamingEnabled := false
	configuration.Agent.Features.Dreaming = &isDreamingEnabled
	worker := agent.New(&agent.Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "worker-fixture"})
	fixture := &jobWorkerFixture{database: database, worker: worker}
	test.Cleanup(func() { worker.Stop(); closeDatabase() })
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		user, err := transaction.CreateUser(&models.User{Username: "worker-fixture"})
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
		fixture.jobId = job.ID
	})
	return fixture
}

func TestWorkerRetriesAfterRepeatedDeferrals(test *testing.T) {
	fixture := newJobWorkerFixture(test)
	claimCount := 0
	fixture.worker.Register(models.AgentJobNoop, func(ctx context.Context, run *agent.Run) error {
		claimCount++
		if claimCount <= 5 {
			return &agent.Deferral{Until: run.Now.Add(time.Minute), Reason: "budget"}
		}
		return errors.New("temporary fixture failure")
	})
	now := time.Now()
	for claimIndex := 0; claimIndex < 6; claimIndex++ {
		if err := fixture.worker.TickAt(context.Background(), now.Add(time.Duration(claimIndex)*2*time.Minute)); err != nil {
			test.Fatal(err)
		}
		fixture.worker.Wait()
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		job, err := transaction.GetAgentJob(fixture.jobId)
		if err != nil {
			test.Fatal(err)
		}
		if job.Status != models.AgentJobQueued || job.Attempts != 6 || job.FailureCount != 1 || job.NotBefore == nil || job.ClaimID != "" {
			test.Fatalf("deferrals exhausted retries: %+v", job)
		}
	})
}

func TestWorkerRecordsCancellationAfterShutdown(test *testing.T) {
	fixture := newJobWorkerFixture(test)
	hasStarted := make(chan struct{})
	fixture.worker.Register(models.AgentJobNoop, func(ctx context.Context, run *agent.Run) error {
		close(hasStarted)
		<-ctx.Done()
		return ctx.Err()
	})
	if err := fixture.worker.TickAt(context.Background(), time.Now()); err != nil {
		test.Fatal(err)
	}
	select {
	case <-hasStarted:
	case <-time.After(5 * time.Second):
		test.Fatal("worker did not start")
	}
	hasStopped := make(chan struct{})
	go func() { fixture.worker.Stop(); close(hasStopped) }()
	select {
	case <-hasStopped:
	case <-time.After(5 * time.Second):
		test.Fatal("worker did not stop")
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		job, err := transaction.GetAgentJob(fixture.jobId)
		if err != nil {
			test.Fatal(err)
		}
		if job.Status != models.AgentJobQueued || job.FailureCount != 0 || job.ClaimID != "" {
			test.Fatalf("shutdown stranded a claim: %+v", job)
		}
	})
}
