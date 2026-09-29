package apigraph

import (
	"context"
	"errors"
	"testing"
	"time"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A survey started through the API is queued and answered at once, wakes
// no conversation, is listed and read back by the person whose it is and
// by nobody else, and stops.
func TestASurveyStartedThroughTheAPIIsQueuedReadAndStopped(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var person, stranger *models.User
	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "surveyor", Name: "Example Surveyor"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		created, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		agentId = created.ID
		if stranger, err = tx.CreateUser(&models.User{Username: "stranger", Name: "Example Stranger"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if _, err := tx.CreateAgent(&models.Agent{UserID: stranger.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	worker := agentpackage.New(&agentpackage.Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	resolver := &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{Agent: worker}}
	allowed := func(user *models.User) *api.Principal {
		return &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	}
	within := func(principal *api.Principal, function func(ctx context.Context)) {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			function(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx))
		})
	}

	// Nothing can run a survey yet: refused at once rather than queued.
	within(allowed(person), func(ctx context.Context) {
		if _, err := resolver.StartAgentSurvey(ctx, StartAgentSurveyArguments{Question: "What stands out?"}); !errors.Is(err, agentpackage.ErrUnavailable) {
			t.Fatalf("a survey nothing can run: %v", err)
		}
	})
	worker.SetOperationsFactory(func(context.Context, *models.User) (agentpackage.Operations, error) { return nil, nil })

	var started *AgentBackgroundWorkView
	within(allowed(person), func(ctx context.Context) {
		if _, err := resolver.StartAgentSurvey(ctx, StartAgentSurveyArguments{Question: " "}); err == nil {
			t.Errorf("a survey of no question")
		}
		var err error
		if started, err = resolver.StartAgentSurvey(ctx, StartAgentSurveyArguments{Question: "How do the orchards fit together?", ScopePath: "themes/orchards"}); err != nil {
			t.Fatalf("StartAgentSurvey: %s", err)
		}
	})
	if started.WorkStatus != "queued" || started.WorkKind != "survey" || started.ConversationID != "" || started.ScopePath != "themes/orchards" ||
		started.Title != "Survey: How do the orchards fit together?" || started.RunIDs == nil {
		t.Fatalf("queued, waking nothing: %+v", started)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		queued, err := tx.CountAgentJobs(&db.AgentJobFilter{AgentID: agentId, Kinds: []models.AgentJobKind{models.AgentJobBackground}, Statuses: []models.AgentJobStatus{models.AgentJobQueued}})
		if err != nil || queued != 1 {
			t.Fatalf("its job is queued: %d %v", queued, err)
		}
	})

	within(allowed(person), func(ctx context.Context) {
		listed, err := resolver.ListAgentBackgroundWork(ctx, ListAgentBackgroundWorkArguments{})
		if err != nil || len(listed) != 1 || listed[0].ID != started.ID {
			t.Fatalf("listed: %+v %v", listed, err)
		}
		read, err := resolver.GetAgentBackgroundWork(ctx, GetAgentBackgroundWorkArguments{ID: started.ID})
		if err != nil || read.Question != "How do the orchards fit together?" {
			t.Fatalf("read: %+v %v", read, err)
		}
	})
	within(allowed(stranger), func(ctx context.Context) {
		if _, err := resolver.GetAgentBackgroundWork(ctx, GetAgentBackgroundWorkArguments{ID: started.ID}); err == nil {
			t.Errorf("somebody else read it")
		}
		if _, err := resolver.StopAgentBackgroundWork(ctx, GetAgentBackgroundWorkArguments{ID: started.ID}); err == nil {
			t.Errorf("somebody else stopped it")
		}
		if listed, err := resolver.ListAgentBackgroundWork(ctx, ListAgentBackgroundWorkArguments{}); err != nil || len(listed) != 0 {
			t.Errorf("somebody else listed it: %+v %v", listed, err)
		}
	})
	within(&api.Principal{User: person, Permissions: models.NewEffectivePermissions(nil)}, func(ctx context.Context) {
		if _, err := resolver.ListAgentBackgroundWork(ctx, ListAgentBackgroundWorkArguments{}); err == nil {
			t.Errorf("listed without agent:use")
		}
	})
	within(allowed(person), func(ctx context.Context) {
		stopped, err := resolver.StopAgentBackgroundWork(ctx, GetAgentBackgroundWorkArguments{ID: started.ID})
		if err != nil || stopped.WorkStatus != "stopped" || stopped.FinishedAt == nil {
			t.Fatalf("stopped: %+v %v", stopped, err)
		}
	})
}
