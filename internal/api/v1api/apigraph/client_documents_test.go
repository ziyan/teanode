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
