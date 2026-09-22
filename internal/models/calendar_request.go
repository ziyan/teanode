package models

import "time"

// CalendarRequestReceipt records a completed command without retaining event
// content. It survives event deletion so a late retry cannot recreate the event.
type CalendarRequestReceipt struct {
	UserID              string
	RequestID           string
	Operation           string
	CalendarID          string
	ObjectID            string
	RequestDigest       string
	CompletedAt         time.Time
	IsMailSendRequired  bool
	IsMailWriteRequired bool
}
