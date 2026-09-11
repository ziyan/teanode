package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A schedule whose time has come is queued by the tick, runs a headless
// turn with the person's operations, and delivers the answer into the
// main conversation; the schedule moves on to its next time. Memories
// addressed to the conversation reach the prompt.
func TestScheduleRunsAndDeliversToTheConversation(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, requests := fakeModel(t, []string{`{"id":"s1","model":"m","choices":[{"delta":{"content":"Nothing needs you today."},"finish_reason":"stop"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":30,"completion_tokens":5}}`})
	defer model.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	worker.SetOperationsFactory(func(ctx context.Context, owner *models.User) (agent.Operations, error) { return operations, nil })

	var owner *models.User
	var found *models.Agent
	var schedule *models.AgentSchedule
	past := time.Now().Add(-time.Minute)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example", Timezone: "Europe/Berlin", TimezoneMode: "fixed"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if _, err := tx.CreateAgentMemory(&models.AgentMemory{AgentID: found.ID, Title: "The accountant", Content: "Maria does the books.", AppliesTo: []models.AgentAudience{models.AudienceAsk}, Pinned: true}); err != nil {
			t.Fatalf("CreateAgentMemory: %s", err)
		}
		if schedule, err = tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: found.ID, Name: "Morning", Cron: "0 8 * * 1-5", Prompt: "What needs me today?", Deliver: "drawer", Enabled: true, NextRunAt: &past}); err != nil {
			t.Fatalf("CreateAgentSchedule: %s", err)
		}
	})
	// The tick queues the due schedule and runs it in the same pass.
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()
	// The scheduled turn runs in its own goroutine under the job; wait for
	// the job to finish.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var done bool
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobSchedule}}, nil)
			done = len(jobs) == 1 && jobs[0].Status == models.AgentJobDone
		})
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobSchedule}}, nil)
		if len(jobs) != 1 || jobs[0].Status != models.AgentJobDone {
			t.Fatalf("the schedule job should be done: %+v", jobs)
		}
		after, _ := tx.GetAgentSchedule(schedule.ID)
		if after.LastRunAt == nil || after.NextRunAt == nil || !after.NextRunAt.After(time.Now()) {
			t.Fatalf("the schedule should have moved on: %+v", after)
		}
		berlin, _ := time.LoadLocation("Europe/Berlin")
		if after.NextRunAt.In(berlin).Hour() != 8 {
			t.Fatalf("the next run should be at eight in the person's zone, got %s", after.NextRunAt.In(berlin))
		}
		main, _ := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationMain}, nil)
		if len(main) != 1 {
			t.Fatalf("the answer should have made the main conversation: %d", len(main))
		}
		messages, _ := tx.ListAgentMessages(main[0].ID, nil)
		if len(messages) != 2 || messages[0].Role != "note" || messages[1].Content != "Nothing needs you today." {
			t.Fatalf("the answer should be in the conversation with a note: %+v", messages)
		}
		runs, _ := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if len(runs) != 1 || runs[0].JobKind != "schedule" {
			t.Fatalf("a run transcript was expected: %+v", runs)
		}
	})
	system := (*requests)[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "Maria does the books") {
		t.Fatal("the pinned memory should be in the prompt")
	}
	if tools, _ := (*requests)[0]["tools"].([]any); len(tools) == 0 {
		t.Fatal("the scheduled turn should have its tools")
	}
}
