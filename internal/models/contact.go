package models

import "time"

// AddressBook is a person's own collection of contacts. It belongs to the
// account rather than to a mailbox: somebody with two mailboxes has one
// address book, and reaches it through either of them.
//
// An account is given one named "Contacts" the first time it looks, so that
// nobody has to create one before they can keep a contact.
type AddressBook struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Contact is one person in an address book.
//
// Card is the whole of it: the vCard text, which is what a phone sends and
// what it is given back. The fields beside it are pulled out of the card when
// it is written, so that listing and searching never parse a vCard; where the
// two disagree the card is right.
//
// Not to be confused with MailboxContact, which is an address learned from
// traffic. That is a list of people who have written to this mailbox; this is
// a list of people somebody chose to keep.
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
}
