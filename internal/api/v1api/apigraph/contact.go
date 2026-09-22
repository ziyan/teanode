package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/addressbook"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The address book: a person's own contacts, which they edit here and their
// phone keeps in step with over CardDAV.
//
// It is the server's only list of people: the composer completes from it, and
// the "sender is known" rule condition asks it.

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

	// AgentGranted says the person has given their agent this book. The
	// agent's own tools read it: what is not granted is not theirs to see.
	AgentGranted bool `json:"agentGranted"`
}

// ContactView is one contact. Card is the whole of it; the fields beside it
// are what the card says, pulled out so that a list does not have to parse
// one. A list leaves Card empty, because a page of whole cards is a great
// deal of text nobody reads.
type ContactView struct {
	ID           string         `json:"id"`
	UID          string         `json:"uid"`
	ETag         string         `json:"etag"`
	Name         string         `json:"name,omitempty"`
	Organization string         `json:"organization,omitempty"`
	Emails       []string       `json:"emails"`
	Phones       []string       `json:"phones"`
	Addresses    []*AddressView `json:"addresses"`
	Note         string         `json:"note,omitempty"`
	Card         string         `json:"card,omitempty"`

	// HasPhoto says whether the card carries a picture. The picture itself
	// is served from its own address, because a card holding a photograph
	// is several hundred kilobytes and a listing of them would be a page
	// that cost megabytes to draw a column of faces.
	HasPhoto bool `json:"hasPhoto"`
}

// AddressView is one postal address, in the components a card keeps it in.
// Written is the same thing on one line, for a list.
type AddressView struct {
	Label      string `json:"label,omitempty"`
	Street     string `json:"street,omitempty"`
	Locality   string `json:"locality,omitempty"`
	Region     string `json:"region,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Country    string `json:"country,omitempty"`
	Written    string `json:"written"`
}

type ListContactsArguments struct {
	AddressBookID string `json:"addressBookId"`

	// Narrows by name, organization, address or number; empty is all of
	// them.
	Query string `json:"query" graphapi:"nullable"`

	// How many, at most 2000.
	First int `json:"first" graphapi:"nullable"`
}

type ContactArguments struct {
	ID string `json:"id"`
}

type SaveContactArguments struct {
	AddressBookID string `json:"addressBookId"`

	// ID names a contact already kept; empty keeps a new one.
	ID string `json:"id" graphapi:"nullable"`

	// Card is a whole vCard, which is what a program that speaks the
	// format sends. When it is given the fields below are ignored.
	Card string `json:"card" graphapi:"nullable"`

	// Pointers, because leaving a field out and emptying it are different
	// instructions: a caller that sends only a name must not thereby
	// delete the note and the numbers, and a form whose box is empty must
	// be able to clear what was there.
	Name         *string    `json:"name" graphapi:"nullable"`
	Organization *string    `json:"organization" graphapi:"nullable"`
	Title        *string    `json:"title" graphapi:"nullable"`
	Emails       *[]string  `json:"emails" graphapi:"nullable"`
	Phones       *[]string  `json:"phones" graphapi:"nullable"`
	Addresses    *[]Address `json:"addresses" graphapi:"nullable"`
	Note         *string    `json:"note" graphapi:"nullable"`
}

// Address is one postal address as a form sends it. Named without a suffix
// because the schema generator appends "Input" to an input type's name:
// calling this AddressInput produced AddressInputInput, and every document
// that named AddressInput was refused with a type error.
type Address struct {
	Street     string `json:"street"`
	Locality   string `json:"locality"`
	Region     string `json:"region"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
}

type SaveAddressBookArguments struct {
	ID          string `json:"id"`
	Name        string `json:"name" graphapi:"nullable"`
	Description string `json:"description" graphapi:"nullable"`
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
			AgentGranted: book.AgentGranted,
		})
	}
	return views, nil
}

