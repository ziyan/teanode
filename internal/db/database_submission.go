package db

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// SubmissionOperation keeps local acceptance and later mailbox reconciliation
// in the same database as mail. Callers must bind a retry to its request digest.
type SubmissionOperation interface {
	// LockSubmission serializes this owner's identifier through transaction end,
	// including when no record exists. A nil result allows initial acceptance.
	LockSubmission(ownerId, submissionId string) (*models.Submission, error)
	CreateSubmission(submission *models.Submission) error
	// ListSubmissionsToReconcile locks pending records without waiting for other
	// workers. Reconcile and mark them in this same transaction.
	ListSubmissionsToReconcile(limit int) ([]*models.Submission, error)
	MarkSubmissionReconciled(ownerId, submissionId string, reconciledAt time.Time) error
	DeferSubmissionReconciliation(ownerId, submissionId string, retryAt time.Time) error
}

type submissionModel models.Submission

func (self *submissionModel) TableName() string { return "mail_submission" }

func validateSubmissionIdentity(ownerId, submissionId string) error {
	if ownerId == "" || len(ownerId) > 32 || submissionId == "" || len(submissionId) > 128 {
		return fmt.Errorf("invalid submission identity")
	}
	return nil
}

func (self *transaction) LockSubmission(ownerId, submissionId string) (*models.Submission, error) {
	if err := validateSubmissionIdentity(ownerId, submissionId); err != nil {
		return nil, err
	}
	// Hash collisions only serialize unrelated submissions. The composite
	// database key, never this hash, determines whether a request was accepted.
	digest := sha256.Sum256([]byte("mail_submission\x00" + ownerId + "\x00" + submissionId))
	lockKey := int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
	if err := self.tx.Exec("SELECT pg_advisory_xact_lock(?)", lockKey).Error; err != nil {
		return nil, err
	}
	var stored submissionModel
	err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_id = ? AND submission_id = ?", ownerId, submissionId).Take(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return (*models.Submission)(&stored), nil
}

func (self *transaction) CreateSubmission(submission *models.Submission) error {
	if submission == nil {
		return fmt.Errorf("submission is required")
	}
	if err := validateSubmissionIdentity(submission.OwnerID, submission.SubmissionID); err != nil {
		return err
	}
	digest, err := hex.DecodeString(submission.RequestDigest)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != submission.RequestDigest {
		return fmt.Errorf("submission requires a SHA-256 request digest")
	}
	if submission.MailboxID == "" || submission.MailID == "" || submission.AcceptedAt.IsZero() {
		return fmt.Errorf("submission requires accepted mail and mailbox identities and time")
	}
	stored := submissionModel(*submission)
	return self.tx.Create(&stored).Error
}

func (self *transaction) ListSubmissionsToReconcile(limit int) ([]*models.Submission, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var stored []submissionModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("reconciled_at IS NULL AND COALESCE(reconcile_after, accepted_at) <= ?", time.Now()).Order("COALESCE(reconcile_after, accepted_at), owner_id, submission_id").Limit(limit).Find(&stored).Error; err != nil {
		return nil, err
	}
	submissions := make([]*models.Submission, len(stored))
	for index := range stored {
		submissions[index] = (*models.Submission)(&stored[index])
	}
	return submissions, nil
}

func (self *transaction) MarkSubmissionReconciled(ownerId, submissionId string, reconciledAt time.Time) error {
	if err := validateSubmissionIdentity(ownerId, submissionId); err != nil {
		return err
	}
	if reconciledAt.IsZero() {
		return fmt.Errorf("submission reconciliation time is required")
	}
	return self.tx.Model(&submissionModel{}).Where("owner_id = ? AND submission_id = ? AND reconciled_at IS NULL", ownerId, submissionId).
		Update("reconciled_at", reconciledAt).Error
}

// DeferSubmissionReconciliation leaves acceptance intact while retrying mailbox work later.
func (self *transaction) DeferSubmissionReconciliation(ownerId, submissionId string, retryAt time.Time) error {
	if err := validateSubmissionIdentity(ownerId, submissionId); err != nil {
		return err
	}
	if retryAt.IsZero() {
		return fmt.Errorf("submission retry time is required")
	}
	return self.tx.Model(&submissionModel{}).Where("owner_id = ? AND submission_id = ? AND reconciled_at IS NULL", ownerId, submissionId).Update("reconcile_after", retryAt).Error
}
