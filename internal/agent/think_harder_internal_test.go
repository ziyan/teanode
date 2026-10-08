package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A spoken turn begins with the model for calls, which may hand the turn
// to the larger model: its first words are spoken, and the rounds after
// the hand-off are the larger model's. A typed turn is never offered the
// hand-off.
func TestASpokenTurnHandsAHardQuestionToTheLargerModel(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var mutex sync.Mutex
	var asked []string
	var toolNames [][]string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
			Tools  []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(raw, &body)
		if !body.Stream {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],"usage":{}}`))
			return
		}
		names := []string{}
		for _, tool := range body.Tools {
			names = append(names, tool.Function.Name)
		}
		mutex.Lock()
		asked = append(asked, body.Model)
		toolNames = append(toolNames, names)
		mutex.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		if body.Model == "quick" {
			_, _ = writer.Write([]byte("data: " + `{"id":"s1","model":"quick","choices":[{"delta":{"content":"Let me think about that properly.","tool_calls":[{"index":0,"id":"call_1","function":{"name":"think_harder","arguments":"{\"reason\":\"a plan with several steps\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = writer.Write([]byte("data: " + `{"id":"s2","model":"thinker","choices":[{"delta":{"content":"Here is the plan."},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer provider.Close()

	worker, run := digestSplitWorldWith(t, database, provider.URL, func(configuration *config.Configuration) {
		configuration.Agent.Voice = config.AgentVoice{Enabled: true, AskModel: "p:quick"}
	})
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})

	for _, surface := range []string{"voice", "drawer"} {
		turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "plan my week", Surface: surface})
		if err != nil {
			t.Fatalf("Ask: %s", err)
		}
		<-turn.done
	}
	if strings.Join(asked, ",") != "quick,thinker,thinker" {
		t.Fatalf("asked %v", asked)
	}
	offered := func(names []string) bool {
		for _, name := range names {
			if name == thinkHarderToolName {
				return true
			}
		}
		return false
	}
	if !offered(toolNames[0]) {
		t.Errorf("the model for calls was not offered the hand-off: %v", toolNames[0])
	}
	if offered(toolNames[1]) {
		t.Errorf("the larger model was offered the hand-off")
	}
	if offered(toolNames[2]) {
		t.Errorf("a typed turn was offered the hand-off")
	}
}
