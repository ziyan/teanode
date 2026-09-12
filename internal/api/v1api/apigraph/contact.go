package apigraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The address book: a person's own contacts, which they edit here and their
// phone keeps in step with over CardDAV.
//
// Not the learned addresses. Those are in mailbox_rules.go as
// ListMailboxContacts, are per mailbox, and are what a mailbox has seen go
// past rather than what somebody chose to keep.

// AddressBookQuery reads a person's own address book.
type AddressBookQuery interface {
	// The caller's address books. An account that has never had one is
	// given one here rather than being asked to make it, so that nothing
	// has to be set up before a contact can be kept. Needs contacts:use.
	ListAddressBooks(ctx context.Context) ([]*AddressBookView, error)

	// The contacts in one of them, by name. A query narrows by name,
	// organization, address or number. Needs contacts:use.
	ListContacts(ctx context.Context, arguments ListContactsArguments) ([]*ContactView, error)

	// One contact, with its card. Needs contacts:use.
	GetContact(ctx context.Context, arguments ContactArguments) (*ContactView, error)
}

// AddressBookMutation changes it.
type AddressBookMutation interface {
	// Keep a contact, or change one that is kept. Give either a whole
	// vCard, which is what a program that speaks the format sends, or the
	// filled-in fields, which is what the dashboard sends; with the fields,
	// anything already on the card that they do not cover is kept, so that
	// correcting a name here does not throw away what a phone put there.
	// Needs contacts:use.
	SaveContact(ctx context.Context, arguments SaveContactArguments) (*ContactView, error)

	// Forget one. Needs contacts:use.
	DeleteContact(ctx context.Context, arguments ContactArguments) (bool, error)

	// Rename an address book or change what it says about itself. Needs
	// contacts:use.
	SaveAddressBook(ctx context.Context, arguments SaveAddressBookArguments) (*AddressBookView, error)
}

// AddressBookView is one address book and how much is in it.
type AddressBookView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Contacts    int    `json:"contacts"`
}

