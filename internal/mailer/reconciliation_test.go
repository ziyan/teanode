package mailer

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func acceptReconciliationFixture(test *testing.T, database db.Database, principal *access.Principal, mailbox *models.Mailbox, acceptor *countedSubmissionAcceptor, submissionId string) *models.Submission {
	test.Helper()
	request := SubmissionRequest{SubmissionID: submissionId, MailboxID: mailbox.ID, RequestContent: []byte(`{"subject":"Fixture"}`)}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		folder, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil {
			test.Fatal(err)
		}
		for _, reference := range []*string{&request.DraftItemID, &request.ReplyItemID, &request.ForwardItemID} {
			mail, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindDraft}, nil)
			if err != nil {
				test.Fatal(err)
			}
			item, err := transaction.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{Draft: new(reference == &request.DraftItemID)})
			if err != nil {
				test.Fatal(err)
			}
			*reference = item.ID
		}
	})
	outcome, err := NewSubmissionCoordinator(database, acceptor).Submit(context.Background(), principal, request, prepareSubmissionFixture)
	if err != nil {
		test.Fatal(err)
	}
	return outcome.Submission
}

func assertReconciliation(test *testing.T, database db.Database, submission *models.Submission, isComplete bool) {
	test.Helper()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.LockSubmission(submission.OwnerID, submission.SubmissionID)
		if err != nil || stored == nil || (stored.ReconciledAt != nil) != isComplete {
			test.Fatalf("reconciliation completion = %+v, %v", stored, err)
		}
		draft, err := transaction.GetItem(submission.DraftItemID)
		if err != nil || (draft == nil) != isComplete {
			test.Fatalf("draft after reconciliation = %+v, %v", draft, err)
		}
		replied, err := transaction.GetItem(submission.ReplyItemID)
		if err != nil || replied == nil || replied.Answered != isComplete {
			test.Fatalf("reply after reconciliation = %+v, %v", replied, err)
		}
		forwarded, err := transaction.GetItem(submission.ForwardItemID)
		if err != nil || forwarded == nil || forwarded.Forwarded != isComplete {
			test.Fatalf("forward after reconciliation = %+v, %v", forwarded, err)
		}
	})
}

func TestSubmissionReconciliationRecoversWithoutResending(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	submission := acceptReconciliationFixture(test, database, principal, mailbox, acceptor, "recover-request")
	var reply *models.AgentReply
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ownerAgent, err := transaction.CreateAgent(&models.Agent{UserID: principal.User.ID, Name: "Fixture agent"})
		if err != nil {
			test.Fatal(err)
		}
		reply, err = transaction.CreateAgentReply(&models.AgentReply{AgentID: ownerAgent.ID, MailboxID: mailbox.ID, DraftItemID: submission.DraftItemID, MailID: submission.MailID, Status: models.AgentReplyHeld, To: "recipient@example.net", Subject: "Fixture", Text: "Held response"})
		if err != nil {
			test.Fatal(err)
		}
	})
	for range 2 {
		// A newly constructed worker has no memory of acceptance or prior recovery.
		if err := NewSubmissionReconciler(database).RunOnce(context.Background()); err != nil {
			test.Fatal(err)
		}
		assertReconciliation(test, database, submission, true)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		updated, err := transaction.GetAgentReply(reply.ID)
		if err != nil || updated == nil || updated.Status != models.AgentReplyCancelled || updated.DraftItemID != "" {
			test.Fatalf("held reply = %+v, %v", updated, err)
		}
		feedback, err := transaction.ListAgentFeedback(reply.AgentID, []models.AgentFeedbackKind{models.FeedbackReplyDeclined}, 10)
		if err != nil || len(feedback) != 1 {
			test.Fatalf("feedback recorded %d times: %v", len(feedback), err)
		}
	})
	if acceptor.acceptCount.Load() != 1 {
		test.Fatal("reconciliation resent accepted mail")
	}
}

