package apigraph

import (
	"math"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"

	"github.com/ziyan/teanode/internal/config"
)

func TestGraphPageSizeBoundsIntegerConversion(test *testing.T) {
	schema := graphWorkSchema(test)
	field := schema.QueryType().Fields()["items"]
	selected := &ast.Field{Arguments: []*ast.Argument{{
		Name:  &ast.Name{Value: "first"},
		Value: &ast.Variable{Name: &ast.Name{Value: "size"}},
	}}}
	for _, pageSize := range []interface{}{
		int64(math.MaxInt32) + 1, float64(math.MaxInt32) + 1, int64(math.MaxInt64),
		math.NaN(), math.Inf(1), math.Inf(-1), 1.5, -1.5,
	} {
		if _, err := graphPageSize(field, selected, map[string]interface{}{"size": pageSize}, math.MaxInt); err == nil {
			test.Errorf("accepted invalid page %v", pageSize)
		}
	}
	if pageSize, err := graphPageSize(field, selected, map[string]interface{}{"size": int64(math.MaxInt32)}, math.MaxInt); err != nil || pageSize != math.MaxInt32 {
		test.Fatalf("maximum signed 32-bit page = %d, %v", pageSize, err)
	}
}

func graphWorkSchema(test *testing.T) graphql.Schema {
	test.Helper()
	var node *graphql.Object
	node = graphql.NewObject(graphql.ObjectConfig{Name: "Node", Fields: graphql.FieldsThunk(func() graphql.Fields {
		return graphql.Fields{
			"id": &graphql.Field{Type: graphql.String},
			"children": &graphql.Field{Type: graphql.NewList(node), Args: graphql.FieldConfigArgument{
				"first": &graphql.ArgumentConfig{Type: graphql.Int},
			}},
		}
	})})
	pagination := graphql.NewInputObject(graphql.InputObjectConfig{Name: "Pagination", Fields: graphql.InputObjectConfigFieldMap{
		"first": &graphql.InputObjectFieldConfig{Type: graphql.Int},
	}})
	schema, err := graphql.NewSchema(graphql.SchemaConfig{Query: graphql.NewObject(graphql.ObjectConfig{Name: "Query", Fields: graphql.Fields{
		"items": &graphql.Field{Type: graphql.NewList(node), Args: graphql.FieldConfigArgument{"first": &graphql.ArgumentConfig{Type: graphql.Int}}},
		"paged": &graphql.Field{Type: graphql.NewList(node), Args: graphql.FieldConfigArgument{"pagination": &graphql.ArgumentConfig{Type: pagination}}},
	}})})
	if err != nil {
		test.Fatal(err)
	}
	return schema
}

func TestGraphWorkBoundsPaginationAndExpansion(test *testing.T) {
	configuration := config.Default()
	configuration.GraphQL.MaximumWorkCount = 3000
	graph := &graph{schema: graphWorkSchema(test), config: config.NewMemoryStore(configuration)}
	for _, fixture := range []struct {
		name          string
		query         string
		variables     map[string]interface{}
		operationName string
		errorMessage  string
	}{
		{name: "literal size", query: `{ items(first: 1001) { id } }`, errorMessage: "maximum list item count"},
		{name: "variable size", query: `query($size: Int) { items(first: $size) { id } }`, variables: map[string]interface{}{"size": float64(1001)}, errorMessage: "maximum list item count"},
		{name: "default size", query: `query($size: Int = 1001) { items(first: $size) { id } }`, errorMessage: "maximum list item count"},
		{name: "object size", query: `{ paged(pagination: {first: 1001}) { id } }`, errorMessage: "maximum list item count"},
		{name: "object variable", query: `query($page: Pagination) { paged(pagination: $page) { id } }`, variables: map[string]interface{}{"page": map[string]interface{}{"first": float64(1001)}}, errorMessage: "maximum list item count"},
		{name: "object default", query: `query($page: Pagination = {first: 1001}) { paged(pagination: $page) { id } }`, errorMessage: "maximum list item count"},
		{name: "nested lists", query: `{ items(first: 50) { children(first: 50) { id } } }`, errorMessage: "maximum work count"},
		{name: "aliased pages", query: `{ left: items(first: 1000) { id } right: items(first: 1000) { id } }`, errorMessage: "maximum work count"},
		{name: "repeated fragments", query: `{ ...Page ...Page } fragment Page on Query { items(first: 1000) { id } }`, errorMessage: "maximum work count"},
		{name: "omitted sizes", query: `{ left: items { id } right: items { id } }`, errorMessage: "maximum work count"},
		{name: "zero sizes", query: `{ left: items(first: 0) { id } right: items(first: 0) { id } }`, errorMessage: "maximum work count"},
		{name: "null variable", query: `query($size: Int = 1) { left: items(first: $size) { id } right: items(first: $size) { id } }`, variables: map[string]interface{}{"size": nil}, errorMessage: "maximum work count"},
		{name: "default overridden", query: `query($size: Int = 1001) { items(first: $size) { id } }`, variables: map[string]interface{}{"size": float64(2)}},
		{name: "inline fragment", query: `{ items(first: 2) { ... on Node { children(first: 2) { id } } } }`},
		{name: "selected operation", query: `query Large { items(first: 1001) { id } } query Small { items(first: 1) { id } }`, operationName: "Small"},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			_, rejected := graph.prepareGraphRequest(&graphRequest{Query: fixture.query, Variables: fixture.variables, OperationName: fixture.operationName})
			if fixture.errorMessage == "" {
				if rejected != nil {
					test.Fatalf("unexpected rejection: %v", rejected.Errors)
				}
				return
			}
			if rejected == nil || len(rejected.Errors) == 0 || !strings.Contains(rejected.Errors[0].Message, fixture.errorMessage) {
				test.Fatalf("rejection = %#v, want %q", rejected, fixture.errorMessage)
			}
		})
	}
}
