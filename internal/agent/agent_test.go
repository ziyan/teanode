package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The delivery hook queues work only for a granted mailbox of an active
// agent, inside the caller's transaction; the worker claims what is due,
// runs it, and records how it ended.
func TestDeliveryHookAndWorker(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "p", Kind: "openai", APIKey: "k"}}
	configuration.Agent.Models.Default = "p:m"

	worker := agent.New(&agent.Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test",
		Tick:          time.Hour, // ticks are driven by hand below
	})

	var owner *models.User
	var mailbox *models.Mailbox
	var mail *models.Mail
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal"}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if mail, err = tx.CreateMail(&models.Mail{Subject: "hello", Kind: models.MailKindIncoming}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		// No agent yet: the hook must queue nothing, and must not complain.
		worker.OnMailboxDelivery(tx, mailbox, nil, mail)
		if count, _ := tx.CountAgentJobs(nil); count != 0 {
			t.Fatalf("nothing should be queued without an agent, got %d", count)
		}
	})

	var found *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		// Granted, but with sorting off: still nothing to queue.
		if mailbox, err = tx.UpdateMailbox(mailbox.ID, func(mailbox *models.Mailbox) error {
			mailbox.Agent = &models.AgentMailbox{Granted: true, Triage: &models.AgentTriage{Enabled: false}}
			return nil
		}); err != nil {
			t.Fatalf("UpdateMailbox: %s", err)
		}
		worker.OnMailboxDelivery(tx, mailbox, nil, mail)
		if count, _ := tx.CountAgentJobs(nil); count != 0 {
			t.Fatalf("nothing should be queued with sorting off, got %d", count)
		}
		if mailbox, err = tx.UpdateMailbox(mailbox.ID, func(mailbox *models.Mailbox) error {
			mailbox.Agent.Triage.Enabled = true
			return nil
		}); err != nil {
			t.Fatalf("UpdateMailbox: %s", err)
		}
		worker.OnMailboxDelivery(tx, mailbox, nil, mail)
		jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID}, nil)
		if len(jobs) != 1 || jobs[0].Kind != models.AgentJobTriage || jobs[0].SubjectID != mail.ID || jobs[0].MailboxID != mailbox.ID {
			t.Fatalf("a triage job for the message was expected, got %+v", jobs)
		}
		// A noop is what this milestone can run end to end.
		if _, err := worker.Enqueue(tx, models.AgentJobNoop, found.ID, mailbox.ID, ""); err != nil {
			t.Fatalf("Enqueue: %s", err)
		}
	})

	// One tick claims and runs what is due; the noop ends done, and the
	// triage — which has no handler yet in this milestone — retries rather
	// than dying on its first attempt.
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		done, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobNoop}}, nil)
		if len(done) != 1 || done[0].Status != models.AgentJobDone || done[0].FinishedAt == nil {
			t.Fatalf("the noop should be done: %+v", done)
		}
		triage, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobTriage}}, nil)
		if len(triage) != 1 || triage[0].Status != models.AgentJobQueued || triage[0].NotBefore == nil || triage[0].Attempts != 1 {
			t.Fatalf("an unhandled job retries later: %+v", triage)
		}
	})

	// The budget: a person over their daily limit has their work deferred
	// to midnight in their own zone, not failed.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.UpdateUser(owner.ID, func(user *models.User) error {
			user.Timezone, user.TimezoneMode = "Asia/Tokyo", "fixed"
			return nil
		}); err != nil {
			t.Fatalf("UpdateUser: %s", err)
		}
		if err := tx.PutAgentUsage(&db.AgentUsage{AgentID: found.ID, Kind: "triage", At: time.Now(), Values: []uint64{150000, 60000, 0, 0, 1}}); err != nil {
			t.Fatalf("PutAgentUsage: %s", err)
		}
		owner, _ = tx.GetUser(owner.ID)
		err := agent.RequireBudget(tx, configuration, found, owner, time.Now())
		deferral, ok := err.(*agent.Deferral)
		if !ok {
			t.Fatalf("expected a deferral, got %v", err)
		}
		tokyo, _ := time.LoadLocation("Asia/Tokyo")
		if deferral.Until.In(tokyo).Hour() != 0 || deferral.Until.In(tokyo).Minute() != 0 {
			t.Fatalf("the deferral should end at midnight in the person's zone, got %s", deferral.Until.In(tokyo))
		}
	})
}
