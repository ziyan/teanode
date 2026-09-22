package apigraph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/config"
)

func TestGraphPreparationRejectsBeforeDatabaseAccess(test *testing.T) {
	// No database or account is supplied: every transport must reject the
	// document without trying to resolve permissions or begin a transaction.
	graph := &graph{schema: buildSchemaForValidation(test)}
	for _, document := range []string{
		"{",
		"{ FieldThatDoesNotExist }",
		"query First { __typename } query Second { __typename }",
	} {
		test.Run(document, func(test *testing.T) {
			expected := graphql.Do(graphql.Params{Schema: graph.schema, RequestString: document})
			if len(expected.Errors) == 0 {
				test.Fatal("fixture must fail validation")
			}
			body, err := json.Marshal(graphRequest{Query: document})
			if err != nil {
				test.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", strings.NewReader(string(body)))
			response := httptest.NewRecorder()
			graph.graphView(response, request)
			if response.Code != http.StatusOK {
				test.Fatalf("status = %d, want GraphQL response", response.Code)
			}
			var actual graphql.Result
			if err := json.Unmarshal(response.Body.Bytes(), &actual); err != nil {
				test.Fatal(err)
			}
			encodedExpected, err := json.Marshal(expected)
			if err != nil {
				test.Fatal(err)
			}
			var expectedWire graphql.Result
			if err := json.Unmarshal(encodedExpected, &expectedWire); err != nil {
				test.Fatal(err)
			}
			if !reflect.DeepEqual(actual.Errors, expectedWire.Errors) {
				test.Fatalf("HTTP errors = %#v, want %#v", actual.Errors, expected.Errors)
			}
			operations := &agentOperations{graph: graph}
			if err := operations.Execute(context.Background(), document, nil, nil); err == nil || err.Error() != expected.Errors[0].Message {
				test.Fatalf("agent error = %v", err)
			}
			connection := &webSocketConnection{graph: graph}
			channel, err := connection.subscribe(context.Background(), &graphRequest{Query: document})
			if err != nil {
				test.Fatal(err)
			}
			encodedOutcome, err := json.Marshal(<-channel)
			if err != nil {
				test.Fatal(err)
			}
			if string(encodedOutcome) != string(encodedExpected) {
				test.Fatalf("subscription response = %s, want %s", encodedOutcome, encodedExpected)
			}
			if _, isOpen := <-channel; isOpen {
				test.Fatal("rejected subscription did not close")
			}
		})
	}
}

func TestPreparedGraphRequestPreservesExecutionArguments(test *testing.T) {
	graph := &graph{schema: buildSchemaForValidation(test)}
	request := &graphRequest{
		Query: `query Other { __typename }
			query Selected($name: String!) { __type(name: $name) { ...Identity } }
			fragment Identity on __Type { name }`,
		Variables:     map[string]interface{}{"name": graph.schema.QueryType().Name()},
		OperationName: "Selected",
	}
	prepared, rejected := graph.prepareGraphRequest(request)
	if rejected != nil {
		test.Fatalf("preparation failed: %v", rejected.Errors)
	}
	expected := graphql.Do(graphql.Params{
		Schema: graph.schema, RequestString: request.Query,
		OperationName: request.OperationName, VariableValues: request.Variables,
	})
	// Execution must depend on the prepared tree, not the original text.
	request.Query = "{"
	actual := graphql.Execute(prepared)
	if !reflect.DeepEqual(actual, expected) || len(actual.Errors) != 0 {
		test.Fatalf("prepared execution = %#v, want %#v", actual, expected)
	}
}

func TestGraphPreparationRejectsFragmentCycles(test *testing.T) {
	graph := &graph{schema: buildSchemaForValidation(test)}
	_, rejected := graph.prepareGraphRequest(&graphRequest{Query: "query { ...Loop } fragment Loop on Query { ...Loop }"})
	if rejected == nil || len(rejected.Errors) == 0 || !strings.Contains(rejected.Errors[0].Message, "Cannot spread fragment") {
		test.Fatalf("cycle rejection = %#v", rejected)
	}
}

func TestGraphPreparationBoundsDocumentWork(test *testing.T) {
	schema := buildSchemaForValidation(test)
	for _, fixture := range []struct {
		name         string
		document     string
		limits       config.GraphQL
		errorMessage string
	}{
		{"parser depth", strings.Repeat("{ field", 40) + strings.Repeat("}", 40), config.GraphQL{MaximumDepth: 32, MaximumTokenCount: 20000, MaximumSelectionCount: 5000}, "maximum depth"},
		{"tokens", "{ __typename __typename __typename }", config.GraphQL{MaximumDepth: 32, MaximumTokenCount: 3, MaximumSelectionCount: 5000}, "maximum token count"},
		{"expanded selections", "{ ...Fields ...Fields ...Fields } fragment Fields on RootQuery { __typename __typename }", config.GraphQL{MaximumDepth: 32, MaximumTokenCount: 20000, MaximumSelectionCount: 8}, "maximum expanded selection count"},
		{"expanded depth", "{ ...Outer } fragment Outer on RootQuery { ...Inner } fragment Inner on RootQuery { __typename }", config.GraphQL{MaximumDepth: 2, MaximumTokenCount: 20000, MaximumSelectionCount: 5000}, "maximum expanded depth"},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			configuration := config.Default()
			configuration.GraphQL = fixture.limits
			graph := &graph{schema: schema, config: config.NewMemoryStore(configuration)}
			_, rejected := graph.prepareGraphRequest(&graphRequest{Query: fixture.document})
			if rejected == nil || len(rejected.Errors) == 0 || !strings.Contains(rejected.Errors[0].Message, fixture.errorMessage) {
				test.Fatalf("rejection = %#v, want %q", rejected, fixture.errorMessage)
			}
		})
	}
}

func TestGraphPreparationDoesNotCountQuotedDelimiters(test *testing.T) {
	graph := &graph{schema: buildSchemaForValidation(test)}
	_, rejected := graph.prepareGraphRequest(&graphRequest{Query: `{ __type(name: "` + strings.Repeat("{", 100) + `") { name } } # {{{`})
	if rejected != nil {
		test.Fatalf("quoted delimiters rejected: %v", rejected.Errors)
	}
}

func TestGraphPreparationAllowsClientAndAgentDocuments(test *testing.T) {
	graph := &graph{schema: buildSchemaForValidation(test)}
	operations := append(readClientOperations(test), readAgentDocuments(test)...)
	for _, operation := range operations {
		if _, rejected := graph.prepareGraphRequest(&graphRequest{Query: operation.text}); rejected != nil {
			test.Errorf("%s: preparation rejected: %v", operation.where, rejected.Errors)
		}
	}
}
