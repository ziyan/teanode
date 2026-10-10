package apigraph

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"

	"github.com/ziyan/teanode/internal/util/graphapi"
)

// The phone app's GraphQL is written in Swift (ios/TeaNodeKit), and an app
// already installed keeps sending what it was built with. A server change that
// renames a field the app reads breaks every phone until it updates, and
// nothing on the server would notice. This reads the app's documents out of
// its source and validates them against the schema the server builds, the way
// TestTheClientQueriesMatchTheSchema does for the command line.

// appDocumentsPath is the Swift file the app's documents live in, relative to
// this package.
const appDocumentsPath = "../../../../ios/TeaNodeKit/Sources/TeaNodeKit/Operations.swift"

// appDocumentPattern is one document: a static string in Swift's multi-line
// form, `static let name = """` to the closing `"""`.
var appDocumentPattern = regexp.MustCompile(`(?s)static let (\w+) = """\n(.*?)\n\s*"""`)

func TestTheAppDocumentsMatchTheSchema(t *testing.T) {
	source, err := os.ReadFile(appDocumentsPath)
	if err != nil {
		t.Fatalf("cannot read the app's documents: %s", err)
	}
	found := appDocumentPattern.FindAllStringSubmatch(string(source), -1)
	// Every multi-line string in the file is a document, so one this
	// pattern cannot read fails here rather than being skipped.
	if opened := strings.Count(string(source), `= """`); len(found) == 0 || len(found) != opened {
		t.Fatalf("read %d documents from %s, which has %d", len(found), appDocumentsPath, opened)
	}

	self := &graph{}
	graphApi := graphapi.New()
	var query Query = self
	var mutation Mutation = self
	var subscription Subscription = self
	if err := graphApi.Register(&query, &mutation, &subscription); err != nil {
		t.Fatalf("cannot register the schema: %s", err)
	}
	schema, err := graphApi.Build()
	if err != nil {
		t.Fatalf("cannot build the schema: %s", err)
	}

	for _, match := range found {
		name, text := match[1], match[2]
		document, err := gqlparser.Parse(gqlparser.ParseParams{Source: text})
		if err != nil {
			t.Errorf("%s: does not parse: %s\n%s", name, err, text)
			continue
		}
		result := graphql.ValidateDocument(&schema, document, nil)
		if result.IsValid {
			continue
		}
		messages := make([]string, 0, len(result.Errors))
		for _, failure := range result.Errors {
			messages = append(messages, failure.Message)
		}
		t.Errorf("the app's %s does not match the schema: %s\n%s", name, strings.Join(messages, "; "), text)
	}
}
