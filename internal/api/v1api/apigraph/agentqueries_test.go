package apigraph

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"
)

// The agent's tools send documents of this schema, written by hand in
// internal/agent, and a tool whose document drifted would fail on every
// call and be blamed on the model. This reads every document out of the
// agent's source — a string literal that begins with query or mutation,
// or a concatenation of them — and validates it against the schema. A
// document built at run time with a format verb is skipped.
const agentDirectory = "../../../agent"

// documentPattern is what a document begins with; the word "query" on its
// own is an argument name, not a document.
var documentPattern = regexp.MustCompile(`^(query|mutation)\s*[({]`)

func TestTheAgentDocumentsMatchTheSchema(t *testing.T) {
	schema := buildSchemaForValidation(t)
	documents := readAgentDocuments(t)
	if len(documents) < 30 {
		t.Fatalf("only %d documents found in %s; the reader is looking in the wrong place", len(documents), agentDirectory)
	}
	for _, operation := range documents {
		document, err := gqlparser.Parse(gqlparser.ParseParams{Source: operation.text})
		if err != nil {
			t.Errorf("%s: does not parse: %s\n%s", operation.where, err, operation.text)
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
		t.Errorf("%s: does not match the schema: %s\n%s", operation.where, strings.Join(messages, "; "), operation.text)
	}
}

func readAgentDocuments(t *testing.T) []clientOperation {
	t.Helper()
	fileSet := token.NewFileSet()
	paths, err := filepath.Glob(filepath.Join(agentDirectory, "*.go"))
	if err != nil {
		t.Fatalf("cannot list the agent package: %s", err)
	}
	var documents []clientOperation
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("%s: %s", path, err)
		}
		constants := map[string]ast.Expr{}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, specification := range general.Specs {
				if value, ok := specification.(*ast.ValueSpec); ok {
					for index, name := range value.Names {
						if index < len(value.Values) {
							constants[name.Name] = value.Values[index]
						}
					}
				}
			}
		}
		var fold func(ast.Expr, int) (string, bool)
		fold = func(expression ast.Expr, depth int) (string, bool) {
			if depth > 16 {
				return "", false
			}
			switch node := expression.(type) {
			case *ast.BasicLit:
				if node.Kind != token.STRING {
					return "", false
				}
				text, err := strconv.Unquote(node.Value)
				return text, err == nil
			case *ast.Ident:
				if value, ok := constants[node.Name]; ok {
					return fold(value, depth+1)
				}
			case *ast.BinaryExpr:
				if node.Op == token.ADD {
					left, ok := fold(node.X, depth+1)
					if !ok {
						return "", false
					}
					right, ok := fold(node.Y, depth+1)
					if !ok {
						return "", false
					}
					return left + right, true
				}
			case *ast.ParenExpr:
				return fold(node.X, depth+1)
			}
			return "", false
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch expression := node.(type) {
			case *ast.BasicLit, *ast.BinaryExpr:
				text, ok := fold(expression.(ast.Expr), 0)
				if !ok {
					return true
				}
				trimmed := strings.TrimSpace(text)
				if !documentPattern.MatchString(trimmed) || strings.Contains(trimmed, "%s") {
					return true
				}
				documents = append(documents, clientOperation{where: fileSet.Position(expression.Pos()).String(), text: text})
				// A concatenation's parts would be read again as
				// fragments; the whole is what is validated.
				return false
			}
			return true
		})
	}
	return documents
}
