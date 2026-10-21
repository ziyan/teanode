package tools

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// privateTools are the tools whose answers are the person's alone, so that a
// turn which called one is kept out of memory filing, out of conversation
// titles and summaries, and out of what another run reads back from the
// conversation. Actions is empty when every call of the tool is private, and
// otherwise the actions that are: the passwords tool's list shows names, sites
// and user names, and reading a note or making up a password puts text into
// the conversation, and none of that belongs in memory or in a later run.
var privateTools = []struct {
	Name    string
	Actions []string
}{
	{Name: "health"},
	{Name: "passwords", Actions: []string{"list", "read_note", "generate"}},
}

// IsPrivateToolCall says whether a tool call is one of the private tools, as
// the model calls it (health, or passwords with an action) or by the name of
// one of its actions (health_summary, passwords_read_note).
func IsPrivateToolCall(call models.AgentToolCall) bool {
	for _, private := range privateTools {
		var action string
		switch {
		case call.Name == private.Name:
			action = ActionOf(json.RawMessage(call.Arguments))
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
	if messages[index].Role != string(llm.RoleUser) {
		return false
	}
	for previous := index - 1; previous >= 0; previous-- {
		switch messages[previous].Role {
		case models.AgentMessageNote, string(llm.RoleUser):
			continue
		case string(llm.RoleTool):
			return false
		case string(llm.RoleAssistant):
			return len(messages[previous].ToolCalls) == 0
		}
		return true
	}
	return true
}

// PrivateTurns says, for each message, whether it belongs to a turn in which
// a private tool was called. A turn runs from a message of the person to the
// next one, and one call of such a tool anywhere in it makes every assistant
// and tool message of it private: the text it read, and what the agent said
// about it. The first stretch, before any message of the person, is a turn
// too: a window that starts after a mark may begin in the middle of one.
func PrivateTurns(messages []*models.AgentMessage) []bool {
	isPrivate := make([]bool, len(messages))
	for start := 0; start < len(messages); {
		end := start + 1
		for end < len(messages) && !IsTurnStart(messages, end) {
			end++
		}
		for _, message := range messages[start:end] {
			for _, call := range message.ToolCalls {
				if IsPrivateToolCall(call) {
					for index := start; index < end; index++ {
						isPrivate[index] = true
					}
				}
			}
		}
		start = end
	}
	return isPrivate
}
