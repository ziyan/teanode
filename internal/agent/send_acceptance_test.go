package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

type heldReplyMailer struct {
	mailer.Mailer
	acceptCount atomic.Int32
	commitCount atomic.Int32
	lastMailId  string
}

func (self *heldReplyMailer) AcceptSubmission(_ context.Context, transaction db.Transaction, envelope *mailparse.Envelope, message *mailer.Message) (*models.Mail, error) {
	self.acceptCount.Add(1)
	accepted, err := transaction.CreateMail(&models.Mail{Subject: message.Subject}, nil)
	if err != nil {
		return nil, err
	}
	self.lastMailId = accepted.ID
	sent, err := transaction.GetFolderByKind(envelope.MailboxID, models.MailboxFolderKindSent)
	if err != nil {
		return nil, err
	}
	if _, err := transaction.AddItem(sent.ID, accepted.ID, "", models.MailboxItemFlags{}); err != nil {
		return nil, err
	}
	transaction.AfterCommit(func() { self.commitCount.Add(1) })
	return accepted, nil
}

func heldReplyFixture(test *testing.T) (db.Database, *Agent, *models.AgentReply, string, *heldReplyMailer) {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "held-reply-owner")
	var reply *models.AgentReply
	var originalItemId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ownerAgent, err := transaction.CreateAgent(&models.Agent{UserID: userId, Name: "Fixture agent"})
		if err != nil {
			test.Fatal(err)
		}
		ownedMailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Reply fixture"})
		if err != nil {
			test.Fatal(err)
		}
		original, err := transaction.CreateMail(&models.Mail{Subject: "Original fixture"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		inbox, err := transaction.GetFolderByKind(ownedMailbox.ID, models.MailboxFolderKindInbox)
		if err != nil {
			test.Fatal(err)
		}
		originalItem, err := transaction.AddItem(inbox.ID, original.ID, "", models.MailboxItemFlags{})
		if err != nil {
			test.Fatal(err)
		}
		originalItemId = originalItem.ID
		draftMail, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindDraft}, nil)
		if err != nil {
			test.Fatal(err)
		}
		drafts, err := transaction.GetFolderByKind(ownedMailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil {
			test.Fatal(err)
		}
		draft, err := transaction.AddItem(drafts.ID, draftMail.ID, "", models.MailboxItemFlags{Draft: new(true)})
		if err != nil {
			test.Fatal(err)
		}
		created, err := transaction.CreateAgentReply(&models.AgentReply{AgentID: ownerAgent.ID, MailboxID: ownedMailbox.ID, MailID: original.ID, DraftItemID: draft.ID, Status: models.AgentReplyHeld, From: "sender@example.com", To: "recipient@example.net", Subject: "Reply fixture", Text: "Reply content"})
		if err != nil {
			test.Fatal(err)
		}
		reply, err = transaction.GetAgentReply(created.ID)
		if err != nil {
			test.Fatal(err)
		}
	})
	sender := &heldReplyMailer{}
	return database, &Agent{settings: &Settings{Mailer: sender}}, reply, originalItemId, sender
}

func TestHeldReplyAcceptanceCommitsOnlyOnce(test *testing.T) {
	database, worker, reply, originalItemId, sender := heldReplyFixture(test)
	var group sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := worker.acceptHeldReply(context.Background(), database, reply, originalItemId, &mailer.Message{Subject: reply.Subject}, time.Now())
			failures <- err
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			test.Fatal(err)
		}
	}
	if sender.acceptCount.Load() != 1 || sender.commitCount.Load() != 1 {
		test.Fatalf("accepts=%d, commits=%d", sender.acceptCount.Load(), sender.commitCount.Load())
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.GetAgentReply(reply.ID)
		if err != nil || stored == nil || stored.Status != models.AgentReplySent || stored.SentMailID != sender.lastMailId || stored.SentAt == nil {
			test.Fatalf("stored reply = %+v, %v", stored, err)
		}
		original, err := transaction.GetItem(originalItemId)
		if err != nil || original == nil || !original.Answered {
			test.Fatalf("original = %+v, %v", original, err)
		}
		draft, err := transaction.GetItem(reply.DraftItemID)
		if err != nil || draft != nil {
			test.Fatalf("draft = %+v, %v", draft, err)
		}
	})
	if quota := dbtest.QueryString(test, database, `SELECT COALESCE(sum("count"), 0)::text FROM mailbox_auto_reply`); quota != "1" {
		test.Fatalf("quota counted %s accepts", quota)
	}
}

