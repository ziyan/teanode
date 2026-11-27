package agent_test

import (
	"context"
	"fmt"
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

// A round the model refuses as against its policy is asked once more, and a
// turn the model refuses twice ends saying so rather than as an outage.
func TestARoundThePolicyFlaggedIsAskedOnceMore(t *testing.T) {
	for _, case_ := range []struct {
		refusalCount int
		isAnswered   bool
	}{{refusalCount: 2, isAnswered: true}, {refusalCount: 4, isAnswered: false}} {
		var requestCount atomic.Int32
		model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodGet {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(writer, `{"data":[{"id":"thinker"}]}`)
				return
			}
			body, _ := io.ReadAll(request.Body)
			// Each try of a round is a stream and, when the stream is refused
			// at its start, the same request plainly: two requests a try.
			if int(requestCount.Add(1)) <= case_.refusalCount {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(writer, `{"error":{"message":"Invalid prompt: your prompt was flagged as potentially violating our usage policy."}}`)
				return
			}
			if strings.Contains(string(body), `"stream":true`) {
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(writer, "data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"Matched.\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{"id":"d","model":"m","choices":[{"message":{"role":"assistant","content":"Matched."},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`)
		}))
		database, closeDatabase := dbtest.AcquireDatabase(t)
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
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			if owner, err = tx.CreateUser(&models.User{Username: "riley"}); err != nil {
				t.Fatal(err)
			}
			if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
		})
		run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: &fakeOperations{permissions: models.NewEffectivePermissions(nil)}, Conversation: conversation, Message: "Match these receipt lines.", Surface: "cli"})
		if err != nil {
			t.Fatal(err)
		}
		answered, failure := false, ""
		for _, event := range collect(run) {
			if event.Kind == agent.EventError {
				failure = event.Error
			}
			if strings.Contains(event.Text, "Matched.") {
				answered = true
			}
		}
		if answered != case_.isAnswered {
			t.Errorf("after %d refusals: answered %v, failure %q", case_.refusalCount, answered, failure)
		}
		if !case_.isAnswered && !strings.Contains(failure, "against its usage policy, twice") {
			t.Errorf("a turn refused twice says why: %q", failure)
		}
		model.Close()
		closeDatabase()
	}
}
