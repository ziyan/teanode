package client

import (
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
)

// IsMutationDocument says whether a GraphQL document would change anything:
// whether any operation in it is a mutation.
//
// A read-only client decides with this what to refuse, so it has to be right
// about documents it did not write — "teanode api graphql" sends whatever it
// was given. The document is parsed with the same library the server parses
// it with, so the two cannot disagree about where a comment ends, what a
// byte order mark is, or whether a "mutation" is a keyword or a field: a
// hand-written tokenizer was tried first and got two of those wrong.
//
// A document that does not parse is reported as not a mutation, and is sent:
// the server, using the same parser, rejects it as a syntax error and
// executes nothing, and that error is the one the caller should see.
func IsMutationDocument(document string) bool {
	parsed, err := parser.Parse(parser.ParseParams{Source: document})
	if err != nil || parsed == nil {
		return false
	}
	for _, definition := range parsed.Definitions {
		if operation, ok := definition.(*ast.OperationDefinition); ok && operation.Operation == ast.OperationTypeMutation {
			return true
		}
	}
	return false
}
