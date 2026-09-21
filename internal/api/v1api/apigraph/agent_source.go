package apigraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The calendar and the address book as sources.
//
// A mailbox has had a switch since the agent did, and the rule the whole
// program is built on is that nothing from a source the person has not
// granted is ever sent to a model. The other two collections never had one:
// they were reachable on the person's own permission, so an agent granted one
// mailbox could read every appointment in the diary, which is not what
// granting a mailbox means.
//
// One switch per collection rather than one for "calendars", because a person
// may keep a work calendar and a family one and mean different things by
// them. The switch is a column on the collection; the tools filter to what is
// granted, and the agent's prompt says which it has.

// AgentCollection is one calendar or address book as a source.
type AgentCollection struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Kind is "calendar" or "addressBook".
	Kind string `json:"kind"`

	// Granted says the agent may read it.
	Granted bool `json:"granted"`

	// Items is how many things are in it: events in the next month for a
	// calendar, people for an address book. What makes the switch a
	// decision rather than a guess.
	Items int `json:"items"`
}

// The kinds a collection may be, as the API says them.
const (
	AgentCollectionCalendar    = "calendar"
	AgentCollectionAddressBook = "addressBook"
)

// GrantAgentSourceArguments name a collection and say whether the agent may
// read it.
type GrantAgentSourceArguments struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Granted bool   `json:"granted"`
}

// GrantAgentSource gives the agent a calendar or an address book, or takes it
// back. Only the person's own, and only their own agent: a collection
// belonging to somebody else is not found rather than refused, because whose
// it is is not the caller's business.
func (self *graph) GrantAgentSource(ctx context.Context, arguments GrantAgentSourceArguments) (*AgentView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	user := principal.User
	if user == nil {
		return nil, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	found, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("turn the agent on before giving it anything to read")
	}
	id := strings.TrimSpace(arguments.ID)
	switch strings.TrimSpace(arguments.Kind) {
	case AgentCollectionCalendar:
		calendar, err := tx.LockCalendar(id)
		if err != nil {
			return nil, err
		}
		if calendar == nil || calendar.UserID != user.ID {
			return nil, api.ErrNotFound
		}
		calendar.AgentGranted = arguments.Granted
		if _, err := tx.UpdateCalendar(calendar); err != nil {
			return nil, translateError(err)
		}
		log.Noticef("%s %s their agent the calendar %q", operatorName(ctx), granting(arguments.Granted), calendar.Name)
	case AgentCollectionAddressBook:
		book, err := tx.LockAddressBook(id)
		if err != nil {
			return nil, err
		}
		if book == nil || book.UserID != user.ID {
			return nil, api.ErrNotFound
		}
		book.AgentGranted = arguments.Granted
		if _, err := tx.UpdateAddressBook(book); err != nil {
			return nil, translateError(err)
		}
		log.Noticef("%s %s their agent the address book %q", operatorName(ctx), granting(arguments.Granted), book.Name)
	default:
		return nil, fmt.Errorf("a source is a calendar or an addressBook")
	}
	return self.agentView(ctx, tx, user)
}

func granting(granted bool) string {
	if granted {
		return "granted"
	}
	return "took back from"
}

// collectionsOf is every calendar and address book the person has, with
// whether the agent may read each and how much is in it.
func (self *graph) collectionsOf(tx db.Transaction, userId string) ([]*AgentCollection, error) {
	collections := []*AgentCollection{}
	calendars, err := tx.ListCalendars(userId)
	if err != nil {
		return nil, err
	}
	for _, calendar := range calendars {
		events, err := tx.CountCalendarObjects(calendar.ID)
		if err != nil {
			return nil, err
		}
		collections = append(collections, &AgentCollection{
			ID: calendar.ID, Name: calendar.Name, Kind: AgentCollectionCalendar,
			Granted: calendar.AgentGranted, Items: int(events),
		})
	}
	books, err := tx.ListAddressBooks(userId)
	if err != nil {
		return nil, err
	}
	for _, book := range books {
		contacts, err := tx.CountContacts(book.ID)
		if err != nil {
			return nil, err
		}
		collections = append(collections, &AgentCollection{
			ID: book.ID, Name: book.Name, Kind: AgentCollectionAddressBook,
			Granted: book.AgentGranted, Items: int(contacts),
		})
	}
	return collections, nil
}
