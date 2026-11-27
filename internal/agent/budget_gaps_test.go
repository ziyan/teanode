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

// A conversation's title is a model call like any other: a person who has
// used their day's tokens has none made, and the model is not asked. Nothing
// is marked, so the title comes once the day has room.
func TestADescriptionWaitsForTheDailyLimit(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, requests := fakeModel(t, []string{describedRound})
	defer model.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(bool)
	configuration.Agent.Limits.DailyTokensPerAgent = 1000
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
	defer worker.Stop()
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) {
		return &fakeOperations{permissions: models.NewEffectivePermissions(nil)}, nil
	})

	var quiet *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
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
		if quiet, err = tx.UpdateAgentConversation(conversation.ID, func(conversation *models.AgentConversation) error {
			conversation.LastAt = time.Now().Add(-10 * time.Minute)
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgentConversation: %s", err)
		}
		// The whole day's tokens are gone.
		if err := tx.PutAgentUsage(&db.AgentUsage{
			AgentID: found.ID, Model: "fake:thinker", Kind: "ask",
			At: agent.DayStart(owner, time.Now()).Add(time.Minute), Values: []uint64{1000, 0, 0, 0, 1},
		}); err != nil {
			t.Fatalf("PutAgentUsage: %s", err)
		}
	})

	if err := worker.TickAt(context.Background(), time.Now()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()
	if len(*requests) != 0 {
		t.Fatalf("a person past their daily limit has a title made: %d requests", len(*requests))
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		described, _ := tx.GetAgentConversation(quiet.ID)
		if described.Summary != "" || described.DescribedAt != nil {
			t.Fatalf("nothing is marked, so it is tried again later: %+v", described)
		}
	})
}
