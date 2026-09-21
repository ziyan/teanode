package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func noticeSubmissionId(run *Run) (string, error) {
	if run.Job == nil || run.Job.ID == "" || run.Owner == nil || run.Agent == nil || run.Agent.UserID != run.Owner.ID || run.Job.AgentID != run.Agent.ID {
		return "", fmt.Errorf("notification requires an owned agent job")
	}
	return "agent-notice-" + run.Job.ID, nil
}

func (self *Agent) hasAcceptedNotice(ctx context.Context, run *Run) (bool, error) {
	submissionId, err := noticeSubmissionId(run)
	if err != nil {
		return false, err
	}
	hasAccepted := false
	err = run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		accepted, err := transaction.GetSubmission(run.Owner.ID, submissionId)
		hasAccepted = accepted != nil
		return err
	})
	return hasAccepted, err
}

// mailToPerson accepts one notification per job. An accepted answer wins even
// if a retry generated different text or the person's notification address
// changed. Each scheduled occurrence or goal continuation has a different job.
func (self *Agent) mailToPerson(ctx context.Context, run *Run, subject, body string) error {
	submissionId, err := noticeSubmissionId(run)
	if err != nil {
		return err
	}
	return run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		accepted, err := transaction.LockSubmission(run.Owner.ID, submissionId)
		if err != nil || accepted != nil {
			return err
		}
		if run.Owner.Email == "" || self.settings.Mailer == nil {
			return fmt.Errorf("the account has no notification address to mail the answer to")
		}
		mailboxes, err := transaction.ListMailboxes(run.Owner.ID)
		if err != nil {
			return err
		}
		var sender *models.Mailbox
		for _, mailbox := range mailboxes {
			if mailbox.Agent != nil && mailbox.Agent.Granted && len(mailbox.Addresses) > 0 {
				sender = mailbox
				break
			}
		}
		if sender == nil {
			return fmt.Errorf("no granted mailbox has an address to send from")
		}
		message := &mailer.Message{
			From: sender.Addresses[0].Address, FromName: run.Agent.DisplayName(), To: []string{run.Owner.Email}, Subject: subject, Text: body,
			Headers: []string{mailparse.UnsplitHeader("Auto-Submitted", "auto-generated"), mailparse.UnsplitHeader("X-Auto-Response-Suppress", "All")},
		}
		content, err := json.Marshal(message)
		if err != nil {
			return err
		}
		// The standing schedule or goal authorizes this notification only from
		// a granted mailbox. This principal is private to this single command.
		principal := &access.Principal{User: run.Owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailSend}})}
		coordinator := mailer.NewSubmissionCoordinator(transaction, self.settings.Mailer)
		if _, err := coordinator.Submit(ctx, principal, mailer.SubmissionRequest{SubmissionID: submissionId, MailboxID: sender.ID, RequestContent: content}, func(context.Context, db.Transaction, *models.Mailbox) (*mailparse.Envelope, *mailer.Message, error) {
			return &mailparse.Envelope{MailboxID: sender.ID}, message, nil
		}); err != nil {
			return err
		}
		// Notifications have no draft or answered/forwarded flags to reconcile.
		return transaction.MarkSubmissionReconciled(run.Owner.ID, submissionId, time.Now())
	})
}
