package agent_test

import (
	"context"
	"encoding/json"
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

// fakeOperations is the API as a test person: it answers the documents the
// mailbox tools send with canned data, and records what was asked.
type fakeOperations struct {
	mutex       sync.Mutex
	documents   []string
	permissions *models.EffectivePermissions
}

func (self *fakeOperations) Permissions() *models.EffectivePermissions { return self.permissions }

func (self *fakeOperations) Execute(ctx context.Context, document string, variables map[string]any, result any) error {
	self.mutex.Lock()
	self.documents = append(self.documents, document)
	self.mutex.Unlock()
	answer := `{}`
	switch {
	case strings.Contains(document, "ListMailboxes"):
		answer = `{"ListMailboxes":[{"mailbox":{"id":"mb1","name":"Personal","userId":"u1","addresses":[{"address":"alice@example.com"}],"rules":[],"agent":{"granted":true,"triage":{"enabled":true}}},"folders":[{"id":"f-inbox","mailboxId":"mb1","name":"Inbox","kind":"inbox","unread":2,"total":10},{"id":"f-trash","mailboxId":"mb1","name":"Trash","kind":"trash","unread":0,"total":0}]}]}`
	case strings.Contains(document, "ListMailboxThreads"):
		answer = `{"ListMailboxThreads":{"total":1,"threads":[{"threadId":"m1","count":1,"unread":1,"flagged":false,"participants":["Bob the Builder"],"itemIds":["item1"],"item":{"id":"item1","folderId":"f-inbox","mailId":"m1","seen":false,"flagged":false,"insight":{"category":"receipt","priority":"normal","needsReply":false,"summary":"An invoice for the roof."},"mail":{"id":"m1","from":"bob@builders.example","fromName":"Bob the Builder","subject":"Invoice 42","receivedAt":"2026-09-09T10:00:00Z"}}}]}}`
	case strings.Contains(document, "GetMailboxThread"):
		answer = `{"GetMailboxThread":{"threadId":"m1","subject":"Invoice 42","summary":null,"items":[{"folderId":"f-inbox","folderName":"Inbox","item":{"id":"item1","mailId":"m1","seen":false,"flagged":false,"draft":false,"mail":{"id":"m1","from":"bob@builders.example","fromName":"Bob the Builder","recipients":["alice@example.com"],"subject":"Invoice 42","receivedAt":"2026-09-09T10:00:00Z","messageId":"<1@builders.example>"}}}]}}`
	case strings.Contains(document, "DeleteMailboxItems"):
		answer = `{"DeleteMailboxItems":true}`
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal([]byte(answer), result)
}

// fakeModel is an OpenAI-shaped server that answers each round of a turn
// from a script: a tool call, then words.
func fakeModel(t *testing.T, script []string) (*httptest.Server, *[]map[string]any) {
	var requests []map[string]any
	var mutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); !stream {
			// The one call that does not stream is the description a
			// conversation gets after a turn; it is not a round of the
			// script.
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"d","model":"m","choices":[{"message":{"role":"assistant","content":"{\"title\":\"The plumber\",\"summary\":\"Finding the plumber's invoice.\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":40,"completion_tokens":12}}`))
			return
		}
		mutex.Lock()
		requests = append(requests, body)
		round := len(requests) - 1
		mutex.Unlock()
		if round >= len(script) {
			round = len(script) - 1
		}
		{
			writer.Header().Set("Content-Type", "text/event-stream")
			for _, line := range strings.Split(script[round], "\n") {
				_, _ = writer.Write([]byte("data: " + line + "\n\n"))
			}
			_, _ = writer.Write([]byte("data: [DONE]\n\n"))
		}
	}))
	return server, &requests
}

const toolCallRound = `{"id":"s1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"mail_search","arguments":"{\"query\":\"plumber\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`

const answerRound = `{"id":"s2","model":"m","choices":[{"delta":{"content":"The invoice is "}}]}
{"id":"s2","choices":[{"delta":{"content":"[Invoice 42](mail:item1)."},"finish_reason":"stop"}]}
{"id":"s2","choices":[],"usage":{"prompt_tokens":120,"completion_tokens":8}}`

const deleteRound = `{"id":"s3","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","function":{"name":"mail_act","arguments":"{\"action\":\"delete_forever\",\"item_ids\":[\"item1\"]}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s3","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`

const declinedRound = `{"id":"s4","model":"m","choices":[{"delta":{"content":"Understood, I left it."},"finish_reason":"stop"}]}
{"id":"s4","choices":[],"usage":{"prompt_tokens":50,"completion_tokens":5}}`

func collect(run *agent.AskRun) []agent.Event {
	events, unsubscribe := run.Subscribe()
	defer unsubscribe()
	var collected []agent.Event
	for event := range events {
		collected = append(collected, event)
	}
	return collected
}

// A turn: the model asks for a search, gets its rows, and answers with a
// citation; the transcript is kept; a second turn that reaches for
// delete_forever stops at a confirmation card the person declines.
func TestAskRunsToolsAndAsksBeforeDestroying(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, requests := fakeModel(t, []string{toolCallRound, answerRound, deleteRound, declinedRound})
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
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}, {Permission: models.PermissionMailWrite}})}

	run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "find the invoice from the plumber", Surface: "cli", Viewing: &agent.Viewing{FolderName: "Inbox"}})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	events := collect(run)
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, string(event.Kind))
	}
	joined := strings.Join(kinds, " ")
	for _, want := range []string{"tool_call tool_result", "text", "message done"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("events %v lack %q", kinds, want)
		}
	}
	var answer string
	for _, event := range events {
		if event.Kind == agent.EventMessage {
			answer = event.Text
		}
		if event.Kind == agent.EventToolResult && !strings.Contains(event.Text, "Invoice 42") {
			t.Fatalf("the search result should carry the row: %s", event.Text)
		}
	}
	if answer != "The invoice is [Invoice 42](mail:item1)." {
		t.Fatalf("answer %q", answer)
	}
	// What the model was given: the conduct with the situation, the tools,
	// the viewing overlay, and on the second round the tool's answer.
	first := (*requests)[0]
	system := first["messages"].([]any)[0].(map[string]any)["content"].(string)
	for _, want := range []string{"Bertie", "Alice Example", "Mailboxes you may reach", `"Personal"`, "## Situation"} {
		if !strings.Contains(system, want) {
			t.Fatalf("the system prompt lacks %q", want)
		}
	}
	if tools, _ := first["tools"].([]any); len(tools) < 5 {
		t.Fatalf("the tools were not sent: %d", len(tools))
	}
	overlay := fmt.Sprint(first["messages"].([]any)[len(first["messages"].([]any))-1].(map[string]any)["content"])
	if !strings.Contains(overlay, "<viewing>") || !strings.Contains(overlay, "<now>") {
		t.Fatalf("the overlays are missing: %q", overlay)
	}
	second := (*requests)[1]
	messages := second["messages"].([]any)
	if role := messages[len(messages)-2].(map[string]any)["role"]; role != "tool" {
		t.Fatalf("the second round should carry the tool's answer before the overlays, got %v", role)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, _ := tx.ListAgentMessages(conversation.ID, nil)
		roles := make([]string, 0, len(stored))
		for _, message := range stored {
			roles = append(roles, message.Role)
		}
		if strings.Join(roles, " ") != "user assistant tool assistant" {
			t.Fatalf("stored roles %v", roles)
		}
		totals, _ := tx.SumAgentUsage(found.ID, time.Now().Add(-time.Hour))
		if totals.Calls != 2 || totals.PromptTokens != 220 {
			t.Fatalf("usage %+v", totals)
		}
	})

	// The second turn: delete_forever asks first, and no means no.
	run, err = worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "delete it for good", Surface: "cli"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	events2, unsubscribe := run.Subscribe()
	var confirmation *agent.Event
	for event := range events2 {
		if event.Kind == agent.EventConfirmation {
			copied := event
			confirmation = &copied
			if !run.Resolve(event.CallID, false) {
				t.Fatal("the card should be open")
			}
		}
		if event.Kind == agent.EventDone {
			break
		}
	}
	unsubscribe()
	if confirmation == nil || confirmation.Tool != "mail_act" || confirmation.Risk != "destructive" {
		t.Fatalf("a confirmation card was expected: %+v", confirmation)
	}
	for _, document := range operations.documents {
		if strings.Contains(document, "DeleteMailboxItems") {
			t.Fatal("nothing may be deleted when the person declines")
		}
	}
	// A declined call is answered to the model as declined, and the loop
	// goes on to the model's last word.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, _ := tx.ListAgentMessages(conversation.ID, nil)
		last := stored[len(stored)-1]
		if last.Role != "assistant" || last.Content != "Understood, I left it." {
			t.Fatalf("the last message should be the model's word after the decline: %+v", last)
		}
		for _, message := range stored {
			if message.Role == "tool" && message.ToolCallID == "call_2" && !strings.Contains(message.Content, "declined") {
				t.Fatalf("the declined call should say so: %s", message.Content)
			}
		}
	})
}
