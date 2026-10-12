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
// the hand-off are the larger model's. A typed turn is not offered the
// hand-off while no quick model is configured.
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

// A turn a person typed begins on the quick model, which hands a hard
// question to the larger one with think_harder; a turn that is not the
// person's own message, one whose person chose a model, one that is to dig
// and a spoken turn do not begin on the quick model.
func TestATypedTurnStartsOnTheQuickModelAndHandsAHardQuestionToTheLargerOne(t *testing.T) {
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
		if body.Model == "quick" || body.Model == "spoken" {
			_, _ = writer.Write([]byte("data: " + `{"id":"s1","model":"` + body.Model + `","choices":[{"delta":{"content":"Let me think about that properly.","tool_calls":[{"index":0,"id":"call_1","function":{"name":"think_harder","arguments":"{\"reason\":\"a plan with several steps\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = writer.Write([]byte("data: " + `{"id":"s2","model":"` + body.Model + `","choices":[{"delta":{"content":"Here is the plan."},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer provider.Close()

	worker, run := digestSplitWorldWith(t, database, provider.URL, func(configuration *config.Configuration) {
		configuration.Agent.Models.Quick = "p:quick"
		configuration.Agent.Models.Choices = []string{"p:chosen"}
		configuration.Agent.Voice = config.AgentVoice{Enabled: true, AskModel: "p:spoken"}
	})
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	choosing := *run.Agent
	choosing.AskModel = "p:chosen"

	offered := func(names []string) bool {
		for _, name := range names {
			if name == thinkHarderToolName {
				return true
			}
		}
		return false
	}
	cases := []struct {
		name           string
		settings       AskSettings
		wantModels     string
		isHandoffGiven bool
	}{
		{"a typed drawer turn", AskSettings{Surface: "drawer"}, "quick,thinker", true},
		{"a typed turn from the app", AskSettings{Surface: "ios"}, "quick,thinker", true},
		{"a spoken turn", AskSettings{Surface: "voice"}, "spoken,thinker", true},
		{"a command line turn", AskSettings{Surface: "cli"}, "thinker", false},
		{"a turn whose person chose a model", AskSettings{Surface: "drawer", Agent: &choosing}, "chosen", false},
		{"a turn that digs", AskSettings{Surface: "drawer", Effort: "high", Research: true}, "thinker", false},
		{"a turn the agent started for itself", AskSettings{Surface: "drawer", Message: models.GoalCheckInMarker + " check in"}, "thinker", false},
	}
	for _, testCase := range cases {
		mutex.Lock()
		asked, toolNames = nil, nil
		mutex.Unlock()
		settings := testCase.settings
		settings.Agent, settings.Owner = firstAgent(settings.Agent, run.Agent), run.Owner
		settings.Operations, settings.Conversation = &digestSplitOperations{}, conversation
		if settings.Message == "" {
			settings.Message = "plan my week"
		}
		turn, err := worker.Ask(&settings)
		if err != nil {
			t.Fatalf("%s: Ask: %s", testCase.name, err)
		}
		<-turn.done
		mutex.Lock()
		if strings.Join(asked, ",") != testCase.wantModels {
			t.Errorf("%s: asked %v, want %s", testCase.name, asked, testCase.wantModels)
		}
		if len(toolNames) > 0 && offered(toolNames[0]) != testCase.isHandoffGiven {
			t.Errorf("%s: the first round's offer of think_harder is %v, want %v", testCase.name, offered(toolNames[0]), testCase.isHandoffGiven)
		}
		if len(toolNames) > 1 && offered(toolNames[1]) {
			t.Errorf("%s: the larger model was offered the hand-off", testCase.name)
		}
		mutex.Unlock()
	}
}

func firstAgent(chosen, fallback *models.Agent) *models.Agent {
	if chosen != nil {
		return chosen
	}
	return fallback
}

// startingModel decides, from the settings alone, which turns begin on a
// small model.
func TestStartingModel(t *testing.T) {
	configuration := &config.Configuration{}
	configuration.Agent.Models.Default = "p:large"
	configuration.Agent.Models.Quick = " p:small "
	configuration.Agent.Voice.AskModel = "p:spoken"
	agent := &models.Agent{}
	chosen := &models.Agent{AskModel: "p:chosen"}
	cases := []struct {
		name     string
		settings AskSettings
		want     string
	}{
		{"drawer", AskSettings{Surface: "drawer", Agent: agent, Message: "hi"}, "p:small"},
		{"extension", AskSettings{Surface: "extension", Agent: agent, Message: "hi"}, "p:small"},
		{"page", AskSettings{Surface: "page", Agent: agent, Message: "hi"}, "p:small"},
		{"phone", AskSettings{Surface: "phone", Agent: agent, Message: "hi"}, "p:small"},
		{"ios", AskSettings{Surface: "ios", Agent: agent, Message: "hi"}, "p:small"},
		{"telegram", AskSettings{Surface: "telegram", Agent: agent, Message: "hi"}, "p:small"},
		{"discord", AskSettings{Surface: "discord", Agent: agent, Message: "hi"}, "p:small"},
		{"voice", AskSettings{Surface: "voice", Agent: agent, Message: "hi"}, "p:spoken"},
		{"a call in the app", AskSettings{Surface: "ios_voice", Agent: agent, Message: "hi"}, "p:spoken"},
		{"cli", AskSettings{Surface: "cli", Agent: agent, Message: "hi"}, ""},
		{"api", AskSettings{Surface: "api", Agent: agent, Message: "hi"}, ""},
		{"mail", AskSettings{Surface: "mail", Agent: agent, Message: "hi"}, ""},
		{"schedule", AskSettings{Surface: "schedule", Agent: agent, Headless: true, Message: "hi"}, ""},
		{"a headless drawer turn", AskSettings{Surface: "drawer", Agent: agent, Headless: true, Message: "hi"}, ""},
		{"a subagent", AskSettings{Surface: "drawer", Agent: agent, subagentDepth: 1, Message: "hi"}, ""},
		{"a named work", AskSettings{Surface: "drawer", Agent: agent, Work: config.AgentWorkScan, Message: "hi"}, ""},
		{"a named model", AskSettings{Surface: "drawer", Agent: agent, Model: "p:other", Message: "hi"}, ""},
		{"the person's own model", AskSettings{Surface: "drawer", Agent: chosen, Message: "hi"}, ""},
		{"high effort", AskSettings{Surface: "drawer", Agent: agent, Effort: "high", Message: "hi"}, ""},
		{"research", AskSettings{Surface: "drawer", Agent: agent, Research: true, Message: "hi"}, ""},
		{"a goal check-in", AskSettings{Surface: "drawer", Agent: agent, Message: models.GoalCheckInMarker + " now"}, ""},
		{"an answered card", AskSettings{Surface: "drawer", Agent: agent, Message: AnsweringMarker + " yes"}, ""},
		{"past the quick model", AskSettings{Surface: "drawer", Agent: agent, Message: "hi", isPastQuickModel: true}, ""},
	}
	for _, testCase := range cases {
		if got := startingModel(configuration, &testCase.settings); got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
	configuration.Agent.Models.Ask = "p:small"
	if got := startingModel(configuration, &AskSettings{Surface: "drawer", Agent: agent, Message: "hi"}); got != "" {
		t.Errorf("the quick model is the ask model already: %q", got)
	}
	configuration.Agent.Models.Quick = ""
	if got := startingModel(configuration, &AskSettings{Surface: "drawer", Agent: agent, Message: "hi"}); got != "" {
		t.Errorf("no quick model: %q", got)
	}
}
