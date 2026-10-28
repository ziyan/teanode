package agent_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// A restart can cut a turn a person is waiting on; the first look after a
// start carries it on. These are the conversations that look finds and the
// ones it leaves alone.

type resumeWorld struct {
	// conversations are the ones made in this world, each recorded as having
	// a turn cut in it at the next look.
	conversations []*models.AgentConversation
	database      db.Database
	worker        *agent.Agent
	owner         *models.User
	found         *models.Agent
	requests      *[]map[string]any
	model         *httptest.Server
}

func newResumeWorld(t *testing.T) *resumeWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	model, requests := fakeModel(t, []string{answerRound})
	t.Cleanup(model.Close)
	return newResumeWorldOn(t, database, model, requests)
}

func newResumeWorldOn(t *testing.T, database db.Database, model *httptest.Server, requests *[]map[string]any) *resumeWorld {
	t.Helper()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
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
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })
	world := &resumeWorld{database: database, worker: worker, requests: requests, model: model}
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

// conversation writes a conversation of a kind holding the messages given.
func (self *resumeWorld) conversation(t *testing.T, kind models.AgentConversationKind, messages ...*models.AgentMessage) *models.AgentConversation {
	t.Helper()
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: self.found.ID, Kind: kind, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		for _, message := range messages {
			message.ConversationID = conversation.ID
			if _, err := tx.AppendAgentMessage(message); err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
		}
	})
	self.conversations = append(self.conversations, conversation)
	return conversation
}

func (self *resumeWorld) messages(t *testing.T, conversationId string) []*models.AgentMessage {
	t.Helper()
	var messages []*models.AgentMessage
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		messages, _ = tx.ListAgentMessages(conversationId, nil)
	})
	return messages
}

// resume records a turn cut at cutAt in every conversation of the world,
// as a server stopping with them in flight does, then looks for the turns
// to carry on, and waits for the ones it begins.
func (self *resumeWorld) resume(t *testing.T, cutAt time.Time) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		for index, conversation := range self.conversations {
			if err := tx.PutAgentTurn(&models.AgentTurn{
				RunID: fmt.Sprintf("cut-%d-%d", cutAt.UnixNano(), index), ConversationID: conversation.ID, Instance: "stopped",
				StartedAt: conversation.CreatedAt, HeartbeatAt: cutAt, CutAt: &cutAt,
			}); err != nil {
				t.Fatalf("PutAgentTurn: %s", err)
			}
		}
	})
	self.worker.ResumeCutTurns(context.Background())
	self.worker.Wait()
}

func (self *resumeWorld) modelCalls() int {
	return len(*self.requests)
}

// A turn that called a tool, whose result was the last thing written two
// minutes before the start, is carried on once, in its own conversation,
// by a turn that begins with the restart marker; a conversation the same
// look found answered is left as it is, and a second start does not carry
// the first on again.
func TestARestartCarriesOnTheTurnItCut(t *testing.T) {
	world := newResumeWorld(t)
	cut := world.conversation(t, models.AgentConversationMain,
		&models.AgentMessage{Role: "user", Content: "Find the invoice from the plumber."},
		&models.AgentMessage{Role: "assistant", ToolCalls: []models.AgentToolCall{{ID: "call_1", Name: "datetime", Arguments: "{}"}}},
		&models.AgentMessage{Role: "tool", ToolCallID: "call_1", Name: "datetime", Content: `{"now":"2026-03-02T09:00:00Z"}`},
	)
	answered := world.conversation(t, models.AgentConversationNamed,
		&models.AgentMessage{Role: "user", Content: "What is the date?"},
		&models.AgentMessage{Role: "assistant", Content: "It is the second of March."},
	)

	startedAt := time.Now().Add(2 * time.Minute)
	world.resume(t, startedAt)

	messages := world.messages(t, cut.ID)
	if len(messages) != 5 || messages[3].Role != "user" || !strings.HasPrefix(messages[3].Content, models.RestartResumeMarker) {
		t.Fatalf("the turn carried on in the conversation it was cut in: %+v", messages)
	}
	if !strings.Contains(messages[3].Content, "Do not repeat a tool call whose result is already there") {
		t.Fatalf("the message tells the agent not to repeat a call: %q", messages[3].Content)
	}
	if messages[4].Role != "assistant" || messages[4].Content == "" {
		t.Fatalf("the carried-on turn answered: %+v", messages[4])
	}
	if !models.IsOwnTurn(messages[3].Content) {
		t.Fatal("the marker is one the transcript and the relays know as the agent's own turn")
	}
	if got := world.messages(t, answered.ID); len(got) != 2 {
		t.Fatalf("the answered conversation was left alone: %+v", got)
	}
	if world.modelCalls() != 1 {
		t.Fatalf("one turn asked the model once, not %d times", world.modelCalls())
	}

	// A second start finds the conversation answered.
	world.resume(t, startedAt.Add(time.Minute))
	if world.modelCalls() != 1 || len(world.messages(t, cut.ID)) != 5 {
		t.Fatalf("a second start does not carry it on twice: %d calls, %+v", world.modelCalls(), world.messages(t, cut.ID))
	}
}

