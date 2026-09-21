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
)

type domainSubmissionAcceptor struct {
	acceptCount atomic.Int32
	commitCount atomic.Int32
}

func (self *domainSubmissionAcceptor) AcceptSubmission(_ context.Context, transaction db.Transaction, envelope *mailparse.Envelope, message *Message) (*models.Mail, error) {
	self.acceptCount.Add(1)
	stored, err := transaction.CreateMail(&models.Mail{DomainID: envelope.DomainID, Subject: message.Subject}, nil)
	if err == nil {
		transaction.AfterCommit(func() { self.commitCount.Add(1) })
	}
	return stored, err
}
func domainSubmissionFixture(test *testing.T) (db.Database, *access.Principal, DomainSubmissionRequest, *domainSubmissionAcceptor) {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "domain-submitter")
	request := DomainSubmissionRequest{SubmissionID: "fixture-domain-send", RequestContent: []byte(`{"subject":"Fixture"}`)}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		domain, err := transaction.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			test.Fatal(err)
		}
		request.DomainID = domain.ID
	})
	principal := &access.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionDomainManage, DomainID: request.DomainID}})}
	return database, principal, request, &domainSubmissionAcceptor{}
}
func prepareDomainFixture(context.Context, db.Transaction, *models.Domain) (*mailparse.Envelope, *Message, error) {
	return &mailparse.Envelope{}, &Message{Subject: "Fixture"}, nil
}

func TestDomainSubmissionAcceptsOnceAcrossConcurrentRetriesAndRetention(test *testing.T) {
	database, principal, request, acceptor := domainSubmissionFixture(test)
	coordinator := NewSubmissionCoordinator(database, acceptor)
	var group sync.WaitGroup
	outcomes := make(chan *models.DomainSubmission, 2)
	failures := make(chan error, 2)
	for range 2 {
		group.Go(func() {
			accepted, err := coordinator.SubmitDomain(context.Background(), principal, request, prepareDomainFixture)
			outcomes <- accepted
			failures <- err
		})
	}
	group.Wait()
	close(outcomes)
	close(failures)
	for err := range failures {
		if err != nil {
			test.Fatal(err)
		}
	}
	var mailId string
	for accepted := range outcomes {
		if accepted == nil {
			test.Fatal("no acceptance")
		}
		if mailId != "" && mailId != accepted.MailID {
			test.Fatal("different accepted messages")
		}
		mailId = accepted.MailID
	}
	if acceptor.acceptCount.Load() != 1 || acceptor.commitCount.Load() != 1 {
		test.Fatal("accepted more than once")
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteMail(mailId, nil); err != nil {
			test.Fatal(err)
		}
	})
	accepted, err := coordinator.SubmitDomain(context.Background(), principal, request, nil)
	if err != nil || accepted == nil || accepted.MailID != mailId {
		test.Fatalf("retained acceptance=%+v, %v", accepted, err)
	}
	request.RequestContent = []byte(`{"subject":"Changed"}`)
	if _, err := coordinator.SubmitDomain(context.Background(), principal, request, prepareDomainFixture); !errors.Is(err, ErrSubmissionConflict) {
		test.Fatalf("changed request=%v", err)
	}
}

