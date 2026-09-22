package apigraph

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// What a program did over MCP, filed where the person reads what their agent
// did.
//
// A tool called over MCP runs outside any conversation, so before this it
// left nothing the person could see: a program could run commands on their
// computer, read their mail and send some, and the dashboard showed none of
// it. Each call is now a run of its own in the activity list, named for the
// program, holding what was asked and what came back. A question put through
// teanode_ask is filed too: the turn itself happens in the person's main
// conversation, where it always has, and the run beside it is what says a
// program asked it.
//
// A run rather than a table of its own because the activity list already
// lists runs, filters them by kind, searches their titles and opens them to
// read; these are one more kind. Runs are not read by the jobs that describe
// conversations or file what they taught, so nothing here is turned into
// memory.

// mcpRunKind is what these runs are filed under, and what the activity list
// filters them by.
const mcpRunKind = "mcp"

// How much of a call is kept. A file read can be megabytes, and the activity
// list is for seeing what happened, not for keeping a second copy of it.
const (
	mcpRecordedCharacters = 16000
	mcpTitleCharacters    = 200
	mcpTitleArguments     = 80
)

// record files one call. A failure to file it is logged and goes no further:
// the program asked for a tool, not for a record, and it gets its answer
// either way.
func (self *mcpCatalog) record(name string, arguments json.RawMessage, answer string, failure error) {
	title, messages := mcpTranscript(self.caller.name, name, arguments, answer, failure)
	err := self.graph.database.Transaction(func(tx db.Transaction) error {
		run, err := tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: self.person.ID,
			Kind:    models.AgentConversationRun,
			Title:   title,
			JobKind: mcpRunKind,
			Surface: mcpSurface,
			LastAt:  time.Now(),
		})
		if err != nil {
			return err
		}
		for _, message := range messages {
			message.ConversationID = run.ID
			if _, err := tx.AppendAgentMessage(message); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Warningf("could not file a call to %s over MCP by %q: %s", name, self.caller.name, err)
	}
}

// mcpTranscript is a call as a title and the messages a run is read as.
func mcpTranscript(caller, name string, arguments json.RawMessage, answer string, failure error) (string, []*models.AgentMessage) {
	outcome := answer
	if failure != nil {
		outcome = "It failed: " + failure.Error()
	}
	outcome = cutForRecord(outcome)

	if name == mcpAskName {
		var asked struct {
			Question string `json:"question"`
		}
		_ = json.Unmarshal(arguments, &asked)
		question := strings.TrimSpace(asked.Question)
		said := fmt.Sprintf("%s asked, over MCP:\n\n%s", caller, question)
		title := fmt.Sprintf("%s asked: %s", caller, question)
		if question == "" {
			said = fmt.Sprintf("%s, over MCP, went on waiting for the answer to its last question.", caller)
			title = fmt.Sprintf("%s waited for an answer", caller)
		}
		return titleOf(title, failure), []*models.AgentMessage{
			{Role: "user", Content: cutForRecord(said)},
			{Role: "assistant", Content: outcome},
		}
	}

	compact := compactArguments(arguments)
	return titleOf(fmt.Sprintf("%s: %s %s", caller, name, cutRunes(compact, mcpTitleArguments)), failure),
		[]*models.AgentMessage{
			{Role: "user", Content: fmt.Sprintf("%s called %s over MCP.", caller, name)},
			{Role: "assistant", ToolCalls: []models.AgentToolCall{{ID: "mcp", Name: name, Arguments: cutForRecord(compact)}}},
			{Role: "tool", ToolCallID: "mcp", Name: name, Content: outcome},
		}
}

// titleOf is a title the column will hold, saying so when the call failed.
func titleOf(title string, failure error) string {
	title = strings.Join(strings.Fields(title), " ")
	if failure != nil {
		title += " (failed)"
	}
	return cutRunes(title, mcpTitleCharacters)
}

// compactArguments is the arguments on one line, as the program sent them.
func compactArguments(arguments json.RawMessage) string {
	var decoded any
	if len(arguments) == 0 || json.Unmarshal(arguments, &decoded) != nil {
		return strings.TrimSpace(string(arguments))
	}
	compact, err := json.Marshal(decoded)
	if err != nil {
		return strings.TrimSpace(string(arguments))
	}
	return string(compact)
}

// cutForRecord keeps a record to a size worth keeping, saying where it was cut.
func cutForRecord(text string) string {
	runes := []rune(text)
	if len(runes) <= mcpRecordedCharacters {
		return text
	}
	return string(runes[:mcpRecordedCharacters]) +
		fmt.Sprintf("\n\n[%d more characters not kept here]", len(runes)-mcpRecordedCharacters)
}

// cutRunes is at most count characters of text, never cutting through one.
func cutRunes(text string, count int) string {
	runes := []rune(text)
	if len(runes) <= count {
		return text
	}
	return string(runes[:count-1]) + "…"
}
