package dav

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"

	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// maximumContactName is how long the last segment of a contact's URL may
// be, which is the width of the column it becomes.
const maximumContactName = 255

// backend is the address book as CardDAV sees it. Every method takes only a
// context, which is why whose request this is travels in one.
type backend struct {
	component *component
	signedIn  *session
}

var _ carddav.Backend = (*backend)(nil)

func (self *backend) who(ctx context.Context) *session {
	if signedIn := signedInFrom(ctx); signedIn != nil {
		return signedIn
	}
	return self.signedIn
}

// CurrentUserPrincipal is the person, as a URL.
func (self *backend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return principalPath(self.who(ctx).userID), nil
}

// AddressBookHomeSetPath is where their address books live.
func (self *backend) AddressBookHomeSetPath(ctx context.Context) (string, error) {
	return homeSetPath(self.who(ctx).userID), nil
}

// ListAddressBooks is every book this person keeps. An account that has never
// had one is given one here, because a phone asked to synchronize an empty
// home set has nothing to point at and some clients then stop asking.
func (self *backend) ListAddressBooks(ctx context.Context) ([]carddav.AddressBook, error) {
	signedIn := self.who(ctx)
	var books []*models.AddressBook
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListAddressBooks(signedIn.userID)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			made, err := tx.CreateAddressBook(&models.AddressBook{UserID: signedIn.userID, Name: "Contacts"})
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
	listed := make([]carddav.AddressBook, 0, len(books))
	for _, book := range books {
		listed = append(listed, self.describe(signedIn, book))
	}
	return listed, nil
}

func (self *backend) describe(signedIn *session, book *models.AddressBook) carddav.AddressBook {
	return carddav.AddressBook{
		Path:        bookPath(signedIn.userID, book.ID),
		Name:        book.Name,
		Description: book.Description,
		// So that a client knows not to try to send a ten megabyte
		// photograph, rather than finding out when it is refused.
		MaxResourceSize: contacts.MaximumCard,
		SupportedAddressData: []carddav.AddressDataType{
			{ContentType: "text/vcard", Version: "4.0"},
			{ContentType: "text/vcard", Version: "3.0"},
		},
	}
}

func (self *backend) GetAddressBook(ctx context.Context, address string) (*carddav.AddressBook, error) {
	signedIn := self.who(ctx)
	book, err := self.bookAt(ctx, signedIn, address)
	if err != nil {
		return nil, err
	}
	described := self.describe(signedIn, book)
	return &described, nil
}

// CreateAddressBook and DeleteAddressBook are refused. A person has one
// address book here, made for them; a client that offers to add or remove one
// is offering something this server does not do, and saying so plainly is
// better than half-doing it.
func (self *backend) CreateAddressBook(ctx context.Context, book *carddav.AddressBook) error {
	return webdav.NewHTTPError(http.StatusForbidden,
		fmt.Errorf("this server keeps one address book per person"))
}

func (self *backend) DeleteAddressBook(ctx context.Context, address string) error {
	return webdav.NewHTTPError(http.StatusForbidden,
		fmt.Errorf("this server keeps one address book per person"))
}

// GetAddressObject is one contact.
func (self *backend) GetAddressObject(ctx context.Context, address string, request *carddav.AddressDataRequest) (*carddav.AddressObject, error) {
	signedIn := self.who(ctx)
	book, contactId, err := self.contactAt(ctx, signedIn, address)
	if err != nil {
		return nil, err
	}
	var found *models.Contact
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		found, err = tx.GetContact(contactId)
		return err
	}); err != nil {
		return nil, err
	}
	if found == nil || found.AddressBookID != book.ID {
		return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("no such contact"))
	}
	return self.object(signedIn, found)
}

// ListAddressObjects is every contact in a book, which is what a client asks
// for with PROPFIND Depth: 1. Without sync-collection this is how a device
// works out what changed: it compares the ETags against what it holds, and
// anything it holds that has stopped being listed has been deleted.
func (self *backend) ListAddressObjects(ctx context.Context, address string, request *carddav.AddressDataRequest) ([]carddav.AddressObject, error) {
	signedIn := self.who(ctx)
	book, err := self.bookAt(ctx, signedIn, address)
	if err != nil {
		return nil, err
	}
	var found []*models.Contact
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		found, err = tx.ListContacts(book.ID, "", 0)
		return err
	}); err != nil {
		return nil, err
	}
	objects := make([]carddav.AddressObject, 0, len(found))
	for _, contact := range found {
		object, err := self.object(signedIn, contact)
		if err != nil {
			return nil, err
		}
		objects = append(objects, *object)
	}
	return objects, nil
}

// QueryAddressObjects answers an addressbook-query REPORT, which is a client
// asking for the cards matching a filter.
func (self *backend) QueryAddressObjects(ctx context.Context, address string, query *carddav.AddressBookQuery) ([]carddav.AddressObject, error) {
	objects, err := self.ListAddressObjects(ctx, address, nil)
	if err != nil {
		return nil, err
	}
	return carddav.Filter(query, objects)
}

