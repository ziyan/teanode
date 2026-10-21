package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A turn that read a note in Passwords is private to the run the person
// started. A later run in the same conversation that the person did not start
// (a chat app, a command left running, a resumed card, the agent speaking
// first) is sent the marker where the turn was; the person's own next message
// is sent all of it, so they can go on talking about the note.
func TestOnlyARunThePersonStartedIsSentAPrivateTurn(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	model, requests := compactingModel("a note", "ok", 0)
	defer model.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(bool)
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	registry, _ := llm.Open(&configuration.Agent)
	store, _ := storage.Open(&storage.Settings{Directory: t.TempDir()})
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) {
		return &fakeOperations{permissions: models.NewEffectivePermissions(nil)}, nil
	})
	var owner *models.User
	var found *models.Agent
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, _ = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		found, _ = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		conversation, _ = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		add := func(role, content, name, callID string, calls ...models.AgentToolCall) {
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: role, Content: content, Name: name, ToolCallID: callID, ToolCalls: calls}); err != nil {
				t.Fatal(err)
			}
		}
		add("user", "read me the shed note", "", "")
		add("assistant", "", "", "", models.AgentToolCall{ID: "c1", Name: "passwords", Arguments: `{"action":"read_note","item_id":"x"}`})
		add("tool", `{"note_text":"the shed padlock opens with 0000-invented"}`, "passwords", "c1")
		add("assistant", "It says the padlock opens with 0000-invented.", "", "")
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions(nil)}
	for surface, isPersons := range map[string]bool{"background": false, "telegram": false, "resume": false, "speak-first:idea": false, "mcp": false, "drawer": true} {
		before := len(*requests)
		message := models.BackgroundCommandMarker + " the command finished. Output: (attacker text)"
		if isPersons {
			message = "and what did the note say again?"
		}
		turn, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: message, Surface: surface, IsPersonsTap: isPersons})
		if err != nil {
			t.Fatal(err)
		}
		collect(turn)
		isSent, isHidden := false, false
		for _, request := range (*requests)[before:] {
			raw, _ := json.Marshal(request["messages"])
			if strings.Contains(string(raw), "0000-invented") {
				isSent = true
			}
			if strings.Contains(string(raw), tools.HiddenPrivateWords) {
				isHidden = true
			}
		}
		if turn.IsStartedByPerson() != isPersons {
			t.Errorf("surface %s: IsStartedByPerson = %v", surface, turn.IsStartedByPerson())
		}
		if isSent != isPersons {
			t.Errorf("surface %s: the note text was sent to the model: %v", surface, isSent)
		}
		if !isPersons && !isHidden {
			t.Errorf("surface %s: the model was not told something was hidden", surface)
		}
	}
}
