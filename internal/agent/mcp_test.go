package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// fakeMCP is a connected server with one tool, over HTTP, that records
// what it was asked.
func fakeMCP(t *testing.T) (*httptest.Server, *[]string) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var message struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(request.Body).Decode(&message)
		if message.ID == nil {
			writer.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch message.Method {
		case "initialize":
			writer.Header().Set("Mcp-Session-Id", "s")
			result = map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]any{"name": "tracker", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "track", "description": "Where a parcel is.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"number": map[string]any{"type": "string"}}}},
				{"name": "cancel", "description": "Cancel a shipment.", "inputSchema": map[string]any{"type": "object"}},
			}}
		case "tools/call":
			calls = append(calls, string(message.Params))
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "the parcel is in Hamburg"}}}
		}
		encoded, _ := json.Marshal(result)
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":` + strings.TrimSpace(string(mustInt(*message.ID))) + `,"result":` + string(encoded) + `}`))
	}))
	return server, &calls
}

func mustInt(value int64) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

const trackRound = `{"id":"s1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_t","function":{"name":"mcp__tracker__track","arguments":"{\"number\":\"42\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`

const cancelRound = `{"id":"s2","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_c","function":{"name":"mcp__tracker__cancel","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s2","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`

const foundRound = `{"id":"s3","model":"m","choices":[{"delta":{"content":"It is in Hamburg."},"finish_reason":"stop"}]}
{"id":"s3","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`

// A connected server's tools reach the person's conversation namespaced by
// server: a read-only one runs without a card, any other stops at one.
func TestConnectedServerToolsAreOfferedWithTheirRisk(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	remote, calls := fakeMCP(t)
	defer remote.Close()
	model, _ := fakeModel(t, []string{trackRound, cancelRound, foundRound})
	defer model.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	configuration.Agent.MCP.Servers = []config.AgentMCPServer{{Name: "tracker", URL: remote.URL, ReadOnly: []string{"track"}}}
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, _ := storage.Open(&storage.Settings{Directory: t.TempDir()})
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})

	var owner *models.User
	var found *models.Agent
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, _ = tx.CreateUser(&models.User{Username: "alice"})
		found, _ = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		conversation, _ = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "where is parcel 42, and cancel it", Surface: "cli"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	events, unsubscribe := run.Subscribe()
	var kinds []string
	var confirmed *agent.Event
	for event := range events {
		kinds = append(kinds, string(event.Kind)+":"+event.Tool)
		if event.Kind == agent.EventConfirmation {
			copied := event
			confirmed = &copied
			run.Resolve(event.CallID, true)
		}
		if event.Kind == agent.EventDone {
			break
		}
	}
	unsubscribe()
	joined := strings.Join(kinds, " ")
	if !strings.Contains(joined, "tool_call:mcp__tracker__track tool_result:mcp__tracker__track") {
		t.Fatalf("the read-only tool should run without a card: %v", kinds)
	}
	if confirmed == nil || confirmed.Tool != "mcp__tracker__cancel" || confirmed.Risk != "outward" {
		t.Fatalf("the other tool should ask first as outward: %+v", confirmed)
	}
	if len(*calls) != 2 || !strings.Contains((*calls)[0], `"number":"42"`) {
		t.Fatalf("the server should have been called twice: %v", *calls)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		messages, _ := tx.ListAgentMessages(conversation.ID, nil)
		for _, message := range messages {
			if message.Role == "tool" && message.Name == "mcp__tracker__track" && !strings.Contains(message.Content, "<untrusted-data>") {
				t.Fatalf("a remote answer is data: %s", message.Content)
			}
		}
	})
}

// Research: a message triage flagged gets notes from a headless turn that
// may only read, and the notes land on the insight.
func TestResearchWritesNotesOnTheInsight(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	model, requests := fakeModel(t, []string{`{"id":"r1","model":"m","choices":[{"delta":{"content":"The tracker says the parcel is in Hamburg, due Friday."},"finish_reason":"stop"}]}
{"id":"r1","choices":[],"usage":{"prompt_tokens":40,"completion_tokens":10}}`})
	defer model.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	registry, _ := llm.Open(&configuration.Agent)
	store, _ := storage.Open(&storage.Settings{Directory: t.TempDir()})
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}, {Permission: models.PermissionMailWrite}})}
	worker.SetOperationsFactory(func(ctx context.Context, owner *models.User) (agent.Operations, error) { return operations, nil })

	var owner *models.User
	var found *models.Agent
	var mailbox *models.Mailbox
	var mail *models.Mail
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		owner, _ = tx.CreateUser(&models.User{Username: "alice", Name: "Alice"})
		found, _ = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true, Research: true}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		inbox, _ := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		mail, _ = tx.CreateMail(&models.Mail{Subject: "Your parcel", Kind: models.MailKindIncoming, Sender: "shop@example.net", ReceivedAt: time.Now()}, nil)
		_ = store.Put(context.Background(), mail.ID, []string{"Content-Type: text/plain"}, []byte("Parcel 42 has shipped."))
		_, _ = tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{})
		_ = tx.PutMailInsight(&models.MailInsight{MailID: mail.ID, MailboxID: mailbox.ID, AgentID: found.ID, Category: "notification", Priority: "normal", ResearchAsked: true, Summary: "A parcel shipped.", ActionItems: []string{"Track parcel 42"}})
		if _, err := worker.Enqueue(tx, models.AgentJobResearch, found.ID, mailbox.ID, mail.ID); err != nil {
			t.Fatalf("Enqueue: %s", err)
		}
	})
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var notes string
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			insights, _ := tx.GetMailInsights(mailbox.ID, []string{mail.ID})
			if insight := insights[mail.ID]; insight != nil {
				notes = insight.Notes
			}
		})
		if notes != "" {
			if !strings.Contains(notes, "Hamburg") {
				t.Fatalf("notes %q", notes)
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		insights, _ := tx.GetMailInsights(mailbox.ID, []string{mail.ID})
		if insights[mail.ID] == nil || insights[mail.ID].Notes == "" || insights[mail.ID].NotesRunID == "" {
			t.Fatalf("the notes should be on the insight with the run: %+v", insights[mail.ID])
		}
	})
	first := (*requests)[0]
	tools, _ := first["tools"].([]any)
	for _, entry := range tools {
		name := entry.(map[string]any)["function"].(map[string]any)["name"]
		if name == "mail_act" || name == "mail_send" || name == "mail_draft" {
			t.Fatalf("a research turn may only read, but was offered %v", name)
		}
	}
	prompt := first["messages"].([]any)[1].(map[string]any)["content"].(string)
	if !strings.Contains(prompt, "Parcel 42 has shipped") || !strings.Contains(prompt, "Track parcel 42") {
		t.Fatalf("the research prompt lacks the message or the action items: %s", prompt)
	}
}
