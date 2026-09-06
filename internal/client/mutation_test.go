package client

import "testing"

func TestIsMutationDocument(t *testing.T) {
	mutations := []string{
		`mutation { DeleteDomain(domainId: "x") }`,
		`mutation ($domainId: String!) { DeleteDomain(domainId: $domainId) }`,
		"  \n\tmutation Named { DeleteDomain(domainId: \"x\") }",
		// A query first, then a mutation: the document changes something.
		`query { ListDomains { id } } mutation { DeleteDomain(domainId: "x") }`,
		// A comment cannot hide the keyword.
		"# nothing here\nmutation { DeleteDomain(domainId: \"x\") }",
		// Nor can a string before it.
		`query ($name: String = "}") { ListDomains { id } } mutation { DeleteDomain(domainId: "x") }`,
	}
	for _, document := range mutations {
		if !IsMutationDocument(document) {
			t.Errorf("should be a mutation: %q", document)
		}
	}

	queries := []string{
		`query { ListDomains { id } }`,
		`{ ListDomains { id } }`,
		`query ($first: Int) { ListMails(pagination: {first: $first}) { id } }`,
		// A field called mutation inside a selection set is a field.
		`{ mutation { id } }`,
		`query { thing { mutation } }`,
		// The keyword inside a string or a comment is neither.
		`query { GetDomain(domainId: "mutation") { id } }`,
		"query { ListDomains { id } } # mutation { DeleteDomain }",
		`query ($note: String = """
			mutation { nothing }
		""") { ListDomains { id } }`,
		// Introspection, which every "teanode api" command starts with.
		`query { __schema { mutationType { name } } }`,
		``,
	}
	for _, document := range queries {
		if IsMutationDocument(document) {
			t.Errorf("should not be a mutation: %q", document)
		}
	}
}
