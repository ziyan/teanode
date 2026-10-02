package graphapi_test

import (
	"context"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/util/graphapi"
)

// Totals is what a query returns here: one number too big for 32 bits and
// one small enough, so the test tells a width problem apart from a
// generator problem.
type Totals struct {
	PromptTokens int64 `json:"promptTokens"`
	Calls        int   `json:"calls"`
}

// TotalsArguments carries the floor back so the generator has to translate
// an int64 on the way in as well as on the way out.
type TotalsArguments struct {
	Floor int64 `json:"floor"`
	First int   `json:"first"`
}

// Listing says back which of its fields the query selected, at its own
// level and under its rows, so the test reads what the resolver saw.
type Listing struct {
	SelectedFieldNames    string        `json:"selectedFieldNames"`
	SelectedRowFieldNames string        `json:"selectedRowFieldNames"`
	TotalCount            int           `json:"totalCount"`
	Rows                  []*ListingRow `json:"rows"`
}

type ListingRow struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type testQuery interface {
	Totals(ctx context.Context, arguments TotalsArguments) (*Totals, error)
	Listing(ctx context.Context) (*Listing, error)
}

// A schema with no mutations at all is not one the library will build, so
// there is one here even though nothing in this file calls it.
type testMutation interface {
	Forget(ctx context.Context) error
}

type testResolver struct{}

func (self *testResolver) Totals(ctx context.Context, arguments TotalsArguments) (*Totals, error) {
	return &Totals{PromptTokens: arguments.Floor + int64(arguments.First), Calls: arguments.First}, nil
}

func (self *testResolver) Listing(ctx context.Context) (*Listing, error) {
	said := func(fieldNames []string, isKnown bool) string {
		if !isKnown {
			return "unknown"
		}
		return strings.Join(fieldNames, " ")
	}
	return &Listing{
		SelectedFieldNames:    said(graphapi.SelectedFieldNames(ctx)),
		SelectedRowFieldNames: said(graphapi.SelectedFieldNames(ctx, "rows")),
		Rows:                  []*ListingRow{{ID: "row-one", Label: "Invented row"}},
	}, nil
}

func (self *testResolver) Forget(ctx context.Context) error {
	return nil
}

var (
	_ testQuery    = &testResolver{}
	_ testMutation = &testResolver{}
)

func buildTestSchema(t *testing.T) graphql.Schema {
	t.Helper()
	api := graphapi.New()
	var query testQuery = &testResolver{}
	var mutation testMutation = &testResolver{}
	if err := api.Register(&query, &mutation, nil); err != nil {
		t.Fatalf("the schema does not register: %s", err)
	}
	schema, err := api.Build()
	if err != nil {
		t.Fatalf("the schema does not build: %s", err)
	}
	return schema
}

// An int64 field is not nullable, and the library serializes a GraphQL Int
// through int32, so before this scalar existed a total past 2147483647 came
// back as "Cannot return null for non-nullable field" and took the whole
// query with it. Anything under the old ceiling always worked, which is why
// the number here is over it.
func TestInt64SurvivesPastTheInt32Ceiling(t *testing.T) {
	t.Parallel()

	schema := buildTestSchema(t)
	result := graphql.Do(graphql.Params{
		Context:        context.Background(),
		Schema:         schema,
		RequestString:  `query ($floor: Int64!, $first: Int!) { Totals(floor: $floor, first: $first) { promptTokens calls } }`,
		VariableValues: map[string]interface{}{"floor": 4000000000, "first": 3},
	})
	if len(result.Errors) > 0 {
		t.Fatalf("the query failed: %v", result.Errors)
	}
	totals := result.Data.(map[string]interface{})["Totals"].(map[string]interface{})
	if totals["promptTokens"] != int64(4000000003) {
		t.Errorf("promptTokens came back as %#v, wanted 4000000003", totals["promptTokens"])
	}
	if totals["calls"] != 3 {
		t.Errorf("calls came back as %#v, wanted 3", totals["calls"])
	}
}

// A literal in the document goes through a different path in the library
// than a variable does, and the two are easy to get one of right.
func TestInt64ReadsALiteral(t *testing.T) {
	t.Parallel()

	schema := buildTestSchema(t)
	result := graphql.Do(graphql.Params{
		Context:       context.Background(),
		Schema:        schema,
		RequestString: `{ Totals(floor: 9000000000, first: 1) { promptTokens } }`,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("the query failed: %v", result.Errors)
	}
	totals := result.Data.(map[string]interface{})["Totals"].(map[string]interface{})
	if totals["promptTokens"] != int64(9000000001) {
		t.Errorf("promptTokens came back as %#v, wanted 9000000001", totals["promptTokens"])
	}
}

// Limits and offsets stay Int. Every query the dashboard and the command
// line already send declares its variables as Int, and a variable only
// validates against the type the argument is declared with, so widening
// them would have failed every one of those queries.
func TestSmallNumbersAreStillInt(t *testing.T) {
	t.Parallel()

	schema := buildTestSchema(t)
	arguments := schema.QueryType().Fields()["Totals"].Args
	for _, argument := range arguments {
		wanted := "Int!"
		if argument.Name() == "floor" {
			wanted = "Int64!"
		}
		if argument.Type.String() != wanted {
			t.Errorf("argument %q is %s, wanted %s", argument.Name(), argument.Type, wanted)
		}
	}
}

// A resolver can leave out work for a field nobody asked for: the rows'
// fields and its own are said by name, and a fragment, whose fields are
// not followed, says it cannot tell rather than that nothing was asked.
func TestSelectedFieldNamesSaysWhatTheQueryAsked(t *testing.T) {
	t.Parallel()

	schema := buildTestSchema(t)
	listing := func(requestString string) map[string]interface{} {
		t.Helper()
		result := graphql.Do(graphql.Params{Context: context.Background(), Schema: schema, RequestString: requestString})
		if len(result.Errors) > 0 {
			t.Fatalf("the query failed: %v", result.Errors)
		}
		return result.Data.(map[string]interface{})["Listing"].(map[string]interface{})
	}

	asked := listing(`{ Listing { selectedFieldNames selectedRowFieldNames rows { id } } }`)
	if asked["selectedFieldNames"] != "selectedFieldNames selectedRowFieldNames rows" {
		t.Errorf("the listing's own fields came back as %q", asked["selectedFieldNames"])
	}
	if asked["selectedRowFieldNames"] != "id" {
		t.Errorf("the rows' fields came back as %q, wanted only id", asked["selectedRowFieldNames"])
	}

	withoutRows := listing(`{ Listing { selectedRowFieldNames totalCount } }`)
	if withoutRows["selectedRowFieldNames"] != "" {
		t.Errorf("rows not asked for came back as %q, wanted nothing", withoutRows["selectedRowFieldNames"])
	}

	throughFragment := listing(`{ Listing { selectedFieldNames selectedRowFieldNames ... on Listing { totalCount } } }`)
	if throughFragment["selectedFieldNames"] != "unknown" || throughFragment["selectedRowFieldNames"] != "unknown" {
		t.Errorf("a fragment came back as %q and %q, wanted unknown", throughFragment["selectedFieldNames"], throughFragment["selectedRowFieldNames"])
	}

	if fieldNames, isKnown := graphapi.SelectedFieldNames(context.Background()); isKnown || fieldNames != nil {
		t.Errorf("outside a request it said %v, wanted unknown", fieldNames)
	}
}
