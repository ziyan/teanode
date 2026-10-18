// Package addressbook implements authorized contact commands independently of transport.
package addressbook

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// TransactionScope lets a contact command join its caller's transaction.
type TransactionScope interface {
	TransactionContext(context.Context, func(db.Transaction) error) error
}

// Commands applies authorized address-book changes independently of transport.
type Commands struct{ transactions TransactionScope }

// New accepts a database or an existing command transaction.
func New(transactions TransactionScope) *Commands {
	return &Commands{transactions: transactions}
}

// SaveRequest preserves omitted fields when editing an existing contact.
// Card, when nonempty, replaces the whole card and takes precedence over Fields.
type SaveRequest struct {
	AddressBookID string
	ID            string
	Card          string
	Fields        contacts.Fields
}

// Save merges a form onto the latest locked card or keeps a complete vCard.
func (self *Commands) Save(ctx context.Context, principal *access.Principal, request SaveRequest) (*models.Contact, error) {
	if err := principal.RequireService(models.ServiceContacts); err != nil {
		return nil, err
	}
	if !canUseContacts(principal) {
		return nil, db.ErrNotFound
	}
	var saved *models.Contact
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		var existing *models.Contact
		var book *models.AddressBook
		var err error
		if strings.TrimSpace(request.ID) != "" {
			existing, book, err = ownContact(transaction, principal.User.ID, request.ID)
		} else {
			book, err = transaction.GetAddressBook(strings.TrimSpace(request.AddressBookID))
			if err == nil && (book == nil || book.UserID != principal.User.ID) {
				return db.ErrNotFound
			}
		}
		if err != nil {
			return err
		}
		var parsed *contacts.Parsed
		if strings.TrimSpace(request.Card) != "" {
			parsed, err = contacts.Parse([]byte(request.Card))
		} else {
			var previous []byte
			if existing != nil {
				previous = []byte(existing.Card)
			}
			parsed, err = contacts.Build(previous, &request.Fields)
		}
		if err != nil {
			return fmt.Errorf("%w: %s", db.ErrInvalidArguments, err)
		}
		contact := &models.Contact{AddressBookID: book.ID, UID: parsed.UID, ETag: contacts.ETag(parsed.Card), Card: string(parsed.Card), Name: parsed.Name, Organization: parsed.Organization, Emails: parsed.Emails, Phones: parsed.Phones}
		if existing != nil {
			contact.ID = existing.ID
			contact.CreatedAt = existing.CreatedAt
		}
		twin, err := transaction.GetContactByUID(book.ID, contact.UID)
		if err != nil {
			return err
		}
		if twin != nil {
			if contact.ID == "" {
				contact.ID = twin.ID
				contact.CreatedAt = twin.CreatedAt
			} else if twin.ID != contact.ID {
				return fmt.Errorf("%w: another contact in this address book already has that identifier", db.ErrInvalidArguments)
			}
		}
		// An existing UID is an update, even when the caller omitted its file ID.
		if contact.ID == "" {
			contactCount, err := transaction.CountContacts(book.ID)
			if err != nil {
				return err
			}
			if contactCount >= db.ContactsPerBook {
				return fmt.Errorf("%w: this address book already holds %d contacts, which is as many as this server keeps", db.ErrInvalidArguments, db.ContactsPerBook)
			}
		}
		saved, err = transaction.PutContact(contact)
		return err
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// Delete removes only a contact owned by the current principal.
func (self *Commands) Delete(ctx context.Context, principal *access.Principal, contactId string) error {
	if err := principal.RequireService(models.ServiceContacts); err != nil {
		return err
	}
	if !canUseContacts(principal) {
		return db.ErrNotFound
	}
	return self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		contact, book, err := ownContact(transaction, principal.User.ID, contactId)
		if err != nil {
			return err
		}
		return transaction.DeleteContact(book.ID, contact.ID)
	})
}

