package mailer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

// DomainSubmissionRequest binds an operator's request to its stable parameters,
// before rendering a template or generating MIME identifiers.
type DomainSubmissionRequest struct {
	SubmissionID   string
	DomainID       string
	RequestContent []byte
}

// DomainSubmissionPreparer resolves content only when acceptance is new.
type DomainSubmissionPreparer func(context.Context, db.Transaction, *models.Domain) (*mailparse.Envelope, *Message, error)

// SubmitDomain accepts once for an account or the console, under domain-manage
// permission. Unlike mailbox sends, it has no later mailbox work to reconcile.
func (self *SubmissionCoordinator) SubmitDomain(ctx context.Context, principal *access.Principal, request DomainSubmissionRequest, prepare DomainSubmissionPreparer) (*models.DomainSubmission, error) {
	if principal == nil || principal.Permissions == nil || !principal.Permissions.HasOverDomain(models.PermissionDomainManage, request.DomainID) {
		return nil, db.ErrNotFound
	}
	principalId := "console"
	if !principal.Console {
		if principal.User == nil || principal.User.ID == "" {
			return nil, db.ErrNotFound
		}
		principalId = "user:" + principal.User.ID
	}
	if len(request.RequestContent) == 0 {
		return nil, fmt.Errorf("%w: submission content is required", db.ErrInvalidArguments)
	}
	if request.SubmissionID == "" {
		request.SubmissionID = security.NewULID()
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(request.DomainID + "\x00"))
	_, _ = digest.Write(request.RequestContent)
	requestDigest := hex.EncodeToString(digest.Sum(nil))
	var accepted *models.DomainSubmission
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		domain, err := transaction.GetDomain(request.DomainID)
		if err != nil {
			return err
		}
		if domain == nil {
			return db.ErrNotFound
		}
		stored, err := transaction.LockDomainSubmission(principalId, request.SubmissionID)
		if err != nil {
			return err
		}
		if stored != nil {
			if stored.DomainID != domain.ID || stored.RequestDigest != requestDigest {
				return ErrSubmissionConflict
			}
			accepted = stored
			return nil
		}
		if prepare == nil {
			return fmt.Errorf("%w: submission preparation is required", db.ErrInvalidArguments)
		}
		envelope, message, err := prepare(ctx, transaction, domain)
		if err != nil {
			return err
		}
		if envelope == nil || message == nil || envelope.MailboxID != "" || envelope.CredentialID != "" || (envelope.DomainID != "" && envelope.DomainID != domain.ID) {
			return db.ErrInvalidArguments
		}
		envelope.DomainID = domain.ID
		mail, err := self.acceptor.AcceptSubmission(ctx, transaction, envelope, message)
		if err != nil {
			return err
		}
		if mail == nil || mail.ID == "" {
			return fmt.Errorf("submission acceptor returned no stored mail")
		}
		stored = &models.DomainSubmission{PrincipalID: principalId, SubmissionID: request.SubmissionID, DomainID: domain.ID, RequestDigest: requestDigest, MailID: mail.ID, AcceptedAt: time.Now().Truncate(time.Microsecond)}
		if err := transaction.CreateDomainSubmission(stored); err != nil {
			return err
		}
		accepted = stored
		return nil
	})
	if err != nil {
		return nil, err
	}
	return accepted, nil
}
