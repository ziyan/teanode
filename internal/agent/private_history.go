package agent

import (
	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A private turn is one in which a private tool was called (see
// tools.PrivateTurns): the person's Health, or a list, a note or a made-up
// password from Passwords. What the agent said and its tools answered in it
// belongs to the turn that made it. A run the person did not start, and every
// summary written for the conversation, is given the marker instead.

// privateMessages says, for each message of a history, whether it belongs to
// a private turn. It reads the history the way tools.PrivateTurns reads the
// stored messages, so it also covers the messages of the turn being run, which
// are not stored with a source yet. A message loaded from a private turn
// (llm.ChatMessage.IsFromPrivateTurn) stays private even when the call that
// made the turn private was compacted away or is not in this history.
func privateMessages(history []llm.ChatMessage) []bool {
	stored := make([]*models.AgentMessage, 0, len(history))
	for _, message := range history {
		found := &models.AgentMessage{Role: string(message.Role)}
		for _, call := range message.ToolCalls {
			found.ToolCalls = append(found.ToolCalls, models.AgentToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		stored = append(stored, found)
	}
	isPrivate := tools.PrivateTurns(stored)
	for index, message := range history {
		if message.IsFromPrivateTurn {
			isPrivate[index] = true
		}
	}
	return isPrivate
}

// withoutPrivateHistory is the history with what the agent said and its tools
// answered in a private turn replaced by tools.HiddenPrivateWords, one marker
// for each unbroken stretch. What the person wrote stays. isPrivate is
// privateMessages of the whole history, or of a longer history this one is the
// start of, so that a turn cut in two is judged by all of it. The marker keeps
// the stored message id of the last message it stands for, so that a
// compaction through it still says where the transcript resumes.
func withoutPrivateHistory(history []llm.ChatMessage, isPrivate []bool) []llm.ChatMessage {
	kept := make([]llm.ChatMessage, 0, len(history))
	for index, message := range history {
		if index >= len(isPrivate) || !isPrivate[index] || message.Role == llm.RoleUser {
			kept = append(kept, message)
			continue
		}
		if last := len(kept) - 1; last >= 0 && kept[last].Content == tools.HiddenPrivateWords && kept[last].Role == llm.RoleAssistant && len(kept[last].ToolCalls) == 0 {
			if message.SourceID != "" {
				kept[last].SourceID = message.SourceID
			}
			continue
		}
		kept = append(kept, llm.ChatMessage{Role: llm.RoleAssistant, Content: tools.HiddenPrivateWords, SourceID: message.SourceID})
	}
	return kept
}

// historyForRun is the history as this run may read it. A run the person
// started reads all of it, so they can go on talking about the note they just
// had read. Any other run (a chat app, a mail, a goal, a schedule the agent
// wrote, a command left running, a program speaking MCP, a card the person
// answered after a turn of those) is given the private turns as the marker.
func (self *AskRun) historyForRun(history []llm.ChatMessage) []llm.ChatMessage {
	if self.IsStartedByPerson() {
		return history
	}
	return withoutPrivateHistory(history, privateMessages(history))
}