func TestHeldReplyAcceptanceRollsBackFailedBookkeeping(test *testing.T) {
	database, worker, reply, originalItemId, sender := heldReplyFixture(test)
	dbtest.Exec(test, database, `ALTER TABLE agent_reply ADD CONSTRAINT fixture_sent CHECK (status <> 'sent')`)
	wasAccepted, err := worker.acceptHeldReply(context.Background(), database, reply, originalItemId, &mailer.Message{Subject: reply.Subject}, time.Now())
	if err == nil || wasAccepted {
		test.Fatal("failed bookkeeping accepted a reply")
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.GetAgentReply(reply.ID)
		if err != nil || stored == nil || stored.Status != models.AgentReplyHeld || stored.DraftItemID != reply.DraftItemID {
			test.Fatalf("failed acceptance changed reply: %+v, %v", stored, err)
		}
		mail, err := transaction.GetMail(sender.lastMailId, nil)
		if err != nil || mail != nil {
			test.Fatalf("failed acceptance retained mail: %+v, %v", mail, err)
		}
		draft, err := transaction.GetItem(reply.DraftItemID)
		if err != nil || draft == nil {
			test.Fatalf("failed acceptance lost draft: %+v, %v", draft, err)
		}
		original, err := transaction.GetItem(originalItemId)
		if err != nil || original == nil || original.Answered {
			test.Fatalf("failed acceptance changed original: %+v, %v", original, err)
		}
	})
	if sender.commitCount.Load() != 0 {
		test.Fatal("failed acceptance ran commit callback")
	}
	if quota := dbtest.QueryString(test, database, `SELECT COALESCE(sum("count"), 0)::text FROM mailbox_auto_reply`); quota != "0" {
		test.Fatalf("rollback consumed quota: %s", quota)
	}
	dbtest.Exec(test, database, `ALTER TABLE agent_reply DROP CONSTRAINT fixture_sent`)
	if wasAccepted, err := worker.acceptHeldReply(context.Background(), database, reply, originalItemId, &mailer.Message{Subject: reply.Subject}, time.Now()); err != nil || !wasAccepted {
		test.Fatalf("retry = %t, %v", wasAccepted, err)
	}
	if sender.commitCount.Load() != 1 {
		test.Fatal("retry did not commit once")
	}
}

func TestHeldReplyTakeoverDefeatsStaleSendSnapshot(test *testing.T) {
	database, worker, reply, originalItemId, sender := heldReplyFixture(test)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := mailbox.TakeOverDraft(context.Background(), transaction, reply.MailboxID, reply.DraftItemID); err != nil {
			test.Fatal(err)
		}
	})
	wasAccepted, err := worker.acceptHeldReply(context.Background(), database, reply, originalItemId, &mailer.Message{Subject: reply.Subject}, time.Now())
	if err != nil || wasAccepted || sender.acceptCount.Load() != 0 {
		test.Fatalf("cancelled snapshot sent: %t, %v", wasAccepted, err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.GetAgentReply(reply.ID)
		if err != nil || stored == nil || stored.Status != models.AgentReplyCancelled {
			test.Fatalf("takeover was overwritten: %+v, %v", stored, err)
		}
		corrections, err := transaction.ListAgentFeedback(reply.AgentID, nil, 10)
		if err != nil || len(corrections) != 1 {
			test.Fatalf("corrections = %d, %v", len(corrections), err)
		}
	})
}

func TestHeldReplyChangedWhilePreparingIsRetried(test *testing.T) {
	database, worker, reply, originalItemId, sender := heldReplyFixture(test)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.UpdateAgentReply(reply.ID, func(current *models.AgentReply) error { current.Text = "Updated response"; return nil }); err != nil {
			test.Fatal(err)
		}
	})
	wasAccepted, err := worker.acceptHeldReply(context.Background(), database, reply, originalItemId, &mailer.Message{Text: reply.Text}, time.Now())
	var deferral *Deferral
	if wasAccepted || !errors.As(err, &deferral) || sender.acceptCount.Load() != 0 {
		test.Fatalf("stale content was not deferred: %t, %v", wasAccepted, err)
	}
}

func TestAgentCleanupPreservesConvertedDraft(test *testing.T) {
	database, worker, reply, _, _ := heldReplyFixture(test)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.SetItemFlags([]string{reply.DraftItemID}, models.MailboxItemFlags{Draft: new(false)}); err != nil {
			test.Fatal(err)
		}
		if err := worker.discardDraft(context.Background(), transaction, reply.DraftItemID); err != nil {
			test.Fatal(err)
		}
		retained, err := transaction.GetItem(reply.DraftItemID)
		if err != nil || retained == nil || retained.Draft {
			test.Fatalf("cleanup removed converted draft: %+v, %v", retained, err)
		}
	})
}

func TestHeldReplyAcceptanceHonorsAtomicHourlyLimit(test *testing.T) {
	database, worker, reply, originalItemId, sender := heldReplyFixture(test)
	now := time.Now()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for range agentReplyHourlyLimit {
			canSend, err := transaction.ClaimAutoReply(reply.MailboxID, now, agentReplyHourlyLimit)
			if err != nil || !canSend {
				test.Fatalf("quota setup: %t, %v", canSend, err)
			}
		}
	})
	wasAccepted, err := worker.acceptHeldReply(context.Background(), database, reply, originalItemId, &mailer.Message{Subject: reply.Subject}, now)
	if wasAccepted || !errors.Is(err, errAutoReplyLimit) || sender.acceptCount.Load() != 0 {
		test.Fatalf("limit allowed acceptance: %t, %v", wasAccepted, err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stored, err := transaction.GetAgentReply(reply.ID)
		if err != nil || stored == nil || stored.Status != models.AgentReplyHeld {
			test.Fatalf("quota refusal left ambiguous send: %+v, %v", stored, err)
		}
	})
}
