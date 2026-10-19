package agent_test

import (
	"context"
	"encoding/json"
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

// describedRound is the one round a titling turn takes: the name and the
// sentence, as the JSON the prompt asks for.
const describedRound = `{"id":"s1","model":"m","choices":[{"delta":{"content":"{\"title\":\"The plumber\",\"summary\":\"Finding the plumber's invoice.\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":40,"completion_tokens":12}}`

// A conversation is described once it has been quiet for a few minutes
// with something said since it was last described; one still moving, or
// already described since its last message, is left alone.
func TestQuietConversationsAreDescribed(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	// Titling is a turn of the loop now, so the answer is streamed like
	// any other round rather than fetched on its own.
	model, _ := fakeModel(t, []string{describedRound})
	defer model.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	// The night is not what this is about, and whether one is due depends
	// on the wall clock: the tick queued a dream in CI at one in the morning
	// and the test saw two jobs where it expected one.
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
	// Titling acts as the person, like every other run of the loop, and so
	// needs somebody to act as.
	operations := &fakeOperations{permissions: models.NewEffectivePermissions(nil)}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

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

// A conversation in which the agent read the person's Health keeps the title
// and the summary it had: the figures it read are not put into a name that
// the list and the picker show. The same conversation without a call of the
// Health tool is described as usual.
func TestAConversationThatReadHealthIsNotDescribed(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, requests := fakeModel(t, []string{describedRound})
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
	operations := &fakeOperations{permissions: models.NewEffectivePermissions(nil)}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

	var readHealth, readActionTool, readNothing *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		make := func(toolName string) *models.AgentConversation {
			conversation, err := tx.CreateAgentConversation(&models.AgentConversation{
				AgentID: found.ID, Kind: models.AgentConversationNamed, Title: "Weekly check", TitledBy: "agent", LastAt: time.Now(),
			})
			if err != nil {
				t.Fatalf("CreateAgentConversation: %s", err)
			}
			assistantCall := &models.AgentMessage{ConversationID: conversation.ID, Role: "assistant", Content: "Let me look."}
			if toolName != "" {
				assistantCall.ToolCalls = []models.AgentToolCall{{ID: "call-1", Name: toolName}}
			}
			for _, message := range []*models.AgentMessage{
				{ConversationID: conversation.ID, Role: "user", Content: "how did I do this week?"},
				assistantCall,
				{ConversationID: conversation.ID, Role: "tool", Content: `{"steps":8412}`},
				{ConversationID: conversation.ID, Role: "assistant", Content: "You averaged 8,412 steps a day."},
			} {
				if _, err := tx.AppendAgentMessage(message); err != nil {
					t.Fatalf("AppendAgentMessage: %s", err)
				}
			}
			updated, err := tx.UpdateAgentConversation(conversation.ID, func(conversation *models.AgentConversation) error {
				conversation.LastAt = time.Now().Add(-10 * time.Minute)
				conversation.Summary = "A check of the week."
				return nil
			})
			if err != nil {
				t.Fatalf("UpdateAgentConversation: %s", err)
			}
			return updated
		}
		readHealth = make("health")
		readActionTool = make("health_summary")
		readNothing = make("weather")
	})

	if err := worker.TickAt(context.Background(), time.Now()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, conversation := range []*models.AgentConversation{readHealth, readActionTool} {
			kept, _ := tx.GetAgentConversation(conversation.ID)
			if kept.Title != "Weekly check" || kept.Summary != "A check of the week." {
				t.Errorf("a conversation that read Health keeps its title and summary: %q, %q", kept.Title, kept.Summary)
			}
			if kept.DescribedAt == nil {
				t.Error("it is still marked described, so that it is not asked about every minute")
			}
		}
		described, _ := tx.GetAgentConversation(readNothing.ID)
		if described.Title != "The plumber" || described.Summary != "Finding the plumber's invoice." {
			t.Errorf("a conversation that read nothing private is described: %q, %q", described.Title, described.Summary)
		}
	})
	// Filing into memory asks the model too; only titling is of interest.
	describing := 0
	for _, request := range *requests {
		raw, _ := json.Marshal(request["messages"])
		if strings.Contains(string(raw), "talking through the describe") {
			describing++
		}
	}
	if describing != 1 {
		t.Errorf("the model was asked to describe %d conversations, want 1: the one that read nothing private", describing)
	}
}
