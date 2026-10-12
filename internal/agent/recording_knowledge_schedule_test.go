package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingIngestUsesConfiguredSourceSchedule(test *testing.T) {
	fixture := newRecordingCurationFixture(test, 1)
	var source *models.AgentKnowledgeSource
	run := &Run{settings: fixture.worker.settings}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		run.Owner, err = transaction.GetUser(fixture.userId)
		if err != nil {
			test.Fatal(err)
		}
		run.Agent, err = transaction.GetAgentByUser(fixture.userId)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(fixture.userId, fixture.source.ID); err != nil {
			test.Fatal(err)
		}
		source, err = transaction.GetAgentSourceByName(run.Agent.ID, "recording:"+fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
	})
	for _, sourceCron := range []string{"*/15 * * * *", ""} {
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			source.Cron = sourceCron
			var err error
			source, err = transaction.PutAgentSource(source)
			if err != nil {
				test.Fatal(err)
			}
		})
		expectedNextRun := fixture.worker.nextRunOf(source, run.Owner)
		if err := fixture.worker.runRecordingIngest(test.Context(), run, source); err != nil {
			test.Fatal(err)
		}
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			actual, err := transaction.GetAgentSource(run.Agent.ID, source.ID)
			if err != nil {
				test.Fatal(err)
			}
			if expectedNextRun.IsZero() {
				if actual.NextRunAt != nil {
					test.Fatal("unscheduled recording was forced into a daily schedule")
				}
			} else if actual.NextRunAt == nil || actual.NextRunAt.Sub(expectedNextRun).Abs() > time.Second {
				test.Fatal("recording source ignored its configured schedule", actual.NextRunAt, expectedNextRun)
			}
		})
	}
}
