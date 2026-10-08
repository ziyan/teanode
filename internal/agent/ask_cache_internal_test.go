package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Every round of a turn sends the same front: the system prompt and the
// tools, byte for byte, so the provider can serve it from its cache.
//
// The self page carries the facts used most lately, and using one during
// the turn -- recall does, after the first round -- used to change which
// the next round's prompt carried, so every round after the first was paid
// for in full.
func TestTheRoundsOfATurnSendTheSamePrompt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var mutex sync.Mutex
	var systems, toolLists []string
	var facts []*models.AgentFact
	var agentId string
	answers := []string{
		`{"id":"s1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"datetime","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"s2","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","function":{"name":"datetime","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"s3","model":"m","choices":[{"delta":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1}}`,
	}
	var worker *Agent
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var body struct {
			Stream   bool              `json:"stream"`
			Messages []json.RawMessage `json:"messages"`
			Tools    json.RawMessage   `json:"tools"`
		}
		_ = json.Unmarshal(raw, &body)
		if !body.Stream {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],"usage":{}}`))
			return
		}
		mutex.Lock()
		round := len(systems)
		systems = append(systems, string(body.Messages[0]))
		toolLists = append(toolLists, string(body.Tools))
		mutex.Unlock()
		// Between rounds, the older fact is used, which puts it first.
		if round == 0 {
			if err := worker.settings.Database.Transaction(func(tx db.Transaction) error {
				return tx.TouchAgentFacts([]string{facts[0].ID}, time.Now().Add(time.Hour))
			}); err != nil {
				t.Errorf("TouchAgentFacts: %s", err)
			}
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: " + answers[min(round, len(answers)-1)] + "\n\ndata: [DONE]\n\n"))
	}))
	defer provider.Close()

	var run *Run
	worker, run = digestSplitWorld(t, database, provider.URL)
	agentId = run.Agent.ID
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		self, err := tx.GetAgentNode(agentId, models.PathSelf)
		if err != nil || self == nil {
			t.Fatalf("GetAgentNode: %v, %v", self, err)
		}
		// More facts than the prompt carries, all used but the first,
		// which is left out until it is used.
		recent := []string{}
		for index := range selfFacts + 1 {
			fact, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: self.ID, Kind: models.FactPlain, Text: fmt.Sprintf("Fact number %d about them.", index)})
			if err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			facts = append(facts, fact)
			if index > 0 {
				recent = append(recent, fact.ID)
			}
		}
		if err := tx.TouchAgentFacts(recent, time.Now()); err != nil {
			t.Fatalf("TouchAgentFacts: %s", err)
		}
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: agentId, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})

	asked, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "what time is it", Surface: "cli"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	<-asked.done

	if !strings.Contains(systems[0], fmt.Sprintf("Fact number %d about them.", selfFacts)) {
		t.Fatalf("the self page is not in the prompt: %.3000s", systems[0])
	}
	if len(systems) < 3 {
		t.Fatalf("the turn took %d rounds, wanted three", len(systems))
	}
	for round := 1; round < len(systems); round++ {
		if systems[round] != systems[0] {
			t.Errorf("round %d sent a different system prompt from the first", round)
		}
		if toolLists[round] != toolLists[0] {
			t.Errorf("round %d sent different tools from the first", round)
		}
	}
}

// One turn after another in a conversation sends the same front too, a
// turn from the phone after one from the drawer included: the facts the prompt carries are
// the conversation's for a while, not read afresh with each turn's order of
// use, and where the turn is happening is said after the history.
func TestTheTurnsOfAConversationSendTheSamePrompt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var mutex sync.Mutex
	var systems, lastMessages []string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var body struct {
			Stream   bool              `json:"stream"`
			Messages []json.RawMessage `json:"messages"`
		}
		_ = json.Unmarshal(raw, &body)
		if !body.Stream {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],"usage":{}}`))
			return
		}
		mutex.Lock()
		systems = append(systems, string(body.Messages[0]))
		lastMessages = append(lastMessages, string(body.Messages[len(body.Messages)-1]))
		mutex.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: " + `{"id":"s1","model":"m","choices":[{"delta":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1}}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer provider.Close()

	worker, run := digestSplitWorld(t, database, provider.URL)
	agentId := run.Agent.ID
	var facts []*models.AgentFact
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		self, err := tx.GetAgentNode(agentId, models.PathSelf)
		if err != nil || self == nil {
			t.Fatalf("GetAgentNode: %v, %v", self, err)
		}
		for index := range selfFacts + 1 {
			fact, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: self.ID, Kind: models.FactPlain, Text: fmt.Sprintf("Fact number %d about them.", index)})
			if err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			facts = append(facts, fact)
		}
		// All used but the first, which is left out until it is used.
		recent := []string{}
		for _, fact := range facts[1:] {
			recent = append(recent, fact.ID)
		}
		if err := tx.TouchAgentFacts(recent, time.Now()); err != nil {
			t.Fatalf("TouchAgentFacts: %s", err)
		}
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: agentId, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})

	for turn, surface := range []string{"drawer", "phone"} {
		asked, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "what time is it", Surface: surface})
		if err != nil {
			t.Fatalf("Ask: %s", err)
		}
		<-asked.done
		if turn == 0 {
			// Between the turns, a fact left out is used, which would put
			// it first.
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				if err := tx.TouchAgentFacts([]string{facts[0].ID}, time.Now().Add(time.Hour)); err != nil {
					t.Fatalf("TouchAgentFacts: %s", err)
				}
			})
		}
	}
	if len(systems) != 2 {
		t.Fatalf("%d requests", len(systems))
	}
	if systems[1] != systems[0] {
		t.Errorf("the second turn sent a different system prompt from the first")
	}
	var overlay struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal([]byte(lastMessages[1]), &overlay)
	if !strings.Contains(overlay.Content, "<where>") || !strings.Contains(overlay.Content, "through the phone") {
		t.Errorf("the second turn does not say where it is after the history: %.500s", lastMessages[1])
	}
}
