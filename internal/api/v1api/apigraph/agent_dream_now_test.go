package apigraph

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"

	agentpackage "github.com/ziyan/teanode/internal/agent"
)

// A dream asked for runs now: outside the agent's dream hours, and a
// minute after the person last spoke. Those rules are for the dreams
// nobody asked for, and they kept the button from doing anything until
// the sweep's own conditions came round. Asking twice queues one dream.
func TestADreamAskedForIsQueuedAtOnce(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var person *models.User
	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "dreamer", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		// Hours that do not include the present, whatever time the test runs.
		later := time.Now().Add(3 * time.Hour)
		created, err := tx.CreateAgent(&models.Agent{
			UserID: person.ID, Enabled: true, Name: "Bertie",
			DreamFrom: later.Format("15:04"), DreamUntil: later.Add(time.Hour).Format("15:04"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		agentId = created.ID
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	isDreaming := true
	configuration.Agent.Features.Dreaming = &isDreaming
	worker := agentpackage.New(&agentpackage.Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	resolver := &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{Agent: worker}}
	principal := &api.Principal{User: person, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	ask := func() {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)
			if _, err := resolver.DreamAgentNow(ctx, DreamAgentNowArguments{}); err != nil {
				t.Fatalf("DreamAgentNow: %s", err)
			}
		})
	}
	ask()
	ask()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		open, err := tx.CountAgentJobs(&db.AgentJobFilter{
			AgentID:  agentId,
			Kinds:    []models.AgentJobKind{models.AgentJobDream},
			Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning},
		})
		if err != nil {
			t.Fatalf("CountAgentJobs: %s", err)
		}
		if open != 1 {
			t.Fatalf("one dream is queued at once outside the agent's hours, not %d", open)
		}
	})
}
