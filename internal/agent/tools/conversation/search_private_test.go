package conversation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A run that a mail steered cannot read a generated password out of a private
// turn one character at a time by searching for it: the conversation search
// leaves a private turn out, and still finds what was said in the others. The
// person's own screen (the database call that says so) searches all of it.
func TestSearchDoesNotSayWhatAPrivateTurnHolds(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	run := &fakeRun{database: database}
	const madeUp = "Zq7-invented-gate-pass"
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, _ := tx.CreateUser(&models.User{Username: "carol", Name: "Carol Example"})
		run.agent, _ = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		run.conversation, _ = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		add := func(role, content, name string, calls ...models.AgentToolCall) {
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: run.conversation.ID, Role: role, Content: content, Name: name, ToolCalls: calls}); err != nil {
				t.Fatal(err)
			}
		}
		add("user", "make me a password for the gate", "")
		add("assistant", "", "", models.AgentToolCall{ID: "c2", Name: "passwords_generate", Arguments: `{}`})
		add("tool", `{"generated_password":"`+madeUp+`"}`, "passwords_generate")
		add("assistant", "Here is "+madeUp+".", "")
		add("user", "and the regatta is on the 21st", "")
		add("assistant", "Noted: the regatta is on the 21st.", "")
	})
	// The reading is refused...
	ctx := tools.WithRun(context.Background(), run)
	conversationTool := find(t, "conversation")
	ask := func(query string) bool {
		result, err := conversationTool.Run(ctx, &tools.Call{ID: "c", Arguments: json.RawMessage(fmt.Sprintf(`{"action":"search","query":%q}`, query))})
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Conversations []map[string]any `json:"conversations"`
		}
		_ = json.Unmarshal([]byte(result.Content), &parsed)
		return len(parsed.Conversations) > 0
	}
	// ...but search says yes or no to any substring of what the assistant said.
	alphabet := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-"
	known, calls := "Here is ", 0
	for {
		grew := false
		for _, character := range alphabet {
			calls++
			if ask(known + string(character)) {
				known += string(character)
				grew = true
				break
			}
		}
		if !grew {
			break
		}
	}
	t.Logf("recovered %q in %d search calls", strings.TrimPrefix(known, "Here is "), calls)
	if strings.Contains(known, madeUp) {
		t.Errorf("search recovered the generated password")
	}
	if ask("Here is") || ask("gate") {
		t.Errorf("search found words of a private turn")
	}
	if !ask("regatta") {
		t.Errorf("search no longer finds what was said outside the private turn")
	}
	// The person's own screen is not held to this.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := tx.SearchAgentConversations(run.agent.ID, "Here is", 10, true)
		if err != nil || len(found) != 1 {
			t.Errorf("the person's own search of a private turn: %v %v", found, err)
		}
		if found, err = tx.SearchAgentConversations(run.agent.ID, "Here is", 10, false); err != nil || len(found) != 0 {
			t.Errorf("a search that is not the person's own found a private turn: %v %v", found, err)
		}
	})
}
