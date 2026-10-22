package models

import "time"

// AddressBook is a collection of contacts: a person's own, or a circle's.
//
// A person's own belongs to the account rather than to a mailbox: somebody
// with two mailboxes has one address book, and reaches it through either of
// them. An account is given one named "Contacts" the first time it looks, so
// that nobody has to create one before they can keep a contact. It is never
// shared.
//
// A circle's belongs to the circle (CircleID set, UserID empty): every member
// reads and edits it, and it goes when the circle goes. Exactly one of the two
// is set.
type AddressBook struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	CircleID   string    `json:"circleId,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// AgentGranted says the person has given their agent this address book.
	// False until they do: nothing from a source the person has not granted
	// is ever sent to a model.
	AgentGranted bool `json:"agentGranted"`
}

// AddressBookAccess is what a person may do with one address book, answered
// by the database's AddressBookAccess and asked by every door into a book.
type AddressBookAccess struct {
	// CanEdit is that the person may read the book and add, change and remove
	// the contacts in it: the owner of a personal book, any member of the
	// circle whose book it is.
	CanEdit bool
	// CanManage is that the person may rename the book and delete it: its
	// owner, or an admin of its circle.
	CanManage bool
	// IsOwner is that the book is the person's own. Everything that is owner
	// only (the account link, the agent's grant, the ties from recordings and
	// photos) asks for this and never for CanEdit.
	IsOwner bool
}

// ReachableAddressBook is an address book a person can use with what they may
// do with it, and the name of the circle when it is a circle's.
type ReachableAddressBook struct {
	Book       *AddressBook
	CircleName string
	Access     AddressBookAccess
}

// Contact is one person in an address book.
//
// Card is the whole of it: the vCard text, which is what a phone sends and
// what it is given back. The fields beside it are pulled out of the card when
// it is written, so that listing and searching never parse a vCard; where the
// two disagree the card is right.
//
// This is the only list of people the server keeps. A mailbox does not learn
// the addresses it sees go past: "is this sender known" means "is this sender
// in the address book", here, and address completion comes from here too.
type Contact struct {
	ID            string    `json:"id"`
	AddressBookID string    `json:"addressBookId"`
	CreatedAt     time.Time `json:"createdAt"`
	ModifiedAt    time.Time `json:"modifiedAt"`

	// UID is the card's own UID property, which is how the same person is
	// recognized across devices; ETag names a version of the card, and is
	// what a conditional write is checked against.
	UID  string `json:"uid"`
	ETag string `json:"etag"`
	Card string `json:"card"`

	Name         string   `json:"name,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Emails       []string `json:"emails,omitempty"`
	Phones       []string `json:"phones,omitempty"`

	// AccountUserID is the account on this server that the owner says this
	// contact is, or empty. Only the owner sees the link: read only while the
	// owner still shares a circle with that account, never carried in the
	// card, and never seen by the account. It is written only by
	// SetContactAccount, so saving a card does not touch it.
	AccountUserID string `json:"accountUserId,omitempty"`
}

// ContactAccountSuggestion is a contact the owner has not linked whose email
// addresses include a mailbox address of an account they share a circle with.
// It says which account, never which address matched.
type ContactAccountSuggestion struct {
	Contact       *Contact `json:"contact"`
	AccountUserID string   `json:"accountUserId"`
}
