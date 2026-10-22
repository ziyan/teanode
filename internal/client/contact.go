package client

import "context"

// A person's own address book: the contacts they keep, as against the
// addresses a mailbox learned from traffic, which are in mailbox.go.

// AddressBook is one address book and how much is in it: the signed-in
// account's own, or a circle's they are in.
type AddressBook struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Contacts    int    `json:"contacts"`

	// CircleID, CircleName and IsCircleBook say whose a circle's book is; the
	// first is null for the account's own. CanManage is that the account may
	// rename it and, for a circle's, delete it.
	CircleID     *string `json:"circleId"`
	CircleName   string  `json:"circleName"`
	IsCircleBook bool    `json:"isCircleBook"`
	CanManage    bool    `json:"canManage"`
}

// Contact is one person kept in an address book. Card is the whole of it and
// comes back only when one contact is asked for by name.
type Contact struct {
	ID           string     `json:"id"`
	UID          string     `json:"uid"`
	ETag         string     `json:"etag"`
	Name         string     `json:"name,omitempty"`
	Organization string     `json:"organization,omitempty"`
	Emails       []string   `json:"emails"`
	Phones       []string   `json:"phones"`
	Addresses    []*Address `json:"addresses,omitempty"`
	Note         string     `json:"note,omitempty"`
	HasPhoto     bool       `json:"hasPhoto"`
	Card         string     `json:"card,omitempty"`

	// AccountUserID and Account say which person in the signed-in account's
	// circles the contact is linked to, when it is and they still share a
	// circle. Null otherwise.
	AccountUserID *string         `json:"accountUserId"`
	Account       *ContactAccount `json:"account"`
}

// ContactAccount is the person a contact is linked to, as the circles show
// them.
type ContactAccount struct {
	UserID           string  `json:"userId"`
	Username         string  `json:"username"`
	Name             string  `json:"name"`
	AvatarURL        *string `json:"avatarUrl"`
	AvatarColorIndex int     `json:"avatarColorIndex"`
}

// ContactAccountSuggestion offers to link a contact to a person in the
// circles, because the contact carries one of that person's mailbox addresses.
// It never says which address.
type ContactAccountSuggestion struct {
	// AddressBookID is the book the contact is in.
	AddressBookID string          `json:"addressBookId"`
	Contact       *Contact        `json:"contact"`
	Account       *ContactAccount `json:"account"`
}

