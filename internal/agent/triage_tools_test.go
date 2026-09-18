package agent_test

import (
	"context"
	"encoding/json"
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

// sortingWorld is a granted mailbox with one message in it, and a model that
// answers whatever the script says: the streaming rounds of the loop, and
// anything asked of the model outside it.
type sortingWorld struct {
	database    db.Database
	worker      *agent.Agent
	owner       *models.User
	agent       *models.Agent
	mailbox     *models.Mailbox
	mail        *models.Mail
	streamed    *[]map[string]any
	directCalls *int
}

// newSortingWorld builds that world. rounds are the streamed rounds of the
// loop, one after another; direct is what any call made outside the loop is
// answered with, and how many of those were made is counted, because the
// sorting run should make none.
func newSortingWorld(t *testing.T, rounds []string, direct string) *sortingWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)

	var requests []map[string]any
	calls := 0
	var mutex sync.Mutex
	model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); !stream {
			// Not a round of the loop: the description a conversation is
			// given, and nothing else.
			mutex.Lock()
			calls++
			mutex.Unlock()
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(direct))
			return
		}
		mutex.Lock()
		requests = append(requests, body)
		round := len(requests) - 1
		mutex.Unlock()
		if round >= len(rounds) {
			round = len(rounds) - 1
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, line := range strings.Split(rounds[round], "\n") {
			_, _ = writer.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(model.Close)

	configuration := config.Default()
	configuration.Agent.Enabled = true
	// The night is not what this is about, and whether one is due depends
	// on the wall clock: the tick queued a dream in CI at one in the morning
	// and the test saw two jobs where it expected one.
	configuration.Agent.Features.Dreaming = new(bool)
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:sorter"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{
		Database: database, Storage: store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

	world := &sortingWorld{database: database, worker: worker, streamed: &requests, directCalls: &calls}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if world.owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if world.agent, err = tx.CreateAgent(&models.Agent{UserID: world.owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if world.mailbox, err = tx.CreateMailbox(&models.Mailbox{
			UserID: world.owner.ID, Name: "Personal",
			Agent: &models.AgentMailbox{Granted: true, Triage: &models.AgentTriage{Enabled: true}},
		}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if world.mail, err = tx.CreateMail(&models.Mail{
			Subject: "Thursday?", Kind: models.MailKindIncoming, Sender: "maria@example.net",
			Recipients: []string{"alice@example.com"},
			Headers:    []string{"From: Maria <maria@example.net>", "To: alice@example.com", "Subject: Thursday?"},
		}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		headers := []string{"From: Maria <maria@example.net>", "To: alice@example.com", "Subject: Thursday?", "Content-Type: text/plain; charset=utf-8"}
		if err := store.Put(context.Background(), world.mail.ID, headers, []byte("Can you do Thursday at 3?\r\n")); err != nil {
			t.Fatalf("store.Put: %s", err)
		}
		worker.OnMailboxDelivery(tx, world.mailbox, nil, world.mail)
	})
	return world
}

func (self *sortingWorld) sort(t *testing.T) *models.MailInsight {
	t.Helper()
	if err := self.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	self.worker.Wait()
	var insight *models.MailInsight
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		insights, err := tx.GetMailInsights(self.mailbox.ID, []string{self.mail.ID})
		if err != nil {
			t.Fatalf("GetMailInsights: %s", err)
		}
		insight = insights[self.mail.ID]
	})
	return insight
}

const sortedObject = `{\"category\":\"personal\",\"priority\":\"high\",\"needs_reply\":true,\"research\":false,\"summary\":\"Maria asks about Thursday.\",\"action_items\":[\"Answer Maria\"]}`

// Sorting can look something up before it decides.
//
// It was one call at one prompt: the model saw the message and nothing else,
// so "is this somebody Alice knows" was a question it could only guess at.
// The run is a turn of the conversation loop now, with a short read-only set
// of tools, and it answers with the same object it always did.
func TestSortingCanLookSomethingUpFirst(t *testing.T) {
	searched := `{"id":"s1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"mail_search","arguments":"{\"query\":\"maria@example.net\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`
	answered := `{"id":"s2","model":"m","choices":[{"delta":{"content":"` + sortedObject + `"},"finish_reason":"stop"}]}
{"id":"s2","choices":[],"usage":{"prompt_tokens":120,"completion_tokens":30}}`

	world := newSortingWorld(t, []string{searched, answered}, `{"id":"d","model":"m","choices":[{"message":{"role":"assistant","content":"{\"title\":\"t\",\"summary\":\"s\"}"},"finish_reason":"stop"}],"usage":{}}`)
	insight := world.sort(t)

	if insight == nil || insight.Category != "personal" || insight.Priority != "high" || !insight.NeedsReply {
		t.Fatalf("the insight the run answered with: %+v", insight)
	}
	if len(*world.streamed) < 2 {
		t.Fatalf("the run should have taken two rounds, took %d", len(*world.streamed))
	}
	// The tools it was offered are the short set, and every one of them is
	// a read: a sorting run that could write would be a stranger's message
	// deciding what happens to a mailbox.
	offered := map[string]bool{}
	for _, tool := range (*world.streamed)[0]["tools"].([]any) {
		function, _ := tool.(map[string]any)["function"].(map[string]any)
		offered[function["name"].(string)] = true
	}
	if !offered["mail_search"] || !offered["mail_read"] || !offered["datetime"] {
		t.Fatalf("the sorting set: %v", offered)
	}
	for _, forbidden := range []string{"mail_send", "mail_act", "mail_draft", "rule", "browser", "shell"} {
		if offered[forbidden] {
			t.Errorf("%s has no business in a sorting run: %v", forbidden, offered)
		}
	}

	// And the transcript is the run, with the lookup in it, named for what
	// the sorting turned out to be.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		runs, err := tx.ListAgentConversations(world.agent.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil || len(runs) != 1 {
			t.Fatalf("one transcript: %v %v", runs, err)
		}
		if runs[0].ID != insight.RunID {
			t.Fatalf("the insight points at it: %q %q", runs[0].ID, insight.RunID)
		}
		if !strings.HasPrefix(runs[0].Title, "Sorted") {
			t.Fatalf("named for what it did: %q", runs[0].Title)
		}
		messages, _ := tx.ListAgentMessages(runs[0].ID, nil)
		calls := 0
		for _, message := range messages {
			calls += len(message.ToolCalls)
		}
		if calls == 0 {
			t.Fatalf("the lookup should be in the transcript: %d messages", len(messages))
		}
	})
}

