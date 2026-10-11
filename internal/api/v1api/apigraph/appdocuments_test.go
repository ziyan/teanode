package apigraph

import (
	"os"
	"path/filepath"
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

// appDocumentsDirectory is where the app's documents live, beside the calls
// that send them, relative to this package.
const appDocumentsDirectory = "../../../../ios/TeaNodeKit/Sources/TeaNodeKit"

// appDocumentPattern is one document: a static string in Swift's multi-line
// form, `static let name = """` to the closing `"""`.
var appDocumentPattern = regexp.MustCompile(`(?s)static let (\w+) = """\n(.*?)\n\s*"""`)

var appSharedFieldsPattern = regexp.MustCompile(`\\\((\w+)\)`)

func TestTheAppDocumentsMatchTheSchema(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(appDocumentsDirectory, "*.swift"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("cannot find the app's sources in %s: %v", appDocumentsDirectory, err)
	}
	var found [][]string
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read %s: %s", path, err)
		}
		documents := appDocumentPattern.FindAllStringSubmatch(string(source), -1)
		// Every static multi-line string is a document, so one this
		// pattern cannot read fails here rather than being skipped.
		if strings.Count(string(source), `= """`) != len(documents) {
			t.Fatalf("read %d documents from %s, which has %d", len(documents), path, strings.Count(string(source), `= """`))
		}
		found = append(found, documents...)
	}
	if len(found) < 12 {
		t.Fatalf("read only %d documents", len(found))
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

	// A string that is not an operation is a list of fields the documents
	// share, written into them as \(name).
	fields := map[string]string{}
	var operations [][]string
	for _, match := range found {
		trimmed := strings.TrimSpace(match[2])
		if strings.HasPrefix(trimmed, "query") || strings.HasPrefix(trimmed, "mutation") ||
			strings.HasPrefix(trimmed, "subscription") || strings.HasPrefix(trimmed, "{") {
			operations = append(operations, match)
		} else {
			fields[match[1]] = match[2]
		}
	}
	for _, match := range operations {
		name, text := match[1], appSharedFieldsPattern.ReplaceAllStringFunc(match[2], func(written string) string {
			if shared, isKnown := fields[written[2:len(written)-1]]; isKnown {
				return shared
			}
			return written
		})
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