func (self *graph) ListContacts(ctx context.Context, arguments ListContactsArguments) ([]*ContactView, error) {
	book, err := self.requireOwnAddressBook(ctx, arguments.AddressBookID)
	if err != nil {
		return nil, err
	}
	// Nothing given means the whole book. The table pages in the browser,
	// so a cut here would not save the reader anything -- it would just
	// hide contacts, with no way to reach them.
	limit := arguments.First
	found, err := self.transaction(ctx).ListContacts(book.ID, arguments.Query, limit)
	if err != nil {
		return nil, err
	}
	views := make([]*ContactView, 0, len(found))
	for _, contact := range found {
		view := contactView(contact, false)
		// A page of notes is a great deal of text nobody reads.
		view.Note = ""
		views = append(views, view)
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
	// A contact is named within its address book, so the book comes first
	// and is checked to be the caller's before anything in it is read.
	tx := self.transaction(ctx)
	books, err := tx.ListAddressBooks(principal.User.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, book := range books {
		contact, err := tx.GetContact(book.ID, strings.TrimSpace(contactId))
		if err != nil {
			return nil, nil, err
		}
		if contact != nil {
			return contact, book, nil
		}
	}
	return nil, nil, api.ErrNotFound
}

func (self *graph) SaveContact(ctx context.Context, arguments SaveContactArguments) (*ContactView, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, err
	}

	fields := &contacts.Fields{
		Name: arguments.Name, Organization: arguments.Organization, Title: arguments.Title,
		Emails: arguments.Emails, Phones: arguments.Phones, Note: arguments.Note,
	}
	if arguments.Addresses != nil {
		wanted := make([]contacts.Address, 0, len(*arguments.Addresses))
		for _, given := range *arguments.Addresses {
			wanted = append(wanted, contacts.Address{
				Street: given.Street, Locality: given.Locality, Region: given.Region,
				PostalCode: given.PostalCode, Country: given.Country,
			})
		}
		fields.Addresses = &wanted
	}
	kept, err := addressbook.New(self.transaction(ctx)).Save(ctx, principal, addressbook.SaveRequest{AddressBookID: arguments.AddressBookID, ID: arguments.ID, Card: arguments.Card, Fields: *fields})
	if errors.Is(err, db.ErrInvalidArguments) {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	if err != nil {
		return nil, translateError(err)
	}
	return contactView(kept, true), nil
}

func (self *graph) DeleteContact(ctx context.Context, arguments ContactArguments) (bool, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := addressbook.New(self.transaction(ctx)).Delete(ctx, principal, arguments.ID); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

func (self *graph) SaveAddressBook(ctx context.Context, arguments SaveAddressBookArguments) (*AddressBookView, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := addressbook.New(self.transaction(ctx)).UpdateBook(ctx, principal, addressbook.UpdateBookRequest{ID: arguments.ID, Name: arguments.Name, Description: arguments.Description})
	if err != nil {
		return nil, translateError(err)
	}
	kept := outcome.Book
	return &AddressBookView{ID: kept.ID, Name: kept.Name, Description: kept.Description, Contacts: int(outcome.ContactCount), AgentGranted: kept.AgentGranted}, nil
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
	// The note and the postal addresses are read out of the card rather
	// than kept in columns of their own, which would be second copies to
	// keep in step with it. Parsed for a list as well as for one contact:
	// a card is a few hundred bytes of text, and a person who keeps an
	// address and no email address -- which is an ordinary thing to do --
	// would otherwise see a row with nothing in it.
	view.Addresses = []*AddressView{}
	if parsed, err := contacts.Parse([]byte(contact.Card)); err == nil {
		view.Note = parsed.Note
		view.HasPhoto = parsed.HasPhoto
		for index := range parsed.Addresses {
			address := parsed.Addresses[index]
			view.Addresses = append(view.Addresses, &AddressView{
				Label: address.Label, Street: address.Street, Locality: address.Locality,
				Region: address.Region, PostalCode: address.PostalCode,
				Country: address.Country, Written: address.Written(),
			})
		}
	}
	if withCard {
		view.Card = contact.Card
	}
	return view
}
