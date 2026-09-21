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

// CalendarRequestOperation stores receipts on the same transaction as the
// calendar command and accepted notification mail. It does not authorize callers.
type CalendarRequestOperation interface {
	GetCalendarRequest(userId, requestId string) (*models.CalendarRequestReceipt, error)
	// LockCalendarRequest serializes even absent identities until commit.
	LockCalendarRequest(userId, requestId string) (*models.CalendarRequestReceipt, error)
	CreateCalendarRequest(receipt *models.CalendarRequestReceipt) error
}

type calendarRequestModel models.CalendarRequestReceipt

func (self *calendarRequestModel) TableName() string { return "calendar_request" }

func validateCalendarRequestIdentity(userId, requestId string) error {
	if userId == "" || len(userId) > 32 || strings.ContainsRune(userId, 0) || requestId == "" || len(requestId) > 128 || strings.ContainsRune(requestId, 0) {
		return fmt.Errorf("%w: invalid calendar request identity", ErrInvalidArguments)
	}
	return nil
}

func (self *transaction) GetCalendarRequest(userId, requestId string) (*models.CalendarRequestReceipt, error) {
	if err := validateCalendarRequestIdentity(userId, requestId); err != nil {
		return nil, err
	}
	return readCalendarRequest(self.tx, userId, requestId)
}

func (self *transaction) LockCalendarRequest(userId, requestId string) (*models.CalendarRequestReceipt, error) {
	if err := validateCalendarRequestIdentity(userId, requestId); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte("calendar_request\x00" + userId + "\x00" + requestId))
	// The composite primary key, not the hash, determines request identity.
	lockKey := int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
	if err := self.tx.Exec("SELECT pg_advisory_xact_lock(?)", lockKey).Error; err != nil {
		return nil, err
	}
	return readCalendarRequest(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}), userId, requestId)
}

func readCalendarRequest(query *gorm.DB, userId, requestId string) (*models.CalendarRequestReceipt, error) {
	var receipt calendarRequestModel
	err := query.Where("user_id = ? AND request_id = ?", userId, requestId).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return (*models.CalendarRequestReceipt)(&receipt), nil
}

func (self *transaction) CreateCalendarRequest(receipt *models.CalendarRequestReceipt) error {
	if receipt == nil {
		return ErrInvalidArguments
	}
	if err := validateCalendarRequestIdentity(receipt.UserID, receipt.RequestID); err != nil {
		return err
	}
	switch receipt.Operation {
	case "save", "delete", "answer":
	default:
		return fmt.Errorf("%w: invalid calendar request operation", ErrInvalidArguments)
	}
	digest, err := hex.DecodeString(receipt.RequestDigest)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != receipt.RequestDigest || receipt.CalendarID == "" || len(receipt.CalendarID) > 32 || strings.ContainsRune(receipt.CalendarID, 0) || receipt.ObjectID == "" || len(receipt.ObjectID) > 255 || strings.ContainsRune(receipt.ObjectID, 0) || receipt.CompletedAt.IsZero() {
		return fmt.Errorf("%w: invalid calendar request receipt", ErrInvalidArguments)
	}
	stored := calendarRequestModel(*receipt)
	return self.tx.Create(&stored).Error
}
