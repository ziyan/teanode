package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
)

// The words a call carries for what it does reach the apps, on the call as
// it runs and on the call as it is kept, and never the tool: a tool that
// reads its arguments strictly would refuse a key it does not know.
func TestACallsIntentionIsShownAndNotHandedToTheTool(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	arguments, _ := json.Marshal(`{"kettle":"blue","intention":"Checking the blue kettle"}`)
	round := fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"kettle_look","arguments":%s}}]},"finish_reason":"tool_calls"}]}`, arguments)
	var handed string
	kettle := &Tool{
		Name: "kettle_look", Family: FamilyGeneral, Core: true, Risk: RiskRead,
		Description: "a tool for a test that looks at a kettle",
		Parameters:  tools.Object(map[string]any{"kettle": map[string]any{"type": "string"}}),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			handed = string(call.Arguments)
			return tools.TextResult("warm"), nil
		},
	}
	worker, run, conversation := batchWorld(t, database, []string{round, saidByModel("It is warm.")}, kettle)

	turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "is the blue kettle warm", Surface: "cli", Short: true})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	events := collectEvents(turn)
	if handed != `{"kettle":"blue"}` {
		t.Fatalf("the tool was handed %s", handed)
	}
	var shown string
	for _, event := range events {
		if event.Kind == EventToolCall {
			shown = event.Intention
		}
	}
	if shown != "Checking the blue kettle" {
		t.Fatalf("the running call says %q", shown)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		messages, err := tx.ListAgentMessages(conversation.ID, nil)
		if err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
		kept := ""
		for _, message := range messages {
			if message.Role == string(llm.RoleAssistant) && len(message.ToolCalls) > 0 {
				kept = message.ToolCalls[0].Intention
			}
		}
		if kept != "Checking the blue kettle" {
			t.Fatalf("the kept call says %q", kept)
		}
	})
}
