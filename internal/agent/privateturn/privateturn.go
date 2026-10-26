// Package privateturn says which turns of a conversation are private: the
// ones in which a private tool was called. It knows nothing of tools or of
// the database, so that both the tool kit and the database search can use it.
package privateturn

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// The roles of a stored message, as models.AgentMessage.Role says them.
const (
	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"
)

// SubagentToolName is the tool that hands work to a run of its own, and
// SubagentHasPrivateCallKey is the key of its answer that says the run called
// a private tool. The subagent's answer comes back to its parent as a tool
// result, so a turn whose subagent made such a call is private too.
const (
	SubagentToolName          = "subagent"
	SubagentHasPrivateCallKey = "has_private_call"
)

// privateTools are the tools whose answers are the person's alone, so that a
// turn which called one is kept out of memory filing, out of conversation
// titles and summaries, and out of what another run reads back from the
// conversation. Actions is empty when every call of the tool is private, and
// otherwise the actions that are: the passwords tool's list shows names, sites
// and user names, and reading a note or making up a password puts text into
// the conversation, and none of that belongs in memory or in a later run. The
// journal's readings hold the person's own words about their days and, under
// their switch, their Health; the journal is already where those days are
// kept, so the turns that read it are not filed again.
var privateTools = []struct {
	Name    string
	Actions []string
}{
	{Name: "health"},
	{Name: "passwords", Actions: []string{"list", "read_note", "generate"}},
	{Name: "journal", Actions: []string{"day", "period", "days", "search"}},
}

// IsToolCall says whether a tool call is one of the private tools, as
// the model calls it (health, or passwords with an action) or by the name of
// one of its actions (health_summary, passwords_read_note).
func IsToolCall(call models.AgentToolCall) bool {
	for _, private := range privateTools {
		var action string
		switch {
		case call.Name == private.Name:
			action = actionOf(call.Arguments)
		case strings.HasPrefix(call.Name, private.Name+"_"):
			action = strings.TrimPrefix(call.Name, private.Name+"_")
		default:
			continue
		}
		if len(private.Actions) == 0 || slices.Contains(private.Actions, action) {
			return true
		}
	}
	return false
}

// IsTurnStart says whether the message at index begins a turn: it is a
// message of the person, and not one they steered into the running turn.
// A steered message comes after the result of a tool call, or after an
// assistant message that made one (notes between are not looked at), or
// after another message of the person's that was itself steered in, since
// a person may steer twice before the turn answers.
func IsTurnStart(messages []*models.AgentMessage, index int) bool {
	if messages[index].Role != roleUser {
		return false
	}
	for previous := index - 1; previous >= 0; previous-- {
		switch messages[previous].Role {
		case models.AgentMessageNote, roleUser:
			continue
		case roleTool:
			return false
		case roleAssistant:
			return len(messages[previous].ToolCalls) == 0
		}
		return true
	}
	return true
}

// Turns says, for each message, whether it belongs to a turn in which
// a private tool was called. A turn runs from a message of the person to the
// next one, and one call of such a tool anywhere in it makes every assistant
// and tool message of it private: the text it read, and what the agent said
// about it. The first stretch, before any message of the person, is a turn
// too: a window that starts after a mark may begin in the middle of one.
func Turns(messages []*models.AgentMessage) []bool {
	isPrivate := make([]bool, len(messages))
	for start := 0; start < len(messages); {
		end := start + 1
		for end < len(messages) && !IsTurnStart(messages, end) {
			end++
		}
		for _, message := range messages[start:end] {
			isTurnPrivate := isSubagentAnswerPrivate(message)
			for _, call := range message.ToolCalls {
				if IsToolCall(call) {
					isTurnPrivate = true
				}
			}
			if isTurnPrivate {
				for index := start; index < end; index++ {
					isPrivate[index] = true
				}
			}
		}
		start = end
	}
	return isPrivate
}

// isSubagentAnswerPrivate says whether a message is the answer of the subagent
// tool for a run that called a private tool: the answer holds whatever that
// call showed, and reaches the parent turn as an ordinary tool result.
func isSubagentAnswerPrivate(message *models.AgentMessage) bool {
	if message.Role != roleTool || message.Name != SubagentToolName {
		return false
	}
	var answer map[string]json.RawMessage
	if json.Unmarshal([]byte(message.Content), &answer) != nil {
		return false
	}
	var hasPrivateCall bool
	return json.Unmarshal(answer[SubagentHasPrivateCallKey], &hasPrivateCall) == nil && hasPrivateCall
}

// actionOf is the action a call's arguments name, in lower case, or empty.
func actionOf(arguments string) string {
	var call struct {
		Action string `json:"action"`
	}
	if arguments == "" || json.Unmarshal([]byte(arguments), &call) != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(call.Action))
}

// ToolNames is the names of the private tools, for a search that needs to
// tell a conversation that may hold a private turn from one that cannot.
func ToolNames() []string {
	names := make([]string, 0, len(privateTools))
	for _, private := range privateTools {
		names = append(names, private.Name)
	}
	return names
}
