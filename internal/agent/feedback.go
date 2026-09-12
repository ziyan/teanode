package agent

import (
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Corrections are recorded from what the person does, never asked for: a
// message the agent sorted one way and the person filed another, a reply
// the agent wrote and the person would not let go. Each becomes a line the
// next run of that kind is shown as an example, for a while.

// RecordFiled records that the person filed a sorted message somewhere the
// agent's sorting did not put it.
func RecordFiled(tx db.Transaction, agentId, mailboxId string, mail *models.Mail, insight *models.MailInsight, folderName string) error {
	if insight == nil || mail == nil || agentId == "" {
		return nil
	}
	from := strings.TrimSpace(mail.From)
	if mail.FromName != "" {
		from = mail.FromName + " <" + from + ">"
	}
	said := fmt.Sprintf("A message from %s with the subject %q was sorted as %s, %s priority; the person filed it in %q.", from, mail.Subject, insight.Category, insight.Priority, folderName)
	_, err := tx.CreateAgentFeedback(&models.AgentFeedback{AgentID: agentId, MailboxID: mailboxId, Kind: models.FeedbackFiled, MailID: mail.ID, Said: said})
	return err
}

// RecordSorted records that the person sorted a message themselves,
// differently from the agent: the category, the priority, whether it
// needs a reply.
func RecordSorted(tx db.Transaction, agentId, mailboxId string, mail *models.Mail, before, after *models.MailInsight) error {
	if mail == nil || agentId == "" || after == nil {
		return nil
	}
	from := strings.TrimSpace(mail.From)
	if mail.FromName != "" {
		from = mail.FromName + " <" + from + ">"
	}
	reply := "no reply needed"
	if after.NeedsReply {
		reply = "needs a reply"
	}
	said := fmt.Sprintf("A message from %s with the subject %q", from, mail.Subject)
	if before != nil {
		said += fmt.Sprintf(" was sorted as %s, %s priority;", before.Category, before.Priority)
	} else {
		said += " was not sorted;"
	}
	said += fmt.Sprintf(" the person sorted it as %s, %s priority, %s.", after.Category, after.Priority, reply)
	_, err := tx.CreateAgentFeedback(&models.AgentFeedback{AgentID: agentId, MailboxID: mailboxId, Kind: models.FeedbackSorted, MailID: mail.ID, Said: said})
	return err
}

// RecordReplyDeclined records that a reply the agent wrote did not go.
func RecordReplyDeclined(tx db.Transaction, reply *models.AgentReply, how string) error {
	if reply == nil || reply.AgentID == "" {
		return nil
	}
	said := fmt.Sprintf("The reply to %s about %q was %s. It began: %q", reply.To, reply.Subject, how, tools.FirstWords(reply.Text, 20))
	_, err := tx.CreateAgentFeedback(&models.AgentFeedback{AgentID: reply.AgentID, MailboxID: reply.MailboxID, Kind: models.FeedbackReplyDeclined, MailID: reply.MailID, Said: said})
	return err
}
