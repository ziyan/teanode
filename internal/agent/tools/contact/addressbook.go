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
// Distinct from contact_search above, which is the addresses a mailbox has
// seen go past. Both are offered because they answer different questions:
// "who do I know" and "who has written to me".

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "contact_book", Family: tools.FamilyMailbox, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionContactsUse},
				Description: "The person's own address book: the contacts they keep, which their phone and computer synchronize over CardDAV. Use list to see them or to search by name, organization, address or number; get for one contact with everything on it; save to keep a new one or change one that is kept; remove to forget one. This is not the same as contact_search, which is the addresses a mailbox has corresponded with and which nobody chose.",
				Parameters: tools.Object(map[string]any{
					"action":       tools.EnumProperty("what to do", "list", "get", "save", "remove"),
					"query":        tools.StringProperty("for list: narrow to contacts whose name, organization, address or number matches"),
					"id":           tools.StringProperty("for get, remove, and for changing one that is kept: the contact's id, as list gives it"),
					"name":         tools.StringProperty("for save: what to call them"),
					"organization": tools.StringProperty("for save: where they work"),
					"title":        tools.StringProperty("for save: what they do there"),
					"emails":       tools.ArrayProperty("for save: their email addresses, replacing what is there", tools.StringProperty("an address")),
					"phones":       tools.ArrayProperty("for save: their telephone numbers, replacing what is there", tools.StringProperty("a number")),
					"note":         tools.StringProperty("for save: anything else worth keeping about them"),
				}, "action"),
				Guidance: "contact_book: contact_search finds the addresses a mailbox has corresponded with; this keeps people. To promote one -- \"save that sender to my contacts\" -- search for them there and save them here with their name and address; there is no separate action for it, because a contact kept from a learned address is an ordinary contact. Saving without an id keeps a new contact; saving with one changes that contact, and anything you leave out is left as it was, so correcting a name does not throw away the address. A contact is stored as the card a phone would send, so a photograph or a birthday put there by a device survives an edit made here. Removing one removes it from the person's devices too, so say who it is and wait to be told to go ahead.",
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
					case "remove":
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
}

const (
	documentBooks = `query { ListAddressBooks { id name contacts } }`

	documentBookContacts = `query ($addressBookId: String!, $query: String, $first: Int) {
  ListContacts(addressBookId: $addressBookId, query: $query, first: $first) {
    id name organization emails phones
  }
}`

	documentBookContact = `query ($id: String!) {
  GetContact(id: $id) { id name organization emails phones note }
}`

	documentBookSave = `mutation ($addressBookId: String!, $id: String, $name: String, $organization: String,
    $title: String, $emails: [String!], $phones: [String!], $note: String) {
  SaveContact(addressBookId: $addressBookId, id: $id, name: $name, organization: $organization,
    title: $title, emails: $emails, phones: $phones, note: $note) { id name emails phones }
}`

	documentBookRemove = `mutation ($id: String!) { DeleteContact(id: $id) }`
)

type bookView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Contacts int    `json:"contacts"`
}

type contactView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Emails       []string `json:"emails,omitempty"`
	Phones       []string `json:"phones,omitempty"`
	Note         string   `json:"note,omitempty"`
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
	book := books.ListAddressBooks[0]

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
		result, err := tools.JSONResult(map[string]any{
			"contacts": answer.ListContacts, "kept": len(answer.ListContacts),
		})
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

	case "remove":
		if strings.TrimSpace(arguments.ID) == "" {
			return nil, fmt.Errorf("which contact: list gives their ids")
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
	}
	return nil, fmt.Errorf("%q is not an action of contact_book", arguments.Action)
}

func emptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
