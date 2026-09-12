package agent_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// fakeChrome answers the DevTools commands the browser tool sends.
type commandLog struct {
	mutex    sync.Mutex
	commands []string
}

func (self *commandLog) add(command string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.commands = append(self.commands, command)
}

func (self *commandLog) joined() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return strings.Join(self.commands, " ")
}

func fakeChrome(t *testing.T) (*httptest.Server, *commandLog) {
	commands := &commandLog{}
	upgrader := websocket.Upgrader{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/json/version" {
			_ = json.NewEncoder(writer).Encode(map[string]any{"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools"})
			return
		}
		socket, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = socket.Close() }()
		for {
			var incoming struct {
				ID        int64           `json:"id"`
				SessionID string          `json:"sessionId"`
				Method    string          `json:"method"`
				Params    json.RawMessage `json:"params"`
			}
			if err := socket.ReadJSON(&incoming); err != nil {
				return
			}
			commands.add(incoming.Method)
			var result any = map[string]any{}
			switch incoming.Method {
			case "Target.createBrowserContext":
				result = map[string]any{"browserContextId": "c"}
			case "Target.createTarget":
				result = map[string]any{"targetId": "t"}
			case "Target.attachToTarget":
				result = map[string]any{"sessionId": "s"}
			case "Runtime.evaluate":
				var params struct {
					Expression string `json:"expression"`
				}
				_ = json.Unmarshal(incoming.Params, &params)
				value := any(true)
				switch {
				case strings.Contains(params.Expression, "location.href, title"):
					value = map[string]any{"url": "https://carrier.example/track/42", "title": "Parcel 42"}
				case strings.Contains(params.Expression, "__teanodeNextRef"):
					value = map[string]any{"title": "Parcel 42", "url": "https://carrier.example/track/42", "text": "h1: Parcel 42\nDelivery window: Friday 9-12\n[ref=1] button \"Change delivery\"", "truncated": false}
				}
				result = map[string]any{"result": map[string]any{"type": "object", "value": value}}
			}
			encoded, _ := json.Marshal(result)
			_ = socket.WriteJSON(map[string]any{"id": incoming.ID, "sessionId": incoming.SessionID, "result": json.RawMessage(encoded)})
		}
	}))
	return server, commands
}

const browseRound = `{"id":"b1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_n","function":{"name":"browser","arguments":"{\"action\":\"steps\",\"steps\":[{\"action\":\"navigate\",\"url\":\"https://carrier.example/track/42\"},{\"action\":\"snapshot\"}]}"}}]},"finish_reason":"tool_calls"}]}
{"id":"b1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`

const typeRound = `{"id":"b2","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_t","function":{"name":"browser","arguments":"{\"action\":\"type\",\"ref\":1,\"text\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"b2","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`

const windowRound = `{"id":"b3","model":"m","choices":[{"delta":{"content":"Friday, 9 to 12."},"finish_reason":"stop"}]}
{"id":"b3","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`

// The headless browser opens the carrier's page in a fresh context, reads
// it, and answers; a run with nobody present may read but not type.
func TestBrowserToolReadsAPageHeadless(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	chrome, commands := fakeChrome(t)
	defer chrome.Close()
	model, _ := fakeModel(t, []string{browseRound, typeRound, windowRound})
	defer model.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	configuration.Agent.Browser.Enabled = true
	configuration.Agent.Browser.CDPEndpoint = chrome.URL
	configuration.Agent.Browser.AllowPrivateAddresses = []string{"carrier.example"}
	registry, _ := llm.Open(&configuration.Agent)
	store, _ := storage.Open(&storage.Settings{Directory: t.TempDir()})
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})

	var owner *models.User
	var found *models.Agent
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, _ = tx.CreateUser(&models.User{Username: "alice"})
		found, _ = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		conversation, _ = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationRun, JobKind: "research", LastAt: time.Now()})
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "what does the carrier say?", Surface: "research", Headless: true, ReadOnly: true, Allow: map[string]bool{"browser": true}})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	events := collect(run)
	var results []string
	answer := ""
	for _, event := range events {
		if event.Kind == agent.EventToolResult {
			results = append(results, event.Text)
		}
		if event.Kind == agent.EventMessage {
			answer = event.Text
		}
	}
	if len(results) != 2 || !strings.Contains(results[0], "Delivery window: Friday 9-12") || !strings.Contains(results[0], "<untrusted-data>") {
		t.Fatalf("the page should have been read as data: %v", results)
	}
	if !strings.Contains(results[1], "may only read") {
		t.Fatalf("a headless run must not type: %s", results[1])
	}
	if answer != "Friday, 9 to 12." {
		t.Fatalf("answer %q", answer)
	}
	joined := commands.joined()
	for _, want := range []string{"Target.createBrowserContext", "Fetch.enable", "Page.navigate", "Target.disposeBrowserContext"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the browser should have %s: %s", want, joined)
		}
	}
}
