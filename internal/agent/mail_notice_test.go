package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func noticeFixture(test *testing.T) (db.Database, *Agent, *Run, *heldReplyMailer) {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	sender := &heldReplyMailer{}
	configuration := config.Default()
	configuration.Agent.Enabled = true
	settings := &Settings{Database: database, Mailer: sender, Configuration: func() *config.Configuration { return configuration }}
	run := &Run{settings: settings}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		run.Owner, err = transaction.CreateUser(&models.User{Username: "notice-owner", Email: "recipient@example.net"})
		if err != nil {
			test.Fatal(err)
		}
		run.Agent, err = transaction.CreateAgent(&models.Agent{UserID: run.Owner.ID, Name: "Notification fixture", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: run.Owner.ID, Name: "Notification mailbox", Agent: &models.AgentMailbox{Granted: true}})
		if err != nil {
			test.Fatal(err)
		}
		domain, err := transaction.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "sender", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID}); err != nil {
			test.Fatal(err)
		}
		run.Mailbox = mailbox
		run.Job = &models.AgentJob{ID: "notice-job", AgentID: run.Agent.ID, Kind: models.AgentJobSchedule}
	})
	return database, &Agent{settings: settings}, run, sender
}

func TestNoticeAcceptanceSurvivesRetryAndRetention(test *testing.T) {
	database, worker, run, sender := noticeFixture(test)
	if err := worker.mailToPerson(context.Background(), run, "First answer", "First content"); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		accepted, err := transaction.GetSubmission(run.Owner.ID, "agent-notice-"+run.Job.ID)
		if err != nil || accepted == nil || accepted.ReconciledAt == nil || accepted.MailID != sender.lastMailId {
			test.Fatalf("accepted notice = %+v, %v", accepted, err)
		}
		if err := transaction.DeleteMail(accepted.MailID, nil); err != nil {
			test.Fatal(err)
		}
	})
	// Simulate a new worker after acceptance committed but before job completion.
	restarted := &Agent{settings: worker.settings}
	run.Owner.Email = ""
	if err := restarted.mailToPerson(context.Background(), run, "Different answer", "Different content"); err != nil {
		test.Fatal(err)
	}
	// No operations factory or model is installed: replay must return before Ask.
	if err := restarted.runSchedule(context.Background(), run); err != nil {
		test.Fatal(err)
	}
	if sender.acceptCount.Load() != 1 || sender.commitCount.Load() != 1 {
		test.Fatalf("accepts=%d commits=%d", sender.acceptCount.Load(), sender.commitCount.Load())
	}
	run.Owner.Email = "recipient@example.net"
	run.Job.ID = "next-notice-job"
	if err := restarted.mailToPerson(context.Background(), run, "Next answer", "Next content"); err != nil {
		test.Fatal(err)
	}
	if sender.acceptCount.Load() != 2 {
		test.Fatal("a new schedule occurrence did not send")
	}
}

func TestNoticeAcceptanceSerializesConcurrentRetries(test *testing.T) {
	_, worker, run, sender := noticeFixture(test)
	var group sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		group.Go(func() { failures <- worker.mailToPerson(context.Background(), run, "Answer", "Content") })
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			test.Fatal(err)
		}
	}
	if sender.acceptCount.Load() != 1 || sender.commitCount.Load() != 1 {
		test.Fatalf("accepts=%d commits=%d", sender.acceptCount.Load(), sender.commitCount.Load())
	}
}

func TestNoticeAcceptanceRollsBackFinalSQLFailure(test *testing.T) {
	database, worker, run, sender := noticeFixture(test)
	dbtest.Exec(test, database, `ALTER TABLE mail_submission ADD CONSTRAINT notice_failure CHECK (reconciled_at IS NULL)`)
	if err := worker.mailToPerson(context.Background(), run, "Answer", "Content"); err == nil {
		test.Fatal("expected final bookkeeping failure")
	}
	if sender.commitCount.Load() != 0 || dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail`) != "0" || dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail_submission`) != "0" {
		test.Fatal("failed notification retained acceptance or dispatched mail")
	}
	dbtest.Exec(test, database, `ALTER TABLE mail_submission DROP CONSTRAINT notice_failure`)
	if err := worker.mailToPerson(context.Background(), run, "Answer", "Content"); err != nil {
		test.Fatal(err)
	}
	if sender.commitCount.Load() != 1 {
		test.Fatal("retry did not commit exactly once")
	}
}

func TestNoticeRequiresOwnedJobAndGrantedMailbox(test *testing.T) {
	database, worker, run, sender := noticeFixture(test)
	run.Job.AgentID = "different-agent"
	if err := worker.mailToPerson(context.Background(), run, "Answer", "Content"); err == nil {
		test.Fatal("accepted a foreign job")
	}
	run.Job.AgentID = run.Agent.ID
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.UpdateMailbox(run.Mailbox.ID, func(mailbox *models.Mailbox) error {
			mailbox.Agent.Granted = false
			return nil
		}); err != nil {
			test.Fatal(err)
		}
	})
	if err := worker.mailToPerson(context.Background(), run, "Answer", "Content"); err == nil {
		test.Fatal("accepted from an ungranted mailbox")
	}
	if sender.acceptCount.Load() != 0 {
		test.Fatal("invalid notification reached acceptance")
	}
}
