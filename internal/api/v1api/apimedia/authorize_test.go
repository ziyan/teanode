package apimedia

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// The routes here are the blind spot in the guarantee next door.
//
// apigraph has a test that reads its own source and fails when a resolver does
// not authorize, which is what makes it safe for the GraphQL endpoint to be
// reachable without a session. These handlers are not resolvers and that test
// does not see them, so a route added here can change something on behalf of
// anybody with a session and nothing will say so.
//
// That is not hypothetical: the logo upload shipped checking only that the
// caller was signed in, while removing the same logo asked for domain:manage.
// Anybody with a mailbox on this server could have replaced any domain's
// published mark.

// writing are the handlers that change something, and what each must ask for
// beyond a session.
var writing = map[string]string{
	"logoUploadView": "canManageDomain",

	// Found by this test on the day it was written: a picture is stored
	// against a domain and served from that domain's name, and every template
	// operation asks for domain:manage, but this asked only for a session.
	"uploadView": "canManageDomain",
}

// reading are the handlers that only serve bytes, with the reason each is safe
// without a permission check.
var reading = map[string]string{
	// The file a BIMI record names. Fetched by receiving mail systems, which
	// have no session; it is a mark published on purpose and the address
	// carries no identifier but the file's own.
	"logoView": "public by design; a receiver fetching a published mark has no session",

	// The same bytes, by domain rather than by file, so the dashboard can draw
	// what it published without knowing today's file identifier. Behind a
	// session, and the same public bytes either way.
	"domainLogoView": "serves bytes that are already public at the address above",

	// The picture store: a picture in a template, and the addressed copy that
	// records having been fetched.
	"fileView": "serves a stored picture, which a mail program fetches without a session",
	"linkView": "serves a picture at a per-message address, which is the point of it",
}

// TestEveryWritingRouteChecksPermission fails when a handler that changes
// something does not ask for more than a session.
func TestEveryWritingRouteChecksPermission(t *testing.T) {
	t.Parallel()

	fileSet := token.NewFileSet()
	found := map[string]*ast.FuncDecl{}
	for _, name := range []string{"apimedia.go", "logo.go"} {
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("cannot parse %s: %s", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !isHandler(function) {
				continue
			}
			found[function.Name.Name] = function
		}
	}

	for name, function := range found {
		if _, ok := reading[name]; ok {
			continue
		}
		required, ok := writing[name]
		if !ok {
			t.Errorf("%s is a route handler that neither list mentions; say whether it changes "+
				"anything, and if it does, what permission it asks for", name)
			continue
		}
		if !calls(function, required) {
			t.Errorf("%s changes something but does not call %s, so a session is all it asks for; "+
				"anybody signed in could use it on any domain", name, required)
		}
	}

	// The lists themselves have to keep describing something real, or they
	// quietly stop covering anything at all.
	for name := range writing {
		if _, ok := found[name]; !ok {
			t.Errorf("writing names %s, which is not a handler here any more", name)
		}
	}
	for name := range reading {
		if _, ok := found[name]; !ok {
			t.Errorf("reading names %s, which is not a handler here any more", name)
		}
	}
}

// isHandler says whether a method has the shape a route handler has.
func isHandler(function *ast.FuncDecl) bool {
	if function.Recv == nil || function.Type.Params == nil || len(function.Type.Params.List) != 2 {
		return false
	}
	if !strings.HasSuffix(function.Name.Name, "View") {
		return false
	}
	return true
}

// calls says whether a function body mentions the named call.
func calls(function *ast.FuncDecl, name string) bool {
	called := false
	ast.Inspect(function, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch target := call.Fun.(type) {
		case *ast.SelectorExpr:
			if target.Sel.Name == name {
				called = true
			}
		case *ast.Ident:
			if target.Name == name {
				called = true
			}
		}
		return !called
	})
	return called
}
