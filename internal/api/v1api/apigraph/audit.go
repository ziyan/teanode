package apigraph

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type AuditQuery interface {
	// List the audit log, newest first: every administrative change, who
	// made it, and the row before and after. Needs audit:read.
	ListAuditEvents(ctx context.Context, arguments ListAuditEventsArguments) (*AuditEventPage, error)
}

type ListAuditEventsArguments struct {
	// Only changes to this kind of thing: user, group, role, domain, ...
	ResourceType *string `json:"resourceType"`

	// Only changes to this row
	ResourceID *string `json:"resourceId"`

	// Only changes made by this user
	ActorUserID *string `json:"actorUserId"`

	// Only changes at or after this time
	Since *time.Time `json:"since"`

	// Only changes before this time
	Until *time.Time `json:"until"`

	// How many to return, at most 200; and how many to skip
	First  *int `json:"first"`
	Offset *int `json:"offset"`
}

// AuditEventPage is one page of the log, with how many there are in all.
type AuditEventPage struct {
	Events []*models.AuditEvent `json:"events"`
	Total  int64                `json:"total"`
}

func (self *graph) ListAuditEvents(ctx context.Context, arguments ListAuditEventsArguments) (*AuditEventPage, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAuditRead); err != nil {
		return nil, err
	}
	options := &db.AuditOptions{Limit: 50}
	if arguments.ResourceType != nil {
		options.ResourceType = *arguments.ResourceType
	}
	if arguments.ResourceID != nil {
		options.ResourceID = *arguments.ResourceID
	}
	if arguments.ActorUserID != nil {
		options.ActorUserID = *arguments.ActorUserID
	}
	options.Since, options.Until = arguments.Since, arguments.Until
	if arguments.First != nil && *arguments.First > 0 {
		options.Limit = min(*arguments.First, 200)
	}
	if arguments.Offset != nil && *arguments.Offset > 0 {
		options.Offset = *arguments.Offset
	}
	tx := self.transaction(ctx)
	events, err := tx.ListAuditEvents(options)
	if err != nil {
		return nil, err
	}
	total, err := tx.CountAuditEvents(options)
	if err != nil {
		return nil, err
	}
	// The actor's current name, read once per distinct actor: a renamed
	// user reads by the name they have now.
	labels := map[string]string{}
	for _, event := range events {
		switch event.ActorKind {
		case models.AuditActorUser:
			if event.ActorUserID == "" {
				event.ActorLabel = "console"
				continue
			}
			label, ok := labels[event.ActorUserID]
			if !ok {
				label = "a deleted user"
				if user, err := tx.GetUser(event.ActorUserID); err == nil && user != nil {
					label = user.Username
				}
				labels[event.ActorUserID] = label
			}
			event.ActorLabel = label
		default:
			event.ActorLabel = string(event.ActorKind)
		}
	}
	return &AuditEventPage{Events: events, Total: total}, nil
}
