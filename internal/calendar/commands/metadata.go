// Package commands applies authorized calendar changes independently of transport.
package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// TransactionScope lets commands join an existing caller transaction.
type TransactionScope interface {
	TransactionContext(context.Context, func(db.Transaction) error) error
}

// Commands owns authorization and atomic calendar changes.
type Commands struct{ transactions TransactionScope }

// New accepts a database or an existing transaction.
func New(transactions TransactionScope) *Commands { return &Commands{transactions: transactions} }

// UpdateRequest changes presentation fields without changing the agent grant.
// Empty name and week start retain their old values; other empty fields clear.
type UpdateRequest struct {
	ID          string
	Name        string
	Description string
	Colour      string
	Timezone    string
	WeekStart   string
}

// UpdateOutcome carries metadata and the event count read within the command.
type UpdateOutcome struct {
	Calendar   *models.Calendar
	EventCount int64
}

// Update authorizes, validates and locks metadata before preserving unedited fields.
func (self *Commands) Update(ctx context.Context, principal *access.Principal, request UpdateRequest) (*UpdateOutcome, error) {
	if principal == nil || principal.User == nil || principal.Permissions == nil || !principal.Permissions.Has(models.PermissionCalendarUse) {
		return nil, db.ErrNotFound
	}
	var outcome *UpdateOutcome
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		calendar, err := transaction.LockCalendar(strings.TrimSpace(request.ID))
		if err != nil {
			return err
		}
		if calendar == nil || calendar.UserID != principal.User.ID {
			return db.ErrNotFound
		}
		if zone := strings.TrimSpace(request.Timezone); zone != "" {
			if _, err := time.LoadLocation(zone); err != nil {
				return fmt.Errorf("%w: this server does not know the time zone %q", db.ErrInvalidArguments, zone)
			}
		}
		if given := strings.TrimSpace(request.WeekStart); given != "" && models.KnownWeekStart(given) == "" {
			return fmt.Errorf("%w: a week starts on sunday or on monday", db.ErrInvalidArguments)
		}
		calendar.Name = request.Name
		calendar.Description = request.Description
		calendar.Colour = request.Colour
		calendar.Timezone = request.Timezone
		calendar.WeekStart = request.WeekStart
		kept, err := transaction.UpdateCalendar(calendar)
		if err != nil {
			return err
		}
		if kept == nil {
			return db.ErrNotFound
		}
		eventCount, err := transaction.CountCalendarObjects(calendar.ID)
		if err != nil {
			return err
		}
		outcome = &UpdateOutcome{Calendar: kept, EventCount: eventCount}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return outcome, nil
}
