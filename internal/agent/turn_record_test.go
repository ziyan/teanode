package agent_test

import (
	"context"
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

// Two instances of the server sharing one database, each running the turns
// that reach it. What one runs, the other has to see.

type twoInstances struct {
	database   db.Database
	here       *agent.Agent
	there      *agent.Agent
	owner      *models.User
	found      *models.Agent
	operations *fakeOperations
	openModel  func()
}

func newTwoInstances(t *testing.T) *twoInstances {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	model, openModel := blockingModel(t)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	off := false
	configuration.Agent.Features.Dreaming = &off
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
	world := &twoInstances{
		database: database, openModel: openModel,
		operations: &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})},
	}
	newWorker := func(instance string) *agent.Agent {
		worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: instance, Tick: time.Hour})
		worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return world.operations, nil })
		worker.Start()
		t.Cleanup(worker.Stop)
		return worker
	}
	world.here = newWorker("here")
	world.there = newWorker("there")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if world.owner, err = tx.CreateUser(&models.User{Username: "robin", Timezone: "UTC", TimezoneMode: "fixed"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if world.found, err = tx.CreateAgent(&models.Agent{UserID: world.owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	return world
}

func (self *twoInstances) conversation(t *testing.T) *models.AgentConversation {
	t.Helper()
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: self.found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	return conversation
}

func (self *twoInstances) ask(t *testing.T, worker *agent.Agent, conversation *models.AgentConversation, message string) *agent.AskRun {
	t.Helper()
	run, err := worker.Ask(&agent.AskSettings{
		Agent: self.found, Owner: self.owner, Operations: self.operations, Conversation: conversation,
		Message: message, Surface: "drawer",
	})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	return run
}

func (self *twoInstances) messages(t *testing.T, conversationId string) []*models.AgentMessage {
	t.Helper()
	var messages []*models.AgentMessage
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		messages, _ = tx.ListAgentMessages(conversationId, nil)
	})
	return messages
}

func (self *twoInstances) turn(t *testing.T, runId string) *models.AgentTurn {
	t.Helper()
	var turn *models.AgentTurn
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if turn, err = tx.GetAgentTurn(runId); err != nil {
			t.Fatalf("GetAgentTurn: %s", err)
		}
	})
	return turn
}

func waitUntil(t *testing.T, what string, isDone func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !isDone() {
		if time.Now().After(deadline) {
			t.Fatalf("not within ten seconds: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A turn running here is recorded as running here, and forgotten when it
// ends. The other instance knows whose it is without having heard of it,
// and a second message there waits for it rather than running beside it.
func TestATurnOnOneInstanceIsWaitedForOnTheOther(t *testing.T) {
	world := newTwoInstances(t)
	conversation := world.conversation(t)
	first := world.ask(t, world.here, conversation, "Find the invoice from the plumber.")
	if turn := world.turn(t, first.ID); turn == nil || turn.Instance != "here" || turn.CutAt != nil {
		t.Fatalf("the turn is recorded as running here: %+v", turn)
	}
	if conversationId, isKnown := world.there.ForeignRun(first.ID); !isKnown || conversationId != conversation.ID {
		t.Fatalf("the other instance knows the run is here, in its conversation: %q %v", conversationId, isKnown)
	}
	second := world.ask(t, world.there, conversation, "And the one from the electrician.")
	time.Sleep(500 * time.Millisecond)
	if got := len(world.messages(t, conversation.ID)); got != 1 {
		t.Fatalf("the second message waits for the first turn: %d messages", got)
	}
	world.openModel()
	waitUntil(t, "both turns answered", func() bool { return len(world.messages(t, conversation.ID)) >= 4 })
	messages := world.messages(t, conversation.ID)
	roles := []string{}
	for _, message := range messages {
		roles = append(roles, message.Role)
	}
	if len(messages) != 4 || messages[0].Content != "Find the invoice from the plumber." || messages[1].Role != "assistant" ||
		messages[2].Content != "And the one from the electrician." || messages[3].Role != "assistant" {
		t.Fatalf("the turns ran one after the other: %v", roles)
	}
	waitUntil(t, "both turns forgotten", func() bool { return world.turn(t, first.ID) == nil && world.turn(t, second.ID) == nil })
}

// Deleting a conversation on one instance stops its turn on the other.
func TestStoppingAConversationStopsItsTurnOnTheOtherInstance(t *testing.T) {
	world := newTwoInstances(t)
	conversation := world.conversation(t)
	run := world.ask(t, world.here, conversation, "Find the invoice from the plumber.")
	waitUntil(t, "the turn began", func() bool { return len(world.messages(t, conversation.ID)) == 1 })
	world.there.StopConversation(conversation.ID)
	waitUntil(t, "the turn was stopped", func() bool { return world.turn(t, run.ID) == nil })
}

// A turn an instance stopped with is carried on by the other one, once,
// however many times the other looks.
func TestATurnCutByOneInstanceIsCarriedOnByTheOtherOnce(t *testing.T) {
	defer agent.SetStopGrace(agent.SetStopGrace(100 * time.Millisecond))
	world := newTwoInstances(t)
	conversation := world.conversation(t)
	run := world.ask(t, world.here, conversation, "Find the invoice from the plumber.")
	waitUntil(t, "the turn began", func() bool { return len(world.messages(t, conversation.ID)) == 1 })
	world.here.Stop()
	if turn := world.turn(t, run.ID); turn == nil || turn.CutAt == nil {
		t.Fatalf("the turn the stop cut is recorded as cut: %+v", turn)
	}
	world.openModel()
	world.there.ResumeCutTurns(context.Background())
	world.there.ResumeCutTurns(context.Background())
	waitUntil(t, "the turn was carried on", func() bool { return len(world.messages(t, conversation.ID)) >= 3 })
	time.Sleep(300 * time.Millisecond)
	messages := world.messages(t, conversation.ID)
	if len(messages) != 3 || !models.IsOwnTurn(messages[1].Content) || messages[2].Role != "assistant" {
		t.Fatalf("the turn was carried on once: %+v", messages)
	}
}

// A run on one instance can be read on the other, from what that instance
// heard of it: the words it began with, and its end.
func TestARunOnOneInstanceIsReadOnTheOther(t *testing.T) {
	world := newTwoInstances(t)
	conversation := world.conversation(t)
	// The other instance's LISTEN takes a moment to be in place.
	time.Sleep(300 * time.Millisecond)
	run := world.ask(t, world.here, conversation, "Find the invoice from the plumber.")
	var events <-chan agent.Event
	waitUntil(t, "the other instance heard of the run", func() bool {
		conversationId, subscribed, _, isKnown := world.there.SubscribeForeignRun(run.ID)
		if isKnown && conversationId == conversation.ID {
			events = subscribed
		}
		return events != nil
	})
	world.openModel()
	var kinds []agent.EventKind
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, isOpen := <-events:
			if !isOpen {
				if len(kinds) == 0 || kinds[0] != agent.EventAsked || kinds[len(kinds)-1] != agent.EventDone {
					t.Fatalf("the run read on the other instance begins with what was asked and ends: %v", kinds)
				}
				return
			}
			kinds = append(kinds, event.Kind)
		case <-deadline:
			t.Fatalf("the run did not end on the other instance: %v", kinds)
		}
	}
}