// A restart that cuts the carried-on turn too is not answered with a third:
// the conversation ends on a tool result of a turn that began with the
// marker, which is not the person's.
func TestARestartDoesNotCarryOnACarriedOnTurnAgain(t *testing.T) {
	world := newResumeWorld(t)
	world.conversation(t, models.AgentConversationMain,
		&models.AgentMessage{Role: "user", Content: "Find the invoice from the plumber."},
		&models.AgentMessage{Role: "user", Content: models.RestartResumeMarker + " the server restarted"},
		&models.AgentMessage{Role: "assistant", ToolCalls: []models.AgentToolCall{{ID: "call_1", Name: "datetime", Arguments: "{}"}}},
		&models.AgentMessage{Role: "tool", ToolCallID: "call_1", Name: "datetime", Content: "{}"},
	)
	world.resume(t, time.Now().Add(time.Minute))
	if world.modelCalls() != 0 {
		t.Fatalf("a turn that was already carried on is not carried on again: %d calls", world.modelCalls())
	}
}

// The person's own message, with nothing after it, is a turn cut before it
// began.
func TestARestartAnswersAMessageItCutBeforeTheFirstRound(t *testing.T) {
	world := newResumeWorld(t)
	cut := world.conversation(t, models.AgentConversationMain,
		&models.AgentMessage{Role: "user", Content: "Remind me what I asked for."},
	)
	world.resume(t, time.Now().Add(time.Minute))
	if messages := world.messages(t, cut.ID); len(messages) != 3 || !strings.HasPrefix(messages[1].Content, models.RestartResumeMarker) {
		t.Fatalf("the message was answered: %+v", messages)
	}
}

// What a restart did not cut: a turn that ended with an answer, one the
// person stopped, one that failed, a card waiting for the person, a turn
// the agent began on its own, and the record of a background job.
func TestARestartLeavesAlonePeopleWhoWereNotWaiting(t *testing.T) {
	world := newResumeWorld(t)
	person := &models.AgentMessage{Role: "user", Content: "Find the invoice from the plumber."}
	call := func() *models.AgentMessage {
		return &models.AgentMessage{Role: "assistant", ToolCalls: []models.AgentToolCall{{ID: "call_1", Name: "datetime", Arguments: "{}"}}}
	}
	result := func() *models.AgentMessage {
		return &models.AgentMessage{Role: "tool", ToolCallID: "call_1", Name: "datetime", Content: "{}"}
	}
	note := func(kind models.AgentNoteKind) *models.AgentMessage {
		return models.NewAgentNote("", kind, "")
	}
	answered := world.conversation(t, models.AgentConversationMain, &models.AgentMessage{Role: "user", Content: "Hello"}, &models.AgentMessage{Role: "assistant", Content: "Hello."})
	stopped := world.conversation(t, models.AgentConversationNamed, &models.AgentMessage{Role: "user", Content: "Hello"}, call(), result(), note(models.NoteStopped))
	failed := world.conversation(t, models.AgentConversationNamed, &models.AgentMessage{Role: "user", Content: "Hello"}, call(), result(), note(models.NoteFailed))
	ownTurn := world.conversation(t, models.AgentConversationMain, &models.AgentMessage{Role: "user", Content: models.ScheduleMarker + " the water reminder"}, call(), result())
	waiting := world.conversation(t, models.AgentConversationNamed, person, call())
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.CreateAgentInteraction(&models.AgentInteraction{
			AgentID: world.found.ID, ConversationID: waiting.ID, RunID: "run_1", CallID: "call_1",
			InteractionKind: models.InteractionQuestion, ToolName: "ask_user", InteractionText: "Which plumber?",
		}); err != nil {
			t.Fatalf("CreateAgentInteraction: %s", err)
		}
	})
	background := world.conversation(t, models.AgentConversationRun, &models.AgentMessage{Role: "user", Content: "Read this page."}, call(), result())
	goal := world.conversation(t, models.AgentConversationGoal, &models.AgentMessage{Role: "user", Content: models.GoalCheckInMarker + " look again"}, call(), result())

	untouched := map[string]*models.AgentConversation{"answered": answered, "stopped": stopped, "failed": failed, "own turn": ownTurn, "card waiting": waiting, "run": background, "goal": goal}
	counts := map[string]int{}
	for name, conversation := range untouched {
		counts[name] = len(world.messages(t, conversation.ID))
	}
	world.resume(t, time.Now().Add(2*time.Minute))
	for name, conversation := range untouched {
		if got := len(world.messages(t, conversation.ID)); got != counts[name] {
			t.Errorf("the %s conversation was written to: %d messages, was %d", name, got, counts[name])
		}
	}
	if world.modelCalls() != 0 {
		t.Fatalf("no turn was started: %d calls", world.modelCalls())
	}
}

