package models

import (
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/dsn"
)

type DeliveryKind string

const (
	DeliveryKindUnknown  DeliveryKind = ""
	DeliveryKindInternal DeliveryKind = "internal"
	DeliveryKindExternal DeliveryKind = "external"
	DeliveryKindForward  DeliveryKind = "forward"
)

func (self DeliveryKind) String() string {
	return string(self)
}

func GetDeliveryKind(value string) DeliveryKind {
	switch value {
	case "internal":
		return DeliveryKindInternal
	case "external":
		return DeliveryKindExternal
	case "forward":
		return DeliveryKindForward
	}
	return DeliveryKindUnknown
}

type DeliveryStatus string

const (
	DeliveryStatusUnknown   DeliveryStatus = ""
	DeliveryStatusQueued    DeliveryStatus = "queued"
	DeliveryStatusDropped   DeliveryStatus = "dropped"
	DeliveryStatusDelivered DeliveryStatus = "delivered"
	DeliveryStatusAttempted DeliveryStatus = "attempted"
	DeliveryStatusFailed    DeliveryStatus = "failed"
	DeliveryStatusDelayed   DeliveryStatus = "delayed"
)

func (self DeliveryStatus) String() string {
	return string(self)
}

func GetDeliveryStatus(value string) DeliveryStatus {
	switch value {
	case "queued":
		return DeliveryStatusQueued
	case "dropped":
		return DeliveryStatusDropped
	case "delivered":
		return DeliveryStatusDelivered
	case "attempted":
		return DeliveryStatusAttempted
	case "failed":
		return DeliveryStatusFailed
	case "delayed":
		return DeliveryStatusDelayed
	}
	return DeliveryStatusUnknown
}

// Delivery is a record of what happened when mail is being delivered to the next server.
type Delivery struct {
	// ID of the Delivery
	ID string `json:"id,omitempty"`

	// Timestamp when the Delivery was created
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the Delivery was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// The Delivery was made for which Mail
	MailID string `json:"mailId,omitempty"`
	Mail   *Mail  `json:"-"`

	// Domain the Mail being delivered arrived for. Not a column of its own —
	// the delivery reaches its domain through the mail — so it is only filled
	// in by the queries that scope to a domain and can therefore state it.
	DomainID string `json:"domainId,omitempty"`

	// Alias that was matched to this delivery. The identifier is stored; the
	// pointer is resolved from the configuration and is nil once the alias has
	// been removed, which historical deliveries have to tolerate.
	AliasID string        `json:"aliasId,omitempty"`
	Alias   *config.Alias `json:"-"`

	// Recipient address, indicating the recipient in the Mail being delivered to
	Recipient string `json:"recipient,omitempty"`

	// Delivery kind, one of: forward, internal, external
	Kind DeliveryKind `json:"kind,omitempty"`

	// Delivery status, one of: queued, dropped, delivered, failed, delayed, relayed, expanded
	Status DeliveryStatus `json:"status,omitempty"`

	// Size of the Delivery
	Size uint64 `json:"size,omitempty"`

	// Timestamp when the Delivery was attempted
	AttemptedAt *time.Time `json:"attemptedAt,omitempty"`

	// Timestamp when the Delivery was delivered successfully
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`

	// Timestamp when the Delivery was dropped
	DroppedAt *time.Time `json:"droppedAt,omitempty"`

	// Timestamp when the Delivery was notified by delivery status
	NotifiedAt *time.Time `json:"notifiedAt,omitempty"`

	// Timestamp when the Delivery will be retried
	RetryAt *time.Time `json:"retryAt,omitempty"`

	// How many attempts has been made
	Attempts uint64 `json:"attempts,omitempty"`

	// Last error reported
	Error string `json:"error,omitempty"`

	// Last known delivery statuses
	DeliveryStatuses []*dsn.DeliveryStatus `json:"deliveryStatuses,omitempty"`
}
