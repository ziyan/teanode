package apigraph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// Handing the tab back answers the agent's newest open request for it, and
// only that one: an older request, in another conversation, asked for
// something else and stays open, as does a question. Naming a request by
// its run and call answers that one instead.
func TestHandingBackAnswersOnlyTheNewestRequestOrTheOneNamed(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	// A model that says one short thing, for the turns the answers resume.
	model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Carrying on.\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer model.Close()

	var person *models.User
	var found *models.Agent
	var older, newer, question *models.AgentInteraction
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "browsing", Name: "Example Person"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		card := func(conversationTitle string, kind models.InteractionKind, text string) *models.AgentInteraction {
			conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationNamed, Title: conversationTitle, Surface: "drawer", LastAt: time.Now()})
			if err != nil {
				t.Fatalf("CreateAgentConversation: %s", err)
			}
			kept, err := tx.CreateAgentInteraction(&models.AgentInteraction{
				AgentID: found.ID, ConversationID: conversation.ID, RunID: "run-" + conversationTitle, CallID: "call-" + conversationTitle,
				InteractionKind: kind, ToolName: "browser", InteractionText: text,
			})
			if err != nil {
				t.Fatalf("CreateAgentInteraction: %s", err)
			}
			time.Sleep(20 * time.Millisecond)
			return kept
		}
		older = card("older", models.InteractionTakeOver, "Sign in to the shop")
		newer = card("newer", models.InteractionTakeOver, "Enter the code sent to your phone")
		question = card("question", models.InteractionQuestion, "Which color?")
	})

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: config.AgentProviderKindOpenAI, BaseURL: model.URL, APIKey: "test-key"}}
	configuration.Agent.Models.Default = "fake:thinker"
	isDreaming := false
	configuration.Agent.Features.Dreaming = &isDreaming
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agentpackage.New(&agentpackage.Settings{
		Database: database, Storage: store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour,
	})
	resolver := &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{Agent: worker}}
	principal := &api.Principal{User: person, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	handBack := func(arguments HandBackAgentTabArguments) bool {
		var isHandedBack bool
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)
			var err error
			if isHandedBack, err = resolver.HandBackAgentTab(ctx, arguments); err != nil {
				t.Fatalf("HandBackAgentTab: %s", err)
			}
		})
		return isHandedBack
	}
	answerOf := func(card *models.AgentInteraction) string {
		var answer string
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			read, err := tx.GetAgentInteractionByCall(found.ID, card.RunID, card.CallID)
			if err != nil || read == nil {
				t.Fatalf("GetAgentInteractionByCall: %v %v", read, err)
			}
			if read.ResolvedAt != nil {
				answer = read.InteractionAnswer
			}
		})
		return answer
	}

	if !handBack(HandBackAgentTabArguments{Note: "Typed the code"}) {
		t.Fatal("handing back with a request open said there was nothing to hand back")
	}
	if answer := answerOf(newer); answer != models.InteractionHandedBack+"\nTyped the code" {
		t.Fatalf("the newest request was answered %q", answer)
	}
	if answer := answerOf(older); answer != "" {
		t.Fatalf("an older request in another conversation was answered %q", answer)
	}
	if answer := answerOf(question); answer != "" {
		t.Fatalf("a question was answered %q", answer)
	}

	// Named by run and call, the older one is answered.
	handBack(HandBackAgentTabArguments{RunID: older.RunID, CallID: older.CallID})
	if answer := answerOf(older); !strings.HasPrefix(answer, models.InteractionHandedBack) {
		t.Fatalf("the request named was answered %q", answer)
	}
	if answer := answerOf(question); answer != "" {
		t.Fatalf("a question was answered %q", answer)
	}
	// Nothing left to answer, and nobody held the tab.
	if handBack(HandBackAgentTabArguments{}) {
		t.Fatal("handing back with nothing open and nothing held said it handed back")
	}
}
