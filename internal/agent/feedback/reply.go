// Package feedback records corrections for subsequent agent work.
package feedback

import (
	"fmt"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// RecordReplyDeclined records that a reply the agent wrote did not go.
func RecordReplyDeclined(tx db.Transaction, reply *models.AgentReply, how string) error {
	if reply == nil || reply.AgentID == "" {
		return nil
	}
	said := fmt.Sprintf("The reply to %s about %q was %s. It began: %q", reply.To, reply.Subject, how, tools.FirstWords(reply.Text, 20))
	_, err := tx.CreateAgentFeedback(&models.AgentFeedback{AgentID: reply.AgentID, MailboxID: reply.MailboxID, Kind: models.FeedbackReplyDeclined, MailID: reply.MailID, Said: said})
	return err
}
