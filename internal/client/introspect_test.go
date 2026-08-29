package client

import (
	"strings"
	"testing"
)

// newTestSchema builds a schema by hand, standing in for what the server
// reports, so the query builder can be tested without one.
func newTestSchema() *Schema {
	scalar := func(name string) *TypeRef { return &TypeRef{Kind: "SCALAR", Name: name} }
	required := func(inner *TypeRef) *TypeRef { return &TypeRef{Kind: "NON_NULL", OfType: inner} }
	object := func(name string) *TypeRef { return &TypeRef{Kind: "OBJECT", Name: name} }
	list := func(inner *TypeRef) *TypeRef { return &TypeRef{Kind: "LIST", OfType: inner} }

	schema := &Schema{
		QueryType:    "RootQuery",
		MutationType: "RootMutation",
		Operations:   map[string]*Operation{},
		Types: map[string]*Type{
			"String":  {Name: "String", Kind: "SCALAR"},
			"Int":     {Name: "Int", Kind: "SCALAR"},
			"Boolean": {Name: "Boolean", Kind: "SCALAR"},
			"Domain": {Name: "Domain", Kind: "OBJECT", Fields: []*Field{
				{Name: "id", Type: required(scalar("String"))},
				{Name: "domain", Type: scalar("String")},
				{Name: "credentials", Type: list(object("Credential"))},
				// A field taking a required argument is a query of its own and
				// must not appear in a generated selection.
				{Name: "aliasFor", Type: object("Alias"), Arguments: []*Argument{
					{Name: "address", Type: required(scalar("String"))},
				}},
			}},
			"Credential": {Name: "Credential", Kind: "OBJECT", Fields: []*Field{
				{Name: "id", Type: scalar("String")},
				// Cyclic: a credential points back at its domain.
				{Name: "domain", Type: object("Domain")},
			}},
			"Alias": {Name: "Alias", Kind: "OBJECT", Fields: []*Field{
				{Name: "id", Type: scalar("String")},
			}},
			"DomainParametersInput": {Name: "DomainParametersInput", Kind: "INPUT_OBJECT", InputFields: []*Argument{
				{Name: "domain", Type: required(scalar("String"))},
			}},
		},
	}
	schema.Operations["ListDomains"] = &Operation{
		Name: "ListDomains", Kind: "query", Type: list(object("Domain")),
	}
	schema.Operations["GetDomain"] = &Operation{
		Name: "GetDomain", Kind: "query", Type: object("Domain"),
		Arguments: []*Argument{{Name: "domainId", Type: required(scalar("String"))}},
	}
	schema.Operations["CreateDomain"] = &Operation{
		Name: "CreateDomain", Kind: "mutation", Type: object("Domain"),
		Arguments: []*Argument{
			{Name: "domainParameters", Type: &TypeRef{Kind: "INPUT_OBJECT", Name: "DomainParametersInput"}},
		},
	}
	return schema
}

func TestBuildQuery(t *testing.T) {
	schema := newTestSchema()

	tests := []struct {
		name      string
		operation string
		arguments map[string]any
		depth     int
		want      string
	}{
		{
			name: "no arguments", operation: "ListDomains", depth: 1,
			want: "query { ListDomains { id domain } }",
		},
		{
			name: "nested to the requested depth", operation: "ListDomains", depth: 2,
			want: "query { ListDomains { id domain credentials { id } } }",
		},
		{
			name:      "an argument becomes a declared variable",
			operation: "GetDomain", arguments: map[string]any{"domainId": "abc"}, depth: 1,
			want: "query ($domainId: String!) { GetDomain(domainId: $domainId) { id domain } }",
		},
		{
			name:      "an input object is passed as a variable, not a literal",
			operation: "CreateDomain",
			arguments: map[string]any{"domainParameters": map[string]any{"domain": "example.com"}}, depth: 1,
			want: "mutation ($domainParameters: DomainParametersInput) { CreateDomain(domainParameters: $domainParameters) { id domain } }",
		},
		{
			name: "depth zero asks for no fields", operation: "ListDomains", depth: 0,
			want: "query { ListDomains }",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, err := schema.BuildQuery(schema.Operations[test.operation], test.arguments, test.depth)
			if err != nil {
				t.Fatalf("BuildQuery: %s", err)
			}
			if query != test.want {
				t.Errorf("query is\n  %s\nwant\n  %s", query, test.want)
			}
		})
	}
}

// TestBuildQueryTerminatesOnACyclicSchema is the reason the selection is
// depth bounded: Domain has Credentials, and a Credential points back at its
// Domain, so an unbounded walk would not finish.
func TestBuildQueryTerminatesOnACyclicSchema(t *testing.T) {
	schema := newTestSchema()
	query, err := schema.BuildQuery(schema.Operations["ListDomains"], nil, 4)
	if err != nil {
		t.Fatalf("BuildQuery: %s", err)
	}
	if strings.Count(query, "credentials") != 2 {
		t.Errorf("expected the cycle to be followed exactly to the depth given, got %s", query)
	}
}

func TestBuildQueryRejectsAMissingRequiredArgument(t *testing.T) {
	schema := newTestSchema()
	_, err := schema.BuildQuery(schema.Operations["GetDomain"], nil, 1)
	if err == nil {
		t.Fatal("expected an error when a required argument is missing")
	}
	if !strings.Contains(err.Error(), "domainId") {
		t.Errorf("the error should name the argument, got %q", err)
	}
}

func TestBuildQueryRejectsAnUnknownArgument(t *testing.T) {
	schema := newTestSchema()
	_, err := schema.BuildQuery(schema.Operations["ListDomains"], map[string]any{"nonsense": 1}, 1)
	if err == nil {
		t.Fatal("expected an error for an argument the operation does not take")
	}
}

func TestFindRecordIgnoresTheTrailingDot(t *testing.T) {
	records := &RecordSet{Records: []*Record{
		{Type: "TXT", Name: "teanode1._domainkey.example.com.", Expected: "v=DKIM1; p=..."},
	}}

	// The server reports fully qualified names; a caller building one from a
	// selector and a domain will not have written the dot.
	if record := records.FindRecord("TXT", "teanode1._domainkey.example.com"); record == nil {
		t.Error("a name without the trailing dot should match one with it")
	}
	if record := records.FindRecord("TXT", "teanode1._domainkey.example.com."); record == nil {
		t.Error("a name with the trailing dot should match itself")
	}
	if record := records.FindRecord("A", "teanode1._domainkey.example.com"); record != nil {
		t.Error("a different record type should not match")
	}
}
