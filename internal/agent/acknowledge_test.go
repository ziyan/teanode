package agent_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// The server puts a reaction on the person's message while the turn is
// still working, whatever the turn's own model does, and tells whoever has
// the conversation open, in the dashboard and not in the terminal. The
// model that picks the emoji answers at once here and the turn's model
// takes a moment, as it does when it is working, so the reaction lands
// before the turn ends and is not cut off with it.
func TestTheServerReactsToThePersonsMessage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var choosingCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(string(raw), "Choose the one emoji") {
			choosingCalls.Add(1)
			if !strings.Contains(string(raw), "thank you for the invoice") {
				t.Errorf("the person's message is not in the prompt: %s", raw)
			}
			_, _ = writer.Write([]byte("data: {\"id\":\"r1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"❤️\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		time.Sleep(800 * time.Millisecond)
		for _, line := range strings.Split(declinedRound, "\n") {
			_, _ = writer.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer model.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(bool)
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

	var owner *models.User
	var found *models.Agent
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

	reactionsOnPersonMessages := func() map[string][]models.AgentMessageReaction {
		found := map[string][]models.AgentMessageReaction{}
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			messages, err := tx.ListAgentMessages(conversation.ID, nil)
			if err != nil {
				t.Fatalf("ListAgentMessages: %s", err)
			}
			for _, message := range messages {
				if message.Role == "user" {
					found[message.Content] = message.Reactions
				}
			}
		})
		return found
	}

	// The terminal draws no reaction: no call to pick one, none stored.
	cliRun, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "list my mailboxes", Surface: "cli"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	collect(cliRun)
	if choosingCalls.Load() != 0 || len(reactionsOnPersonMessages()["list my mailboxes"]) != 0 {
		t.Fatalf("the command line was reacted to: %d calls, %+v", choosingCalls.Load(), reactionsOnPersonMessages())
	}

	// The drawer does, before the answer, and the watchers are told.
	events, unsubscribe := worker.SubscribeConversation(conversation.ID)
	defer unsubscribe()
	drawerRun, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "thank you for the invoice", Surface: "drawer"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	var told *agent.Event
	deadline := time.After(10 * time.Second)
	for told == nil {
		select {
		case event := <-events:
			if event.Kind == agent.EventReacted {
				copied := event
				told = &copied
			}
		case <-deadline:
			t.Fatal("nobody was told of a reaction")
		}
	}
	if told.Emoji != "❤️" || told.ReactorKind != string(models.ReactorAgent) {
		t.Fatalf("the event: %+v", told)
	}
	collect(drawerRun)
	reactions := reactionsOnPersonMessages()["thank you for the invoice"]
	if len(reactions) != 1 || reactions[0].Emoji != "❤️" || reactions[0].ReactorKind != models.ReactorAgent {
		t.Fatalf("the agent's reaction is on the person's message: %+v", reactions)
	}
	if choosingCalls.Load() != 1 {
		t.Fatalf("one call to pick the emoji, got %d", choosingCalls.Load())
	}
}
