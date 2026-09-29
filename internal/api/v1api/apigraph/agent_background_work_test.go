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
	"github.com/ziyan/teanode/internal/util/graphapi"
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

// Operations narrowed to a limit say the narrower set and are refused
// every call beyond it, whatever the person may do: what a subagent in
// the background is held to, the permissions of the turn that started it.
func TestNarrowedOperationsAreRefusedBeyondTheirLimit(t *testing.T) {
	endpoint, _, reader := mcpEndpoint(t)
	graphApi := graphapi.New()
	var query Query = endpoint
	var mutation Mutation = endpoint
	if err := graphApi.Register(&query, &mutation, nil); err != nil {
		t.Fatalf("cannot register the schema: %s", err)
	}
	schema, err := graphApi.Build()
	if err != nil {
		t.Fatalf("cannot build the schema: %s", err)
	}
	endpoint.schema = schema

	var permissions *models.EffectivePermissions
	dbtest.RunTransactionOn(t, endpoint.database, func(tx db.Transaction) {
		if permissions, err = tx.EffectivePermissions(reader.ID); err != nil {
			t.Fatal(err)
		}
	})
	if !permissions.Has(models.PermissionAgentUse) || !permissions.Has(models.PermissionMailRead) {
		t.Fatalf("the reader may talk to their agent and read their mail: %+v", permissions)
	}
	operations := &agentOperations{graph: endpoint, user: reader, permissions: permissions}
	const listing = `query { ListAgentBackgroundWork { id } }`
	if err := operations.Execute(context.Background(), listing, nil, nil); err != nil {
		t.Fatalf("the person's own operations list their work: %s", err)
	}

	readingOnly := models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})
	narrowed := operations.NarrowedTo(readingOnly)
	if narrowed.Permissions().Has(models.PermissionAgentUse) || !narrowed.Permissions().Has(models.PermissionMailRead) {
		t.Fatalf("narrowed to reading mail: %+v", narrowed.Permissions())
	}
	if err := narrowed.Execute(context.Background(), listing, nil, nil); err == nil {
		t.Fatalf("a call beyond the limit was made")
	}
	if !operations.Permissions().Has(models.PermissionAgentUse) || operations.permissionLimit != nil {
		t.Fatalf("narrowing changed the operations it was made from")
	}
}
