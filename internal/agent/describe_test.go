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

// A conversation is described once it has been quiet for a few minutes
// with something said since it was last described; one still moving, or
// already described since its last message, is left alone.
func TestQuietConversationsAreDescribed(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, _ := fakeModel(t, []string{answerRound})
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

	var quiet, busy, named *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		make := func(kind models.AgentConversationKind, title string, ago time.Duration) *models.AgentConversation {
			conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: kind, Title: title, LastAt: time.Now()})
			if err != nil {
				t.Fatalf("CreateAgentConversation: %s", err)
			}
			for _, message := range []*models.AgentMessage{
				{ConversationID: conversation.ID, Role: "user", Content: "find the invoice from the plumber"},
				{ConversationID: conversation.ID, Role: "assistant", Content: "Invoice 42 is in Receipts."},
			} {
				if _, err := tx.AppendAgentMessage(message); err != nil {
					t.Fatalf("AppendAgentMessage: %s", err)
				}
			}
			updated, err := tx.UpdateAgentConversation(conversation.ID, func(conversation *models.AgentConversation) error {
				conversation.LastAt = time.Now().Add(-ago)
				return nil
			})
			if err != nil {
				t.Fatalf("UpdateAgentConversation: %s", err)
			}
			return updated
		}
		quiet = make(models.AgentConversationMain, "", 10*time.Minute)
		busy = make(models.AgentConversationMain, "", 10*time.Second)
		named = make(models.AgentConversationNamed, "My own name", 10*time.Minute)
		if _, err := tx.UpdateAgentConversation(named.ID, func(conversation *models.AgentConversation) error {
			conversation.TitledBy = "person"
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgentConversation: %s", err)
		}
	})

	if err := worker.TickAt(context.Background(), time.Now()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	// Describing runs beside the tick, not inside it.
	worker.Wait()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		described, _ := tx.GetAgentConversation(quiet.ID)
		if described.Summary != "Finding the plumber's invoice." || described.DescribedAt == nil {
			t.Fatalf("the quiet conversation should be described: %+v", described)
		}
		if described.Title != "" {
			t.Fatalf("the main conversation keeps its name, got %q", described.Title)
		}
		moving, _ := tx.GetAgentConversation(busy.ID)
		if moving.Summary != "" || moving.DescribedAt != nil {
			t.Fatalf("a conversation still moving is left alone: %+v", moving)
		}
		own, _ := tx.GetAgentConversation(named.ID)
		if own.Title != "My own name" || own.Summary != "Finding the plumber's invoice." {
			t.Fatalf("a person's title stays while the summary is written: %+v", own)
		}
		due, _ := tx.ListAgentConversationsToDescribe(time.Now(), 10)
		for _, conversation := range due {
			if conversation.ID == quiet.ID || conversation.ID == named.ID {
				t.Fatal("a described conversation is not due again until something is said")
			}
		}
	})
}
