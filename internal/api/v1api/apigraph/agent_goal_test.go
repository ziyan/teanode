package apigraph

import (
	"context"
	"testing"
	"time"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A goal started through the API is the person's: listed and read back
// with what it made, told and closed by them, and by nobody else.
func TestAGoalStartedThroughTheAPIIsListedReadAndClosed(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var person, stranger *models.User
	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "keeper", Name: "Example Keeper"}); err != nil {
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

	var started *AgentGoalView
	within(allowed(person), func(ctx context.Context) {
		if _, err := resolver.StartAgentGoal(ctx, StartAgentGoalArguments{GoalTitle: "Nothing", GoalDescription: " "}); err == nil {
			t.Errorf("a goal with no description")
		}
		var err error
		if started, err = resolver.StartAgentGoal(ctx, StartAgentGoalArguments{GoalTitle: "Boiler reply", GoalDescription: "Watch for the landlord's reply about the boiler; ask me if none by Friday."}); err != nil {
			t.Fatalf("StartAgentGoal: %s", err)
		}
	})
	if started.GoalState != "working" || started.GoalTitle != "Boiler reply" || started.ConversationID == "" {
		t.Fatalf("started: %+v", started)
	}
	// What the goal made: a schedule in its conversation, and a mail rule
	// noted against it.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		next := time.Now().Add(time.Hour)
		if _, err := tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: agentId, Name: "Look for the reply", Cron: "0 9 * * *", Prompt: "look", Deliver: "drawer", ConversationID: started.ConversationID, Enabled: true, NextRunAt: &next}); err != nil {
			t.Fatalf("CreateAgentSchedule: %s", err)
		}
		if _, err := tx.AddAgentGoalArtifact(&models.AgentGoalArtifact{AgentID: agentId, ConversationID: started.ConversationID, GoalArtifactKind: models.GoalArtifactMailRule, ArtifactReference: "Landlord", ArtifactTitle: "mail rule Landlord"}); err != nil {
			t.Fatalf("AddAgentGoalArtifact: %s", err)
		}
	})

	within(allowed(person), func(ctx context.Context) {
		listed, err := resolver.ListAgentGoals(ctx, ListAgentGoalsArguments{GoalStates: []string{"working"}})
		if err != nil || len(listed) != 1 || listed[0].ConversationID != started.ConversationID {
			t.Fatalf("ListAgentGoals: %+v %v", listed, err)
		}
		if _, err := resolver.ListAgentGoals(ctx, ListAgentGoalsArguments{GoalStates: []string{"sleeping"}}); err == nil {
			t.Errorf("a state that is not one")
		}
		read, err := resolver.GetAgentGoal(ctx, GetAgentGoalArguments{ConversationID: started.ConversationID})
		if err != nil {
			t.Fatalf("GetAgentGoal: %s", err)
		}
		if len(read.Activity) != 1 || len(read.Schedules) != 1 || len(read.Artifacts) != 1 || read.Schedules[0].Name != "Look for the reply" {
			t.Fatalf("the goal with what it made: %+v", read)
		}
	})

	// Nobody else reads, tells or closes it.
	within(allowed(stranger), func(ctx context.Context) {
		if _, err := resolver.GetAgentGoal(ctx, GetAgentGoalArguments{ConversationID: started.ConversationID}); err == nil {
			t.Errorf("a stranger read the goal")
		}
		if _, err := resolver.TellAgentGoal(ctx, TellAgentGoalArguments{ConversationID: started.ConversationID, Text: "stop"}); err == nil {
			t.Errorf("a stranger told the goal something")
		}
		if _, err := resolver.SetAgentGoalState(ctx, SetAgentGoalStateArguments{ConversationID: started.ConversationID, GoalState: "dropped"}); err == nil {
			t.Errorf("a stranger dropped the goal")
		}
		if listed, err := resolver.ListAgentGoals(ctx, ListAgentGoalsArguments{}); err != nil || len(listed) != 0 {
			t.Errorf("a stranger listed goals: %+v %v", listed, err)
		}
	})

	within(allowed(person), func(ctx context.Context) {
		if _, err := resolver.SetAgentGoalState(ctx, SetAgentGoalStateArguments{ConversationID: started.ConversationID, GoalState: "waiting"}); err == nil {
			t.Errorf("the person put the goal in waiting")
		}
		met, err := resolver.SetAgentGoalState(ctx, SetAgentGoalStateArguments{ConversationID: started.ConversationID, GoalState: "met"})
		if err != nil || met.GoalState != "met" {
			t.Fatalf("SetAgentGoalState: %+v %v", met, err)
		}
	})
}

// Starting a goal with a schedule that is not the person's starts nothing:
// the goal and the move are one write.
func TestAGoalStartedWithAStrangersScheduleIsNotStarted(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	var person *models.User
	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "keeper", Name: "Example Keeper"}); err != nil {
			t.Fatal(err)
		}
		created, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		agentId = created.ID
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	worker := agentpackage.New(&agentpackage.Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	resolver := &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{Agent: worker}}
	principal := &api.Principal{User: person, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	err := database.TransactionContext(context.Background(), func(tx db.Transaction) error {
		_, err := resolver.StartAgentGoal(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx),
			StartAgentGoalArguments{GoalTitle: "Boiler", GoalDescription: "Watch for the reply.", ScheduleIDs: []string{"no-such-schedule"}})
		return err
	})
	if err == nil {
		t.Fatal("a goal with a schedule that is not there was started")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if goals, err := tx.ListAgentGoals(agentId, nil, 10); err != nil || len(goals) != 0 {
			t.Fatalf("the goal stayed behind: %+v %v", goals, err)
		}
	})
}