// Address is one postal address on a contact. Written is the same thing on
// one line, the way somebody would put it on an envelope.
type Address struct {
	Label      string `json:"label,omitempty"`
	Street     string `json:"street,omitempty"`
	Locality   string `json:"locality,omitempty"`
	Region     string `json:"region,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Country    string `json:"country,omitempty"`
	Written    string `json:"written"`
}

const (
	addressBookSelection = `{ id name description contacts circleId circleName isCircleBook canManage }`

	DocumentListAddressBooks = `query { ListAddressBooks ` + addressBookSelection + ` }`

	DocumentCreateCircleAddressBook = `mutation ($circleId: String!, $name: String) {
  CreateCircleAddressBook(circleId: $circleId, name: $name) ` + addressBookSelection + `
}`

	DocumentSaveAddressBook = `mutation ($id: String!, $name: String, $description: String) {
  SaveAddressBook(id: $id, name: $name, description: $description) ` + addressBookSelection + `
}`

	DocumentDeleteCircleAddressBook = `mutation ($addressBookId: String!) { DeleteCircleAddressBook(addressBookId: $addressBookId) }`

	contactAccountSelection = `{ userId username name avatarUrl avatarColorIndex }`

	DocumentListContacts = `query ($addressBookId: String!, $query: String, $first: Int) {
  ListContacts(addressBookId: $addressBookId, query: $query, first: $first) {
    id uid name organization emails phones hasPhoto addresses { written }
    accountUserId account ` + contactAccountSelection + `
  }
}`

	DocumentGetContact = `query ($id: String!) {
  GetContact(id: $id) {
    id uid etag name organization emails phones note hasPhoto card
    addresses { label street locality region postalCode country written }
    accountUserId account ` + contactAccountSelection + `
  }
}`

	DocumentSetContactAccount = `mutation ($addressBookId: String!, $contactId: String!, $accountUserId: String) {
  SetContactAccount(addressBookId: $addressBookId, contactId: $contactId, accountUserId: $accountUserId) {
    id uid name emails phones accountUserId account ` + contactAccountSelection + `
  }
}`

	DocumentListContactAccountSuggestions = `query {
  ListContactAccountSuggestions {
    addressBookId
    contact { id uid name organization emails phones }
    account ` + contactAccountSelection + `
  }
}`

	DocumentSaveContact = `mutation ($addressBookId: String!, $id: String, $card: String,
    $name: String, $organization: String, $title: String,
    $emails: [String!], $phones: [String!], $note: String) {
  SaveContact(addressBookId: $addressBookId, id: $id, card: $card,
    name: $name, organization: $organization, title: $title,
    emails: $emails, phones: $phones, note: $note) { id uid name emails phones }
}`

	DocumentDeleteContact = `mutation ($id: String!) { DeleteContact(id: $id) }`

	DocumentRemoveContactPicture = `mutation ($id: String!) { RemoveContactPicture(id: $id) { id uid name emails phones hasPhoto } }`
)

// ListAddressBooks are the caller's. An account that has never had one is
// given one by the server when it first looks.
func ListAddressBooks(ctx context.Context, connection *Client) ([]*AddressBook, error) {
	var result struct {
		ListAddressBooks []*AddressBook `json:"ListAddressBooks"`
	}
	if err := connection.Execute(ctx, DocumentListAddressBooks, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAddressBooks, nil
}

// CreateCircleAddressBook makes the address book a circle keeps for itself.
func CreateCircleAddressBook(ctx context.Context, executor Executor, circleId, name string) (*AddressBook, error) {
	arguments := map[string]any{"circleId": circleId, "name": nil}
	if name != "" {
		arguments["name"] = name
	}
	var result struct {
		CreateCircleAddressBook *AddressBook `json:"CreateCircleAddressBook"`
	}
	if err := executor.Execute(ctx, DocumentCreateCircleAddressBook, arguments, &result); err != nil {
		return nil, err
	}
	return result.CreateCircleAddressBook, nil
}

// SaveAddressBook renames a book and changes what it says about itself; an
// empty name keeps the old one.
func SaveAddressBook(ctx context.Context, executor Executor, id, name, description string) (*AddressBook, error) {
	var result struct {
		SaveAddressBook *AddressBook `json:"SaveAddressBook"`
	}
	if err := executor.Execute(ctx, DocumentSaveAddressBook, map[string]any{"id": id, "name": name, "description": description}, &result); err != nil {
		return nil, err
	}
	return result.SaveAddressBook, nil
}

// DeleteCircleAddressBook deletes a circle's address book with every card in
// it.
func DeleteCircleAddressBook(ctx context.Context, executor Executor, addressBookId string) error {
	var result struct {
		DeleteCircleAddressBook bool `json:"DeleteCircleAddressBook"`
	}
	return executor.Execute(ctx, DocumentDeleteCircleAddressBook, map[string]any{"addressBookId": addressBookId}, &result)
}

// ListContacts are the contacts in one book, narrowed by a query when there
// is one.
func ListContacts(ctx context.Context, connection *Client, addressBookId, query string, first int) ([]*Contact, error) {
	var result struct {
		ListContacts []*Contact `json:"ListContacts"`
	}
	arguments := map[string]any{"addressBookId": addressBookId, "query": nil, "first": nil}
	if query != "" {
		arguments["query"] = query
	}
	if first > 0 {
		arguments["first"] = first
	}
	if err := connection.Execute(ctx, DocumentListContacts, arguments, &result); err != nil {
		return nil, err
	}
	return result.ListContacts, nil
}

// GetContact is one contact, with its card.
func GetContact(ctx context.Context, connection *Client, id string) (*Contact, error) {
	var result struct {
		GetContact *Contact `json:"GetContact"`
	}
	if err := connection.Execute(ctx, DocumentGetContact, map[string]any{"id": id}, &result); err != nil {
		return nil, err
	}
	return result.GetContact, nil
}

// SaveContactFields keeps a contact from filled-in fields. A nil field is
// left as it was; a pointer to an empty value clears it.
type SaveContactFields struct {
	AddressBookID string
	ID            string
	Card          string
	Name          *string
	Organization  *string
	Title         *string
	Emails        *[]string
	Phones        *[]string
	Note          *string
}

// SaveContact keeps one, or changes one that is kept.
func SaveContact(ctx context.Context, connection *Client, fields *SaveContactFields) (*Contact, error) {
	arguments := map[string]any{
		"addressBookId": fields.AddressBookID,
		"id":            nil, "card": nil, "name": nil, "organization": nil,
		"title": nil, "emails": nil, "phones": nil, "note": nil,
	}
	if fields.ID != "" {
		arguments["id"] = fields.ID
	}
	if fields.Card != "" {
		arguments["card"] = fields.Card
	}
	if fields.Name != nil {
		arguments["name"] = *fields.Name
	}
	if fields.Organization != nil {
		arguments["organization"] = *fields.Organization
	}
	if fields.Title != nil {
		arguments["title"] = *fields.Title
	}
	if fields.Emails != nil {
		arguments["emails"] = *fields.Emails
	}
	if fields.Phones != nil {
		arguments["phones"] = *fields.Phones
	}
	if fields.Note != nil {
		arguments["note"] = *fields.Note
	}
	var result struct {
		SaveContact *Contact `json:"SaveContact"`
	}
	if err := connection.Execute(ctx, DocumentSaveContact, arguments, &result); err != nil {
		return nil, err
	}
	return result.SaveContact, nil
}

// DeleteContact forgets one.
func DeleteContact(ctx context.Context, connection *Client, id string) error {
	var result struct {
		DeleteContact bool `json:"DeleteContact"`
	}
	return connection.Execute(ctx, DocumentDeleteContact, map[string]any{"id": id}, &result)
}

// RemoveContactPicture takes the picture off a contact and leaves the rest of
// the card as it was.
func RemoveContactPicture(ctx context.Context, executor Executor, id string) (*Contact, error) {
	var result struct {
		RemoveContactPicture *Contact `json:"RemoveContactPicture"`
	}
	if err := executor.Execute(ctx, DocumentRemoveContactPicture, map[string]any{"id": id}, &result); err != nil {
		return nil, err
	}
	return result.RemoveContactPicture, nil
}

// SetContactAccount links a contact to the person in the signed-in account's
// circles it is, or takes the link away when accountUserId is empty.
func SetContactAccount(ctx context.Context, executor Executor, addressBookId, contactId, accountUserId string) (*Contact, error) {
	arguments := map[string]any{"addressBookId": addressBookId, "contactId": contactId, "accountUserId": nil}
	if accountUserId != "" {
		arguments["accountUserId"] = accountUserId
	}
	var result struct {
		SetContactAccount *Contact `json:"SetContactAccount"`
	}
	if err := executor.Execute(ctx, DocumentSetContactAccount, arguments, &result); err != nil {
		return nil, err
	}
	return result.SetContactAccount, nil
}

// ListContactAccountSuggestions are the contacts that look like people in the
// circles, for the caller to confirm one by one.
func ListContactAccountSuggestions(ctx context.Context, executor Executor) ([]*ContactAccountSuggestion, error) {
	var result struct {
		ListContactAccountSuggestions []*ContactAccountSuggestion `json:"ListContactAccountSuggestions"`
	}
	if err := executor.Execute(ctx, DocumentListContactAccountSuggestions, nil, &result); err != nil {
		return nil, err
	}
	return result.ListContactAccountSuggestions, nil
}
