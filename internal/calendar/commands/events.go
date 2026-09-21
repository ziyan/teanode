package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// EventRequest names the owned calendar and optionally an existing event.
type EventRequest struct {
	CalendarID string
	ID         string
}

// EventPreparer merges input onto the event locked by the command.
type EventPreparer func(context.Context, db.Transaction, *models.CalendarObject) (*calendar.Parsed, error)

// EventNotification accepts notification mail on the same transaction. It must
// never dispatch externally or commit independently of the event command.
type EventNotification func(context.Context, db.Transaction, *models.CalendarObject, *models.CalendarObject) error

// SaveEvent keeps an event, occurrence index and notification acceptance atomic.
func (self *Commands) SaveEvent(ctx context.Context, principal *access.Principal, request EventRequest, prepare EventPreparer, notify EventNotification) (*models.CalendarObject, error) {
	if !canUseCalendar(principal) {
		return nil, db.ErrNotFound
	}
	var saved *models.CalendarObject
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		owned, err := transaction.LockCalendar(strings.TrimSpace(request.CalendarID))
		if err != nil {
			return err
		}
		if owned == nil || owned.UserID != principal.User.ID {
			return db.ErrNotFound
		}
		var existing *models.CalendarObject
		if id := strings.TrimSpace(request.ID); id != "" {
			existing, err = transaction.LockCalendarObject(owned.ID, id)
			if err != nil {
				return err
			}
			if existing == nil {
				return db.ErrNotFound
			}
		}
		if prepare == nil {
			return db.ErrInvalidArguments
		}
		parsed, err := prepare(ctx, transaction, existing)
		if err != nil {
			return err
		}
		if parsed == nil {
			return db.ErrInvalidArguments
		}
		object := &models.CalendarObject{CalendarID: owned.ID, UID: parsed.UID, ETag: calendar.ETag(parsed.Data), Data: string(parsed.Data), Summary: parsed.Summary, Location: parsed.Location, StartsAt: parsed.StartsAt, EndsAt: parsed.EndsAt, AllDay: parsed.AllDay, Recurring: parsed.Recurring, Status: parsed.Status}
		twin, err := transaction.GetCalendarObjectByUID(owned.ID, object.UID)
		if err != nil {
			return err
		}
		if twin != nil {
			if existing == nil {
				existing, err = transaction.LockCalendarObject(owned.ID, twin.ID)
				if err != nil {
					return err
				}
				if existing == nil {
					return db.ErrNotFound
				}
			} else if twin.ID != existing.ID {
				return fmt.Errorf("%w: another event in this calendar already has that identifier", db.ErrInvalidArguments)
			}
		}
		if existing != nil {
			object.ID = existing.ID
			object.CreatedAt = existing.CreatedAt
		} else {
			eventCount, err := transaction.CountCalendarObjects(owned.ID)
			if err != nil {
				return err
			}
			if eventCount >= db.ObjectsPerCalendar {
				return fmt.Errorf("%w: this calendar already holds %d events, which is as many as this server keeps", db.ErrInvalidArguments, db.ObjectsPerCalendar)
			}
		}
		occurrences, indexedUntil, err := calendar.Indexed(parsed)
		if err != nil {
			return fmt.Errorf("%w: %s", db.ErrInvalidArguments, err)
		}
		rows := make([]models.Occurrence, 0, len(occurrences))
		for _, occurrence := range occurrences {
			rows = append(rows, models.Occurrence{StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, AllDay: occurrence.AllDay})
		}
		object.IndexedUntil = &indexedUntil
		object.IndexedAt = new(time.Now().UTC())
		saved, err = transaction.PutCalendarObject(object, rows)
		if err != nil {
			return err
		}
		if notify != nil {
			return notify(ctx, transaction, saved, existing)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// DeleteEvent accepts cancellation mail and removes the event in one command.
func (self *Commands) DeleteEvent(ctx context.Context, principal *access.Principal, request EventRequest, notify EventNotification) error {
	if !canUseCalendar(principal) {
		return db.ErrNotFound
	}
	return self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		owned, err := transaction.LockCalendar(strings.TrimSpace(request.CalendarID))
		if err != nil {
			return err
		}
		if owned == nil || owned.UserID != principal.User.ID {
			return db.ErrNotFound
		}
		object, err := transaction.LockCalendarObject(owned.ID, strings.TrimSpace(request.ID))
		if err != nil {
			return err
		}
		if object == nil {
			return db.ErrNotFound
		}
		if notify != nil {
			if err := notify(ctx, transaction, nil, object); err != nil {
				return err
			}
		}
		return transaction.DeleteCalendarObject(owned.ID, object.ID)
	})
}

func canUseCalendar(principal *access.Principal) bool {
	return principal != nil && principal.User != nil && principal.Permissions != nil && principal.Permissions.Has(models.PermissionCalendarUse)
}
