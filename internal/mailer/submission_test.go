package mailer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

type countedSubmissionAcceptor struct {
	acceptCount atomic.Int32
	acceptError error
}

func (self *countedSubmissionAcceptor) AcceptSubmission(_ context.Context, transaction db.Transaction, envelope *mailparse.Envelope, message *Message) (*models.Mail, error) {
	self.acceptCount.Add(1)
	mail, err := transaction.CreateMail(&models.Mail{Subject: message.Subject, MessageID: "<" + security.NewULID() + "@example.com>"}, nil)
	if err != nil {
		return nil, err
	}
	if self.acceptError != nil {
		return nil, self.acceptError
	}
	sent, err := transaction.GetFolderByKind(envelope.MailboxID, models.MailboxFolderKindSent)
	if err != nil {
		return nil, err
	}
	if _, err := transaction.AddItem(sent.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
		return nil, err
	}
	return mail, nil
}

func coordinatorFixture(test *testing.T) (db.Database, *access.Principal, *models.Mailbox, *countedSubmissionAcceptor) {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "submission-owner")
	principal := &access.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailSend}})}
	var mailbox *models.Mailbox
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		mailbox, err = transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Submission fixture"})
		if err != nil {
			test.Fatal(err)
		}
	})
	return database, principal, mailbox, &countedSubmissionAcceptor{}
}

func prepareSubmissionFixture(context.Context, db.Transaction, *models.Mailbox) (*mailparse.Envelope, *Message, error) {
	return &mailparse.Envelope{}, &Message{Subject: "Fixture"}, nil
}

func TestSubmissionRetryReturnsOriginalAfterDraftAndMailDeletion(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	coordinator := NewSubmissionCoordinator(database, acceptor)
	request := SubmissionRequest{SubmissionID: "fixture-request", MailboxID: mailbox.ID, RequestContent: []byte(`{"subject":"Fixture"}`)}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mail, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindDraft}, nil)
		if err != nil {
			test.Fatal(err)
		}
		drafts, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil {
			test.Fatal(err)
		}
		item, err := transaction.AddItem(drafts.ID, mail.ID, "", models.MailboxItemFlags{Draft: new(true)})
		if err != nil {
			test.Fatal(err)
		}
		request.DraftItemID = item.ID
	})
	first, err := coordinator.Submit(context.Background(), principal, request, prepareSubmissionFixture)
	if err != nil || first.IsReplay || first.Submission.SentItemID == "" {
		test.Fatalf("initial acceptance = %+v, %v", first, err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.DeleteItems([]string{request.DraftItemID, first.Submission.SentItemID}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteMail(first.Submission.MailID, nil); err != nil {
			test.Fatal(err)
		}
		if err := transaction.MarkSubmissionReconciled(principal.User.ID, request.SubmissionID, time.Now()); err != nil {
			test.Fatal(err)
		}
	})
	// A replay must not rebuild content using the draft that is already gone.
	replayed, err := coordinator.Submit(context.Background(), principal, request, nil)
	if err != nil || !replayed.IsReplay || replayed.Submission.MailID != first.Submission.MailID || replayed.Submission.SentItemID != first.Submission.SentItemID {
		test.Fatalf("replay changed acceptance: %+v, %v", replayed, err)
	}
	if acceptor.acceptCount.Load() != 1 {
		test.Fatal("replay accepted another envelope")
	}
	request.RequestContent = []byte(`{"subject":"Changed"}`)
	if _, err := coordinator.Submit(context.Background(), principal, request, prepareSubmissionFixture); !errors.Is(err, ErrSubmissionConflict) {
		test.Fatalf("changed content reused identity: %v", err)
	}
	request.RequestContent = []byte(`{"subject":"Fixture"}`)
	request.DraftItemID = ""
	if _, err := coordinator.Submit(context.Background(), principal, request, nil); !errors.Is(err, ErrSubmissionConflict) {
		test.Fatalf("changed bookkeeping reused identity: %v", err)
	}
}

