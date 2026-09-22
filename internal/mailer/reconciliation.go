package mailer

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/models"
)

// SubmissionReconciler finishes mailbox work recorded by an accepted send. It
// never composes or sends mail, including when recovery repeats after a crash.
type SubmissionReconciler struct {
	transactions submissionTransactions
}

// NewSubmissionReconciler builds recovery without starting background work.
func NewSubmissionReconciler(transactions submissionTransactions) *SubmissionReconciler {
	return &SubmissionReconciler{transactions: transactions}
}

// RunOnce processes a bounded batch, sharing pending work across instances with
// row locks. Each failed command rolls back independently and waits before retry.
func (self *SubmissionReconciler) RunOnce(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		pending, err := transaction.ListSubmissionsToReconcile(32)
		if err != nil {
			return err
		}
		for _, submission := range pending {
			// Leave time to roll back a timed-out savepoint, defer that record and
			// commit earlier successes instead of exhausting the whole batch deadline.
			deadline, _ := ctx.Deadline()
			if time.Until(deadline) < 8*time.Second {
				break
			}
			commandContext, cancelCommand := context.WithTimeout(ctx, 2*time.Second)
			err := transaction.TransactionContext(commandContext, func(command db.Transaction) error {
				return reconcileSubmission(commandContext, command, submission)
			})
			cancelCommand()
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Warningf("cannot reconcile submission %q: %s", submission.SubmissionID, err)
				if err := transaction.DeferSubmissionReconciliation(submission.OwnerID, submission.SubmissionID, time.Now().Add(time.Minute)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Reconcile finishes one accepted identity, including during its caller's SQL
// transaction. A failure leaves the record pending for background recovery.
func (self *SubmissionReconciler) Reconcile(ctx context.Context, ownerId, submissionId string) error {
	return self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		submission, err := transaction.LockSubmission(ownerId, submissionId)
		if err != nil || submission == nil || submission.ReconciledAt != nil {
			return err
		}
		return reconcileSubmission(ctx, transaction, submission)
	})
}

func reconcileSubmission(ctx context.Context, transaction db.Transaction, submission *models.Submission) error {
	ownedMailbox, err := transaction.GetMailbox(submission.MailboxID)
	if err != nil {
		return err
	}
	// Deletion or reassignment ends the former owner's bookkeeping rights.
	if ownedMailbox != nil && ownedMailbox.UserID == submission.OwnerID {
		for _, reference := range []struct {
			itemId string
			flags  models.MailboxItemFlags
		}{
			{submission.ReplyItemID, models.MailboxItemFlags{Answered: new(true)}},
			{submission.ForwardItemID, models.MailboxItemFlags{Forwarded: new(true)}},
		} {
			item, err := transaction.GetItem(reference.itemId)
			if err != nil {
				return err
			}
			if item == nil {
				continue
			}
			folder, err := transaction.GetFolder(item.FolderID)
			if err != nil {
				return err
			}
			if folder != nil && folder.MailboxID == ownedMailbox.ID {
				if _, err := transaction.SetItemFlags([]string{item.ID}, reference.flags); err != nil {
					return err
				}
			}
		}
		if submission.DraftItemID != "" {
			// A draft converted to a regular item after acceptance is no longer
			// disposable; recovery must not undo that later user action.
			item, err := transaction.LockItem(submission.DraftItemID)
			if err != nil {
				return err
			}
			if item != nil && item.Draft {
				if err := mailbox.RemoveDraft(ctx, transaction, ownedMailbox.ID, item.ID); err != nil {
					return err
				}
			}
		}
	}
	return transaction.MarkSubmissionReconciled(submission.OwnerID, submission.SubmissionID, time.Now())
}
