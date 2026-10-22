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
// the "sender is known" rule condition asks it. A person's own books are never
// shared. A circle has an address book of its own, which every member of the
// circle reads and edits and an admin renames or deletes; the database's
// AddressBookAccess is the one answer to who may use a book, and what is owner
// only (the account link, the agent's grant, the known-sender check, the ties
// from recordings and photos) never reads a circle's.

// AddressBookQuery reads a person's own address book.
type AddressBookQuery interface {
	// The caller's own address books. An account that has never had one is
	// given one here rather than being asked to make it, so that nothing
	// has to be set up before a contact can be kept. A circle's book is not
	// among them: everything that means "my address book" (the composer, the
	// ties from photos and recordings, the agent) reads this, and
	// ListReachableAddressBooks has the circles' as well. Needs contacts:use.
	ListAddressBooks(ctx context.Context) ([]*AddressBookView, error)

	// The caller's own address books, then the books of the circles they are
	// in, each with the circle's name and what the caller may do with it.
	// Needs contacts:use.
	ListReachableAddressBooks(ctx context.Context) ([]*AddressBookView, error)

	// The contacts in one of them, by name; a book of the caller's own or of
	// a circle they are in. A query narrows by name,
	// organization, address or number. Needs contacts:use.
	ListContacts(ctx context.Context, arguments ListContactsArguments) ([]*ContactView, error)

	// One contact, with its card. Needs contacts:use.
	GetContact(ctx context.Context, arguments ContactArguments) (*ContactView, error)

	// The caller's contacts that have no link and whose email addresses
	// include a mailbox address of somebody the caller shares a circle with,
	// each with that person. Never says which address matched. A suggestion
	// is only a suggestion: nothing is linked until SetContactAccount is
	// called. Needs contacts:use.
	ListContactAccountSuggestions(ctx context.Context) ([]*ContactAccountSuggestionView, error)
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

	// Take the picture off a contact, whoever put it there, and leave the
	// rest of the card as it was. A contact with no picture is answered as
	// it is. Needs contacts:use.
	RemoveContactPicture(ctx context.Context, arguments ContactArguments) (*ContactView, error)

	// Link a contact to a person in the caller's circles, or take the link
	// away (an empty accountUserId). Only the caller sees the link: the
	// person is not told and sees nothing of it. Refused for the caller's own
	// account and for somebody they share no circle with, and when another
	// contact in the same address book is already that person. Needs
	// contacts:use.
	SetContactAccount(ctx context.Context, arguments SetContactAccountArguments) (*ContactView, error)

	// Rename an address book or change what it says about itself: the
	// caller's own, or a circle's when they are an admin of the circle. Needs
	// contacts:use.
	SaveAddressBook(ctx context.Context, arguments SaveAddressBookArguments) (*AddressBookView, error)

	// Make the address book a circle keeps for itself, called what the circle
	// is called when no name is given. Any member of the circle may; a circle
	// has one, and asking again says so. Needs contacts:use and circle:use.
	CreateCircleAddressBook(ctx context.Context, arguments CreateCircleAddressBookArguments) (*AddressBookView, error)

	// Delete a circle's address book with every card in it. Only an admin of
	// the circle may. A person's own book cannot be deleted. Needs
	// contacts:use and circle:use.
	DeleteCircleAddressBook(ctx context.Context, arguments DeleteCircleAddressBookArguments) (bool, error)
}

// AddressBookView is one address book and how much is in it.
type AddressBookView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Contacts    int    `json:"contacts"`

	// AgentGranted says the person has given their agent this book. The
	// agent's own tools read it: what is not granted is not theirs to see.
	// Always false for a circle's book, which is not one person's to give.
	AgentGranted bool `json:"agentGranted"`

	// The circle a circle's book belongs to; null for the caller's own.
	CircleID     *string `json:"circleId"`
	CircleName   string  `json:"circleName"`
	IsCircleBook bool    `json:"isCircleBook"`

	// CanManage says the caller may rename the book and, for a circle's,
	// delete it: always for their own, for an admin of the circle otherwise.
	CanManage bool `json:"canManage"`
}