// PutAddressObject keeps a card a device sent.
func (self *backend) PutAddressObject(ctx context.Context, address string, card vcard.Card, options *carddav.PutAddressObjectOptions) (*carddav.AddressObject, error) {
	signedIn := self.who(ctx)
	book, contactId, err := self.contactAt(ctx, signedIn, address)
	if err != nil {
		return nil, err
	}
	encoded, err := contacts.Encode(card)
	if err != nil {
		return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
	}
	parsed, err := contacts.Parse(encoded)
	if err != nil {
		// A card larger than this server keeps is the one case worth its
		// own status: 507 tells a client the request was understood and
		// there is no room, which is what makes it stop resending.
		if strings.Contains(err.Error(), "larger than") {
			return nil, webdav.NewHTTPError(http.StatusInsufficientStorage, err)
		}
		return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
	}

	kept := &models.Contact{
		ID: contactId, AddressBookID: book.ID, UID: parsed.UID,
		ETag: contacts.ETag(parsed.Card), Card: string(parsed.Card),
		Name: parsed.Name, Organization: parsed.Organization,
		Emails: parsed.Emails, Phones: parsed.Phones,
	}
	var written *models.Contact
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) error {
		existing, err := tx.GetContact(contactId)
		if err != nil {
			return err
		}
		if existing != nil && existing.AddressBookID != book.ID {
			return webdav.NewHTTPError(http.StatusForbidden, fmt.Errorf("that contact is not in this address book"))
		}
		// The conditional headers, which are how two devices editing the
		// same person at once are stopped from silently overwriting one
		// another.
		if options != nil {
			if options.IfMatch.IsSet() {
				// "Only if it is still the version I read." A client that
				// read a card, thought about it, and is now writing back
				// what it decided.
				var matched bool
				if existing != nil {
					matched, _ = options.IfMatch.MatchETag(existing.ETag)
				}
				if !matched {
					return webdav.NewHTTPError(http.StatusPreconditionFailed,
						fmt.Errorf("that contact has changed since you read it"))
				}
			}
			if options.IfNoneMatch.IsWildcard() && existing != nil {
				// "Only if it is not there yet." A client creating
				// somebody, which must not quietly become an overwrite.
				return webdav.NewHTTPError(http.StatusPreconditionFailed,
					fmt.Errorf("there is already a contact there"))
			}
		}
		// A card names the person it is about, and two devices adding
		// somebody at the same time choose different file names but agree
		// on that name. Landing the second on the first is what stops the
		// same person being kept twice.
		if existing == nil {
			twin, err := tx.GetContactByUID(book.ID, kept.UID)
			if err != nil {
				return err
			}
			if twin != nil && twin.ID != contactId {
				kept.ID = twin.ID
				kept.CreatedAt = twin.CreatedAt
			}
		} else {
			kept.CreatedAt = existing.CreatedAt
		}
		written, err = tx.PutContact(kept)
		return err
	}); err != nil {
		return nil, err
	}
	return self.object(signedIn, written)
}

func (self *backend) DeleteAddressObject(ctx context.Context, address string) error {
	signedIn := self.who(ctx)
	book, contactId, err := self.contactAt(ctx, signedIn, address)
	if err != nil {
		return err
	}
	return self.component.database.TransactionContext(ctx, func(tx db.Transaction) error {
		existing, err := tx.GetContact(contactId)
		if err != nil {
			return err
		}
		if existing == nil || existing.AddressBookID != book.ID {
			return webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("no such contact"))
		}
		return tx.DeleteContact(contactId)
	})
}

// object is one stored contact as the protocol describes it.
func (self *backend) object(signedIn *session, contact *models.Contact) (*carddav.AddressObject, error) {
	card, err := vcard.NewDecoder(strings.NewReader(contact.Card)).Decode()
	if err != nil {
		return nil, fmt.Errorf("dav: a stored contact cannot be read back: %w", err)
	}
	return &carddav.AddressObject{
		Path:          contactPath(signedIn.userID, contact.AddressBookID, contact.ID),
		ModTime:       contact.ModifiedAt,
		ContentLength: int64(len(contact.Card)),
		ETag:          contact.ETag,
		Card:          card,
	}, nil
}

// bookAt is the address book a URL names, refused unless it is this person's.
func (self *backend) bookAt(ctx context.Context, signedIn *session, address string) (*models.AddressBook, error) {
	segments := segmentsOf(address)
	if len(segments) < 3 {
		return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("no such address book"))
	}
	var book *models.AddressBook
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		book, err = tx.GetAddressBook(segments[2])
		return err
	}); err != nil {
		return nil, err
	}
	if book == nil || book.UserID != signedIn.userID {
		return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("no such address book"))
	}
	return book, nil
}

// contactAt is the book and the contact identifier a URL names. The file name
// is the client's to choose and clients do not agree on what it should be, so
// whatever it is becomes the identifier, with the .vcf taken off.
func (self *backend) contactAt(ctx context.Context, signedIn *session, address string) (*models.AddressBook, string, error) {
	book, err := self.bookAt(ctx, signedIn, address)
	if err != nil {
		return nil, "", err
	}
	segments := segmentsOf(address)
	if len(segments) < 4 || segments[3] == "" {
		return nil, "", webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("no such contact"))
	}
	name := strings.TrimSuffix(segments[3], cardSuffix)
	// The client chose this, so check it rather than trust it. The length
	// is the column's: iOS names a card after its UID, which is a
	// thirty-six character UUID, and anything much longer than that is a
	// client doing something strange rather than a person with a long name.
	if name == "" || len(name) > maximumContactName || strings.ContainsAny(name, "/\\") ||
		strings.ContainsFunc(name, func(letter rune) bool { return letter < 0x20 || letter == 0x7f }) {
		return nil, "", webdav.NewHTTPError(http.StatusBadRequest, fmt.Errorf("that is not a name this server can keep a contact under"))
	}
	return book, name, nil
}

// segmentsOf is a URL under the mount, split: the account, "contacts", the
// book, and the file.
func segmentsOf(address string) []string {
	rest := strings.Trim(strings.TrimPrefix(path.Clean(address), Prefix), "/")
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
}
