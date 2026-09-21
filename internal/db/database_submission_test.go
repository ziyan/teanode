package db_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

func submissionFixture() *models.Submission {
	return &models.Submission{
		OwnerID: "fixture-owner", SubmissionID: "fixture-request", MailboxID: "fixture-mailbox",
		RequestDigest: strings.Repeat("a", 64), MailID: "fixture-mail", SentItemID: "fixture-sent",
		DraftItemID: "fixture-draft", ReplyItemID: "fixture-reply", ForwardItemID: "fixture-forward",
		AcceptedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestSubmissionAcceptanceAndRecoveryShareTransaction(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	submission := submissionFixture()
	injectedErr := errors.New("acceptance interrupted")
	err := database.Transaction(func(transaction db.Transaction) error {
		mail, err := transaction.CreateMail(&models.Mail{Subject: "Submission fixture"}, nil)
		if err != nil {
			return err
		}
		submission.MailID = mail.ID
		if err := transaction.CreateSubmission(submission); err != nil {
			return err
		}
		return injectedErr
	})
	if !errors.Is(err, injectedErr) {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.LockSubmission(submission.OwnerID, submission.SubmissionID)
		if err != nil || stored != nil {
			test.Fatalf("rolled-back identity survived: %+v, %v", stored, err)
		}
		mail, err := transaction.GetMail(submission.MailID, nil)
		if err != nil || mail != nil {
			test.Fatalf("rolled-back message survived: %+v, %v", mail, err)
		}
		mail, err = transaction.CreateMail(&models.Mail{Subject: "Accepted fixture"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		submission.MailID = mail.ID
		if err := transaction.CreateSubmission(submission); err != nil {
			test.Fatal(err)
		}
	})
	// A failed bookkeeping transaction leaves the accepted identity and work.
	err = database.Transaction(func(transaction db.Transaction) error {
		if err := transaction.MarkSubmissionReconciled(submission.OwnerID, submission.SubmissionID, time.Now()); err != nil {
			return err
		}
		return injectedErr
	})
	if !errors.Is(err, injectedErr) {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		pending, err := transaction.ListSubmissionsToReconcile(1)
		if err != nil || len(pending) != 1 {
			test.Fatalf("pending recovery = %+v, %v", pending, err)
		}
		stored := *pending[0]
		stored.AcceptedAt = stored.AcceptedAt.UTC()
		if stored != *submission {
			test.Fatalf("acceptance changed: %+v, want %+v", stored, submission)
		}
		if err := transaction.MarkSubmissionReconciled(submission.OwnerID, submission.SubmissionID, time.Now()); err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteMail(submission.MailID, nil); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.LockSubmission(submission.OwnerID, submission.SubmissionID)
		if err != nil || stored == nil || stored.MailID != submission.MailID || stored.ReconciledAt == nil {
			test.Fatalf("retention erased accepted identity: %+v, %v", stored, err)
		}
		pending, err := transaction.ListSubmissionsToReconcile(100)
		if err != nil || len(pending) != 0 {
			test.Fatalf("completed work stayed pending: %+v, %v", pending, err)
		}
	})
}

func TestSubmissionLocksAreScopedToOwnerAndReleasedOnRollback(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	hasLocked := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstFinished := make(chan error, 1)
	injectedErr := errors.New("rollback first acceptance")
	go func() {
		defer close(hasLocked)
		firstFinished <- database.Transaction(func(transaction db.Transaction) error {
			if _, err := transaction.LockSubmission("first-owner", "same-request"); err != nil {
				return err
			}
			hasLocked <- struct{}{}
			<-releaseFirst
			return injectedErr
		})
	}()
	finishFirst := sync.OnceFunc(func() {
		close(releaseFirst)
		if err := <-firstFinished; !errors.Is(err, injectedErr) {
			test.Errorf("first transaction: %v", err)
		}
	})
	test.Cleanup(finishFirst)
	<-hasLocked
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
		_, err := transaction.LockSubmission("second-owner", "same-request")
		return err
	}); err != nil {
		test.Fatalf("different owner waited on first: %v", err)
	}
	ctx, cancelSame := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelSame()
	if err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
		_, err := transaction.LockSubmission("first-owner", "same-request")
		return err
	}); err == nil || ctx.Err() == nil {
		test.Fatalf("same identity was not serialized: %v", err)
	}
	finishFirst()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.LockSubmission("first-owner", "same-request")
		if err != nil || stored != nil {
			test.Fatalf("rollback did not release the unused identity: %+v, %v", stored, err)
		}
	})
}

func TestSubmissionMigrationCanBeReversedAndReapplied(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	for _, migration := range migrations.Migrations() {
		if migration.ID != "0091_mail_submission" {
			continue
		}
		dbtest.Exec(test, database, migration.ReverseSQL)
		dbtest.Exec(test, database, migration.SQL)
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			if err := transaction.CreateSubmission(submissionFixture()); err != nil {
				test.Fatal(err)
			}
		})
		return
	}
	test.Fatal("submission migration is missing")
}

func TestSubmissionRecoveryWorkersTakeSeparateRecords(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for _, submissionId := range []string{"first-request", "second-request"} {
			submission := submissionFixture()
			submission.SubmissionID = submissionId
			if err := transaction.CreateSubmission(submission); err != nil {
				test.Fatal(err)
			}
		}
	})
	firstClaim := make(chan []*models.Submission, 1)
	firstFinished := make(chan error, 1)
	releaseFirst := make(chan struct{})
	go func() {
		defer close(firstClaim)
		firstFinished <- database.Transaction(func(transaction db.Transaction) error {
			submissions, err := transaction.ListSubmissionsToReconcile(1)
			firstClaim <- submissions
			if err != nil {
				return err
			}
			<-releaseFirst
			return nil
		})
	}()
	test.Cleanup(func() {
		close(releaseFirst)
		if err := <-firstFinished; err != nil {
			test.Errorf("first recovery worker: %v", err)
		}
	})
	firstBatch := <-firstClaim
	if len(firstBatch) != 1 {
		test.Fatalf("first recovery batch = %d", len(firstBatch))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
		secondBatch, err := transaction.ListSubmissionsToReconcile(1)
		if err != nil {
			return err
		}
		if len(secondBatch) != 1 || secondBatch[0].SubmissionID == firstBatch[0].SubmissionID {
			return errors.New("recovery workers selected the same record")
		}
		return transaction.MarkSubmissionReconciled(secondBatch[0].OwnerID, secondBatch[0].SubmissionID, time.Now())
	}); err != nil {
		test.Fatal(err)
	}
}
