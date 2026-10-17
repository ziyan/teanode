package access

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A tap is the dashboard's session or the phone app, from the person
// themselves: not a command line token, not the console, not the agent's own
// operations (which carry no credential), and not an operator signed in as the
// person.
func TestOnlyASessionOrTheAppFromThePersonIsATap(t *testing.T) {
	person := &models.User{ID: "person"}
	operator := &models.User{ID: "operator"}
	for name, test := range map[string]struct {
		principal *Principal
		expected  bool
	}{
		"the dashboard's session":   {&Principal{User: person, CredentialKind: models.CredentialSession}, true},
		"the phone app":             {&Principal{User: person, CredentialKind: models.CredentialApp}, true},
		"a command line token":      {&Principal{User: person, CredentialKind: models.CredentialToken}, false},
		"no credential":             {&Principal{User: person}, false},
		"the console":               {&Principal{Console: true, CredentialKind: models.CredentialSession}, false},
		"an operator as the person": {&Principal{User: person, Impersonator: operator, CredentialKind: models.CredentialSession}, false},
		"nobody signed in":          {nil, false},
		"a kind this code made up":  {&Principal{User: person, CredentialKind: "other"}, false},
	} {
		if got := test.principal.IsPersonsTap(); got != test.expected {
			t.Errorf("%s: %v, expected %v", name, got, test.expected)
		}
	}
}
