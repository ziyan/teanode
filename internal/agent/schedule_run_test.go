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

// A schedule whose time has come is queued by the tick and takes a
// headless turn with the person's operations in the main conversation,
// opened by a message marked as the schedule's; the schedule moves on to
// its next time. Memories addressed to the conversation reach the prompt.
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
	// No nightly run in a test about schedules. A tick queues everything
	// that is due, and whether a night is due depends on the wall clock
	// -- the default window is one in the morning to six, in the person's
	// zone, and this person is in Berlin. Left on, this test rewrote the
	// pinned page's summary with the fake model's canned answer and
	// failed, but only when it was run between those hours.
	off := false
	configuration.Agent.Features.Dreaming = &off
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
		// A page the prompt's index must carry: pinned, so it is first
		// whatever the nightly importance says.
		page, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: found.ID, Path: "people/maria", Kind: models.NodePerson,
			Name: "Maria", Summary: "Maria does the books.", Pinned: true,
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: found.ID, NodeID: page.ID, Kind: models.FactPlain,
			Text: "The accountant.", Audiences: []models.AgentAudience{models.AudienceAsk},
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
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
		if len(messages) != 2 || messages[0].Role != "user" || !strings.HasPrefix(messages[0].Content, models.ScheduleMarker) ||
			!strings.Contains(messages[0].Content, "What needs me today?") || messages[1].Content != "Nothing needs you today." {
			t.Fatalf("the turn should be in the conversation, opened by the schedule's marked message: %+v", messages)
		}
		runs, _ := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if len(runs) != 0 {
			t.Fatalf("no transcript of its own: %+v", runs)
		}
		if spoke, _ := tx.LastAgentPersonMessageAt(main[0].ID); spoke != nil {
			t.Fatalf("the schedule's message is not the person's: %v", spoke)
		}
	})
	system := (*requests)[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "people/maria") || !strings.Contains(system, "Maria does the books") {
		t.Fatal("the pinned page should be in the prompt's index")
	}
	if tools, _ := (*requests)[0]["tools"].([]any); len(tools) == 0 {
		t.Fatal("the scheduled turn should have its tools")
	}
}

// A schedule for one moment, made in a side conversation, takes its turn
// there when the moment comes, and only then goes. It used to be switched
// off as its run was queued, and the run, finding it off, did nothing.
func TestAScheduleForOneMomentRunsInItsConversationAndThenGoes(t *testing.T) {
	world := startGoalWorld(t, []string{answerRound}, "")
	defer world.close()
	past := time.Now().Add(-time.Minute)
	var side *models.AgentConversation
	var schedule *models.AgentSchedule
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if side, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: world.found.ID, Kind: models.AgentConversationNamed, Title: "Invoices", LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		if schedule, err = tx.CreateAgentSchedule(&models.AgentSchedule{
			AgentID: world.found.ID, Name: "Reminder", Cron: "@at " + past.Format("2006-01-02 15:04"), Prompt: "Remind them about the invoice.",
			WrittenBy: models.WrittenByAgent, Deliver: "drawer", ConversationID: side.ID, Enabled: true, NextRunAt: &past,
		}); err != nil {
			t.Fatalf("CreateAgentSchedule: %s", err)
		}
	})
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	deadline := time.Now().Add(15 * time.Second)
	var after *models.AgentSchedule
	var messages []*models.AgentMessage
	for time.Now().Before(deadline) {
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			after, _ = tx.GetAgentSchedule(schedule.ID)
			messages, _ = tx.ListAgentMessages(side.ID, nil)
		})
		if after == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if after != nil {
		t.Fatalf("after its one run it is gone: %+v", after)
	}
	if len(messages) != 2 || !strings.HasPrefix(messages[0].Content, models.ScheduleMarker) || messages[1].Role != "assistant" {
		t.Fatalf("the turn is in the conversation it was made in: %+v", messages)
	}
	// The agent wrote it, so its words arrive fenced, as a note to itself.
	if !strings.Contains(messages[0].Content, "<untrusted-data>") || !strings.Contains(messages[0].Content, "Remind them about the invoice.") {
		t.Fatalf("an agent's own prompt is fenced: %s", messages[0].Content)
	}
}
