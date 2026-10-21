package agent_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/conversation"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

type conversationReader struct {
	tools.Run
	database     db.Database
	agent        *models.Agent
	conversation *models.AgentConversation
}

func (self *conversationReader) Database() db.Database                   { return self.database }
func (self *conversationReader) Agent() *models.Agent                    { return self.agent }
func (self *conversationReader) Conversation() *models.AgentConversation { return self.conversation }
func (self *conversationReader) Headless() bool                          { return true }
func (self *conversationReader) CanAsk() bool                            { return false }
func (self *conversationReader) Configuration() *config.Configuration    { return config.Default() }
func (self *conversationReader) Offered() []*tools.Tool                  { return nil }
func (self *conversationReader) Owner() *models.User                     { return nil }

func TestCompactionDoesNotCarryAPrivateNote(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	// The summarizer writes a note of its own. What matters is what it is
	// sent: the transcript, with the private turn in it or not, and the runs
	// that hold each chunk's prompt for the conversation tool to read.
	const secret = "0000-invented"
	model, requests := compactingModel("Learned: the person asked about the shed note and the regatta.", "ok", 0)
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
		add("user", "read me the shed note", "", "")
		add("assistant", "", "", "", models.AgentToolCall{ID: "c1", Name: "passwords", Arguments: `{"action":"read_note","item_id":"x"}`})
		add("tool", `{"note_text":"the shed padlock opens with 0000-invented"}`, "passwords", "c1")
		add("assistant", "It says the padlock opens with 0000-invented.", "", "")
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
	summarizerSaw := 0
	for _, request := range *requests {
		raw, _ := json.Marshal(request["messages"])
		if strings.Contains(string(raw), "Write a note that stands in for it") {
			summarizerSaw++
			if strings.Contains(string(raw), secret) {
				t.Errorf("the summarizer was sent the private note")
			}
		}
	}
	if summarizerSaw == 0 {
		t.Fatalf("the conversation was not compacted, so nothing was proved")
	}

	// A later run that a mail steered: a different conversation of the same agent.
	run := &conversationReader{database: database, agent: found}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		run.conversation, _ = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationRun, LastAt: time.Now()})
	})
	var conversationTool *tools.Tool
	for _, tool := range tools.Build().All() {
		if tool.Name == "conversation" {
			conversationTool = tool
		}
	}
	ctx := tools.WithRun(context.Background(), run)
	call := func(arguments string) string {
		result, err := conversationTool.Run(ctx, &tools.Call{ID: "c", Arguments: json.RawMessage(arguments)})
		if err != nil {
			t.Fatal(err)
		}
		return result.Content
	}
	listed := call(`{"action":"list","limit":50}`)
	var parsed struct {
		Conversations []struct {
			ID    string `json:"conversation_id"`
			Kind  string `json:"kind"`
			Title string `json:"title"`
		} `json:"conversations"`
	}
	_ = json.Unmarshal([]byte(listed), &parsed)
	leaks := 0
	for _, entry := range parsed.Conversations {
		body := call(`{"action":"read","conversation_id":"` + entry.ID + `","limit":100}`)
		if strings.Contains(body, secret) {
			leaks++
			t.Logf("the note is read back from conversation %s (%s, %q)", entry.ID, entry.Kind, entry.Title)
		}
	}
	if leaks > 0 {
		t.Errorf("%d conversation(s) read back the private note", leaks)
	}
}
