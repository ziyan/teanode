package db

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// The address book: a person's own contacts, kept as vCards.
//
// Not the same thing as mailbox_contact in database_mailbox.go, which is
// every address a mailbox has written to or heard from, learned from traffic.
// That is a list of people who have corresponded; this is a list of people
// somebody chose to keep.

type addressBookModel struct {
	ID string `gorm:"column:id;primaryKey"`
	// Exactly one of UserID and CircleID is set: a book is a person's own, or
	// a circle's, which every member of the circle reads and edits.
	UserID      *string   `gorm:"column:user_id"`
	CircleID    *string   `gorm:"column:circle_id"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ModifiedAt  time.Time `gorm:"column:modified_at"`
	Name        string    `gorm:"column:name"`
	Description string    `gorm:"column:description"`
	// AgentGranted is the person's switch for this book as a source.
	AgentGranted bool `gorm:"column:agent_granted"`
}

func (addressBookModel) TableName() string { return "addressbook" }

func (self *addressBookModel) toModel() *models.AddressBook {
	return &models.AddressBook{
		ID: self.ID, UserID: stringOrEmpty(self.UserID), CircleID: stringOrEmpty(self.CircleID),
		CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt, Name: self.Name, Description: self.Description,
		AgentGranted: self.AgentGranted,
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

	// AccountUserID is written only by SetContactAccount: PutContact leaves it
	// out of the columns it updates, so a card sync or an edit keeps it.
	AccountUserID *string `gorm:"column:account_user_id"`
}

func (contactModel) TableName() string { return "contact" }

func (self *contactModel) toModel() *models.Contact {
	return &models.Contact{
		ID: self.ID, AddressBookID: self.AddressBookID, CreatedAt: self.CreatedAt,
		ModifiedAt: self.ModifiedAt, UID: self.UID, ETag: self.ETag, Card: self.Card,
		Name: self.Name, Organization: self.Organization,
		Emails: splitLines(self.Emails), Phones: splitLines(self.Phones),
		AccountUserID: stringOrEmpty(self.AccountUserID),
	}
}

func splitLines(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

// ListAddressBooks are one account's own, oldest first, which is the order
// they were made in and so the order somebody expects to see them. A circle's
// book is not among them: everything that asks for "the person's address
// books" (the known-sender check, completion, the ties from recordings and
// photos, the agent's grants) means the person's own, and only the doors that
// say so use ListReachableAddressBooks.
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

// ListReachableAddressBooks are the books the person can use: their own, oldest
// first, then the books of the circles they are in, oldest first. The join on
// circle_member is the access: a person who leaves a circle stops reaching its
// book in that transaction, with no row to clean up. A circle's books are
// listed only for a person who holds circle:use, as every other circle door
// requires.
func (self *transaction) ListReachableAddressBooks(userId string) ([]*models.ReachableAddressBook, error) {
	reachable := []*models.ReachableAddressBook{}
	if userId == "" {
		return reachable, nil
	}
	owned, err := self.ListAddressBooks(userId)
	if err != nil {
		return nil, err
	}
	for _, book := range owned {
		reachable = append(reachable, &models.ReachableAddressBook{
			Book: book, Access: models.AddressBookAccess{CanEdit: true, CanManage: true, IsOwner: true},
		})
	}
	var rows []struct {
		Book       addressBookModel `gorm:"embedded"`
		CircleName string           `gorm:"column:circle_name"`
		MemberRole string           `gorm:"column:member_role"`
	}
	if err := self.tx.Raw(`SELECT addressbook.*, circle.name AS circle_name, member.member_role AS member_role FROM "addressbook" addressbook
		JOIN "circle_member" member ON member.circle_id = addressbook.circle_id AND member.user_id = ?
		JOIN "circle" circle ON circle.id = addressbook.circle_id
		ORDER BY addressbook.created_at ASC, addressbook.id ASC`, userId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return reachable, nil
	}
	hasCircleUse, err := self.hasCircleUse(userId)
	if err != nil {
		return nil, err
	}
	if !hasCircleUse {
		return reachable, nil
	}
	for index := range rows {
		reachable = append(reachable, &models.ReachableAddressBook{
			Book: rows[index].Book.toModel(), CircleName: rows[index].CircleName,
			Access: models.AddressBookAccess{CanEdit: true, CanManage: rows[index].MemberRole == string(models.MemberRoleAdmin)},
		})
	}
	return reachable, nil
}

// AddressBookAccess is what the person may do with the book. Every door into
// an address book asks it: the dashboard's API, the picture route, CardDAV and
// the agent's tool. A person's own book is theirs alone; a circle's is edited
// by each member and renamed or deleted by an admin, each of them needing
// circle:use as every other circle door does; anybody else gets nothing, which
// the door answers as "not found".
func (self *transaction) AddressBookAccess(userId string, book *models.AddressBook) (models.AddressBookAccess, error) {
	if book == nil || userId == "" {
		return models.AddressBookAccess{}, nil
	}
	if book.UserID != "" {
		if book.UserID == userId {
			return models.AddressBookAccess{CanEdit: true, CanManage: true, IsOwner: true}, nil
		}
		return models.AddressBookAccess{}, nil
	}
	if book.CircleID == "" {
		return models.AddressBookAccess{}, nil
	}
	var members []circleMemberModel
	if err := self.tx.Where("\"circle_id\" = ? AND \"user_id\" = ?", book.CircleID, userId).Limit(1).Find(&members).Error; err != nil {
		return models.AddressBookAccess{}, err
	}
	if len(members) == 0 {
		return models.AddressBookAccess{}, nil
	}
	hasCircleUse, err := self.hasCircleUse(userId)
	if err != nil || !hasCircleUse {
		return models.AddressBookAccess{}, err
	}
	return models.AddressBookAccess{CanEdit: true, CanManage: members[0].MemberRole == string(models.MemberRoleAdmin)}, nil
}

// hasCircleUse is whether the person's roles grant circle:use, which a circle's
// address book needs on top of contacts:use.
func (self *transaction) hasCircleUse(userId string) (bool, error) {
	permissions, err := self.EffectivePermissions(userId)
	if err != nil {
		return false, err
	}
	return permissions.Has(models.PermissionCircleUse), nil
}

// GetCircleAddressBook is the circle's book, or nil.
func (self *transaction) GetCircleAddressBook(circleId string) (*models.AddressBook, error) {
	if circleId == "" {
		return nil, nil
	}
	var found []addressBookModel
	if err := self.tx.Where("\"circle_id\" = ?", circleId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// CreateCircleAddressBook makes the book a circle keeps for itself. The caller
// has checked that the person asking is in the circle.
func (self *transaction) CreateCircleAddressBook(circleId, name string) (*models.AddressBook, error) {
	// The circle membership lock comes before the circle's row, as in every
	// change to circles (see circleMembershipLockKey), and as in
	// CreateCircleCalendar.
	if err := lockCircleMembership(self.tx); err != nil {
		return nil, err
	}
	var locked circleModel
	if err := lockRow(self.tx, &locked, circleId); err != nil {
		return nil, err
	}
	existing, err := self.GetCircleAddressBook(circleId)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrAlreadyExists
	}
	now := time.Now()
	row := &addressBookModel{
		ID: newID(), CircleID: &circleId, CreatedAt: now, ModifiedAt: now,
		Name: truncateRunes(strings.TrimSpace(name), 200),
	}
	if row.Name == "" {
		row.Name = locked.Name
	}
	if err := self.applyMutation(models.AuditResourceAddressBook, row.ID, models.AuditActionCreate,
		nil, row.toModel(), func(tx *gorm.DB) error {
			return tx.Create(row).Error
		}); err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) GetAddressBook(addressBookId string) (*models.AddressBook, error) {
	return readAddressBook(self.tx, addressBookId)
}

// LockAddressBook serializes metadata edits and grant changes until commit.
func (self *transaction) LockAddressBook(addressBookId string) (*models.AddressBook, error) {
	return readAddressBook(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}), addressBookId)
}

func readAddressBook(query *gorm.DB, addressBookId string) (*models.AddressBook, error) {
	var found []addressBookModel
	if err := query.Where("\"id\" = ?", addressBookId).Limit(1).Find(&found).Error; err != nil {
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
		ID: newID(), UserID: nullableText(book.UserID), CreatedAt: now, ModifiedAt: now,
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
		ID: book.ID, UserID: nullableText(before.UserID), CircleID: nullableText(before.CircleID),
		CreatedAt: before.CreatedAt, ModifiedAt: time.Now(),
		Name: truncateRunes(strings.TrimSpace(book.Name), 200), Description: book.Description,
		// A circle's book is never given to anybody's agent: it is not one
		// person's to give.
		AgentGranted: book.AgentGranted && before.CircleID == "",
	}
	if row.Name == "" {
		row.Name = before.Name
	}
	if err := self.applyMutation(models.AuditResourceAddressBook, row.ID, models.AuditActionUpdate,
		before, row.toModel(), func(tx *gorm.DB) error {
			return tx.Model(&addressBookModel{}).Where("\"id\" = ?", row.ID).
				Updates(map[string]any{"modified_at": row.ModifiedAt, "name": row.Name, "description": row.Description,
					"agent_granted": row.AgentGranted}).Error
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
	if err := self.applyMutation(models.AuditResourceAddressBook, addressBookId, models.AuditActionDelete,
		before, nil, func(tx *gorm.DB) error {
			return tx.Where("\"id\" = ?", addressBookId).Delete(&addressBookModel{}).Error
		}); err != nil {
		return err
	}
	return self.untieContactsGone(before.UserID, nil)
}

// ContactsPerBook is how many contacts one address book may hold.
//
// There has to be a number, because the listing a client reads is the whole
// book in one response and nothing else bounds it. It is enforced where a
// contact is written, so a client is told plainly, rather than by cutting the
// listing short, which would read to a phone as "those people were deleted".
// ContactsPerBook is exported because both doors into an address book -- the
// dashboard's API and CardDAV -- have to enforce the same number, and a limit
// enforced on one of two doors is not a limit.
const ContactsPerBook = 10000

// BytesPerBook is how much one address book may hold, counted in the cards
// themselves.
//
// The count above bounds the wrong thing on its own. A card may be a megabyte
// -- a photograph makes one -- so ten thousand of them is ten gigabytes, and
// the listing a client reads is the whole book built in memory and then
// serialised whole, because the protocol library has no streaming. Two
// thousand legal cards were therefore a way for one account to take the
// server down for everybody on it.
//
// Sixty-four megabytes is far past any address book a person keeps: sixty
// thousand ordinary cards, or thirteen hundred carrying a photograph. It is
// checked where a card is written, so a client is told plainly.
const BytesPerBook = 64 << 20

// ErrBookFull is a book that cannot take another card. Named, because the
// answer a client is given for it is a status that makes it stop rather than
// retry for ever.
var ErrBookFull = errors.New("db: that address book is full")

// bookBytes is what a book holds now, not counting the card being replaced.
func (self *transaction) bookBytes(addressBookId, exceptId string) (int64, error) {
	var total int64
	query := self.tx.Model(&contactModel{}).Where("\"addressbook_id\" = ?", addressBookId)
	if strings.TrimSpace(exceptId) != "" {
		query = query.Where("\"id\" <> ?", strings.TrimSpace(exceptId))
	}
	if err := query.Select("COALESCE(SUM(LENGTH(\"card\")), 0)").Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

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
		limit = ContactsPerBook + 1
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
	if err := self.hideUnsharedAccounts(addressBookId, "", contacts); err != nil {
		return nil, err
	}
	return contacts, nil
}

func (self *transaction) GetContact(addressBookId, contactId string) (*models.Contact, error) {
	return self.contact(addressBookId, contactId, false)
}

// LockContact is GetContact for a caller about to write it: the row is held
// for the rest of the transaction, so that two devices holding the same
// version cannot both be told it is still theirs and both write.
func (self *transaction) LockContact(addressBookId, contactId string) (*models.Contact, error) {
	return self.contact(addressBookId, contactId, true)
}

func (self *transaction) contact(addressBookId, contactId string, holding bool) (*models.Contact, error) {
	query := self.tx
	if holding {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var found []contactModel
	if err := query.Where("\"addressbook_id\" = ? AND \"id\" = ?", addressBookId, contactId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	contact := found[0].toModel()
	if err := self.hideUnsharedAccounts(addressBookId, "", []*models.Contact{contact}); err != nil {
		return nil, err
	}
	return contact, nil
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
	contact := found[0].toModel()
	if err := self.hideUnsharedAccounts(addressBookId, "", []*models.Contact{contact}); err != nil {
		return nil, err
	}
	return contact, nil
}

// PutContact writes one, replacing what was there under that id. It names the
// columns it updates and leaves account_user_id out, so that a card sync or an
// edit, which build the contact afresh from a card, keep the link the owner
// made; SetContactAccount is the only writer of it.
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
	// And the book has a size, not only a count. Ten thousand cards of a
	// megabyte each is ten gigabytes, and a listing is the whole book in
	// one answer, built in memory before a byte of it is written.
	held, err := self.bookBytes(row.AddressBookID, row.ID)
	if err != nil {
		return nil, err
	}
	if held+int64(len(row.Card)) > BytesPerBook {
		return nil, fmt.Errorf("%w: it holds %d bytes and this card is %d, against a limit of %d",
			ErrBookFull, held, len(row.Card), BytesPerBook)
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "addressbook_id"}, {Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"created_at", "modified_at", "uid", "etag", "card", "name", "organization", "emails", "phones",
		}),
	}).Create(row).Error; err != nil {
		return nil, err
	}
	return self.GetContact(row.AddressBookID, row.ID)
}

func (self *transaction) DeleteContact(addressBookId, contactId string) error {
	book, err := self.GetAddressBook(addressBookId)
	if err != nil {
		return err
	}
	if err := self.tx.Where("\"addressbook_id\" = ? AND \"id\" = ?", addressBookId, contactId).
		Delete(&contactModel{}).Error; err != nil {
		return err
	}
	if book == nil {
		return nil
	}
	return self.untieContactsGone(book.UserID, []string{contactId})
}

// contactGoneSQL holds when the contact ID in the column it is formatted with
// is in none of the person's address books: a contact is only unique inside
// its book, so the same ID in another of them is still that person's.
const contactGoneSQL = `NOT EXISTS (SELECT 1 FROM "contact" JOIN "addressbook" ON "addressbook"."id" = "contact"."addressbook_id"
	WHERE "addressbook"."user_id" = ? AND "contact"."id" = %s)`

// untieContactsGone takes off what named a contact that is in none of the
// person's address books any more: the memory page about them, the link that
// said the page is them, and the account's own card. The page and what it
// remembers stay. Nothing holds these by foreign key, as a page names a
// contact by its ID alone, so a deleted contact would otherwise go on being
// named, and the dreams leave a page that names a contact alone. Empty
// contactIds looks at every tie the person has, for an address book deleted
// whole.
func (self *transaction) untieContactsGone(userId string, contactIds []string) error {
	if userId == "" {
		return nil
	}
	agents := `SELECT "id" FROM "agent" WHERE "user_id" = ?`
	pages := self.tx.Model(&agentNodeModel{}).
		Where(`"agent_id" IN (`+agents+`) AND "contact_id" IS NOT NULL`, userId).
		Where(fmt.Sprintf(contactGoneSQL, `"agent_node"."contact_id"`), userId)
	links := self.tx.Where(`"agent_id" IN (`+agents+`) AND "record_kind" = ? AND "record_relation" = ?`,
		userId, string(models.RecordContact), string(models.RecordRelationIsContact)).
		Where(fmt.Sprintf(contactGoneSQL, `"agent_record_link"."record_id"`), userId)
	account := self.tx.Model(&userModel{}).Where(`"id" = ? AND "contact_id" IS NOT NULL`, userId).
		Where(fmt.Sprintf(contactGoneSQL, `"user"."contact_id"`), userId)
	if len(contactIds) > 0 {
		pages = pages.Where(`"contact_id" IN ?`, contactIds)
		links = links.Where(`"record_id" IN ?`, contactIds)
		account = account.Where(`"contact_id" IN ?`, contactIds)
	}
	if err := pages.Update("contact_id", nil).Error; err != nil {
		return err
	}
	var removed []agentRecordLinkModel
	if err := links.Clauses(clause.Returning{}).Delete(&removed).Error; err != nil {
		return err
	}
	for index := range removed {
		self.note(removed[index].AgentID, removed[index].NodeID, models.RevisionUnlinked,
			map[string]any{"path": models.FormatRecordReference(models.RecordContact, removed[index].RecordID, 0, 0), "relation": removed[index].RecordRelation}, nil, "")
	}
	return account.Update("contact_id", nil).Error
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

// FindContactByAddress is the contact in any of the person's address books
// that carries this address, or nil.
//
// What "the sender is known" means now. It used to mean a row in a ledger of
// every address that had ever written to the mailbox; it means the person
// keeps them.
//
// The addresses of a card are stored one per line, so the match is on a whole
// line rather than on a substring: without that, keeping "ada@example.com"
// would make "not-ada@example.com" a known sender too.
func (self *transaction) FindContactByAddress(userId, address string) (*models.Contact, error) {
	address = strings.ToLower(strings.TrimSpace(address))
	if userId == "" || address == "" {
		return nil, nil
	}
	// The addresses are one column with a line each, so the column is read
	// back as the list it is and the address is compared to a whole entry.
	//
	// This was four LIKE patterns, which was wrong twice over: an address
	// holding an underscore -- alice_smith@example.com, an ordinary address
	// -- matched a contact with any character in that place, and an address
	// holding a per cent sign matched a great deal more than that. "Known"
	// is what the rule condition and the reply scope turn on, so a sender
	// who could widen it by choosing their own address could decide they
	// were somebody this person keeps.
	var rows []contactModel
	if err := self.tx.
		Joins("JOIN \"addressbook\" ON \"addressbook\".\"id\" = \"contact\".\"addressbook_id\"").
		Where("\"addressbook\".\"user_id\" = ?", userId).
		Where("? = ANY(string_to_array(LOWER(\"contact\".\"emails\"), E'\\n'))", address).
		Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	contact := rows[0].toModel()
	if err := self.hideUnsharedAccounts("", userId, []*models.Contact{contact}); err != nil {
		return nil, err
	}
	return contact, nil
}

// contactAccountSharedCondition holds for a contact row, joined to its
// address book, whose owner shares a circle with the account the contact is
// linked to. Only the owner sees a link to a person in their circles, and it
// is read only while that is true; endContactAccountsOutsideCircles clears it
// in the transaction in which it stops being true.
const contactAccountSharedCondition = `EXISTS (SELECT 1 FROM "circle_member" "mine" JOIN "circle_member" "theirs" ON "theirs"."circle_id" = "mine"."circle_id"
	WHERE "mine"."user_id" = "addressbook"."user_id" AND "theirs"."user_id" = "contact"."account_user_id")`

// hideUnsharedAccounts blanks the link on every contact whose owner no longer
// shares a circle with the account, so a row the cleanup missed is never shown.
// The owner is the address book's, or ownerUserId when the caller knows it.
func (self *transaction) hideUnsharedAccounts(addressBookId, ownerUserId string, contacts []*models.Contact) error {
	accountUserIds := []string{}
	seen := map[string]bool{}
	for _, contact := range contacts {
		if contact.AccountUserID != "" && !seen[contact.AccountUserID] {
			seen[contact.AccountUserID] = true
			accountUserIds = append(accountUserIds, contact.AccountUserID)
		}
	}
	if len(accountUserIds) == 0 {
		return nil
	}
	if ownerUserId == "" {
		var ownerUserIds []string
		if err := self.tx.Model(&addressBookModel{}).Where("\"id\" = ?", addressBookId).Pluck("COALESCE(user_id, '')", &ownerUserIds).Error; err != nil {
			return err
		}
		if len(ownerUserIds) == 0 || ownerUserIds[0] == "" {
			return nil
		}
		ownerUserId = ownerUserIds[0]
	}
	var sharedUserIds []string
	if err := self.tx.Table("circle_member AS mine").
		Joins("JOIN circle_member AS theirs ON theirs.circle_id = mine.circle_id").
		Where("mine.user_id = ? AND theirs.user_id IN ?", ownerUserId, accountUserIds).
		Distinct().Pluck("theirs.user_id", &sharedUserIds).Error; err != nil {
		return err
	}
	shared := map[string]bool{}
	for _, userId := range sharedUserIds {
		shared[userId] = true
	}
	for _, contact := range contacts {
		if !shared[contact.AccountUserID] {
			contact.AccountUserID = ""
		}
	}
	return nil
}

// SetContactAccount links a contact to the account on this server it is, or
// clears the link when accountUserId is empty. It is the only writer of the
// link. It refuses, in its own transaction, the owner's own account and an
// account the owner shares no circle with (the owner is reached through the
// address book, so neither can be a CHECK), and an account another contact in
// the same book already is. The circle lock is held so that the check and the
// write cannot straddle two people leaving their last circle.
func (self *transaction) SetContactAccount(addressBookId, contactId, accountUserId string) error {
	accountUserId = strings.TrimSpace(accountUserId)
	if err := lockCircleMembership(self.tx); err != nil {
		return err
	}
	book, err := self.GetAddressBook(addressBookId)
	if err != nil {
		return err
	}
	if book == nil {
		return ErrNotFound
	}
	var rows []contactModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("\"addressbook_id\" = ? AND \"id\" = ?", addressBookId, contactId).Limit(1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return ErrNotFound
	}
	if accountUserId == "" {
		return self.tx.Model(&contactModel{}).Where("\"addressbook_id\" = ? AND \"id\" = ?", addressBookId, contactId).
			Update("account_user_id", nil).Error
	}
	if book.UserID == "" {
		// The link is the owner's private note about a person. A book that
		// every member of a circle reads has no owner to hold it.
		return fmt.Errorf("%w: a contact in a circle's address book cannot be linked to an account", ErrInvalidArguments)
	}
	if accountUserId == book.UserID {
		return fmt.Errorf("%w: a contact cannot be linked to its owner's own account", ErrInvalidArguments)
	}
	shares, err := self.SharesCircle(book.UserID, accountUserId)
	if err != nil {
		return err
	}
	if !shares {
		return fmt.Errorf("%w: that account is not in a circle with the owner of the address book", ErrInvalidArguments)
	}
	var taken int64
	if err := self.tx.Model(&contactModel{}).
		Where("\"addressbook_id\" = ? AND \"account_user_id\" = ? AND \"id\" <> ?", addressBookId, accountUserId, contactId).
		Count(&taken).Error; err != nil {
		return err
	}
	if taken > 0 {
		return fmt.Errorf("%w: another contact in this address book is already linked to that account", ErrAlreadyExists)
	}
	return self.tx.Model(&contactModel{}).Where("\"addressbook_id\" = ? AND \"id\" = ?", addressBookId, contactId).
		Update("account_user_id", accountUserId).Error
}

// ListContactAccountLinks are the owner's contacts that are linked to an
// account they still share a circle with, in the order the owner's books were
// made (then by contact id), so that where two books link the same account the
// first one comes first.
func (self *transaction) ListContactAccountLinks(ownerUserId string) ([]*models.Contact, error) {
	contacts := []*models.Contact{}
	if ownerUserId == "" {
		return contacts, nil
	}
	var rows []contactModel
	if err := self.tx.Table("contact").
		Select("contact.*").
		Joins("JOIN addressbook ON addressbook.id = contact.addressbook_id").
		Where("addressbook.user_id = ? AND contact.account_user_id IS NOT NULL AND "+contactAccountSharedCondition, ownerUserId).
		Order("addressbook.created_at ASC, addressbook.id ASC, contact.id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		contacts = append(contacts, rows[index].toModel())
	}
	return contacts, nil
}

// FindContactByAccount is the owner's contact that is this account, or nil:
// the first address book that has one, by the book's created_at then id, and
// only while they share a circle.
func (self *transaction) FindContactByAccount(ownerUserId, accountUserId string) (*models.Contact, error) {
	if ownerUserId == "" || accountUserId == "" || ownerUserId == accountUserId {
		return nil, nil
	}
	var rows []contactModel
	if err := self.tx.Table("contact").
		Select("contact.*").
		Joins("JOIN addressbook ON addressbook.id = contact.addressbook_id").
		Where("addressbook.user_id = ? AND contact.account_user_id = ? AND "+contactAccountSharedCondition, ownerUserId, accountUserId).
		Order("addressbook.created_at ASC, addressbook.id ASC, contact.id ASC").
		Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].toModel(), nil
}

// ListPhotoPeopleAccountUserIDs are the accounts the owner linked a contact to
// that a face group of theirs is named by, in one query, for a prompt that
// says which of their people can be found in photos.
func (self *transaction) ListPhotoPeopleAccountUserIDs(ownerUserId string) ([]string, error) {
	accountUserIds := []string{}
	if ownerUserId == "" {
		return accountUserIds, nil
	}
	if err := self.tx.Table("contact").
		Joins("JOIN addressbook ON addressbook.id = contact.addressbook_id").
		Joins("JOIN photo_face_group ON photo_face_group.addressbook_id = contact.addressbook_id AND photo_face_group.contact_id = contact.id AND photo_face_group.user_id = addressbook.user_id").
		Where("addressbook.user_id = ? AND contact.account_user_id IS NOT NULL AND "+contactAccountSharedCondition+
			" AND EXISTS (SELECT 1 FROM photo_face WHERE photo_face.photo_face_group_id = photo_face_group.id)", ownerUserId).
		Distinct().Order("contact.account_user_id ASC").Pluck("contact.account_user_id", &accountUserIds).Error; err != nil {
		return nil, err
	}
	return accountUserIds, nil
}

// ListContactAccountSuggestions are the owner's unlinked contacts whose email
// addresses include a mailbox address of an account the owner shares a circle
// with, one account for each contact (the first of the card's addresses that
// matches), by contact name. Only mailbox addresses are matched: an account's
// notification address (User.Email) is private profile data. The answer names
// the account and never the address that matched. An account the contact's
// book already links to another contact is not suggested, since the link would
// be refused.
func (self *transaction) ListContactAccountSuggestions(ownerUserId string) ([]*models.ContactAccountSuggestion, error) {
	suggestions := []*models.ContactAccountSuggestion{}
	if ownerUserId == "" {
		return suggestions, nil
	}
	circleUserIds, err := self.CircleUserIDs(ownerUserId)
	if err != nil {
		return nil, err
	}
	if len(circleUserIds) == 0 {
		return suggestions, nil
	}
	var mailboxes []struct {
		ID     string `gorm:"column:id"`
		UserID string `gorm:"column:user_id"`
	}
	if err := self.tx.Table("mailbox").Select("id, user_id").Where("user_id IN ?", circleUserIds).Scan(&mailboxes).Error; err != nil {
		return nil, err
	}
	mailboxIds := make([]string, 0, len(mailboxes))
	for _, mailbox := range mailboxes {
		mailboxIds = append(mailboxIds, mailbox.ID)
	}
	addresses, err := loadMailboxAddresses(self.tx, mailboxIds)
	if err != nil {
		return nil, err
	}
	accountByAddress := map[string]string{}
	for _, mailbox := range mailboxes {
		for _, address := range addresses[mailbox.ID] {
			accountByAddress[strings.ToLower(address.Address)] = mailbox.UserID
		}
	}
	if len(accountByAddress) == 0 {
		return suggestions, nil
	}

	var rows []contactModel
	if err := self.tx.Table("contact").
		Select("contact.*").
		Joins("JOIN addressbook ON addressbook.id = contact.addressbook_id").
		Where("addressbook.user_id = ? AND contact.emails <> ''", ownerUserId).
		Order("contact.name ASC, contact.addressbook_id ASC, contact.id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	// Every linked contact of the owner counts, including one with no email
	// addresses, since the book's link is refused for any of them.
	var linkedRows []struct {
		AddressBookID string `gorm:"column:addressbook_id"`
		AccountUserID string `gorm:"column:account_user_id"`
	}
	if err := self.tx.Table("contact").
		Select("contact.addressbook_id, contact.account_user_id").
		Joins("JOIN addressbook ON addressbook.id = contact.addressbook_id").
		Where("addressbook.user_id = ? AND contact.account_user_id IS NOT NULL", ownerUserId).
		Scan(&linkedRows).Error; err != nil {
		return nil, err
	}
	linked := map[string]bool{}
	for _, linkedRow := range linkedRows {
		linked[linkedRow.AddressBookID+"\n"+linkedRow.AccountUserID] = true
	}
	for index := range rows {
		if rows[index].AccountUserID != nil {
			continue
		}
		for _, address := range splitLines(rows[index].Emails) {
			accountUserId, found := accountByAddress[strings.ToLower(strings.TrimSpace(address))]
			if !found || linked[rows[index].AddressBookID+"\n"+accountUserId] {
				continue
			}
			suggestions = append(suggestions, &models.ContactAccountSuggestion{Contact: rows[index].toModel(), AccountUserID: accountUserId})
			break
		}
	}
	return suggestions, nil
}

// endContactAccountsOutsideCircles clears every link between this person and
// an account they no longer share any circle with, in the transaction that
// changed the circles, as endRelationshipsOutsideCircles does: both the
// person's own contacts that point at somebody, and other people's contacts
// that point at the person (the owner is the address book's).
func endContactAccountsOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`UPDATE "contact" SET "account_user_id" = NULL FROM "addressbook"
		WHERE "addressbook"."id" = "contact"."addressbook_id" AND "contact"."account_user_id" IS NOT NULL
		AND ("addressbook"."user_id" = ? OR "contact"."account_user_id" = ?) AND NOT `+contactAccountSharedCondition, userId, userId).Error
}
