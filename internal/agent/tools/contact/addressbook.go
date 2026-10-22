// Package contact is the person's address book: the people they keep.
package contact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

// The address book: the people the person chose to keep, which their phone
// synchronizes over CardDAV.
//
// This is the only answer to "who do I know". Nothing else keeps a list of
// people: a mailbox no longer learns the addresses it has seen go past, and
// "have they written before" is a search of the mail itself.

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "contact_book", Family: tools.FamilyMailbox, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionContactsUse},
				Description: "The person's own address book: the contacts they keep, which their phone and computer synchronize over CardDAV. Use list to see them or to search by name, organization, address or number; get for one contact with everything on it; save to keep a new one or change one that is kept; remove to forget one; remove-picture to take only the picture off one; link-account to say a contact is a person in their circles (a person with an account on this server), and unlink-account to take that back.",
				Parameters: tools.Object(map[string]any{
					"action":          tools.EnumProperty("what to do", "list", "get", "save", "remove", "remove-picture", "link-account", "unlink-account"),
					"query":           tools.StringProperty("for list: narrow to contacts whose name, organization, address or number matches"),
					"id":              tools.StringProperty("for get, remove, remove-picture, and for changing one that is kept: the contact's id, as list gives it"),
					"contact_id":      tools.StringProperty("for link-account and unlink-account: the contact's id, as list gives it"),
					"address_book_id": tools.StringProperty("for link-account and unlink-account: the address book the contact is in, as list gives it; the one you were given when left out"),
					"person":          tools.StringProperty("for link-account: the username of the person in their circles the contact is, as the person tools give it"),
					"account_user_id": tools.StringProperty("for link-account: the same person by account id, instead of person"),
					"name":            tools.StringProperty("for save: what to call them"),
					"organization":    tools.StringProperty("for save: where they work"),
					"title":           tools.StringProperty("for save: what they do there"),
					"emails":          tools.ArrayProperty("for save: their email addresses, replacing what is there", tools.StringProperty("an address")),
					"phones":          tools.ArrayProperty("for save: their telephone numbers, replacing what is there", tools.StringProperty("a number")),
					"note":            tools.StringProperty("for save: anything else worth keeping about them"),
				}, "action"),
				Guidance: "contact_book: this is the person's own list of people, and the only one there is -- to answer whether somebody has written before, search the mail instead. To keep a sender -- \"save that sender to my contacts\" -- save them here with their name and address; there is no separate action for it, because a contact kept from a message is an ordinary contact. Saving without an id keeps a new contact; saving with one changes that contact, and anything you leave out is left as it was, so correcting a name does not throw away the address. A contact is stored as the card a phone would send, so a photograph or a birthday put there by a device survives an edit made here. Removing one removes it from the person's devices too, so say who it is and wait to be told to go ahead. A contact may have a picture, which the server sometimes cuts from a face in the person's photos when they name somebody; remove-picture takes just the picture off and leaves the rest of the contact. link-account links a contact to a person in the circles they share (the account is chosen by username from the people they share a circle with); only the person sees the link, nobody is told, and the person it names never sees it. Make the link only when the person says so or confirms a suggestion that list reports; never because two names look alike. Rows of list show person for a linked contact, and when some contacts carry an address of somebody in their circles, list says so in suggested_links, which you offer to the person and do not act on. The photo tool then finds a linked person's photos by person.",
				Preview: func(arguments json.RawMessage) string {
					var call contactBookArguments
					if err := json.Unmarshal(arguments, &call); err != nil {
						return "The address book"
					}
					switch call.Action {
					case "save":
						if call.ID != "" {
							return "Change a contact in the address book"
						}
						return fmt.Sprintf("Keep %s in the address book", strings.TrimSpace(call.Name+" "+strings.Join(call.Emails, " ")))
					case "remove":
						return "Forget a contact, on every device too"
					case "remove-picture":
						return "Take the picture off a contact, on every device too"
					case "link-account":
						return "Link a contact to a person in their circles"
					case "unlink-account":
						return "Unlink a contact from a person in their circles"
					}
					return "The address book"
				},
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call contactBookArguments
					if err := json.Unmarshal(arguments, &call); err != nil {
						return tools.RiskWrite
					}
					switch call.Action {
					case "list", "get":
						return tools.RiskRead
					case "link-account", "unlink-account":
						// A link only the person sees: nobody is told and nothing
						// leaves the server, so it is a write and not an outward act.
						return tools.RiskWrite
					case "remove", "remove-picture":
						// It reaches the person's phone, and there is no
						// tombstone to undo it with.
						return tools.RiskDestructive
					}
					return tools.RiskWrite
				},
				Run: runContactBook,
			},
		}
	})
}