// SetPicture puts a picture on a contact that has none, and answers whether it
// did. A contact that has a picture keeps it: the one a phone put there, or the
// person chose, is not replaced by one made for them. The contact is locked
// first, so a picture is not put over one that arrives while this runs.
func (self *Commands) SetPicture(ctx context.Context, principal *access.Principal, contactId string, jpegPicture []byte) (*models.Contact, bool, error) {
	if err := principal.RequireService(models.ServiceContacts); err != nil {
		return nil, false, err
	}
	if !canUseContacts(principal) {
		return nil, false, db.ErrNotFound
	}
	var kept *models.Contact
	var isSet bool
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		existing, _, err := ownContact(transaction, principal.User.ID, contactId)
		if err != nil {
			return err
		}
		if contacts.HasPhoto([]byte(existing.Card)) {
			kept = existing
			return nil
		}
		parsed, err := contacts.WithPhoto([]byte(existing.Card), jpegPicture)
		if err != nil {
			return fmt.Errorf("%w: %s", db.ErrInvalidArguments, err)
		}
		if kept, err = transaction.PutContact(withCard(existing, parsed)); err != nil {
			return err
		}
		isSet = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return kept, isSet, nil
}

// RemovePicture takes the picture off a contact. A contact with none is left as
// it is, and that is not an error.
func (self *Commands) RemovePicture(ctx context.Context, principal *access.Principal, contactId string) (*models.Contact, error) {
	if err := principal.RequireService(models.ServiceContacts); err != nil {
		return nil, err
	}
	if !canUseContacts(principal) {
		return nil, db.ErrNotFound
	}
	var kept *models.Contact
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		existing, _, err := ownContact(transaction, principal.User.ID, contactId)
		if err != nil {
			return err
		}
		if !contacts.HasPhoto([]byte(existing.Card)) {
			kept = existing
			return nil
		}
		parsed, err := contacts.WithoutPhoto([]byte(existing.Card))
		if err != nil {
			return fmt.Errorf("%w: %s", db.ErrInvalidArguments, err)
		}
		kept, err = transaction.PutContact(withCard(existing, parsed))
		return err
	})
	if err != nil {
		return nil, err
	}
	return kept, nil
}

// withCard is the contact with the card it has been given, and what is read
// out of the card.
func withCard(existing *models.Contact, parsed *contacts.Parsed) *models.Contact {
	return &models.Contact{
		ID: existing.ID, AddressBookID: existing.AddressBookID, CreatedAt: existing.CreatedAt,
		UID: parsed.UID, ETag: contacts.ETag(parsed.Card), Card: string(parsed.Card),
		Name: parsed.Name, Organization: parsed.Organization, Emails: parsed.Emails, Phones: parsed.Phones,
	}
}

func canUseContacts(principal *access.Principal) bool {
	return principal != nil && principal.User != nil && principal.Permissions != nil && principal.Permissions.Has(models.PermissionContactsUse)
}

func ownContact(transaction db.Transaction, userId, contactId string) (*models.Contact, *models.AddressBook, error) {
	books, err := transaction.ListAddressBooks(userId)
	if err != nil {
		return nil, nil, err
	}
	for _, book := range books {
		contact, err := transaction.LockContact(book.ID, strings.TrimSpace(contactId))
		if err != nil {
			return nil, nil, err
		}
		if contact != nil {
			return contact, book, nil
		}
	}
	return nil, nil, db.ErrNotFound
}

// UpdateBookRequest changes presentation without changing the agent's grant.
// An empty name retains the old name; an empty description clears it.
type UpdateBookRequest struct {
	ID          string
	Name        string
	Description string
}

// BookOutcome carries metadata and the contact count from the command snapshot.
type BookOutcome struct {
	Book         *models.AddressBook
	ContactCount int64
}

// UpdateBook authorizes and locks metadata before preserving unedited fields.
func (self *Commands) UpdateBook(ctx context.Context, principal *access.Principal, request UpdateBookRequest) (*BookOutcome, error) {
	if err := principal.RequireService(models.ServiceContacts); err != nil {
		return nil, err
	}
	if !canUseContacts(principal) {
		return nil, db.ErrNotFound
	}
	var outcome *BookOutcome
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		book, err := transaction.LockAddressBook(strings.TrimSpace(request.ID))
		if err != nil {
			return err
		}
		if book == nil || book.UserID != principal.User.ID {
			return db.ErrNotFound
		}
		book.Name = request.Name
		book.Description = request.Description
		kept, err := transaction.UpdateAddressBook(book)
		if err != nil {
			return err
		}
		if kept == nil {
			return db.ErrNotFound
		}
		contactCount, err := transaction.CountContacts(book.ID)
		if err != nil {
			return err
		}
		outcome = &BookOutcome{Book: kept, ContactCount: contactCount}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return outcome, nil
}
