package agent_test

import (
	"context"
	"fmt"
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

// speakFirstWorld is a worker with a fake model, and a person whose agent
// was switched on and never written to.
type speakFirstWorld struct {
	database db.Database
	worker   *agent.Agent
	owner    *models.User
	agent    *models.Agent
	requests *[]map[string]any
}

func newSpeakFirstWorld(t *testing.T) *speakFirstWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	model, requests := fakeModel(t, []string{`{"id":"s1","model":"m","choices":[{"delta":{"content":"Hello, I am your new agent. What would you like to call me?"},"finish_reason":"stop"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":30,"completion_tokens":5}}`})
	t.Cleanup(model.Close)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	// No night: whether one is due depends on the wall clock.
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
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	worker.SetOperationsFactory(func(ctx context.Context, owner *models.User) (agent.Operations, error) { return operations, nil })
	world := &speakFirstWorld{database: database, worker: worker, requests: requests}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if world.owner, err = tx.CreateUser(&models.User{Username: "robin", Timezone: "UTC", TimezoneMode: "fixed"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if world.agent, err = tx.CreateAgent(&models.Agent{UserID: world.owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	return world
}

// tick runs one pass of the worker and waits for what it started.
func (self *speakFirstWorld) tick(t *testing.T) {
	t.Helper()
	if err := self.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	self.worker.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var open int64
		dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
			open, _ = tx.CountAgentJobs(&db.AgentJobFilter{AgentID: self.agent.ID, Kinds: []models.AgentJobKind{models.AgentJobSpeakFirst}, Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning}})
		})
		if open == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the speak_first job did not finish")
}

func (self *speakFirstWorld) speakFirstJobs(t *testing.T) []*models.AgentJob {
	t.Helper()
	var jobs []*models.AgentJob
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		jobs, _ = tx.ListAgentJobs(&db.AgentJobFilter{AgentID: self.agent.ID, Kinds: []models.AgentJobKind{models.AgentJobSpeakFirst}}, nil)
	})
	return jobs
}

// A person who has the dashboard open and has never written to their new
// agent is greeted in the main conversation, by a turn the agent opened
// with a message of its own; once, not at every tick.
func TestTheAgentIntroducesItselfToAPersonWhoIsThere(t *testing.T) {
	world := newSpeakFirstWorld(t)
	world.worker.ReportPresence(world.owner.ID, true, 0, time.Now())
	world.tick(t)

	jobs := world.speakFirstJobs(t)
	if len(jobs) != 1 || jobs[0].SubjectID != agent.SpeakFirstOnboarding || jobs[0].Status != models.AgentJobDone {
		t.Fatalf("one onboarding job, done: %+v", jobs)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		main, _ := tx.ListAgentConversations(world.agent.ID, []models.AgentConversationKind{models.AgentConversationMain}, nil)
		if len(main) != 1 {
			t.Fatalf("the greeting is in the main conversation: %d", len(main))
		}
		messages, _ := tx.ListAgentMessages(main[0].ID, nil)
		if len(messages) != 2 || !strings.HasPrefix(messages[0].Content, models.SpeakFirstMarker) || !strings.Contains(messages[1].Content, "What would you like to call me?") {
			t.Fatalf("the turn opened by the agent's own marked message: %+v", messages)
		}
		if spoke, _ := tx.LastAgentPersonWordAt(world.agent.ID); spoke != nil {
			t.Fatalf("the agent's message is not the person's: %v", spoke)
		}
		after, _ := tx.GetAgent(world.agent.ID)
		if after.SpokeFirstAt == nil || after.OnboardedAt != nil {
			t.Fatalf("spoken first, introduction still open: %+v", after)
		}
	})
	if request := fmt.Sprint((*world.requests)[0]["messages"]); !strings.Contains(request, "<introduction>") {
		t.Fatal("the introduction's overlay says what is still to ask")
	}

	// The next tick leaves it be: the greeting is waiting for an answer.
	world.worker.ReportPresence(world.owner.ID, true, 0, time.Now().Add(2*time.Minute))
	if err := world.worker.TickAt(context.Background(), time.Now().Add(2*time.Minute)); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	if jobs := world.speakFirstJobs(t); len(jobs) != 1 {
		t.Fatalf("introduced once: %+v", jobs)
	}
}

// Nobody there, nobody greeted.
func TestTheAgentDoesNotSpeakFirstToSomebodyWhoIsAway(t *testing.T) {
	world := newSpeakFirstWorld(t)
	// No report at all; then a hidden tab; then a visible one that
	// reported long ago.
	world.tick(t)
	world.worker.ReportPresence(world.owner.ID, false, 0, time.Now())
	world.worker.ReportPresence(world.owner.ID, true, 0, time.Now().Add(-10*time.Minute))
	if err := world.worker.TickAt(context.Background(), time.Now().Add(2*time.Minute)); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	if jobs := world.speakFirstJobs(t); len(jobs) != 0 {
		t.Fatalf("nobody was there to read it: %+v", jobs)
	}
}

// Somebody who has already written to their agent knows it: they are not
// greeted as a newcomer, and the introduction is over.
func TestTheAgentDoesNotIntroduceItselfToSomebodyItKnows(t *testing.T) {
	world := newSpeakFirstWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		main, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: world.agent.ID, Kind: models.AgentConversationMain, Surface: "drawer", LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: main.ID, Role: "user", Content: "what is on today?"}); err != nil {
			t.Fatal(err)
		}
	})
	// Present, but quiet for less than half an hour: nothing yet.
	world.worker.ReportPresence(world.owner.ID, true, 0, time.Now())
	world.tick(t)
	if jobs := world.speakFirstJobs(t); len(jobs) != 0 {
		t.Fatalf("not while they are talking: %+v", jobs)
	}
	// An hour later, still nothing, and the introduction is marked over.
	later := time.Now().Add(time.Hour)
	world.worker.ReportPresence(world.owner.ID, true, 0, later)
	if err := world.worker.TickAt(context.Background(), later); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	if jobs := world.speakFirstJobs(t); len(jobs) != 0 {
		t.Fatalf("somebody who wrote is not greeted: %+v", jobs)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		after, _ := tx.GetAgent(world.agent.ID)
		if after.OnboardedAt == nil {
			t.Fatal("the introduction is over for somebody who already wrote")
		}
	})
}

// A snoozed agent keeps quiet, and asked from the command line it speaks
// anyway: the rules are for the times nobody asked.
func TestNotNowKeepsTheAgentQuietUnlessAsked(t *testing.T) {
	world := newSpeakFirstWorld(t)
	until := time.Now().Add(time.Hour)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.MarkAgentSpokeFirst(world.agent.ID, nil, nil, &until); err != nil {
			t.Fatal(err)
		}
	})
	world.worker.ReportPresence(world.owner.ID, true, 0, time.Now())
	world.tick(t)
	if jobs := world.speakFirstJobs(t); len(jobs) != 0 {
		t.Fatalf("snoozed: %+v", jobs)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		found, _ := tx.GetAgent(world.agent.ID)
		if err := world.worker.SpeakFirstNow(tx, found, "a reason nobody has"); err == nil {
			t.Fatal("an unknown reason is refused")
		}
		if err := world.worker.SpeakFirstNow(tx, found, agent.SpeakFirstOnboarding); err != nil {
			t.Fatal(err)
		}
	})
	if jobs := world.speakFirstJobs(t); len(jobs) != 1 {
		t.Fatalf("asked for, it is queued: %+v", jobs)
	}
}
