package apigraph

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// CalendarRequestArguments identifies completion without resending event content.
type CalendarRequestArguments struct {
	RequestID string `json:"requestId"`
}

// CalendarRequestView survives deletion of the event and calendar.
type CalendarRequestView struct {
	RequestID   string    `json:"requestId"`
	CalendarID  string    `json:"calendarId"`
	ObjectID    string    `json:"objectId"`
	Operation   string    `json:"operation"`
	CompletedAt time.Time `json:"completedAt"`
	IsMissing   bool      `json:"isMissing"`
}

// GetCalendarRequest returns only this account's receipt and current existence.
// An absent receipt can mean the original request has not committed yet.
func (self *graph) GetCalendarRequest(ctx context.Context, arguments CalendarRequestArguments) (*CalendarRequestView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	receipt, err := self.transaction(ctx).GetCalendarRequest(principal.User.ID, arguments.RequestID)
	if err != nil {
		return nil, translateError(err)
	}
	if receipt == nil {
		return nil, nil
	}
	if receipt.Operation == "answer" {
		if _, err := self.requirePermission(ctx, models.PermissionMailRead); err != nil {
			return nil, err
		}
	}
	if receipt.IsMailSendRequired {
		if _, err := self.requirePermission(ctx, models.PermissionMailSend); err != nil {
			return nil, err
		}
	}
	object, err := self.transaction(ctx).GetCalendarObject(receipt.CalendarID, receipt.ObjectID)
	if err != nil {
		return nil, translateError(err)
	}
	return &CalendarRequestView{RequestID: receipt.RequestID, CalendarID: receipt.CalendarID, ObjectID: receipt.ObjectID, Operation: receipt.Operation, CompletedAt: receipt.CompletedAt, IsMissing: object == nil}, nil
}