// ContactView is one contact. Card is the whole of it; the fields beside it
// are what the card says, pulled out so that a list does not have to parse
// one. A list leaves Card empty, because a page of whole cards is a great
// deal of text nobody reads.
type ContactView struct {
	ID string `json:"id"`

	// AddressBookID is the book the contact is in: a contact is named by
	// the two together.
	AddressBookID string `json:"addressBookId"`

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

	// AccountUserID is the account on this server that the caller linked this
	// contact to, and Account that person as ListCirclePeople shows them. Both
	// are null unless the contact is linked and the caller still shares a
	// circle with that account.
	AccountUserID *string       `json:"accountUserId"`
	Account       *CircleMember `json:"account"`
}

// ContactAccountSuggestionView offers a link for the caller to confirm.
type ContactAccountSuggestionView struct {
	// AddressBookID is the book the contact is in, which SetContactAccount
	// takes with the contact's id.
	AddressBookID string        `json:"addressBookId"`
	Contact       *ContactView  `json:"contact"`
	Account       *CircleMember `json:"account"`
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

type SetContactAccountArguments struct {
	AddressBookID string `json:"addressBookId"`
	ContactID     string `json:"contactId"`

	// The account to link, or empty or null to take the link away.
	AccountUserID string `json:"accountUserId" graphapi:"nullable"`
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

type CreateCircleAddressBookArguments struct {
	CircleID string `json:"circleId"`

	// What to call it; the circle's name when left out
	Name string `json:"name" graphapi:"nullable"`
}

type DeleteCircleAddressBookArguments struct {
	AddressBookID string `json:"addressBookId"`
}

type SaveAddressBookArguments struct {
	ID          string `json:"id"`
	Name        string `json:"name" graphapi:"nullable"`
	Description string `json:"description" graphapi:"nullable"`
}

// requireAddressBookPerson is the caller, if they may keep contacts.
func (self *graph) requireAddressBookPerson(ctx context.Context) (*api.Principal, error) {
	if err := self.requireService(models.ServiceContacts); err != nil {
		return nil, err
	}
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

// requireUsableAddressBook is one book, if the caller may use it: their own,
// or the circle's they are in. Anything else is not found, whose it is is not
// the caller's business. The answer is the database's AddressBookAccess.
func (self *graph) requireUsableAddressBook(ctx context.Context, addressBookId string) (*models.AddressBook, models.AddressBookAccess, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, models.AddressBookAccess{}, err
	}
	tx := self.transaction(ctx)
	book, err := tx.GetAddressBook(strings.TrimSpace(addressBookId))
	if err != nil {
		return nil, models.AddressBookAccess{}, err
	}
	if book == nil {
		return nil, models.AddressBookAccess{}, api.ErrNotFound
	}
	bookAccess, err := tx.AddressBookAccess(principal.User.ID, book)
	if err != nil {
		return nil, models.AddressBookAccess{}, err
	}
	if !bookAccess.CanEdit {
		return nil, models.AddressBookAccess{}, api.ErrNotFound
	}
	return book, bookAccess, nil
}

func (self *graph) addressBookView(ctx context.Context, reachable *models.ReachableAddressBook) (*AddressBookView, error) {
	count, err := self.transaction(ctx).CountContacts(reachable.Book.ID)
	if err != nil {
		return nil, err
	}
	view := &AddressBookView{
		ID: reachable.Book.ID, Name: reachable.Book.Name, Description: reachable.Book.Description, Contacts: int(count),
		AgentGranted: reachable.Book.AgentGranted, CanManage: reachable.Access.CanManage,
	}
	if reachable.Book.CircleID != "" {
		circleId := reachable.Book.CircleID
		view.CircleID, view.CircleName, view.IsCircleBook = &circleId, reachable.CircleName, true
	}
	return view, nil
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
		view, err := self.addressBookView(ctx, &models.ReachableAddressBook{
			Book: book, Access: models.AddressBookAccess{CanEdit: true, CanManage: true, IsOwner: true},
		})
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (self *graph) ListReachableAddressBooks(ctx context.Context) ([]*AddressBookView, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, err
	}
	var books []*models.ReachableAddressBook
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListAddressBooks(principal.User.ID)
		if err != nil {
			return err
		}
		// Their own book is made here as ListAddressBooks makes it, so the
		// list a screen draws never lacks the one every phone is pointed at.
		if len(found) == 0 {
			if _, err := tx.CreateAddressBook(&models.AddressBook{UserID: principal.User.ID, Name: "Contacts"}); err != nil {
				return err
			}
		}
		books, err = tx.ListReachableAddressBooks(principal.User.ID)
		return err
	}); err != nil {
		return nil, err
	}
	views := make([]*AddressBookView, 0, len(books))
	for _, reachable := range books {
		view, err := self.addressBookView(ctx, reachable)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (self *graph) ListContacts(ctx context.Context, arguments ListContactsArguments) ([]*ContactView, error) {
	book, _, err := self.requireUsableAddressBook(ctx, arguments.AddressBookID)
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
	if err := self.attachContactAccounts(ctx, book.UserID, found, views); err != nil {
		return nil, err
	}
	return views, nil
}

func (self *graph) GetContact(ctx context.Context, arguments ContactArguments) (*ContactView, error) {
	contact, book, err := self.usableContact(ctx, arguments.ID)
	if err != nil {
		return nil, err
	}
	return self.contactViewWithAccount(ctx, book.UserID, contact, true)
}

// attachContactAccounts fills in the linked person of each view, reading the
// caller's circle people once and only when some contact is linked.
func (self *graph) attachContactAccounts(ctx context.Context, ownerUserId string, contactList []*models.Contact, views []*ContactView) error {
	var people map[string]*CircleMember
	for index, contact := range contactList {
		if contact.AccountUserID == "" {
			continue
		}
		if people == nil {
			var err error
			if people, err = self.circlePeopleOf(ctx, ownerUserId); err != nil {
				return err
			}
		}
		// A contact is linked only to somebody the owner shares a circle
		// with, and the database reads the link only while that holds, so a
		// person missing here is one who left in this very moment.
		if account := people[contact.AccountUserID]; account != nil {
			accountUserId := contact.AccountUserID
			views[index].AccountUserID = &accountUserId
			views[index].Account = account
		}
	}
	return nil
}

// circlePeopleOf is everybody the person shares a circle with, by account id,
// as the same type ListCirclePeople answers.
func (self *graph) circlePeopleOf(ctx context.Context, userId string) (map[string]*CircleMember, error) {
	people, err := self.transaction(ctx).ListCirclePeople(userId)
	if err != nil {
		return nil, err
	}
	byUserId := make(map[string]*CircleMember, len(people))
	for _, person := range people {
		byUserId[person.UserID] = describeCircleMember(person, userId, nil, nil)
	}
	return byUserId, nil
}

func (self *graph) contactViewWithAccount(ctx context.Context, ownerUserId string, contact *models.Contact, withCard bool) (*ContactView, error) {
	view := contactView(contact, withCard)
	if err := self.attachContactAccounts(ctx, ownerUserId, []*models.Contact{contact}, []*ContactView{view}); err != nil {
		return nil, err
	}
	return view, nil
}

func (self *graph) ListContactAccountSuggestions(ctx context.Context) ([]*ContactAccountSuggestionView, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, err
	}
	found, err := self.transaction(ctx).ListContactAccountSuggestions(principal.User.ID)
	if err != nil {
		return nil, err
	}
	people, err := self.circlePeopleOf(ctx, principal.User.ID)
	if err != nil {
		return nil, err
	}
	suggestions := make([]*ContactAccountSuggestionView, 0, len(found))
	for _, suggestion := range found {
		account := people[suggestion.AccountUserID]
		if account == nil {
			continue
		}
		// The card is left out, as in a list. The contact is not linked yet,
		// so it carries no account of its own.
		view := contactView(suggestion.Contact, false)
		view.Note = ""
		suggestions = append(suggestions, &ContactAccountSuggestionView{AddressBookID: suggestion.Contact.AddressBookID, Contact: view, Account: account})
	}
	return suggestions, nil
}

func (self *graph) SetContactAccount(ctx context.Context, arguments SetContactAccountArguments) (*ContactView, error) {
	book, err := self.requireOwnAddressBook(ctx, arguments.AddressBookID)
	if err != nil {
		return nil, err
	}
	contactId := strings.TrimSpace(arguments.ContactID)
	if err := self.transaction(ctx).SetContactAccount(book.ID, contactId, strings.TrimSpace(arguments.AccountUserID)); err != nil {
		switch {
		case errors.Is(err, db.ErrInvalidArguments):
			return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		case errors.Is(err, db.ErrAlreadyExists):
			return nil, fmt.Errorf("%w: %s", api.ErrAlreadyExists, err)
		}
		return nil, translateError(err)
	}
	contact, err := self.transaction(ctx).GetContact(book.ID, contactId)
	if err != nil {
		return nil, err
	}
	if contact == nil {
		return nil, api.ErrNotFound
	}
	return self.contactViewWithAccount(ctx, book.UserID, contact, true)
}

// usableContact is one contact, if it is in a book the caller may use: their
// own first, then their circles'.
func (self *graph) usableContact(ctx context.Context, contactId string) (*models.Contact, *models.AddressBook, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx := self.transaction(ctx)
	reachable, err := tx.ListReachableAddressBooks(principal.User.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, each := range reachable {
		contact, err := tx.GetContact(each.Book.ID, strings.TrimSpace(contactId))
		if err != nil {
			return nil, nil, err
		}
		if contact != nil {
			return contact, each.Book, nil
		}
	}
	return nil, nil, api.ErrNotFound
}

// ownContact is one contact, if it is in a book belonging to the caller. What
// is owner only (naming a face group with a contact) asks for this and never
// reaches a circle's book.
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
	return self.contactViewWithAccount(ctx, principal.User.ID, kept, true)
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

func (self *graph) RemoveContactPicture(ctx context.Context, arguments ContactArguments) (*ContactView, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, err
	}
	kept, err := addressbook.New(self.transaction(ctx)).RemovePicture(ctx, principal, arguments.ID)
	if err != nil {
		return nil, translateError(err)
	}
	return self.contactViewWithAccount(ctx, principal.User.ID, kept, true)
}

func (self *graph) SaveAddressBook(ctx context.Context, arguments SaveAddressBookArguments) (*AddressBookView, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := addressbook.New(self.transaction(ctx)).UpdateBook(ctx, principal, addressbook.UpdateBookRequest{ID: arguments.ID, Name: arguments.Name, Description: arguments.Description})
	if errors.Is(err, addressbook.ErrRefused) {
		return nil, fmt.Errorf("%w: only an admin of this circle can do that", api.ErrPermissionDenied)
	}
	if err != nil {
		return nil, translateError(err)
	}
	kept := outcome.Book
	view := &AddressBookView{ID: kept.ID, Name: kept.Name, Description: kept.Description, Contacts: int(outcome.ContactCount), AgentGranted: kept.AgentGranted, CanManage: true}
	if kept.CircleID != "" {
		circle, err := self.transaction(ctx).GetCircle(kept.CircleID)
		if err != nil {
			return nil, err
		}
		circleId := kept.CircleID
		view.CircleID, view.IsCircleBook = &circleId, true
		if circle != nil {
			view.CircleName = circle.Name
		}
	}
	return view, nil
}

func (self *graph) CreateCircleAddressBook(ctx context.Context, arguments CreateCircleAddressBookArguments) (*AddressBookView, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return nil, err
	}
	circle, member, err := self.requireCircleMember(ctx, arguments.CircleID)
	if err != nil {
		return nil, err
	}
	made, err := self.transaction(ctx).CreateCircleAddressBook(circle.ID, strings.TrimSpace(arguments.Name))
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s made an address book for the circle %q", principal.User.Username, circle.Name)
	return self.addressBookView(ctx, &models.ReachableAddressBook{
		Book: made, CircleName: circle.Name,
		Access: models.AddressBookAccess{CanEdit: true, CanManage: member.MemberRole == models.MemberRoleAdmin},
	})
}

func (self *graph) DeleteCircleAddressBook(ctx context.Context, arguments DeleteCircleAddressBookArguments) (bool, error) {
	principal, err := self.requireAddressBookPerson(ctx)
	if err != nil {
		return false, err
	}
	book, bookAccess, err := self.requireUsableAddressBook(ctx, arguments.AddressBookID)
	if err != nil {
		return false, err
	}
	// Only a circle's book can be deleted. A person's own is the one every
	// phone is pointed at, and is never made or removed by a request.
	if book.CircleID == "" {
		return false, api.ErrNotFound
	}
	if !bookAccess.CanManage {
		return false, fmt.Errorf("%w: only an admin of this circle can do that", api.ErrPermissionDenied)
	}
	if err := self.transaction(ctx).DeleteAddressBook(book.ID); err != nil {
		return false, translateError(err)
	}
	log.Noticef("%s deleted the address book of a circle", principal.User.Username)
	return true, nil
}

func contactView(contact *models.Contact, withCard bool) *ContactView {
	view := &ContactView{
		ID: contact.ID, AddressBookID: contact.AddressBookID, UID: contact.UID, ETag: contact.ETag,
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
