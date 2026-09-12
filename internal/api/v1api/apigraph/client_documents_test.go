package apigraph

import (
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
)

// The command line's queries are written by hand, and the schema is derived
// from Go types by reflection. Nothing connected the two, so renaming a
// field or an argument on the server broke a command silently, in somebody's
// terminal rather than here. Every document the client sends is validated
// against the schema the server builds.
//
// A document is added to this list by name, so that writing one and
// forgetting to check it is a failure of this test rather than a silence.
func TestClientDocumentsMatchTheSchema(test *testing.T) {
	test.Parallel()

	component, err := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, &api.Settings{})
	if err != nil {
		test.Fatalf("the schema does not build: %s", err)
	}
	schema := component.(*graph).schema

	documents := map[string]string{
		"ListMailboxes":            client.DocumentListMailboxes,
		"ListAllMailboxes":         client.DocumentListAllMailboxes,
		"CreateMailboxFolder":      client.DocumentCreateMailboxFolder,
		"UpdateMailboxFolder":      client.DocumentUpdateMailboxFolder,
		"SetMailboxFolderPinned":   client.DocumentSetMailboxFolderPinned,
		"DeleteMailboxFolder":      client.DocumentDeleteMailboxFolder,
		"UpdateMailbox":            client.DocumentUpdateMailbox,
		"TestMailboxRules":         client.DocumentTestMailboxRules,
		"ApplyMailboxRules":        client.DocumentApplyMailboxRules,
		"ListAddressBooks":         client.DocumentListAddressBooks,
		"ListContacts":             client.DocumentListContacts,
		"GetContact":               client.DocumentGetContact,
		"SaveContact":              client.DocumentSaveContact,
		"DeleteContact":            client.DocumentDeleteContact,
		"ListMailboxContacts":      client.DocumentListMailboxContacts,
		"SaveMailboxContact":       client.DocumentSaveMailboxContact,
		"DeleteMailboxContact":     client.DocumentDeleteMailboxContact,
		"ListMailboxAppPasswords":  client.DocumentListMailboxAppPasswords,
		"CreateMailboxAppPassword": client.DocumentCreateMailboxAppPassword,
		"DeleteMailboxAppPassword": client.DocumentDeleteMailboxAppPassword,
		"GetMailProgramSettings":   client.DocumentGetMailProgramSettings,
		"ListGroups":               client.DocumentListGroups,
		"CreateGroup":              client.DocumentCreateGroup,
		"UpdateGroup":              client.DocumentUpdateGroup,
		"DeleteGroup":              client.DocumentDeleteGroup,
		"ListRoles":                client.DocumentListRoles,
		"ListPermissions":          client.DocumentListPermissions,
		"CreateRole":               client.DocumentCreateRole,
		"UpdateRole":               client.DocumentUpdateRole,
		"DeleteRole":               client.DocumentDeleteRole,
		"ListAuditEvents":          client.DocumentListAuditEvents,
	}

	for name, document := range documents {
		parsed, err := parser.Parse(parser.ParseParams{Source: source.NewSource(&source.Source{Body: []byte(document), Name: name})})
		if err != nil {
			test.Errorf("%s does not parse: %s", name, err)
			continue
		}
		validation := graphql.ValidateDocument(&schema, parsed, nil)
		if validation.IsValid {
			continue
		}
		for _, failure := range validation.Errors {
			test.Errorf("%s is not valid against the schema: %s", name, failure.Message)
		}
	}
}

// The dashboard's own documents are TypeScript and cannot be checked the way
// the client's are, so the names they depend on are checked here instead.
//
// This exists because of a real failure: an input type declared in Go as
// AddressInput reached the schema as AddressInputInput -- the generator
// appends "Input" to an input type's name -- and every document that named
// AddressInput was refused. Nothing caught it, because the only documents
// naming it were in a .tsx file, and saving a contact from the dashboard was
// broken until somebody pressed the button.
func TestTheSchemaHasWhatTheDashboardNames(test *testing.T) {
	test.Parallel()

	component, err := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, &api.Settings{})
	if err != nil {
		test.Fatalf("the schema does not build: %s", err)
	}
	schema := component.(*graph).schema

	// One document per shape the contacts pages send, written as they write
	// it. A name that stops existing fails here rather than in a browser.
	for name, document := range map[string]string{
		"the address books": `query { ListAddressBooks { id name description contacts } }`,
		"the contact list": `query ($addressBookId: String!, $query: String, $first: Int) {
  ListContacts(addressBookId: $addressBookId, query: $query, first: $first) {
    id uid name organization emails phones hasPhoto addresses { written }
  }
}`,
		"one contact": `query ($id: String!) {
  GetContact(id: $id) {
    id name organization emails phones note hasPhoto
    addresses { street locality region postalCode country }
  }
}`,
		"saving a contact": `mutation ($addressBookId: String!, $id: String, $name: String, $organization: String,
          $emails: [String!], $phones: [String!], $note: String, $addresses: [AddressInput!]) {
  SaveContact(addressBookId: $addressBookId, id: $id, name: $name, organization: $organization,
              emails: $emails, phones: $phones, note: $note, addresses: $addresses) { id name }
}`,
		"forgetting one": `mutation ($id: String!) { DeleteContact(id: $id) }`,
	} {
		parsed, err := parser.Parse(parser.ParseParams{
			Source: source.NewSource(&source.Source{Body: []byte(document), Name: name}),
		})
		if err != nil {
			test.Errorf("the document for %s does not parse: %s", name, err)
			continue
		}
		validation := graphql.ValidateDocument(&schema, parsed, nil)
		for _, failure := range validation.Errors {
			test.Errorf("the document for %s does not match the schema: %s", name, failure.Message)
		}
	}
}
