package channel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/channel"
	"github.com/ziyan/teanode/internal/channel/telegram"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// fakeModel answers each streamed round with the next line of its script;
// what does not stream — the title after a turn — gets a fixed answer.
func fakeModel(script []string) *httptest.Server {
	var mutex sync.Mutex
	round := 0
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); !stream {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"d","model":"m","choices":[{"message":{"role":"assistant","content":"{\"title\":\"Hello\",\"summary\":\"A greeting.\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`))
			return
		}
		mutex.Lock()
		current := round
		round++
		mutex.Unlock()
		if current >= len(script) {
			current = len(script) - 1
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, line := range strings.Split(script[current], "\n") {
			_, _ = writer.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
}

const answerRound = `{"id":"s1","model":"m","choices":[{"delta":{"content":"The invoice is "}}]}
{"id":"s1","choices":[{"delta":{"content":"in Receipts."},"finish_reason":"stop"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`

const deleteRound = `{"id":"s2","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"mail_act","arguments":"{\"action\":\"delete_forever\",\"item_ids\":[\"item1\"]}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s2","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`

const declinedRound = `{"id":"s3","model":"m","choices":[{"delta":{"content":"Understood, I left it."},"finish_reason":"stop"}]}
{"id":"s3","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`

const pageRound = `{"id":"s4","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","function":{"name":"artifact","arguments":"{\"title\":\"The chart\",\"kind\":\"html\",\"content\":\"<!doctype html><html><body><h1>Chart</h1></body></html>\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s4","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`

const pageDoneRound = `{"id":"s5","model":"m","choices":[{"delta":{"content":"Here is the chart."},"finish_reason":"stop"}]}
{"id":"s5","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`

type fakeOperations struct {
	permissions *models.EffectivePermissions
}

func (self *fakeOperations) Permissions() *models.EffectivePermissions { return self.permissions }
func (self *fakeOperations) Execute(ctx context.Context, document string, variables map[string]any, result any) error {
	return json.Unmarshal([]byte(`{}`), result)
}

// fakeTelegram is the Bot API as the bot sees it: updates it is handed,
// and everything it sent.
type fakeTelegram struct {
	mutex   sync.Mutex
	updates []map[string]any
	nextId  int64
	sent    []string // method: body
}

func (self *fakeTelegram) push(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.nextId++
	self.updates = append(self.updates, map[string]any{"update_id": self.nextId, "message": map[string]any{
		"message_id": self.nextId, "from": map[string]any{"id": 42, "first_name": "Alice"},
		"chat": map[string]any{"id": 42, "type": "private"}, "text": text,
	}})
}

func (self *fakeTelegram) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		method := request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:]
		writer.Header().Set("Content-Type", "application/json")
		switch method {
		case "getMe":
			_, _ = writer.Write([]byte(`{"ok":true,"result":{"id":7,"is_bot":true,"first_name":"Bertie","username":"bertie_bot"}}`))
		case "getUpdates":
			var parameters struct {
				Offset int64 `json:"offset"`
			}
			_ = json.Unmarshal(body, &parameters)
			var pending []map[string]any
			for waited := 0; waited < 10; waited++ {
				self.mutex.Lock()
				for _, update := range self.updates {
					if update["update_id"].(int64) >= parameters.Offset {
						pending = append(pending, update)
					}
				}
				self.mutex.Unlock()
				if len(pending) > 0 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			encoded, _ := json.Marshal(map[string]any{"ok": true, "result": pending})
			_, _ = writer.Write(encoded)
		case "sendMessage":
			self.mutex.Lock()
			self.nextId++
			id := self.nextId
			self.sent = append(self.sent, method+": "+string(body))
			self.mutex.Unlock()
			_, _ = fmt.Fprintf(writer, `{"ok":true,"result":{"message_id":%d,"chat":{"id":42,"type":"private"}}}`, id)
		default:
			self.mutex.Lock()
			self.sent = append(self.sent, method+": "+string(body))
			self.mutex.Unlock()
			_, _ = writer.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
}

// wait is for something the bot sent containing the words, or fails.
func (self *fakeTelegram) wait(t *testing.T, words string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		self.mutex.Lock()
		for _, line := range self.sent {
			if strings.Contains(line, words) {
				self.mutex.Unlock()
				return
			}
		}
		self.mutex.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	t.Fatalf("the bot never sent %q; it sent:\n%s", words, strings.Join(self.sent, "\n"))
}

func (self *fakeTelegram) count(method string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	total := 0
	for _, line := range self.sent {
		if strings.HasPrefix(line, method+":") {
			total++
		}
	}
	return total
}

// A bot linked with the code answers a greeting with the streamed answer
// edited into its message, asks for a yes before a deletion and takes
// "no" from the chat, and refuses a chat that is not the linked one.
func TestTelegramBotCarriesThePrimaryConversation(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model := fakeModel([]string{answerRound, deleteRound, declinedRound, pageRound, pageDoneRound})
	defer model.Close()
	configuration := config.Default()
	configuration.Server.Secret = "a-secret-long-enough-to-seal-with-1234567890"
	configuration.Server.Name = "mail.example.com"
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
	permissions := models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}, {Permission: models.PermissionMailWrite}, {Permission: models.PermissionAgentUse}})
	worker.SetOperationsFactory(func(ctx context.Context, owner *models.User) (agent.Operations, error) {
		return &fakeOperations{permissions: permissions}, nil
	})

	sealed, err := worker.SealSecret("TOKEN")
	if err != nil {
		t.Fatalf("SealSecret: %s", err)
	}
	var found *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if _, err := tx.PutAgentChannel(&models.AgentChannel{AgentID: found.ID, Kind: models.AgentChannelTelegram, Token: sealed, LinkCode: "ABC123", Enabled: true}); err != nil {
			t.Fatalf("PutAgentChannel: %s", err)
		}
	})

	fake := &fakeTelegram{}
	server := fake.serve()
	defer server.Close()
	manager := channel.New(&channel.Settings{
		Worker: worker, Database: database, Storage: store, Configuration: func() *config.Configuration { return configuration }, Instance: "test",
		Openers: map[models.AgentChannelKind]channel.Opener{models.AgentChannelTelegram: func(ctx context.Context, token string) (channel.Bot, error) {
			if token != "TOKEN" {
				return nil, fmt.Errorf("the token was not opened: %q", token)
			}
			return telegram.OpenAt(ctx, token, server.URL)
		}},
	})
	manager.Start()
	defer manager.Stop()

	// Unlinked: told how to link; a wrong code refused; the right one links.
	fake.push("hello")
	fake.wait(t, "/link CODE")
	fake.push("/link WRONG")
	fake.wait(t, "not the code")
	fake.push("/link abc123")
	fake.wait(t, "Linked.")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, _ := tx.GetAgentChannel(found.ID, models.AgentChannelTelegram)
		if stored == nil || stored.LinkedID != "42" || stored.LinkedName == "" || stored.BotName != "@bertie_bot" {
			t.Fatalf("the link is kept on the row: %+v", stored)
		}
	})

	// A greeting: typing, a preview, and the answer in the end.
	fake.push("where is the invoice?")
	fake.wait(t, "The invoice is in Receipts.")
	if fake.count("sendChatAction") == 0 {
		t.Fatal("the bot types while it works")
	}

	// A deletion asks, and no means no.
	fake.push("delete the invoice for good")
	fake.wait(t, "Reply yes or no")
	fake.push("no")
	fake.wait(t, "Understood, I left it.")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		conversations, _ := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationMain}, nil)
		if len(conversations) != 1 {
			t.Fatalf("one primary conversation: %d", len(conversations))
		}
		messages, _ := tx.ListAgentMessages(conversations[0].ID, nil)
		roles := ""
		for _, message := range messages {
			roles += message.Role + " "
		}
		if !strings.HasPrefix(roles, "user assistant user assistant") {
			t.Fatalf("the turns are in the primary conversation: %s", roles)
		}
	})

	// /status answers; /new starts a fresh primary conversation.
	fake.push("/status")
	fake.wait(t, "Agent: Bertie")
	fake.push("/new")
	fake.wait(t, "A fresh primary conversation.")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mains, _ := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationMain}, nil)
		named, _ := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationNamed}, nil)
		if len(mains) != 1 || len(named) != 1 {
			t.Fatalf("a fresh main and the old one named: %d main, %d named", len(mains), len(named))
		}
	})

	// A page the agent made goes as a link that opens without a sign-in,
	// not as a file: the app would show the file and never draw it.
	documents := fake.count("sendDocument")
	fake.push("make me a chart")
	fake.wait(t, "Here is the chart.")
	fake.wait(t, "https://mail.example.com/api/v1/agent/attachments/")
	fake.wait(t, "?share=")
	if fake.count("sendDocument") != documents {
		t.Fatal("the page is not sent as a file too")
	}
}
