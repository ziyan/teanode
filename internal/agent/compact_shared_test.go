package agent_test

import (
	"context"
	"strconv"
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

// A note that stands in for a part of the conversation in which pages other
// people shared were read carries the shared-page marker, because the model
// that wrote it may have put their words in it, and the model that reads the
// note afterwards must not take them for the person's own. A note written
// from a part that read none does not.
func TestCompactionNoteCarriesTheSharedPageMarker(t *testing.T) {
	for name, readsSharedPage := range map[string]bool{"with a shared page read": true, "without one": false} {
		t.Run(name, func(t *testing.T) {
			database, closeDatabase := dbtest.AcquireDatabase(t)
			defer closeDatabase()

			model, _ := compactingModel("Learned: the person asked about the garden and the regatta.", "ok", 0)
			defer model.Close()
			configuration := config.Default()
			configuration.Agent.Enabled = true
			configuration.Agent.Features.Dreaming = new(bool)
			configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
			configuration.Agent.Models.Default = "fake:thinker"
			registry, err := llm.Open(&configuration.Agent)
			if err != nil {
				t.Fatal(err)
			}
			store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
			worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) {
				return &fakeOperations{permissions: models.NewEffectivePermissions(nil)}, nil
			})
			var owner *models.User
			var found *models.Agent
			var conversation *models.AgentConversation
			filler := strings.Repeat("The plumber's invoice, the mooring fee, the regatta on the 21st. ", 60)
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				owner, _ = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
				found, _ = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
				conversation, _ = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
				add := func(role, content, name, callID string, calls ...models.AgentToolCall) {
					if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: role, Content: content, Name: name, ToolCallID: callID, ToolCalls: calls}); err != nil {
						t.Fatal(err)
					}
				}
				add("user", "what did Sam say about the garden?", "", "")
				add("assistant", "", "", "", models.AgentToolCall{ID: "c1", Name: "memory", Arguments: `{"action":"get","path":"projects/garden","owner":"sam"}`})
				answer := "the garden is where the bees are kept"
				if readsSharedPage {
					answer = tools.SharedPageMarker + "\n" + answer
				}
				add("tool", answer, "memory", "c1")
				add("assistant", "Sam keeps the bees in the garden.", "", "")
				for turn := 1; turn <= 30; turn++ {
					add("user", "turn "+strconv.Itoa(turn)+": "+filler, "", "")
					add("assistant", "reply "+strconv.Itoa(turn)+": "+filler, "", "")
				}
			})
			operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
			turn, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "and the regatta?", Surface: "cli"})
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range collect(turn) {
				if event.Kind == agent.EventError {
					t.Fatalf("turn failed: %s", event.Error)
				}
			}
			var notes []string
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				messages, err := tx.ListAgentMessages(conversation.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, message := range messages {
					if message.Role == "compaction" {
						notes = append(notes, message.Content)
					}
				}
			})
			if len(notes) == 0 {
				t.Fatal("the conversation was not compacted, so nothing was proved")
			}
			if carries := strings.Contains(notes[len(notes)-1], tools.SharedPageMarker); carries != readsSharedPage {
				t.Errorf("the note carries the marker: %v, expected %v:\n%s", carries, readsSharedPage, notes[len(notes)-1])
			}
		})
	}
}
