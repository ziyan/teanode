package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func heldReplyAPIFixture(test *testing.T) (db.Database, *graph, *api.Principal, *models.AgentReply) {
	test.Helper()
	database, resolver, principal, arguments, _ := submissionAPIFixture(test)
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})
	var reply *models.AgentReply
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ownerAgent, err := transaction.CreateAgent(&models.Agent{UserID: principal.User.ID, Name: "Fixture agent"})
		if err != nil {
			test.Fatal(err)
		}
		draft, err := transaction.GetItem(arguments.Message.DraftItemID)
		if err != nil {
			test.Fatal(err)
		}
		reply, err = transaction.CreateAgentReply(&models.AgentReply{AgentID: ownerAgent.ID, MailboxID: arguments.MailboxID, DraftItemID: draft.ID, MailID: draft.MailID, Status: models.AgentReplyHeld, To: "recipient@example.net", Subject: "Fixture", Text: "Held response"})
		if err != nil {
			test.Fatal(err)
		}
	})
	return database, resolver, principal, reply
}

func TestCancelHeldReplyRecordsOneCorrectionAndRollsBackFailures(test *testing.T) {
	for _, hasFeedbackFailure := range []bool{false, true} {
		name := "success"
		if hasFeedbackFailure {
			name = "feedback-failure"
		}
		test.Run(name, func(test *testing.T) {
			database, resolver, principal, reply := heldReplyAPIFixture(test)
			if hasFeedbackFailure {
				dbtest.Exec(test, database, `ALTER TABLE agent_feedback ADD CONSTRAINT fixture_feedback CHECK (false)`)
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
				updated, err := resolver.CancelAgentReply(ctx, CancelAgentReplyArguments{ReplyID: reply.ID})
				if hasFeedbackFailure {
					if err == nil || updated != nil {
						test.Fatal("failed feedback did not fail the cancellation command")
					}
				} else {
					if err != nil || updated == nil || updated.Status != models.AgentReplyCancelled || updated.Reason != "cancelled by the person" {
						test.Fatalf("cancel = %+v, %v", updated, err)
					}
					if _, err := resolver.CancelAgentReply(ctx, CancelAgentReplyArguments{ReplyID: reply.ID}); err != nil {
						test.Fatal(err)
					}
				}
			})
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				stored, err := transaction.GetAgentReply(reply.ID)
				if err != nil || stored == nil || (stored.Status == models.AgentReplyHeld) != hasFeedbackFailure {
					test.Fatalf("reply = %+v, %v", stored, err)
				}
				draft, err := transaction.GetItem(reply.DraftItemID)
				if err != nil || (draft != nil) != hasFeedbackFailure {
					test.Fatalf("draft = %+v, %v", draft, err)
				}
				corrections, err := transaction.ListAgentFeedback(reply.AgentID, nil, 10)
				expectedCount := 1
				if hasFeedbackFailure {
					expectedCount = 0
				}
				if err != nil || len(corrections) != expectedCount {
					test.Fatalf("corrections = %d, %v", len(corrections), err)
				}
			})
		})
	}
}

type staleReplyTransaction struct {
	db.Transaction
	snapshot *models.AgentReply
}

func (self *staleReplyTransaction) GetAgentReply(replyId string) (*models.AgentReply, error) {
	if self.snapshot != nil && self.snapshot.ID == replyId {
		snapshot := self.snapshot
		self.snapshot = nil
		return snapshot, nil
	}
	return self.Transaction.GetAgentReply(replyId)
}

func TestCancelHeldReplyDoesNotOverwriteAcceptedReply(test *testing.T) {
	database, resolver, principal, reply := heldReplyAPIFixture(test)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.UpdateAgentReply(reply.ID, func(current *models.AgentReply) error {
			current.Status = models.AgentReplySent
			current.DraftItemID = ""
			current.SentMailID = "accepted-fixture"
			return nil
		}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.DeleteItems([]string{reply.DraftItemID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		// Reproduce a read of Held before the send committed; the guarded update
		// must observe the newer Sent row after obtaining its locks.
		stale := &staleReplyTransaction{Transaction: transaction, snapshot: reply}
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), stale)
		updated, err := resolver.CancelAgentReply(ctx, CancelAgentReplyArguments{ReplyID: reply.ID})
		if err != nil || updated == nil || updated.Status != models.AgentReplySent || updated.SentMailID != "accepted-fixture" {
			test.Fatalf("stale cancellation = %+v, %v", updated, err)
		}
		corrections, err := transaction.ListAgentFeedback(reply.AgentID, nil, 10)
		if err != nil || len(corrections) != 0 {
			test.Fatalf("recorded cancellation of sent reply: %d, %v", len(corrections), err)
		}
	})
}
