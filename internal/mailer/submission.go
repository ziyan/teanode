package mailer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

// ErrSubmissionConflict means this owner already used the identifier for a
// different request. The accepted result cannot be replaced or sent again.
var ErrSubmissionConflict = errors.New("submission identifier already used for different content")

type submissionTransactions interface {
	TransactionContext(context.Context, func(db.Transaction) error) error
}

// SubmissionAcceptor joins composition and exchange acceptance to a transaction.
type SubmissionAcceptor interface {
	AcceptSubmission(context.Context, db.Transaction, *mailparse.Envelope, *Message) (*models.Mail, error)
}

// SubmissionRequest binds an identifier to stable, server-serialized request
// content. RequestContent excludes generated MIME identifiers and timestamps.
type SubmissionRequest struct {
	SubmissionID   string
	MailboxID      string
	RequestContent []byte
	DraftItemID    string
	ReplyItemID    string
	ForwardItemID  string
}

// SubmissionOutcome names the original acceptance, including on a retry.
// Pending reconciliation is recorded in Submission.ReconciledAt.
type SubmissionOutcome struct {
	Submission *models.Submission
	IsReplay   bool
}

// SubmissionPreparer resolves draft/attachment content only for a new request.
// It must use the supplied transaction and must not send or commit mail itself.
type SubmissionPreparer func(context.Context, db.Transaction, *models.Mailbox) (*mailparse.Envelope, *Message, error)

// SubmissionCoordinator authorizes, serializes and records local acceptance.
// If constructed with a transaction, its result remains subject to that caller's
// commit. Delivery and pending mailbox reconciliation run after that commit.
type SubmissionCoordinator struct {
	transactions submissionTransactions
	acceptor     SubmissionAcceptor
}

// NewSubmissionCoordinator accepts a database or an existing transaction scope.
func NewSubmissionCoordinator(transactions submissionTransactions, acceptor SubmissionAcceptor) *SubmissionCoordinator {
	return &SubmissionCoordinator{transactions: transactions, acceptor: acceptor}
}

// Submit accepts a new request once, or returns its original persisted result.
func (self *SubmissionCoordinator) Submit(ctx context.Context, principal *access.Principal, request SubmissionRequest, prepare SubmissionPreparer) (*SubmissionOutcome, error) {
	if principal == nil || principal.User == nil || principal.Permissions == nil || !principal.Permissions.Has(models.PermissionMailSend) {
		return nil, db.ErrNotFound
	}
	if len(request.RequestContent) == 0 {
		return nil, fmt.Errorf("%w: submission request content is required", db.ErrInvalidArguments)
	}
	// Older callers get a new identity for each call. Only callers retaining
	// and resupplying an identifier get protection across independent retries.
	if request.SubmissionID == "" {
		request.SubmissionID = security.NewULID()
	}
	digestRequest := request
	digestRequest.SubmissionID = ""
	digestRequest.RequestContent = nil
	content, err := json.Marshal(digestRequest)
	if err != nil {
		return nil, err
	}
	// Keep the potentially large message content out of JSON's base64 copy.
	// JSON metadata contains no raw NUL, which separates it from the content.
	digest := sha256.New()
	_, _ = digest.Write(content)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(request.RequestContent)
	requestDigest := hex.EncodeToString(digest.Sum(nil))
	var outcome *SubmissionOutcome
	err = self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		mailbox, err := transaction.GetMailbox(request.MailboxID)
		if err != nil {
			return err
		}
		if mailbox == nil || mailbox.UserID != principal.User.ID {
			return db.ErrNotFound
		}
		stored, err := transaction.LockSubmission(principal.User.ID, request.SubmissionID)
		if err != nil {
			return err
		}
		if stored != nil {
			if stored.MailboxID != request.MailboxID || stored.RequestDigest != requestDigest {
				return ErrSubmissionConflict
			}
			outcome = &SubmissionOutcome{Submission: stored, IsReplay: true}
			return nil
		}
		if err := validateSubmissionItems(transaction, request); err != nil {
			return err
		}
		if prepare == nil {
			return fmt.Errorf("%w: submission preparation is required", db.ErrInvalidArguments)
		}
		envelope, message, err := prepare(ctx, transaction, mailbox)
		if err != nil {
			return err
		}
		if envelope == nil || message == nil || (envelope.MailboxID != "" && envelope.MailboxID != mailbox.ID) {
			return db.ErrInvalidArguments
		}
		envelope.MailboxID = mailbox.ID
		accepted, err := self.acceptor.AcceptSubmission(ctx, transaction, envelope, message)
		if err != nil {
			return err
		}
		if accepted == nil || accepted.ID == "" {
			return fmt.Errorf("submission acceptor returned no stored mail")
		}
		stored = &models.Submission{
			OwnerID: principal.User.ID, SubmissionID: request.SubmissionID, MailboxID: mailbox.ID,
			RequestDigest: requestDigest, MailID: accepted.ID, AcceptedAt: time.Now().Truncate(time.Microsecond),
			DraftItemID: request.DraftItemID, ReplyItemID: request.ReplyItemID, ForwardItemID: request.ForwardItemID,
		}
		sent, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindSent)
		if err != nil {
			return err
		}
		if copied, err := mx.FindSentCopy(transaction, sent, accepted.MessageID); err != nil {
			return err
		} else if copied != nil {
			stored.SentItemID = copied.ID
		}
		if err := transaction.CreateSubmission(stored); err != nil {
			return err
		}
		outcome = &SubmissionOutcome{Submission: stored}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return outcome, nil
}

func validateSubmissionItems(transaction db.Transaction, request SubmissionRequest) error {
	for _, reference := range []struct {
		itemId  string
		isDraft bool
	}{{request.DraftItemID, true}, {request.ReplyItemID, false}, {request.ForwardItemID, false}} {
		if reference.itemId == "" {
			continue
		}
		item, err := transaction.GetItem(reference.itemId)
		if err != nil {
			return err
		}
		if item == nil {
			if reference.isDraft {
				continue
			}
			return db.ErrNotFound
		}
		folder, err := transaction.GetFolder(item.FolderID)
		if err != nil {
			return err
		}
		if folder == nil || folder.MailboxID != request.MailboxID {
			return db.ErrNotFound
		}
		if reference.isDraft && !item.Draft {
			return db.ErrInvalidArguments
		}
	}
	return nil
}
