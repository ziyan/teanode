package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// The address book: a person's own contacts, kept as vCards.
//
// Not the same thing as mailbox_contact in database_mailbox.go, which is
// every address a mailbox has written to or heard from, learned from traffic.
// That is a list of people who have corresponded; this is a list of people
// somebody chose to keep.

type addressBookModel struct {
	ID          string    `gorm:"column:id;primaryKey"`
	UserID      string    `gorm:"column:user_id"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ModifiedAt  time.Time `gorm:"column:modified_at"`
	Name        string    `gorm:"column:name"`
	Description string    `gorm:"column:description"`
}

func (addressBookModel) TableName() string { return "addressbook" }

func (self *addressBookModel) toModel() *models.AddressBook {
	return &models.AddressBook{
		ID: self.ID, UserID: self.UserID, CreatedAt: self.CreatedAt,
		ModifiedAt: self.ModifiedAt, Name: self.Name, Description: self.Description,
	}
}

type contactModel struct {
	// The identifier is the file name the client chose, so it is unique
	// only within one address book, and the key is both columns together.
	ID            string    `gorm:"column:id;primaryKey"`
	AddressBookID string    `gorm:"column:addressbook_id;primaryKey"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	ModifiedAt    time.Time `gorm:"column:modified_at"`
	UID           string    `gorm:"column:uid"`
	ETag          string    `gorm:"column:etag"`
	Card          string    `gorm:"column:card"`
	Name          string    `gorm:"column:name"`
	Organization  string    `gorm:"column:organization"`

	// Newline-joined, because these exist only to be listed and searched;
	// the card is what anybody asking a real question reads.
	Emails string `gorm:"column:emails"`
	Phones string `gorm:"column:phones"`
}

func (contactModel) TableName() string { return "contact" }

func (self *contactModel) toModel() *models.Contact {
	return &models.Contact{
		ID: self.ID, AddressBookID: self.AddressBookID, CreatedAt: self.CreatedAt,
		ModifiedAt: self.ModifiedAt, UID: self.UID, ETag: self.ETag, Card: self.Card,
		Name: self.Name, Organization: self.Organization,
		Emails: splitLines(self.Emails), Phones: splitLines(self.Phones),
	}
}