// ContactView is one contact. Card is the whole of it; the fields beside it
// are what the card says, pulled out so that a list does not have to parse
// one. A list leaves Card empty, because a page of whole cards is a great
// deal of text nobody reads.
type ContactView struct {
	ID           string   `json:"id"`
	UID          string   `json:"uid"`
	ETag         string   `json:"etag"`
	Name         string   `json:"name,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Emails       []string `json:"emails"`
	Phones       []string `json:"phones"`
	Card         string   `json:"card,omitempty"`
}

type ListContactsArguments struct {
	AddressBookID string `json:"addressBookId"`
	Query         string `json:"query"`
	First         int    `json:"first"`
}

type ContactArguments struct {
	ID string `json:"id"`
}

type SaveContactArguments struct {
	AddressBookID string `json:"addressBookId"`

	// ID names a contact already kept; empty keeps a new one.
	ID string `json:"id"`

	// Card is a whole vCard. When it is given the fields are ignored.
	Card string `json:"card"`

	Name         string   `json:"name"`
	Organization string   `json:"organization"`
	Title        string   `json:"title"`
	Emails       []string `json:"emails"`
	Phones       []string `json:"phones"`
	Note         string   `json:"note"`
}

type SaveAddressBookArguments struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// requireAddressBookPerson is the caller, if they may keep contacts.
func (self *graph) requireAddressBookPerson(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requirePermission(ctx, models.PermissionContactsUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal, nil
}

// requireOwnAddressBook is one book, if it is the caller's. A book belonging
// to somebody else is not found, rather than refused, because whose it is is
// not the caller's business.
func (self *graph) requireOwnAddressBook(ctx context.Context, addressBookId string) (*models.AddressBook, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, err
	}
	book, err := self.transaction(ctx).GetAddressBook(strings.TrimSpace(addressBookId))
	if err != nil {
		return nil, err
	}
	if book == nil || book.UserID != principal.User.ID {
		return nil, api.ErrNotFound
	}
	return book, nil
}

func (self *graph) ListAddressBooks(ctx context.Context) ([]*AddressBookView, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, err
	}
	var books []*models.AddressBook
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListAddressBooks(principal.User.ID)
		if err != nil {
			return err
		}
		// Nobody should have to make an address book before they can keep
		// somebody in it, and the CardDAV layout needs one to exist before
		// a phone can be pointed at it.
		if len(found) == 0 {
			made, err := tx.CreateAddressBook(&models.AddressBook{UserID: principal.User.ID, Name: "Contacts"})
			if err != nil {
				return err
			}
			found = []*models.AddressBook{made}
		}
		books = found
		return nil
	}); err != nil {
		return nil, err
	}
	views := make([]*AddressBookView, 0, len(books))
	for _, book := range books {
		count, err := self.transaction(ctx).CountContacts(book.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, &AddressBookView{
			ID: book.ID, Name: book.Name, Description: book.Description, Contacts: int(count),
		})
	}
	return views, nil
}

func (self *graph) ListContacts(ctx context.Context, arguments ListContactsArguments) ([]*ContactView, error) {
	book, err := self.requireOwnAddressBook(ctx, arguments.AddressBookID)
	if err != nil {
		return nil, err
	}
	limit := arguments.First
	if limit <= 0 {
		limit = 500
	}
	found, err := self.transaction(ctx).ListContacts(book.ID, arguments.Query, limit)
	if err != nil {
		return nil, err
	}
	views := make([]*ContactView, 0, len(found))
	for _, contact := range found {
		views = append(views, contactView(contact, false))
	}
	return views, nil
}

func (self *graph) GetContact(ctx context.Context, arguments ContactArguments) (*ContactView, error) {
	contact, _, err := self.ownContact(ctx, arguments.ID)
	if err != nil {
		return nil, err
	}
	return contactView(contact, true), nil
}

// ownContact is one contact, if it is in a book belonging to the caller.
func (self *graph) ownContact(ctx context.Context, contactId string) (*models.Contact, *models.AddressBook, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx := self.transaction(ctx)
	contact, err := tx.GetContact(strings.TrimSpace(contactId))
	if err != nil {
		return nil, nil, err
	}
	if contact == nil {
		return nil, nil, api.ErrNotFound
	}
	book, err := tx.GetAddressBook(contact.AddressBookID)
	if err != nil {
		return nil, nil, err
	}
	if book == nil || book.UserID != principal.User.ID {
		return nil, nil, api.ErrNotFound
	}
	return contact, book, nil
}

func (self *graph) SaveContact(ctx context.Context, arguments SaveContactArguments) (*ContactView, error) {
	var existing *models.Contact
	var book *models.AddressBook
	var err error
	if strings.TrimSpace(arguments.ID) != "" {
		if existing, book, err = self.ownContact(ctx, arguments.ID); err != nil {
			return nil, err
		}
	} else if book, err = self.requireOwnAddressBook(ctx, arguments.AddressBookID); err != nil {
		return nil, err
	}

	parsed, err := parseSaved(&arguments, existing)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}

	contact := &models.Contact{
		AddressBookID: book.ID, UID: parsed.UID, ETag: contacts.ETag(parsed.Card),
		Card: string(parsed.Card), Name: parsed.Name, Organization: parsed.Organization,
		Emails: parsed.Emails, Phones: parsed.Phones,
	}
	if existing != nil {
		contact.ID = existing.ID
		contact.CreatedAt = existing.CreatedAt
	}
	var kept *models.Contact
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		// The card's own identifier decides which person this is. Two
		// devices adding somebody at the same time pick different file
		// names but agree on the identifier, and the second must land on
		// the first rather than making a second copy of them.
		if contact.ID == "" {
			twin, err := tx.GetContactByUID(book.ID, contact.UID)
			if err != nil {
				return err
			}
			if twin != nil {
				contact.ID = twin.ID
				contact.CreatedAt = twin.CreatedAt
			}
		}
		kept, err = tx.PutContact(contact)
		return err
	}); err != nil {
		return nil, translateError(err)
	}
	return contactView(kept, true), nil
}

// parseSaved turns what was sent into a card: whole vCard text when a program
// sent one, otherwise the filled-in fields applied to whatever is already
// kept.
func parseSaved(arguments *SaveContactArguments, existing *models.Contact) (*contacts.Parsed, error) {
	if strings.TrimSpace(arguments.Card) != "" {
		return contacts.Parse([]byte(arguments.Card))
	}
	var previous []byte
	if existing != nil {
		previous = []byte(existing.Card)
	}
	return contacts.Build(previous, &contacts.Fields{
		Name: arguments.Name, Organization: arguments.Organization, Title: arguments.Title,
		Emails: arguments.Emails, Phones: arguments.Phones, Note: arguments.Note,
	})
}

func (self *graph) DeleteContact(ctx context.Context, arguments ContactArguments) (bool, error) {
	contact, _, err := self.ownContact(ctx, arguments.ID)
	if err != nil {
		return false, err
	}
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.DeleteContact(contact.ID)
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (self *graph) SaveAddressBook(ctx context.Context, arguments SaveAddressBookArguments) (*AddressBookView, error) {
	book, err := self.requireOwnAddressBook(ctx, arguments.ID)
	if err != nil {
		return nil, err
	}
	var kept *models.AddressBook
	var count int64
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if kept, err = tx.UpdateAddressBook(&models.AddressBook{
			ID: book.ID, Name: arguments.Name, Description: arguments.Description,
		}); err != nil {
			return err
		}
		count, err = tx.CountContacts(book.ID)
		return err
	}); err != nil {
		return nil, err
	}
	if kept == nil {
		return nil, api.ErrNotFound
	}
	return &AddressBookView{ID: kept.ID, Name: kept.Name, Description: kept.Description, Contacts: int(count)}, nil
}

func contactView(contact *models.Contact, withCard bool) *ContactView {
	view := &ContactView{
		ID: contact.ID, UID: contact.UID, ETag: contact.ETag,
		Name: contact.Name, Organization: contact.Organization,
		Emails: contact.Emails, Phones: contact.Phones,
	}
	if view.Emails == nil {
		view.Emails = []string{}
	}
	if view.Phones == nil {
		view.Phones = []string{}
	}
	if withCard {
		view.Card = contact.Card
	}
	return view
}