func TestSubmissionReconciliationDefersFailedRecordAndContinues(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	failed := acceptReconciliationFixture(test, database, principal, mailbox, acceptor, "blocked-request")
	healthy := acceptReconciliationFixture(test, database, principal, mailbox, acceptor, "healthy-request")
	// Fail the last SQL write, after flags and draft cleanup, to prove the whole
	// command rolls back and a database error does not abort the remaining batch.
	dbtest.Exec(test, database, `ALTER TABLE mail_submission ADD CONSTRAINT fixture_reconciliation CHECK (reconciled_at IS NULL OR submission_id <> 'blocked-request')`)
	reconciler := NewSubmissionReconciler(database)
	if err := reconciler.RunOnce(context.Background()); err != nil {
		test.Fatal(err)
	}
	assertReconciliation(test, database, failed, false)
	assertReconciliation(test, database, healthy, true)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.LockSubmission(failed.OwnerID, failed.SubmissionID)
		if err != nil || stored == nil || stored.ReconcileAfter == nil || !stored.ReconcileAfter.After(time.Now()) {
			test.Fatalf("failure was not deferred: %+v, %v", stored, err)
		}
		due, err := transaction.ListSubmissionsToReconcile(32)
		if err != nil || len(due) != 0 {
			test.Fatalf("failed record blocks next batch: %d, %v", len(due), err)
		}
	})
	dbtest.Exec(test, database, `ALTER TABLE mail_submission DROP CONSTRAINT fixture_reconciliation`)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeferSubmissionReconciliation(failed.OwnerID, failed.SubmissionID, time.Now().Add(-time.Second)); err != nil {
			test.Fatal(err)
		}
	})
	if err := NewSubmissionReconciler(database).RunOnce(context.Background()); err != nil {
		test.Fatal(err)
	}
	assertReconciliation(test, database, failed, true)
	if acceptor.acceptCount.Load() != 2 {
		test.Fatal("retry resent accepted mail")
	}
}

func TestSubmissionRecoveryPreservesLaterDraftConversion(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	submission := acceptReconciliationFixture(test, database, principal, mailbox, acceptor, "converted-draft")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.SetItemFlags([]string{submission.DraftItemID}, models.MailboxItemFlags{Draft: new(false)}); err != nil {
			test.Fatal(err)
		}
	})
	if err := NewSubmissionReconciler(database).Reconcile(context.Background(), submission.OwnerID, submission.SubmissionID); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.LockSubmission(submission.OwnerID, submission.SubmissionID)
		if err != nil || stored == nil || stored.ReconciledAt == nil {
			test.Fatalf("completion = %+v, %v", stored, err)
		}
		retained, err := transaction.GetItem(submission.DraftItemID)
		if err != nil || retained == nil || retained.Draft {
			test.Fatalf("converted draft was removed: %+v, %v", retained, err)
		}
	})
}

func TestSubmissionRecoveryWaitsForConcurrentDraftConversion(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	submission := acceptReconciliationFixture(test, database, principal, mailbox, acceptor, "concurrent-conversion")
	// This request has only draft cleanup, so no earlier flag write can serialize the test.
	dbtest.Exec(test, database, `UPDATE mail_submission SET reply_item_id = '', forward_item_id = ''`)
	conversionHeld := make(chan struct{})
	releaseConversion := make(chan struct{})
	conversionFinished := make(chan error, 1)
	go func() {
		conversionFinished <- database.TransactionContext(context.Background(), func(transaction db.Transaction) error {
			if _, err := transaction.SetItemFlags([]string{submission.DraftItemID}, models.MailboxItemFlags{Draft: new(false)}); err != nil {
				return err
			}
			close(conversionHeld)
			<-releaseConversion
			return nil
		})
	}()
	select {
	case <-conversionHeld:
	case err := <-conversionFinished:
		test.Fatalf("conversion did not acquire the folder: %v", err)
	}
	recoveryFinished := make(chan error, 1)
	go func() { recoveryFinished <- NewSubmissionReconciler(database).RunOnce(context.Background()) }()
	select {
	case err := <-recoveryFinished:
		close(releaseConversion)
		<-conversionFinished
		test.Fatalf("recovery ignored the uncommitted conversion: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseConversion)
	if err := <-conversionFinished; err != nil {
		test.Fatal(err)
	}
	if err := <-recoveryFinished; err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		item, err := transaction.GetItem(submission.DraftItemID)
		if err != nil || item == nil || item.Draft {
			test.Fatalf("recovery deleted concurrently converted draft: %+v, %v", item, err)
		}
		stored, err := transaction.LockSubmission(submission.OwnerID, submission.SubmissionID)
		if err != nil || stored == nil || stored.ReconciledAt == nil {
			test.Fatalf("recovery did not finish: %+v, %v", stored, err)
		}
	})
}