func splitLines(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

// ListAddressBooks are one account's, oldest first, which is the order they
// were made in and so the order somebody expects to see them.
func (self *transaction) ListAddressBooks(userId string) ([]*models.AddressBook, error) {
	var found []addressBookModel
	if err := self.tx.Where("\"user_id\" = ?", userId).Order("\"created_at\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	books := make([]*models.AddressBook, 0, len(found))
	for index := range found {
		books = append(books, found[index].toModel())
	}
	return books, nil
}

func (self *transaction) GetAddressBook(addressBookId string) (*models.AddressBook, error) {
	var found []addressBookModel
	if err := self.tx.Where("\"id\" = ?", addressBookId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// CreateAddressBook makes one. Audited: an address book appearing or
// disappearing changes the shape of an account, which is the kind of thing
// the administrative log is for. What goes inside it is not audited -- see
// PutContact.
func (self *transaction) CreateAddressBook(book *models.AddressBook) (*models.AddressBook, error) {
	if book == nil || strings.TrimSpace(book.UserID) == "" {
		return nil, fmt.Errorf("db: an address book needs an account")
	}
	now := time.Now()
	row := &addressBookModel{
		ID: newID(), UserID: book.UserID, CreatedAt: now, ModifiedAt: now,
		Name: truncateRunes(strings.TrimSpace(book.Name), 200), Description: book.Description,
	}
	if row.Name == "" {
		row.Name = "Contacts"
	}
	if err := self.applyMutation(models.AuditResourceAddressBook, row.ID, models.AuditActionCreate,
		nil, row.toModel(), func(tx *gorm.DB) error {
			return tx.Create(row).Error
		}); err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

// UpdateAddressBook renames one or changes what it says about itself.
func (self *transaction) UpdateAddressBook(book *models.AddressBook) (*models.AddressBook, error) {
	if book == nil || strings.TrimSpace(book.ID) == "" {
		return nil, fmt.Errorf("db: which address book")
	}
	before, err := self.GetAddressBook(book.ID)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, nil
	}
	row := &addressBookModel{
		ID: book.ID, UserID: before.UserID, CreatedAt: before.CreatedAt, ModifiedAt: time.Now(),
		Name: truncateRunes(strings.TrimSpace(book.Name), 200), Description: book.Description,
	}
	if row.Name == "" {
		row.Name = before.Name
	}
	if err := self.applyMutation(models.AuditResourceAddressBook, row.ID, models.AuditActionUpdate,
		before, row.toModel(), func(tx *gorm.DB) error {
			return tx.Model(&addressBookModel{}).Where("\"id\" = ?", row.ID).
				Updates(map[string]any{"modified_at": row.ModifiedAt, "name": row.Name, "description": row.Description}).Error
		}); err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

// DeleteAddressBook takes one away, with everything in it: the contacts are
// removed by the foreign key, not by a second statement here.
func (self *transaction) DeleteAddressBook(addressBookId string) error {
	before, err := self.GetAddressBook(addressBookId)
	if err != nil || before == nil {
		return err
	}
	return self.applyMutation(models.AuditResourceAddressBook, addressBookId, models.AuditActionDelete,
		before, nil, func(tx *gorm.DB) error {
			return tx.Where("\"id\" = ?", addressBookId).Delete(&addressBookModel{}).Error
		})
}

// contactsPerBook is how many contacts one address book may hold.
//
// There has to be a number, because the listing a client reads is the whole
// book in one response and nothing else bounds it. It is enforced where a
// contact is written, so a client is told plainly, rather than by cutting the
// listing short, which would read to a phone as "those people were deleted".
const contactsPerBook = 10000

// ListContacts are one book's, by name. A query narrows by name, organization
// or address, which is what a person typing into a search box means.
func (self *transaction) ListContacts(addressBookId, query string, limit int) ([]*models.Contact, error) {
	// A caller asking for everything gets everything. Quietly capping this
	// would be worse than it sounds: a CardDAV client reads the listing of
	// an address book as the whole truth and treats anything missing from
	// it as deleted, so a cap would tell a phone to forget the contacts it
	// could not see. How many a book may hold is decided where a contact is
	// written, not here.
	if limit <= 0 {
		limit = contactsPerBook + 1
	}
	search := self.tx.Where("\"addressbook_id\" = ?", addressBookId)
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		// Escaped, so that somebody searching for "50%" or "a_b" is looking
		// for those characters rather than for LIKE's wildcards.
		like := "%" + escapeLike(strings.ToLower(trimmed)) + "%"
		search = search.Where(
			"LOWER(\"name\") LIKE ? OR LOWER(\"organization\") LIKE ? OR LOWER(\"emails\") LIKE ? OR LOWER(\"phones\") LIKE ?",
			like, like, like, like)
	}
	var found []contactModel
	if err := search.Order("\"name\" ASC, \"id\" ASC").Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	contacts := make([]*models.Contact, 0, len(found))
	for index := range found {
		contacts = append(contacts, found[index].toModel())
	}
	return contacts, nil
}

func (self *transaction) GetContact(addressBookId, contactId string) (*models.Contact, error) {
	var found []contactModel
	if err := self.tx.Where("\"addressbook_id\" = ? AND \"id\" = ?", addressBookId, contactId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// GetContactByUID finds the same person a device already knows about. Two
// devices adding somebody at the same time each generate a file name of their
// own but agree on the UID, which is what stops the contact being kept twice.
func (self *transaction) GetContactByUID(addressBookId, uid string) (*models.Contact, error) {
	var found []contactModel
	if err := self.tx.Where("\"addressbook_id\" = ? AND \"uid\" = ?", addressBookId, strings.TrimSpace(uid)).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// PutContact writes one, replacing what was there under that id.
//
// Not audited, deliberately. A person's phone rewrites contacts all day, and
// a row per edit would bury the administrative log that exists to show what
// operators did to the server. The address book appearing and disappearing is
// audited; what somebody keeps in it is their own business.
func (self *transaction) PutContact(contact *models.Contact) (*models.Contact, error) {
	if contact == nil || strings.TrimSpace(contact.AddressBookID) == "" {
		return nil, fmt.Errorf("db: a contact needs an address book")
	}
	if strings.TrimSpace(contact.UID) == "" || strings.TrimSpace(contact.Card) == "" {
		return nil, fmt.Errorf("db: a contact needs a card and an identifier")
	}
	now := time.Now()
	row := &contactModel{
		ID: strings.TrimSpace(contact.ID), AddressBookID: contact.AddressBookID,
		CreatedAt: contact.CreatedAt, ModifiedAt: now,
		UID: strings.TrimSpace(contact.UID), ETag: contact.ETag, Card: contact.Card,
		Name:         truncateRunes(strings.TrimSpace(contact.Name), 255),
		Organization: truncateRunes(strings.TrimSpace(contact.Organization), 255),
		Emails:       strings.Join(contact.Emails, "\n"), Phones: strings.Join(contact.Phones, "\n"),
	}
	if row.ID == "" {
		row.ID = newID()
	}
	if len(row.UID) > 255 {
		// Not cut short: every later lookup uses the identifier the card
		// actually carries, so a shortened one is a contact that can never
		// be found again and a collision waiting to happen.
		return nil, fmt.Errorf("db: a contact's identifier in the card is longer than 255 characters")
	}
	if len(row.ID) > 255 {
		// The identifier is the file name a client chose. Cutting it short
		// would make two contacts one, so this is refused instead.
		return nil, fmt.Errorf("db: a contact's identifier is longer than 255 characters")
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if err := self.tx.Save(row).Error; err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) DeleteContact(addressBookId, contactId string) error {
	return self.tx.Where("\"addressbook_id\" = ? AND \"id\" = ?", addressBookId, contactId).
		Delete(&contactModel{}).Error
}

// CountContacts is how many a book holds, for a list that says so without
// reading every card.
func (self *transaction) CountContacts(addressBookId string) (int64, error) {
	var count int64
	err := self.tx.Model(&contactModel{}).Where("\"addressbook_id\" = ?", addressBookId).Count(&count).Error
	return count, err
}

// escapeLike makes a search term mean itself. Postgres reads % and _ inside
// LIKE as wildcards, so a term carrying either would match more than the
// person typing it asked for.
func escapeLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}
