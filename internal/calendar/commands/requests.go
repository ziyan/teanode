package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// RequestIdentity binds a caller-retained key to canonical adapter parameters.
// Content must exclude generated timestamps, UIDs and transport metadata.
type RequestIdentity struct {
	RequestID  string
	CalendarID string
	Operation  string
	Content    []byte
}

// RequestOutcome identifies completion even if the event has since been deleted.
// Replay never reapplies older content or changes a later participation response.
type RequestOutcome struct {
	Receipt  *models.CalendarRequestReceipt
	IsReplay bool
}

// RequestAction changes the event and accepts any mail on this transaction,
// returning the affected object's ID. It must not independently commit or send.
type RequestAction func(context.Context, db.Transaction) (string, error)

// ExecuteRequest records one calendar command once for a user's request ID.
// Callers must check any additional permissions required by their operation,
// including mail-send and mailbox-read, before invoking this wrapper on replay.
func (self *Commands) ExecuteRequest(ctx context.Context, principal *access.Principal, request RequestIdentity, requestAction RequestAction) (*RequestOutcome, error) {
	if !canUseCalendar(principal) {
		return nil, db.ErrNotFound
	}
	if request.RequestID == "" || request.CalendarID == "" || len(request.Content) == 0 {
		return nil, db.ErrInvalidArguments
	}
	switch request.Operation {
	case "save", "delete", "answer":
	default:
		return nil, db.ErrInvalidArguments
	}
	encoded, err := json.Marshal([]any{request.Operation, request.CalendarID, request.Content})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	requestDigest := hex.EncodeToString(digest[:])
	var outcome *RequestOutcome
	err = self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		receipt, err := transaction.LockCalendarRequest(principal.User.ID, request.RequestID)
		if err != nil {
			return err
		}
		if receipt != nil {
			if receipt.CalendarID != request.CalendarID || receipt.Operation != request.Operation || receipt.RequestDigest != requestDigest {
				return fmt.Errorf("%w: calendar request identifier already used for different content", db.ErrInvalidArguments)
			}
			outcome = &RequestOutcome{Receipt: receipt, IsReplay: true}
			return nil
		}
		owned, err := transaction.GetCalendar(request.CalendarID)
		if err != nil {
			return err
		}
		if owned == nil || owned.UserID != principal.User.ID {
			return db.ErrNotFound
		}
		if requestAction == nil {
			return db.ErrInvalidArguments
		}
		objectId, err := requestAction(ctx, transaction)
		if err != nil {
			return err
		}
		receipt = &models.CalendarRequestReceipt{UserID: principal.User.ID, RequestID: request.RequestID, Operation: request.Operation, CalendarID: request.CalendarID, ObjectID: objectId, RequestDigest: requestDigest, CompletedAt: time.Now().Truncate(time.Microsecond)}
		if err := transaction.CreateCalendarRequest(receipt); err != nil {
			return err
		}
		outcome = &RequestOutcome{Receipt: receipt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return outcome, nil
}
