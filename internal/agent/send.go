package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/aggregate"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// runSend is the handler for a send job; its subject is the held reply. It
// climbs the ladder again — the person may have answered meanwhile, or
// taken the draft over, or switched answering off — and only then sends.
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
	if reply == nil || reply.Status != models.AgentReplyHeld {
		return nil // cancelled, or already sent
	}
	if reply.SendAfter != nil && now.Before(*reply.SendAfter) {
		return &Deferral{Until: *reply.SendAfter, Reason: "the hold has not ended"}
	}
	mailbox := run.Mailbox
	settle := func(status models.AgentReplyStatus, reason string) error {
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			if err := self.discardDraft(ctx, tx, reply.DraftItemID); err != nil {
				return err
			}
			_, err := tx.UpdateAgentReply(reply.ID, func(reply *models.AgentReply) error {
				reply.Status = status
				reply.Reason = reason
				reply.DraftItemID = ""
				return nil
			})
			return err
		})
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
		if draft == nil {
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
	envelope := &mailparse.Envelope{MailboxID: mailbox.ID}
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
	if err := self.settings.Mailer.Send(acting, envelope, message); err != nil {
		if run.Job.Attempts+1 >= len(retryLadder) {
			// The last try: the reply stays unsent, and the person is told
			// why in the activity rather than left with a draft that never
			// went.
			if settleErr := settle(models.AgentReplyFailed, "could not be sent: "+err.Error()); settleErr != nil {
				log.Warningf("cannot record the failed reply %q: %s", reply.ID, settleErr)
			}
		}
		return fmt.Errorf("sending the reply: %w", err)
	}

	return run.Database().TransactionContext(acting, func(tx db.Transaction) error {
		if err := self.discardDraft(ctx, tx, reply.DraftItemID); err != nil {
			return err
		}
		yes := true
		if _, err := tx.SetItemFlags([]string{originalItem.ID}, models.MailboxItemFlags{Answered: &yes}); err != nil {
			return err
		}
		if err := tx.MarkContactAutoReplied(mailbox.ID, reply.To, now); err != nil {
			return err
		}
		sentMailId := ""
		_, domainName := mailparse.SplitAddress(reply.From)
		if domain, err := tx.GetDomainByName(domainName); err != nil {
			return err
		} else if domain != nil {
			mails, err := tx.ListMails(domain.ID, &db.Options{Limit: 1, Columns: aggregate.Columns{"envelopeId": `"envelope_id"`}, Aggregations: aggregate.Pipeline{{Match: &aggregate.Filter{
				Operation: aggregate.OperationEqual,
				Field:     "envelopeId",
				Value:     &envelope.ID,
			}}}})
			if err != nil {
				return err
			}
			if len(mails) > 0 {
				sentMailId = mails[0].ID
			}
		}
		if _, err := tx.UpdateAgentReply(reply.ID, func(reply *models.AgentReply) error {
			reply.Status = models.AgentReplySent
			reply.DraftItemID = ""
			reply.SentMailID = sentMailId
			reply.SentAt = &now
			return nil
		}); err != nil {
			return err
		}
		log.Noticef("the agent of mailbox %q answered %q from %q", mailbox.ID, strings.TrimSpace(reply.To), reply.From)
		return nil
	})
}