func TestSubmissionCoordinatorSerializesConcurrentRetries(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	request := SubmissionRequest{SubmissionID: "concurrent-request", MailboxID: mailbox.ID, RequestContent: []byte(`{"subject":"Fixture"}`)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var waitGroup sync.WaitGroup
	outcomes := make([]*SubmissionOutcome, 2)
	callErrors := make([]error, 2)
	start := make(chan struct{})
	for index := range outcomes {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			outcomes[index], callErrors[index] = NewSubmissionCoordinator(database, acceptor).Submit(ctx, principal, request, prepareSubmissionFixture)
		}(index)
	}
	close(start)
	waitGroup.Wait()
	for _, err := range callErrors {
		if err != nil {
			test.Fatal(err)
		}
	}
	if acceptor.acceptCount.Load() != 1 || outcomes[0].Submission.MailID != outcomes[1].Submission.MailID || outcomes[0].IsReplay == outcomes[1].IsReplay {
		test.Fatalf("concurrent calls did not share acceptance: %+v", outcomes)
	}
}

func TestSubmissionCoordinatorFailureCanRetryAndAuthorizationCannotBeBypassed(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	coordinator := NewSubmissionCoordinator(database, acceptor)
	request := SubmissionRequest{SubmissionID: "failed-request", MailboxID: mailbox.ID, RequestContent: []byte(`{"subject":"Fixture"}`)}
	for _, refused := range []*access.Principal{
		nil, {User: principal.User}, {User: &models.User{ID: "other-owner"}, Permissions: principal.Permissions},
	} {
		if _, err := coordinator.Submit(context.Background(), refused, request, prepareSubmissionFixture); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("authorization = %v", err)
		}
	}
	if acceptor.acceptCount.Load() != 0 {
		test.Fatal("unauthorized request reached acceptance")
	}
	acceptor.acceptError = errors.New("storage refused acceptance")
	if _, err := coordinator.Submit(context.Background(), principal, request, prepareSubmissionFixture); !errors.Is(err, acceptor.acceptError) {
		test.Fatal(err)
	}
	if mailCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail`); mailCount != "0" {
		test.Fatal("failed acceptance left a mail record")
	}
	acceptor.acceptError = nil
	accepted, err := coordinator.Submit(context.Background(), principal, request, prepareSubmissionFixture)
	if err != nil || accepted.IsReplay || acceptor.acceptCount.Load() != 2 {
		test.Fatalf("failed attempt retained an accepted identity: %+v, %v", accepted, err)
	}
}

func TestSubmissionCoordinatorRejectsReferencesOutsideItsMailbox(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	var otherItemId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		otherMailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: principal.User.ID, Name: "Other fixture"})
		if err != nil {
			test.Fatal(err)
		}
		drafts, err := transaction.GetFolderByKind(otherMailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil {
			test.Fatal(err)
		}
		mail, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindDraft}, nil)
		if err != nil {
			test.Fatal(err)
		}
		item, err := transaction.AddItem(drafts.ID, mail.ID, "", models.MailboxItemFlags{Draft: new(true)})
		if err != nil {
			test.Fatal(err)
		}
		otherItemId = item.ID
	})
	coordinator := NewSubmissionCoordinator(database, acceptor)
	for _, request := range []SubmissionRequest{
		{DraftItemID: otherItemId}, {ReplyItemID: otherItemId}, {ForwardItemID: otherItemId},
	} {
		request.MailboxID = mailbox.ID
		request.RequestContent = []byte(`{"subject":"Fixture"}`)
		if _, err := coordinator.Submit(context.Background(), principal, request, prepareSubmissionFixture); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("reference to another mailbox accepted: %v", err)
		}
	}
	if acceptor.acceptCount.Load() != 0 {
		test.Fatal("invalid references reached acceptance")
	}
}

func TestSubmissionWithoutIdentifierRemainsANewSendEachTime(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	coordinator := NewSubmissionCoordinator(database, acceptor)
	request := SubmissionRequest{MailboxID: mailbox.ID, RequestContent: []byte(`{"subject":"Fixture"}`)}
	first, err := coordinator.Submit(context.Background(), principal, request, prepareSubmissionFixture)
	if err != nil {
		test.Fatal(err)
	}
	second, err := coordinator.Submit(context.Background(), principal, request, prepareSubmissionFixture)
	if err != nil {
		test.Fatal(err)
	}
	if first.Submission.SubmissionID == "" || first.Submission.SubmissionID == second.Submission.SubmissionID || first.Submission.MailID == second.Submission.MailID || acceptor.acceptCount.Load() != 2 {
		test.Fatal("unidentified calls were incorrectly deduplicated")
	}
}

func TestSubmissionTakeoverRollsBackWithFailedAcceptance(test *testing.T) {
	database, principal, mailbox, acceptor := coordinatorFixture(test)
	var held *models.AgentReply
	request := SubmissionRequest{SubmissionID: "takeover-request", MailboxID: mailbox.ID, RequestContent: []byte(`{"subject":"Fixture"}`)}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ownerAgent, err := transaction.CreateAgent(&models.Agent{UserID: principal.User.ID, Name: "Fixture agent"})
		if err != nil {
			test.Fatal(err)
		}
		mail, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindDraft}, nil)
		if err != nil {
			test.Fatal(err)
		}
		drafts, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil {
			test.Fatal(err)
		}
		draft, err := transaction.AddItem(drafts.ID, mail.ID, "", models.MailboxItemFlags{Draft: new(true)})
		if err != nil {
			test.Fatal(err)
		}
		request.DraftItemID = draft.ID
		held, err = transaction.CreateAgentReply(&models.AgentReply{AgentID: ownerAgent.ID, MailboxID: mailbox.ID, MailID: mail.ID, DraftItemID: draft.ID, Status: models.AgentReplyHeld, To: "recipient@example.net", Subject: "Fixture", Text: "Held text"})
		if err != nil {
			test.Fatal(err)
		}
	})
	acceptor.acceptError = errors.New("acceptance failed")
	coordinator := NewSubmissionCoordinator(database, acceptor)
	if _, err := coordinator.Submit(context.Background(), principal, request, prepareSubmissionFixture); err == nil {
		test.Fatal("expected failed acceptance")
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		reply, err := transaction.GetAgentReply(held.ID)
		if err != nil || reply == nil || reply.Status != models.AgentReplyHeld || reply.DraftItemID != request.DraftItemID {
			test.Fatalf("failed acceptance cancelled reply: %+v, %v", reply, err)
		}
		corrections, err := transaction.ListAgentFeedback(held.AgentID, nil, 10)
		if err != nil || len(corrections) != 0 {
			test.Fatalf("failed acceptance recorded correction: %d, %v", len(corrections), err)
		}
	})
	acceptor.acceptError = nil
	if _, err := coordinator.Submit(context.Background(), principal, request, prepareSubmissionFixture); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		accepted, err := transaction.LockSubmission(principal.User.ID, request.SubmissionID)
		if err != nil || accepted == nil || accepted.ReconciledAt != nil {
			test.Fatalf("acceptance = %+v, %v", accepted, err)
		}
		reply, err := transaction.GetAgentReply(held.ID)
		if err != nil || reply == nil || reply.Status != models.AgentReplyCancelled || reply.DraftItemID != "" {
			test.Fatalf("accepted takeover left reply eligible: %+v, %v", reply, err)
		}
		draft, err := transaction.GetItem(request.DraftItemID)
		if err != nil || draft == nil {
			test.Fatalf("takeover removed draft before reconciliation: %+v, %v", draft, err)
		}
		corrections, err := transaction.ListAgentFeedback(held.AgentID, nil, 10)
		if err != nil || len(corrections) != 1 {
			test.Fatalf("accepted takeover corrections = %d, %v", len(corrections), err)
		}
	})
}
