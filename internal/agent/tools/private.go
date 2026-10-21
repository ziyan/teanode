package tools

import (
	"github.com/ziyan/teanode/internal/agent/privateturn"
	"github.com/ziyan/teanode/internal/models"
)

// HiddenPrivateWords stands for what the agent or one of its tools said in a
// private turn, wherever a run is shown the conversation without it.
const HiddenPrivateWords = "[hidden: from a turn that used Health or Passwords]"

// The private tools are the tools whose answers are the person's alone, so
// that a turn which called one is kept out of memory filing, out of
// conversation titles and summaries, out of a search of the conversation and
// out of what another run reads back from the conversation. The list and the
// rules live in internal/agent/privateturn, which the database search shares.

// IsPrivateToolCall says whether a tool call is one of the private tools, as
// the model calls it (health, or passwords with an action) or by the name of
// one of its actions (health_summary, passwords_read_note).
func IsPrivateToolCall(call models.AgentToolCall) bool {
	return privateturn.IsToolCall(call)
}

// IsTurnStart says whether the message at index begins a turn: it is a
// message of the person, and not one they steered into the running turn.
func IsTurnStart(messages []*models.AgentMessage, index int) bool {
	return privateturn.IsTurnStart(messages, index)
}

// PrivateTurns says, for each message, whether it belongs to a turn in which
// a private tool was called. A turn runs from a message of the person to the
// next one, and one call of such a tool anywhere in it makes every assistant
// and tool message of it private: the text it read, and what the agent said
// about it. The first stretch, before any message of the person, is a turn
// too: a window that starts after a mark may begin in the middle of one.
func PrivateTurns(messages []*models.AgentMessage) []bool {
	return privateturn.Turns(messages)
}
