package agent

import (
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// replyQuoteCharacters is the most of a message replied to that a turn
// keeps: enough to say which part of a long answer is meant, and kept on
// every later turn the history carries, so not the whole of one.
const replyQuoteCharacters = 1000

// isReplyReference says the reference is a message of the conversation
// the person is replying to, rather than a thread, a page or a finance
// transaction.
func isReplyReference(reference models.AgentReference) bool {
	return reference.AgentMessageID != "" || reference.QuotedText != ""
}

// resolveReply is a message replied to as it is kept and told. One named
// by its id is read from the conversation, and its role and words are
// taken from the stored message whatever the dashboard sent; one that is
// not in the conversation is dropped. One without an id, a line the
// dashboard had not been given an id for yet, keeps the words the person
// saw, bounded.
func resolveReply(tx db.Transaction, conversationId string, reference models.AgentReference) (*models.AgentReference, error) {
	reply := models.AgentReference{AgentMessageID: strings.TrimSpace(reference.AgentMessageID)}
	if reply.AgentMessageID != "" {
		message, err := tx.GetAgentConversationMessage(conversationId, reply.AgentMessageID)
		if err != nil {
			return nil, err
		}
		if message == nil || (message.Role != string(llm.RoleUser) && message.Role != string(llm.RoleAssistant)) {
			log.Warningf("a turn in conversation %s replied to message %q, which is not a message of it; left out", conversationId, reply.AgentMessageID)
			return nil, nil
		}
		reply.QuotedRole = message.Role
		reply.QuotedText = message.Content
		if message.Role == string(llm.RoleAssistant) {
			reply.QuotedText = models.StripSuggestedReplies(reply.QuotedText)
		}
	} else {
		reply.QuotedRole = string(llm.RoleAssistant)
		if reference.QuotedRole == string(llm.RoleUser) {
			reply.QuotedRole = string(llm.RoleUser)
		}
		reply.QuotedText = reference.QuotedText
	}
	reply.QuotedText = cutMarked(strings.TrimSpace(reply.QuotedText), replyQuoteCharacters)
	if reply.QuotedText == "" {
		return nil, nil
	}
	return &reply, nil
}