type contactBookArguments struct {
	Action       string   `json:"action"`
	Query        string   `json:"query"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Organization string   `json:"organization"`
	Title        string   `json:"title"`
	Emails       []string `json:"emails"`
	Phones       []string `json:"phones"`
	Note         string   `json:"note"`

	ContactID     string `json:"contact_id"`
	AddressBookID string `json:"address_book_id"`
	Person        string `json:"person"`
	AccountUserID string `json:"account_user_id"`
}

const (
	documentBooks = `query { ListAddressBooks { id name contacts agentGranted isCircleBook } }`

	documentBookContacts = `query ($addressBookId: String!, $query: String, $first: Int) {
  ListContacts(addressBookId: $addressBookId, query: $query, first: $first) {
    id name organization emails phones account { userId username }
  }
}`

	documentBookContact = `query ($id: String!) {
  GetContact(id: $id) { id addressBookId name organization emails phones note account { userId username } }
}`

	// Which book a contact is in, for the actions that name only the contact.
	documentBookContactBook = `query ($id: String!) { GetContact(id: $id) { addressBookId } }`

	documentBookPeople = `query { ListCirclePeople { userId username name } }`

	documentBookSuggestions = `query { ListContactAccountSuggestions { addressBookId contact { id name } account { userId username } } }`

	documentBookLink = `mutation ($addressBookId: String!, $contactId: String!, $accountUserId: String) {
  SetContactAccount(addressBookId: $addressBookId, contactId: $contactId, accountUserId: $accountUserId) {
    id name account { userId username }
  }
}`

	documentBookSave = `mutation ($addressBookId: String!, $id: String, $name: String, $organization: String,
    $title: String, $emails: [String!], $phones: [String!], $note: String) {
  SaveContact(addressBookId: $addressBookId, id: $id, name: $name, organization: $organization,
    title: $title, emails: $emails, phones: $phones, note: $note) { id name emails phones }
}`

	documentBookRemove = `mutation ($id: String!) { DeleteContact(id: $id) }`

	documentBookRemovePicture = `mutation ($id: String!) { RemoveContactPicture(id: $id) { id name } }`
)

type bookView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Contacts     int    `json:"contacts"`
	AgentGranted bool   `json:"agentGranted"`

	// IsCircleBook is a circle's address book, which every member of the
	// circle writes and so is not one person's to give to an agent.
	IsCircleBook bool `json:"isCircleBook"`
}

type contactView struct {
	ID string `json:"id"`

	// AddressBookID is read to check the contact is in the book they gave you;
	// the model is not shown it.
	AddressBookID string `json:"-"`

	Name         string   `json:"name,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Emails       []string `json:"emails,omitempty"`
	Phones       []string `json:"phones,omitempty"`
	Note         string   `json:"note,omitempty"`

	// Account is the person in the circles the contact is linked to, as the
	// server answers it; Person is the username the model is shown.
	Account *accountView `json:"-"`
	Person  string       `json:"person,omitempty"`
}

