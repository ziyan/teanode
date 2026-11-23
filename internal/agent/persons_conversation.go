package agent

import (
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// personsConversation is where a turn the person should read takes place:
// the conversation named, while it is still one of theirs, or the main one,
// made if there is none yet.
func personsConversation(tx db.Transaction, agentId, conversationId string) (*models.AgentConversation, error) {
	if conversationId != "" {
		found, err := tx.GetAgentConversation(conversationId)
		if err != nil {
			return nil, err
		}
		if found != nil && found.AgentID == agentId &&
			(found.Kind == models.AgentConversationMain || found.Kind == models.AgentConversationNamed || found.IsGoal()) {
			return found, nil
		}
	}
	// Made with the drawer as its surface, which is where the person reads
	// it; left empty, the turn that made it would have named it.
	return tx.MainAgentConversation(agentId, "drawer")
}
