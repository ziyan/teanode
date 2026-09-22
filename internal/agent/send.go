package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// runSend is the handler for a send job; its subject is the held reply. It
// climbs the ladder again — the person may have answered meanwhile, or
// taken the draft over, or switched answering off — and only then sends.
// agentReplyHourlyLimit is the most answers one mailbox sends by itself in an
// hour, counted with the out-of-office replies because they are the same
// thing to whoever receives them: mail this server sent without a person.
// The same fifty the away reply has always had.
const agentReplyHourlyLimit = 50

func (self *Agent) runSend(ctx context.Context, run *Run) error {
	now := run.Now
	if now.IsZero() {
		now = time.Now()
	}
	var reply *models.AgentReply
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		reply, err = tx.GetAgentReply(run.Job.SubjectID)
		return err
	}); err != nil {
		return err
	}
	if reply == nil || (reply.Status != models.AgentReplyHeld && reply.Status != models.AgentReplySending) {
		return nil // cancelled, or already sent
	}
	if reply.SendAfter != nil && now.Before(*reply.SendAfter) {
		return &Deferral{Until: *reply.SendAfter, Reason: "the hold has not ended"}
	}
	mailbox := run.Mailbox
	settle := func(status models.AgentReplyStatus, reason string) error {
		settleContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		err := run.Database().TransactionContext(settleContext, func(tx db.Transaction) error {
			if _, err := tx.LockItem(reply.DraftItemID); err != nil {
				return err
			}
			if _, err := tx.UpdateAgentReply(reply.ID, func(current *models.AgentReply) error {
				if current.Status != reply.Status {
					return errReplyChanged
				}
				if current.DraftItemID != reply.DraftItemID || !current.ModifiedAt.Equal(reply.ModifiedAt) {
					if current.Status == models.AgentReplyHeld {
						return &Deferral{Until: time.Now().Add(time.Second), Reason: "the held reply changed before settlement"}
					}
					return errReplyChanged
				}
				current.Status = status
				current.Reason = reason
				current.DraftItemID = ""
				return nil
			}); err != nil {
				return err
			}
			return self.discardDraft(settleContext, tx, reply.DraftItemID)
		})
		if errors.Is(err, errReplyChanged) {
			return nil
		}
		return err
	}

	if reply.Status == models.AgentReplySending {
		// The mailer had it and the record of the send never happened —
		// the process stopped between the two. Sending again could send
		// twice, so it is left, and the person is told to look in Sent.
		return settle(models.AgentReplyFailed, "the send was interrupted before it was recorded; look in Sent before answering by hand")
	}
	source := run.Source
	if mailbox == nil || source == nil || !source.Granted || source.AutoReply == nil || !source.AutoReply.Enabled || !FeatureAllowed(run.Configuration(), "autoReply") {
		return settle(models.AgentReplyCancelled, "answering was switched off before the hold ended")
	}
	if self.settings.Mailer == nil {
		return fmt.Errorf("no mailer")
	}
	policy := source.AutoReply

	var original *models.Mail
	var originalItem *models.MailboxItem
	refused := ""
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		draft, err := tx.GetItem(reply.DraftItemID)
		if err != nil {
			return err
		}
		if draft == nil || !draft.Draft {
			refused = "the draft was changed or removed by hand"
			return nil
		}
		found, err := tx.GetMails([]string{reply.MailID}, nil)
		if err != nil {
			return err
		}
		if len(found) == 0 || found[0] == nil {
			refused = "the message is gone"
			return nil
		}
		original = found[0]
		if err := LoadHeaders(ctx, run.Storage(), original); err != nil {
			return err
		}
		if originalItem, err = inboxItemOf(tx, mailbox, original); err != nil {
			return err
		}
		if originalItem == nil {
			refused = "the message is no longer in the Inbox"
			return nil
		}
		insights, err := tx.GetMailInsights(mailbox.ID, []string{original.ID})
		if err != nil {
			return err
		}
		// The ladder reads the quiet period; the sender is marked replied to
		// only once the reply has gone, so that a send that fails and is
		// retried is not refused by its own first try. The job is claimed by
		// one instance, so no two instances send the same reply.
		refused, err = self.replyRefusal(tx, run, policy, original, originalItem, reply.From, insights[original.ID], now, true)
		return err
	}); err != nil {
		return err
	}
	if refused != "" {
		if original == nil {
			return settle(models.AgentReplyCancelled, refused)
		}
		return settle(models.AgentReplyRefused, refused)
	}

	// Sent as the person, marked as the agent's doing in the audit trail,
	// and marked automatic so that no responder answers it.
	acting := db.ContextWithAuditPrincipal(ctx, db.AuditPrincipal{ActorKind: models.AuditActorAgent, UserID: run.Owner.ID})
	message := &mailer.Message{
		From:     reply.From,
		FromName: mailbox.Name,
		To:       []string{reply.To},
		Subject:  reply.Subject,
		Text:     reply.Text,
		Headers: append(threadingHeaders(original),
			mailparse.UnsplitHeader("Auto-Submitted", "auto-replied"),
			mailparse.UnsplitHeader("X-Auto-Response-Suppress", "All"),
		),
	}
	wasAccepted, err := self.acceptHeldReply(acting, run.Database(), reply, originalItem.ID, message, now)
	if errors.Is(err, errAutoReplyLimit) {
		return settle(models.AgentReplyRefused, "the mailbox has reached its automatic reply limit for this hour")
	}
	if err != nil {
		var deferral *Deferral
		if errors.As(err, &deferral) {
			return err
		}
		if !errors.Is(err, context.Canceled) && run.Job.FailureCount+1 > len(retryLadder) {
			if settleErr := settle(models.AgentReplyFailed, "could not be sent: "+err.Error()); settleErr != nil {
				log.Warningf("cannot record the failed reply %q: %s", reply.ID, settleErr)
			}
		}
		return fmt.Errorf("sending the reply: %w", err)
	}
	if wasAccepted {
		log.Noticef("the agent of mailbox %q answered %q from %q", mailbox.ID, strings.TrimSpace(reply.To), reply.From)
	}
	return nil
}
