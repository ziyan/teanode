package apigraph

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"

	"github.com/ziyan/teanode/internal/util/graphapi"
)

// The command line client's queries are written by hand in internal/client and
// sent to this schema, and nothing until now compared the two. They drifted:
// UpdateUser, SetUserPassword and DeleteUser went on naming an account by
// username after the schema moved to an identifier, and GetReport asked for a
// structure without a selection. Every one of those commands failed on every
// invocation, and the dashboard's own check — make check-queries — could not
// see them, because it reads web/src and needs a server running.
//
// This reads the client's source, builds the same schema the server builds,
// and validates one against the other. It needs no server and no database, so
// it runs in the ordinary test job.
//
// It reads source rather than calling the client because a query is only sent
// when its function is called, and calling all of them would need the server
// this test exists to avoid.

// clientDirectory is where the client's queries live, relative to this package.
const clientDirectory = "../../../client"

func TestTheClientQueriesMatchTheSchema(t *testing.T) {
	schema := buildSchemaForValidation(t)

	for _, operation := range readClientOperations(t) {
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
		t.Errorf("%s: does not match the schema: %s\n%s",
			operation.where, strings.Join(messages, "; "), operation.text)
	}
}

// buildSchemaForValidation builds the schema the way New does. The resolvers
// are never called, so the zero graph is enough: reflection is over the Query
// and Mutation interfaces, not over anything they hold.
func buildSchemaForValidation(t *testing.T) graphql.Schema {
	t.Helper()

	self := &graph{}
	graphApi := graphapi.New()
	var query Query = self
	var mutation Mutation = self
	if err := graphApi.Register(&query, &mutation, nil); err != nil {
		t.Fatalf("cannot register the schema: %s", err)
	}
	schema, err := graphApi.Build()
	if err != nil {
		t.Fatalf("cannot build the schema: %s", err)
	}
	return schema
}

type clientOperation struct {
	where string
	text  string
}

// readClientOperations returns every document the client sends, read out of
// its source and with the selection constants folded in.
//
// It finds them by their call site rather than by looking for strings that
// begin with "query", so a document this cannot read is a failure rather than
// a silent skip. That mattered while writing it: the first version read string
// literals, and a selection built by concatenating two constants looked like
// two half-documents.
func readClientOperations(t *testing.T) []clientOperation {
	t.Helper()

	fileSet := token.NewFileSet()
	paths, err := filepath.Glob(filepath.Join(clientDirectory, "*.go"))
	if err != nil {
		t.Fatalf("cannot list the client: %s", err)
	}
	var files []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("%s: %s", path, err)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		t.Fatalf("no client source at %s; this test is in the wrong place", clientDirectory)
	}

	// Every package-level string, kept as its expression: a selection
	// constant is often itself a concatenation of other constants.
	values := map[string]ast.Expr{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || (general.Tok != token.CONST && general.Tok != token.VAR) {
				continue
			}
			for _, specification := range general.Specs {
				value, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range value.Names {
					if index < len(value.Values) {
						values[name.Name] = value.Values[index]
					}
				}
			}
		}
	}

	// Resolving an identifier means looking at the locals of the function
	// being read first, then the package's constants. Most documents are
	// built into a local named "query" and handed straight to Execute.
	var fold func(ast.Expr, map[string]ast.Expr, int) (string, bool)
	fold = func(expression ast.Expr, locals map[string]ast.Expr, depth int) (string, bool) {
		if depth > 32 {
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
			if value, ok := locals[node.Name]; ok {
				return fold(value, locals, depth+1)
			}
			value, ok := values[node.Name]
			if !ok {
				return "", false
			}
			return fold(value, locals, depth+1)
		case *ast.BinaryExpr:
			if node.Op != token.ADD {
				return "", false
			}
			left, ok := fold(node.X, locals, depth+1)
			if !ok {
				return "", false
			}
			right, ok := fold(node.Y, locals, depth+1)
			if !ok {
				return "", false
			}
			return left + right, true
		}
		return "", false
	}

	var operations []clientOperation
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}

			locals := map[string]ast.Expr{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				assignment, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for index, left := range assignment.Lhs {
					name, ok := left.(*ast.Ident)
					if !ok || index >= len(assignment.Rhs) {
						continue
					}
					locals[name.Name] = assignment.Rhs[index]
				}
				return true
			})

			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Execute" || len(call.Args) < 2 {
					return true
				}
				// Execute(ctx, query, variables, result): the document is second.
				where := fileSet.Position(call.Pos())
				text, ok := fold(call.Args[1], locals, 0)
				if !ok {
					t.Errorf("%s:%d: cannot read the document passed to Execute; "+
						"build it from string constants so this test can check it",
						filepath.Base(where.Filename), where.Line)
					return true
				}
				operations = append(operations, clientOperation{
					where: fmt.Sprintf("%s:%d", filepath.Base(where.Filename), where.Line),
					text:  text,
				})
				return true
			})
		}
	}

	// A client that sends nothing means the search broke, not that the client
	// is clean.
	if len(operations) < 50 {
		t.Fatalf("only found %d documents in the client, which is too few to believe",
			len(operations))
	}
	sort.Slice(operations, func(one, two int) bool { return operations[one].where < operations[two].where })
	return operations
}