// A model that will not end with the object has not sorted the message.
//
// There used to be a single call behind the loop to catch this: a turn that
// ended in prose was asked again, at one prompt, and whatever it said then
// was filed. Every model call is a turn of the loop now, and there is no
// second way of asking, so a run that ends in prose files nothing and the
// job says why -- which is a failure the person can see and retry, rather
// than a sorting done quietly by another route.
func TestSortingWillNotFileProseAsAnInsight(t *testing.T) {
	prose := `{"id":"s1","model":"m","choices":[{"delta":{"content":"I think this one is personal, and fairly urgent."},"finish_reason":"stop"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`
	// Nothing outside the loop should be asked at all; this is what such a
	// call would be answered with, so that making one is visible.
	direct := `{"id":"c1","model":"m","choices":[{"message":{"role":"assistant","content":"` + sortedObject + `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":80,"completion_tokens":20}}`

	world := newSortingWorld(t, []string{prose}, direct)

	if insight := world.sort(t); insight != nil {
		t.Fatalf("prose is not a sorting: %+v", insight)
	}
	if *world.directCalls != 0 {
		t.Fatalf("no call was made for sorting outside the loop, got %d", *world.directCalls)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		jobs, err := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: world.agent.ID}, nil)
		if err != nil {
			t.Fatalf("ListAgentJobs: %s", err)
		}
		if len(jobs) != 1 || jobs[0].Kind != models.AgentJobTriage {
			t.Fatalf("the one triage job: %+v", jobs)
		}
		if !strings.Contains(jobs[0].Error, "did not end with the object") {
			t.Fatalf("the job should say what went wrong: %+v", jobs[0])
		}
	})
}
