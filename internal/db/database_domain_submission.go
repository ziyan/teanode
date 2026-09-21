package db

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

type domainSubmissionModel models.DomainSubmission

func (self *domainSubmissionModel) TableName() string { return "domain_submission" }

func validateDomainSubmissionIdentity(principalId, submissionId string) error {
	if principalId == "" || len(principalId) > 64 || strings.ContainsRune(principalId, 0) {
		return fmt.Errorf("%w: invalid submission principal", ErrInvalidArguments)
	}
	return validateSubmissionIdentity("domain", submissionId)
}

func (self *transaction) LockDomainSubmission(principalId, submissionId string) (*models.DomainSubmission, error) {
	if err := validateDomainSubmissionIdentity(principalId, submissionId); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte("domain_submission\x00" + principalId + "\x00" + submissionId))
	// Hash collisions serialize requests; the composite key determines identity.
	lockKey := int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
	if err := self.tx.Exec("SELECT pg_advisory_xact_lock(?)", lockKey).Error; err != nil {
		return nil, err
	}
	var stored domainSubmissionModel
	err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("principal_id = ? AND submission_id = ?", principalId, submissionId).Take(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return (*models.DomainSubmission)(&stored), nil
}

func (self *transaction) CreateDomainSubmission(submission *models.DomainSubmission) error {
	if submission == nil {
		return ErrInvalidArguments
	}
	if err := validateDomainSubmissionIdentity(submission.PrincipalID, submission.SubmissionID); err != nil {
		return err
	}
	digest, err := hex.DecodeString(submission.RequestDigest)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != submission.RequestDigest || submission.DomainID == "" || submission.MailID == "" || submission.AcceptedAt.IsZero() {
		return fmt.Errorf("%w: invalid domain submission", ErrInvalidArguments)
	}
	stored := domainSubmissionModel(*submission)
	return self.tx.Create(&stored).Error
}
