package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// compactingModel answers a compaction turn with the note and any other
// round with the answer, and keeps what it was sent.
//
// Writing the note is a turn of the loop in its own right now, so which
// call this is cannot be told from how many came before it: the older
// conversation is read in as many parts as it takes.
func compactingModel(note, answer string) (*httptest.Server, *[]map[string]any) {
	var requests []map[string]any
	var mutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		mutex.Lock()
		requests = append(requests, body)
		mutex.Unlock()
		said := answer
		if strings.Contains(lastThingAsked(body), "Write a note that stands in for it") {
			said = note
		}
		content, _ := json.Marshal(said)
		if stream, _ := body["stream"].(bool); !stream {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer,
				`{"id":"d","model":"m","choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
				content)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer,
			"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
			content)
	}))
	return server, &requests
}

// lastThingAsked is the last message the person's side sent, which is
// where the prompt sits: the persona goes in front of it as a system
// message, so the last message of all is not it.
func lastThingAsked(body map[string]any) string {
	asked := ""
	messages, _ := body["messages"].([]any)
	for _, message := range messages {
		row, _ := message.(map[string]any)
		if row["role"] == "user" {
			asked = toString(row["content"])
		}
	}
	return asked
}

// A long conversation is compacted before the round: the model gets a
// note and the recent turns verbatim. The next turn gets the same note
// and the same tail, not the note alone, and does not compact again.
func TestCompactionKeepsTheRecentTurns(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	const note = "Decided: the regatta is on the 21st. Open: what the mooring costs."
	const answered = "The invoice is [Invoice 42](mail:item1)."
	model, requests := compactingModel(note, answered)
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
	// Writing the note is a turn of its own, and a turn acts as the person.
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) {
		return &fakeOperations{permissions: models.NewEffectivePermissions(nil)}, nil
	})

	var owner *models.User
	var found *models.Agent
	var conversation *models.AgentConversation
	filler := strings.Repeat("The plumber's invoice, the mooring fee, the regatta on the 21st. ", 60)
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
		// Thirty exchanges of a few thousand characters each: well past
		// what a round carries.
		for turn := 1; turn <= 30; turn++ {
			for _, message := range []*models.AgentMessage{
				{ConversationID: conversation.ID, Role: "user", Content: "turn " + strconv.Itoa(turn) + ": " + filler},
				{ConversationID: conversation.ID, Role: "assistant", Content: "reply " + strconv.Itoa(turn) + ": " + filler},
			} {
				if _, err := tx.AppendAgentMessage(message); err != nil {
					t.Fatalf("AppendAgentMessage: %s", err)
				}
			}
		}
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}

	ask := func(message string) {
		run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: message, Surface: "cli"})
		if err != nil {
			t.Fatalf("Ask: %s", err)
		}
		for _, event := range collect(run) {
			if event.Kind == agent.EventError {
				t.Fatalf("the turn failed: %s", event.Error)
			}
		}
	}
	contents := func(request map[string]any) []string {
		var texts []string
		for _, message := range request["messages"].([]any) {
			texts = append(texts, strings.ToValidUTF8(strings.Join(strings.Fields(strings.TrimSpace(toString(message.(map[string]any)["content"]))), " "), ""))
		}
		return texts
	}
	has := func(texts []string, want string) bool {
		for _, text := range texts {
			if strings.Contains(text, want) {
				return true
			}
		}
		return false
	}
	// The round that carried a question, which is the turn itself rather
	// than one of the calls that wrote the note.
	roundAsking := func(question string) []string {
		for _, request := range *requests {
			if texts := contents(request); has(texts, question) {
				return texts
			}
		}
		t.Fatalf("no round carried %q; %d were asked", question, len(*requests))
		return nil
	}

	ask("and the regatta?")
	first := roundAsking("and the regatta?")
	if !strings.HasPrefix(first[1], "Note on the earlier conversation") {
		t.Fatalf("the round should open with the note after the conduct, got %q", first[1][:min(len(first[1]), 60)])
	}
	if !strings.Contains(first[1], note) {
		t.Fatalf("and carry what the note said, got %q", first[1])
	}
	if !has(first, "reply 30:") || has(first, "turn 3:") {
		t.Fatalf("the recent turns stay verbatim and the old ones go: %d messages", len(first))
	}
	if !has(first, "and the regatta?") {
		t.Fatal("the question itself is missing")
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		compacted, _ := tx.GetAgentConversation(conversation.ID)
		if compacted.CompactedThrough == "" {
			t.Fatal("the conversation should record where the transcript resumes")
		}
		stored, _ := tx.ListAgentMessages(conversation.ID, nil)
		notes := 0
		for _, message := range stored {
			if message.Role == "compaction" {
				notes++
			}
			if message.ID == compacted.CompactedThrough && message.Role == "compaction" {
				t.Fatal("the resume point names the note itself, so the kept tail would be lost")
			}
		}
		if notes != 1 {
			t.Fatalf("one note expected, got %d", notes)
		}
		conversation = compacted
	})

	ask("and the mooring fee?")
	second := roundAsking("and the mooring fee?")
	if !strings.HasPrefix(second[1], "Note on the earlier conversation") {
		t.Fatal("the next turn should open with the note")
	}
	for _, want := range []string{"reply 30:", "and the regatta?", answered, "and the mooring fee?"} {
		if !has(second, want) {
			t.Fatalf("the next turn lost %q", want)
		}
	}
	if has(second, "turn 3:") {
		t.Fatal("the next turn replayed what the note stands in for")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, _ := tx.ListAgentMessages(conversation.ID, nil)
		notes := 0
		for _, message := range stored {
			if message.Role == "compaction" {
				notes++
			}
		}
		if notes != 1 {
			t.Fatalf("a conversation that fits is not compacted again, got %d notes", notes)
		}
	})
}

func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
