package apigraph

import (
	"fmt"
	"reflect"

	"github.com/graphql-go/graphql"
)

// passwordsMutationsPerDocumentMost is how many Passwords mutations one GraphQL
// document may name. Every one of them takes the revision lock and the circle
// membership lock and keeps them until the whole request commits, and a
// document can name a mutation many times under aliases, so without a limit one
// request holds the locks every other change of Passwords waits for, for as
// long as it takes to run all of them. The clients never name more than a few:
// they send one mutation for each change, and a call that changes several
// items (AddPasswordShareKeys) takes them as one argument.
const passwordsMutationsPerDocumentMost = 25

// passwordsMutationNames are the names of the Passwords mutations, read from
// the interface that declares them so that a new one is counted without anybody
// remembering to add it here.
var passwordsMutationNames = func() map[string]bool {
	names := map[string]bool{}
	mutations := reflect.TypeOf((*PasswordsMutation)(nil)).Elem()
	for index := 0; index < mutations.NumMethod(); index++ {
		names[mutations.Method(index).Name] = true
	}
	return names
}()

// passwordsDocumentCounts counts, while a document is walked, the Passwords
// operations it names: what checkGraphWork cannot see as work, because the work
// is the person's whole Passwords and not a page.
type passwordsDocumentCounts struct {
	listings  int
	mutations int
}

// count adds one selected root field and refuses the document when it names
// ListPasswordItems more than once, which answers everything the person holds
// and is not paged (naming it many times under aliases would multiply the
// memory of one list), or more than passwordsMutationsPerDocumentMost Passwords
// mutations.
func (self *passwordsDocumentCounts) count(schema *graphql.Schema, parentType graphql.Type, fieldName string) error {
	switch {
	case parentType == graphql.Type(schema.QueryType()) && fieldName == "ListPasswordItems":
		self.listings++
		if self.listings > 1 {
			return fmt.Errorf("GraphQL operation may list Passwords items only once")
		}
	case parentType == graphql.Type(schema.MutationType()) && passwordsMutationNames[fieldName]:
		self.mutations++
		if self.mutations > passwordsMutationsPerDocumentMost {
			return fmt.Errorf("GraphQL operation may change Passwords at most %d times", passwordsMutationsPerDocumentMost)
		}
	}
	return nil
}
