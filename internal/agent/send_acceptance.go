package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

var errReplyChanged = errors.New("held reply changed before acceptance")
var errAutoReplyLimit = errors.New("automatic reply limit reached")

// The reply row is the durable send identity. Its Sent state and accepted mail
// commit together, so a repeated job cannot accept a second message after a crash.
func (self *Agent) acceptHeldReply(ctx context.Context, transactions mailbox.TransactionScope, reply *models.AgentReply, originalItemId string, message *mailer.Message, now time.Time) (bool, error) {
	err := transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		// Match human draft takeover's lock order: folder/item before reply.
		draft, err := transaction.LockItem(reply.DraftItemID)
		if err != nil {
			return err
		}
		if draft == nil || !draft.Draft {
			current, err := transaction.GetAgentReply(reply.ID)
			if err != nil {
				return err
			}
			if current != nil && current.Status == models.AgentReplyHeld {
				return &Deferral{Until: time.Now().Add(time.Second), Reason: "the draft changed before sending"}
			}
			return errReplyChanged
		}
		if _, err := transaction.UpdateAgentReply(reply.ID, func(current *models.AgentReply) error {
			if current.Status != models.AgentReplyHeld {
				return errReplyChanged
			}
			if current.DraftItemID != reply.DraftItemID || !current.ModifiedAt.Equal(reply.ModifiedAt) {
				return &Deferral{Until: time.Now().Add(time.Second), Reason: "the held reply changed before sending"}
			}
			current.Status = models.AgentReplySending
			return nil
		}); err != nil {
			return err
		}
		canSend, err := transaction.ClaimAutoReply(reply.MailboxID, now, agentReplyHourlyLimit)
		if err != nil {
			return err
		}
		if !canSend {
			return errAutoReplyLimit
		}
		accepted, err := self.settings.Mailer.AcceptSubmission(ctx, transaction, &mailparse.Envelope{MailboxID: reply.MailboxID}, message)
		if err != nil {
			return err
		}
		if accepted == nil || accepted.ID == "" {
			return fmt.Errorf("reply acceptance returned no stored mail")
		}
		if err := self.discardDraft(ctx, transaction, reply.DraftItemID); err != nil {
			return err
		}
		if _, err := transaction.SetItemFlags([]string{originalItemId}, models.MailboxItemFlags{Answered: new(true)}); err != nil {
			return err
		}
		_, err = transaction.UpdateAgentReply(reply.ID, func(current *models.AgentReply) error {
			current.Status = models.AgentReplySent
			current.DraftItemID = ""
			current.SentMailID = accepted.ID
			current.SentAt = &now
			return nil
		})
		return err
	})
	if errors.Is(err, errReplyChanged) {
		return false, nil
	}
	return err == nil, err
}
