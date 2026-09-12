package client

import "context"

// A person's own address book: the contacts they keep, as against the
// addresses a mailbox learned from traffic, which are in mailbox.go.

// AddressBook is one address book and how much is in it.
type AddressBook struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Contacts    int    `json:"contacts"`
}

// Contact is one person kept in an address book. Card is the whole of it and
// comes back only when one contact is asked for by name.
type Contact struct {
	ID           string   `json:"id"`
	UID          string   `json:"uid"`
	ETag         string   `json:"etag"`
	Name         string   `json:"name,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Emails       []string `json:"emails"`
	Phones       []string `json:"phones"`
	Note         string   `json:"note,omitempty"`
	Card         string   `json:"card,omitempty"`
}

const (
	DocumentListAddressBooks = `query { ListAddressBooks { id name description contacts } }`

	DocumentListContacts = `query ($addressBookId: String!, $query: String, $first: Int) {
  ListContacts(addressBookId: $addressBookId, query: $query, first: $first) {
    id uid name organization emails phones
  }
}`

	DocumentGetContact = `query ($id: String!) {
  GetContact(id: $id) { id uid etag name organization emails phones note card }
}`

	DocumentSaveContact = `mutation ($addressBookId: String!, $id: String, $card: String,
    $name: String, $organization: String, $title: String,
    $emails: [String!], $phones: [String!], $note: String) {
  SaveContact(addressBookId: $addressBookId, id: $id, card: $card,
    name: $name, organization: $organization, title: $title,
    emails: $emails, phones: $phones, note: $note) { id uid name emails phones }
}`

	DocumentDeleteContact = `mutation ($id: String!) { DeleteContact(id: $id) }`
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