// A turn cut twenty minutes before the start is too old to answer out of
// the blue; the person has moved on.
func TestARestartLeavesATurnTwentyMinutesOldAlone(t *testing.T) {
	world := newResumeWorld(t)
	cut := world.conversation(t, models.AgentConversationMain,
		&models.AgentMessage{Role: "user", Content: "Find the invoice from the plumber."},
		&models.AgentMessage{Role: "assistant", ToolCalls: []models.AgentToolCall{{ID: "call_1", Name: "datetime", Arguments: "{}"}}},
		&models.AgentMessage{Role: "tool", ToolCallID: "call_1", Name: "datetime", Content: "{}"},
	)
	world.resume(t, time.Now().Add(20*time.Minute))
	if world.modelCalls() != 0 || len(world.messages(t, cut.ID)) != 3 {
		t.Fatalf("nothing was written: %d calls, %+v", world.modelCalls(), world.messages(t, cut.ID))
	}
}

// At most three conversations of an agent are carried on, the newest first.
func TestARestartCarriesOnTheNewestFewConversations(t *testing.T) {
	world := newResumeWorld(t)
	var conversations []*models.AgentConversation
	for index := 0; index < 5; index++ {
		conversations = append(conversations, world.conversation(t, models.AgentConversationNamed,
			&models.AgentMessage{Role: "user", Content: fmt.Sprintf("Question %d", index)},
		))
	}
	world.resume(t, time.Now().Add(time.Minute))
	for index, conversation := range conversations {
		isResumed := len(world.messages(t, conversation.ID)) > 1
		if want := index >= 2; isResumed != want {
			t.Errorf("conversation %d carried on is %v, want %v", index, isResumed, want)
		}
	}
}

// With the agent off for this person nothing is carried on.
func TestARestartLeavesAnAgentThatIsOffAlone(t *testing.T) {
	world := newResumeWorld(t)
	cut := world.conversation(t, models.AgentConversationMain, &models.AgentMessage{Role: "user", Content: "Hello"})
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.UpdateAgent(world.found.ID, func(found *models.Agent) error {
			found.Enabled = false
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
	})
	world.resume(t, time.Now().Add(time.Minute))
	if world.modelCalls() != 0 || len(world.messages(t, cut.ID)) != 1 {
		t.Fatalf("an agent that is off says nothing: %d calls", world.modelCalls())
	}
}

// blockingModel answers a round after the gate is opened, or never.
func blockingModel(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-gate:
		case <-request.Context().Done():
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, line := range strings.Split(answerRound, "\n") {
			_, _ = writer.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	open := func() { once.Do(func() { close(gate) }) }
	// Opened before the server is closed, so that a round nobody waits
	// for any longer does not hold the close.
	t.Cleanup(open)
	return server, open
}

func askInMainConversation(t *testing.T, world *resumeWorld) *models.AgentConversation {
	t.Helper()
	conversation := world.conversation(t, models.AgentConversationMain)
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	if _, err := world.worker.Ask(&agent.AskSettings{
		Agent: world.found, Owner: world.owner, Operations: operations, Conversation: conversation,
		Message: "Find the invoice from the plumber.", Surface: "drawer",
	}); err != nil {
		t.Fatalf("Ask: %s", err)
	}
	// The model is being waited on once the person's message is kept.
	deadline := time.Now().Add(10 * time.Second)
	for len(world.messages(t, conversation.ID)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the turn did not begin")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return conversation
}

// Stopping the server gives a turn a person is reading a moment to answer.
func TestStoppingTheServerLetsATurnFinishWithinTheGrace(t *testing.T) {
	defer agent.SetStopGrace(agent.SetStopGrace(10 * time.Second))
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	model, open := blockingModel(t)
	world := newResumeWorldOn(t, database, model, &[]map[string]any{})
	conversation := askInMainConversation(t, world)
	time.AfterFunc(300*time.Millisecond, open)
	world.worker.Stop()
	messages := world.messages(t, conversation.ID)
	if len(messages) < 2 || messages[len(messages)-1].Role != "assistant" || messages[len(messages)-1].Content == "" {
		t.Fatalf("the turn answered before it was cancelled: %+v", messages)
	}
}

// A turn still running when the grace is over is cancelled and writes
// nothing, so the conversation ends on the person's message and the next
// start finds it.
func TestStoppingTheServerWritesNothingForATurnItCuts(t *testing.T) {
	defer agent.SetStopGrace(agent.SetStopGrace(100 * time.Millisecond))
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	model, _ := blockingModel(t)
	world := newResumeWorldOn(t, database, model, &[]map[string]any{})
	conversation := askInMainConversation(t, world)
	world.worker.Stop()
	messages := world.messages(t, conversation.ID)
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("the conversation ends on the person's message, no note after it: %+v", messages)
	}
}