type accountView struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// UnmarshalJSON reads the server's contact and fills the username the model
// is shown from the linked account.
func (self *contactView) UnmarshalJSON(data []byte) error {
	type plain contactView
	var decoded struct {
		plain
		AddressBookID string       `json:"addressBookId"`
		Account       *accountView `json:"account"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*self = contactView(decoded.plain)
	self.AddressBookID = decoded.AddressBookID
	self.Account = decoded.Account
	if decoded.Account != nil {
		self.Person = decoded.Account.Username
	}
	return nil
}

func runContactBook(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[contactBookArguments](call)
	if err != nil {
		return nil, err
	}
	// Which fields the model actually wrote, so that leaving one out
	// means "leave it alone" rather than "empty it".
	given := map[string]bool{}
	var written map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &written); err == nil {
		for name := range written {
			given[name] = true
		}
	}

	run := tools.MustRun(ctx)
	operations := run.Operations()

	var books struct {
		ListAddressBooks []*bookView `json:"ListAddressBooks"`
	}
	if err := operations.Execute(ctx, documentBooks, nil, &books); err != nil {
		return nil, err
	}
	if len(books.ListAddressBooks) == 0 {
		return nil, fmt.Errorf("this person has no address book")
	}
	// Granted, not merely owned: an address book is a source like a
	// mailbox, and what the person has not handed over is not the agent's
	// to read. A circle's book is never one of them: the text in its cards was
	// typed by anybody in the circle.
	var book *bookView
	for _, candidate := range books.ListAddressBooks {
		if candidate.AgentGranted && !candidate.IsCircleBook && (book == nil || candidate.ID == arguments.AddressBookID) {
			book = candidate
			if candidate.ID == arguments.AddressBookID {
				break
			}
		}
	}
	if book == nil {
		return nil, fmt.Errorf("they have not given you their address book; it is a switch on their agent's page, beside the mailboxes")
	}
	// Every book they gave you, for the actions that name only a contact.
	givenBookIds := map[string]bool{}
	for _, candidate := range books.ListAddressBooks {
		if candidate.AgentGranted && !candidate.IsCircleBook {
			givenBookIds[candidate.ID] = true
		}
	}
	// A named book that is not one they gave you is an error, never the first
	// book instead: a contact id from another book must not be acted on here.
	if arguments.AddressBookID != "" && book.ID != arguments.AddressBookID {
		return nil, fmt.Errorf("address_book_id %q is not an address book they have given you; list gives the ids", arguments.AddressBookID)
	}

	switch arguments.Action {
	case "list":
		var answer struct {
			ListContacts []*contactView `json:"ListContacts"`
		}
		if err := operations.Execute(ctx, documentBookContacts, map[string]any{
			"addressBookId": book.ID, "query": emptyToNil(arguments.Query), "first": 200,
		}, &answer); err != nil {
			return nil, err
		}
		answered := map[string]any{
			"contacts": answer.ListContacts, "kept": len(answer.ListContacts),
		}
		// Offered, never acted on: the person confirms each one. A failure
		// here only leaves the line out, since the list is good without it.
		var suggestions struct {
			ListContactAccountSuggestions []struct {
				AddressBookID string `json:"addressBookId"`
				Contact       struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"contact"`
				Account accountView `json:"account"`
			} `json:"ListContactAccountSuggestions"`
		}
		if err := operations.Execute(ctx, documentBookSuggestions, nil, &suggestions); err == nil && len(suggestions.ListContactAccountSuggestions) > 0 {
			offered := make([]map[string]any, 0, maximumSuggestionsShown)
			for _, suggestion := range suggestions.ListContactAccountSuggestions {
				if len(offered) == maximumSuggestionsShown {
					break
				}
				// The book is what link-account takes with the contact, and
				// is named when there is more than one to tell apart.
				offer := map[string]any{
					"contact_id": suggestion.Contact.ID, "contact_name": suggestion.Contact.Name,
					"person": suggestion.Account.Username, "address_book_id": suggestion.AddressBookID,
				}
				if len(books.ListAddressBooks) > 1 {
					for _, candidate := range books.ListAddressBooks {
						if candidate.ID == suggestion.AddressBookID {
							offer["address_book_name"] = candidate.Name
						}
					}
				}
				offered = append(offered, offer)
			}
			answered["suggested_links"] = offered
			answered["suggestions"] = fmt.Sprintf("%d contacts look like people in the person's circles; ask them, and confirm with link-account",
				len(suggestions.ListContactAccountSuggestions))
		}
		result, err := tools.JSONResult(answered)
		if err != nil {
			return nil, err
		}
		result.Note = fmt.Sprintf("read the address book (%d)", len(answer.ListContacts))
		return result, nil

	case "get":
		if strings.TrimSpace(arguments.ID) == "" {
			return nil, fmt.Errorf("which contact: list gives their ids")
		}
		var answer struct {
			GetContact *contactView `json:"GetContact"`
		}
		if err := operations.Execute(ctx, documentBookContact, map[string]any{"id": arguments.ID}, &answer); err != nil {
			return nil, err
		}
		if answer.GetContact == nil || !givenBookIds[answer.GetContact.AddressBookID] {
			return nil, errNotInGivenBook
		}
		return tools.JSONResult(answer.GetContact)

	case "save":
		if strings.TrimSpace(arguments.ID) == "" &&
			strings.TrimSpace(arguments.Name) == "" && len(arguments.Emails) == 0 {
			return nil, fmt.Errorf("a new contact needs at least a name or an email address")
		}
		// Only what was given. A field left out is left as it is, so that
		// correcting a name does not throw away the rest of the card.
		variables := map[string]any{
			"addressBookId": book.ID, "id": emptyToNil(arguments.ID),
			"name": nil, "organization": nil, "title": nil,
			"emails": nil, "phones": nil, "note": nil,
		}
		if given["name"] {
			variables["name"] = arguments.Name
		}
		if given["organization"] {
			variables["organization"] = arguments.Organization
		}
		if given["title"] {
			variables["title"] = arguments.Title
		}
		if given["note"] {
			variables["note"] = arguments.Note
		}
		if given["emails"] {
			variables["emails"] = arguments.Emails
		}
		if given["phones"] {
			variables["phones"] = arguments.Phones
		}
		var answer struct {
			SaveContact *contactView `json:"SaveContact"`
		}
		if err := operations.Execute(ctx, documentBookSave, variables, &answer); err != nil {
			return nil, err
		}
		result, err := tools.JSONResult(answer.SaveContact)
		if err != nil {
			return nil, err
		}
		result.Note = "kept a contact"
		return result, nil

	case "link-account":
		contactId := strings.TrimSpace(firstNonEmpty(arguments.ContactID, arguments.ID))
		if contactId == "" {
			return nil, fmt.Errorf("which contact: list gives their ids")
		}
		accountUserId := strings.TrimSpace(arguments.AccountUserID)
		if accountUserId == "" {
			username := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(arguments.Person), "@"))
			if username == "" {
				return nil, fmt.Errorf("which person: give the username of somebody in their circles")
			}
			var people struct {
				ListCirclePeople []accountView `json:"ListCirclePeople"`
			}
			if err := operations.Execute(ctx, documentBookPeople, nil, &people); err != nil {
				return nil, err
			}
			for _, person := range people.ListCirclePeople {
				if strings.EqualFold(person.Username, username) {
					accountUserId = person.UserID
				}
			}
			if accountUserId == "" {
				return nil, fmt.Errorf("nobody in their circles has the username %q", username)
			}
		}
		var answer struct {
			SetContactAccount *contactView `json:"SetContactAccount"`
		}
		if err := operations.Execute(ctx, documentBookLink, map[string]any{
			"addressBookId": book.ID, "contactId": contactId, "accountUserId": accountUserId,
		}, &answer); err != nil {
			return nil, err
		}
		result, err := tools.JSONResult(answer.SetContactAccount)
		if err != nil {
			return nil, err
		}
		result.Note = "linked a contact to a person in the circles"
		return result, nil

	case "unlink-account":
		contactId := strings.TrimSpace(firstNonEmpty(arguments.ContactID, arguments.ID))
		if contactId == "" {
			return nil, fmt.Errorf("which contact: list gives their ids")
		}
		var answer struct {
			SetContactAccount *contactView `json:"SetContactAccount"`
		}
		if err := operations.Execute(ctx, documentBookLink, map[string]any{
			"addressBookId": book.ID, "contactId": contactId, "accountUserId": nil,
		}, &answer); err != nil {
			return nil, err
		}
		result := tools.TextResult("this contact is no longer linked to a person in their circles; the contact is as it was")
		result.Note = "unlinked a contact from a person in the circles"
		return result, nil

	case "remove":
		if strings.TrimSpace(arguments.ID) == "" {
			return nil, fmt.Errorf("which contact: list gives their ids")
		}
		if err := requireContactInBook(ctx, operations, givenBookIds, arguments.ID); err != nil {
			return nil, err
		}
		var answer struct {
			DeleteContact bool `json:"DeleteContact"`
		}
		if err := operations.Execute(ctx, documentBookRemove, map[string]any{"id": arguments.ID}, &answer); err != nil {
			return nil, err
		}
		result := tools.TextResult("forgot that contact, here and on the person's devices")
		result.Note = "removed a contact"
		return result, nil

	case "remove-picture":
		if strings.TrimSpace(arguments.ID) == "" {
			return nil, fmt.Errorf("which contact: list gives their ids")
		}
		if err := requireContactInBook(ctx, operations, givenBookIds, arguments.ID); err != nil {
			return nil, err
		}
		var answer struct {
			RemoveContactPicture *contactView `json:"RemoveContactPicture"`
		}
		if err := operations.Execute(ctx, documentBookRemovePicture, map[string]any{"id": arguments.ID}, &answer); err != nil {
			return nil, err
		}
		result := tools.TextResult("took the picture off that contact; the rest of the contact is as it was")
		result.Note = "removed a contact's picture"
		return result, nil
	}
	return nil, fmt.Errorf("%q is not an action of contact_book", arguments.Action)
}

var errNotInGivenBook = fmt.Errorf("that contact is not in an address book they have given you; list gives the ids")

// requireContactInBook refuses an action that names only a contact unless the
// contact is in a book they gave you. A contact is named within its book, so
// the same id may be in a book of the person's that is not given, or in a
// circle's, and the operation behind these actions would act on whichever it
// finds first.
func requireContactInBook(ctx context.Context, operations tools.Operations, givenBookIds map[string]bool, contactId string) error {
	var answer struct {
		GetContact *contactView `json:"GetContact"`
	}
	if err := operations.Execute(ctx, documentBookContactBook, map[string]any{"id": contactId}, &answer); err != nil {
		return err
	}
	if answer.GetContact == nil || !givenBookIds[answer.GetContact.AddressBookID] {
		return errNotInGivenBook
	}
	return nil
}

// maximumSuggestionsShown bounds the links list offers to the person at once.
const maximumSuggestionsShown = 10

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func emptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