func TestDomainSubmissionRollsBackFailedFinalSQL(test *testing.T) {
	database, principal, request, acceptor := domainSubmissionFixture(test)
	dbtest.Exec(test, database, `ALTER TABLE domain_submission ADD CONSTRAINT fail_acceptance CHECK (mail_id = '')`)
	coordinator := NewSubmissionCoordinator(database, acceptor)
	if _, err := coordinator.SubmitDomain(context.Background(), principal, request, prepareDomainFixture); err == nil {
		test.Fatal("expected final SQL failure")
	}
	if acceptor.commitCount.Load() != 0 || dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail`) != "0" {
		test.Fatal("failed acceptance committed mail or dispatch")
	}
	dbtest.Exec(test, database, `ALTER TABLE domain_submission DROP CONSTRAINT fail_acceptance`)
	if _, err := coordinator.SubmitDomain(context.Background(), principal, request, prepareDomainFixture); err != nil {
		test.Fatal(err)
	}
	if acceptor.commitCount.Load() != 1 {
		test.Fatal("retry did not commit once")
	}
}

func TestDomainSubmissionSeparatesConsoleAndAccountsAndChecksPermissions(test *testing.T) {
	database, principal, request, acceptor := domainSubmissionFixture(test)
	coordinator := NewSubmissionCoordinator(database, acceptor)
	console := &access.Principal{Console: true, Permissions: principal.Permissions}
	for _, actor := range []*access.Principal{principal, console} {
		if _, err := coordinator.SubmitDomain(context.Background(), actor, request, prepareDomainFixture); err != nil {
			test.Fatal(err)
		}
	}
	if acceptor.acceptCount.Load() != 2 {
		test.Fatal("console and account shared an identity")
	}
	denied := &access.Principal{User: principal.User, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailSend}})}
	if _, err := coordinator.SubmitDomain(context.Background(), denied, request, nil); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("replay without permission=%v", err)
	}
	request.SubmissionID = "invalid-preparation"
	if _, err := coordinator.SubmitDomain(context.Background(), principal, request, func(context.Context, db.Transaction, *models.Domain) (*mailparse.Envelope, *Message, error) {
		return &mailparse.Envelope{DomainID: "other-domain"}, &Message{}, nil
	}); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("foreign domain=%v", err)
	}
}

func TestDomainSubmissionLookupScopesIdentityAndSurvivesRetention(test *testing.T) {
	database, principal, request, acceptor := domainSubmissionFixture(test)
	coordinator := NewSubmissionCoordinator(database, acceptor)
	original, err := coordinator.SubmitDomain(test.Context(), principal, request, prepareDomainFixture)
	if err != nil {
		test.Fatal(err)
	}
	otherDomainId := ""
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteMail(original.MailID, nil); err != nil {
			test.Fatal(err)
		}
		domain, err := transaction.CreateDomain(&models.Domain{Domain: "example.net"})
		if err != nil {
			test.Fatal(err)
		}
		otherDomainId = domain.ID
	})
	accepted, err := coordinator.GetDomainSubmission(test.Context(), principal, request.DomainID, request.SubmissionID)
	if err != nil || accepted == nil || accepted.MailID != original.MailID {
		test.Fatalf("lookup=%+v, %v", accepted, err)
	}
	for _, actor := range []*access.Principal{
		{Console: true, Permissions: principal.Permissions},
		{User: &models.User{ID: "different-account"}, Permissions: principal.Permissions},
	} {
		accepted, err := coordinator.GetDomainSubmission(test.Context(), actor, request.DomainID, request.SubmissionID)
		if err != nil || accepted != nil {
			test.Fatalf("other principal saw acceptance: %+v, %v", accepted, err)
		}
	}
	denied := &access.Principal{User: principal.User, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailSend}})}
	if _, err := coordinator.GetDomainSubmission(test.Context(), denied, request.DomainID, request.SubmissionID); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("permission=%v", err)
	}
	otherDomainPrincipal := &access.Principal{User: principal.User, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionDomainManage, DomainID: otherDomainId}})}
	if accepted, err := coordinator.GetDomainSubmission(test.Context(), otherDomainPrincipal, otherDomainId, request.SubmissionID); err != nil || accepted != nil {
		test.Fatalf("other domain saw acceptance: %+v, %v", accepted, err)
	}
	if accepted, err := coordinator.GetDomainSubmission(test.Context(), principal, request.DomainID, "missing"); err != nil || accepted != nil {
		test.Fatalf("missing=%+v, %v", accepted, err)
	}
}

func TestDomainSubmissionLookupDoesNotWaitForUncommittedAcceptance(test *testing.T) {
	database, principal, request, acceptor := domainSubmissionFixture(test)
	isPrepared := make(chan struct{})
	canCommit := make(chan struct{})
	completed := make(chan error, 1)
	go func() {
		completed <- database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
			_, err := NewSubmissionCoordinator(transaction, acceptor).SubmitDomain(test.Context(), principal, request, prepareDomainFixture)
			close(isPrepared)
			<-canCommit
			return err
		})
	}()
	defer func() {
		close(canCommit)
		if err := <-completed; err != nil {
			test.Error(err)
		}
	}()
	select {
	case <-isPrepared:
	case <-time.After(5 * time.Second):
		test.Fatal("acceptance did not prepare")
	}
	ctx, cancel := context.WithTimeout(test.Context(), 2*time.Second)
	defer cancel()
	accepted, err := NewSubmissionCoordinator(database, acceptor).GetDomainSubmission(ctx, principal, request.DomainID, request.SubmissionID)
	if err != nil || accepted != nil {
		test.Fatalf("uncommitted lookup=%+v, %v", accepted, err)
	}
}
