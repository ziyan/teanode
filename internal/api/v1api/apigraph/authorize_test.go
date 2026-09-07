package apigraph

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// authorizing are the helpers that establish the caller may do a thing. Every
// one of them refuses when there is no operator.
var authorizing = map[string]bool{
	"requireSignedIn":         true,
	"requirePermission":       true,
	"requireAnyPermission":    true,
	"requireManagement":       true,
	"requireDomainPermission": true,
	// Resolves a domain filter into the domains to query, and refuses a caller
	// who holds the permission over none of them on the way.
	"domainsToList": true,

	// requireSignedIn, plus the account it resolved to. Anything about a
	// person rather than about the server needs the account itself.
	"requireAccount": true,
	// requireAccount, plus the passkey, which it refuses unless it belongs to
	// that account.
	"requireOwnPasskey": true,
	// The row, refused unless the caller may manage its domain.
	"requireAlias":      true,
	"requireCredential": true,
	// Whoever may see the roles or groups.
	"requireRoleReader":  true,
	"requireGroupReader": true,
	// A message the caller may see, and a mailbox, folder or items they own.
	"requireReadableMail": true,
	"requireMailbox":      true,
	"requireFolder":       true,
	"requireItems":        true,
	// The mailbox holding a draft, refused unless the caller owns it.
	"requireDraftOwner": true,
}

// unauthenticated are the operations that must work before the caller is
// anybody, with the reason each one is safe.
var unauthenticated = map[string]string{
	// You cannot be an operator before logging in.
	"Login": "exchanges a password for a session; refuses a wrong one",
	// Reports who you are. Telling an anonymous caller that they are anonymous
	// discloses nothing, and the dashboard needs it to decide what to render.
	"GetSession": "reports whether the caller is authenticated",
	// Clearing a cookie you already hold needs no permission.
	"Logout": "clears the session cookie",
	// Claims a server that has nobody. Refuses once an account exists.
	"CreateFirstAccount": "claims a server with no account; refuses once one exists",
	// Signing in with a passkey, which is the same position as Login: you
	// cannot be an operator before you have signed in. The challenge is minted
	// for nobody in particular and says nothing about who has an account here,
	// and the assertion is refused unless it verifies against a stored public
	// key.
	"BeginPasskeyAssertion":  "mints a WebAuthn challenge for whoever is signing in",
	"FinishPasskeyAssertion": "exchanges a signed challenge for a session; refuses one that does not verify",
}

// TestEveryOperationAuthorises is what makes it safe for the GraphQL endpoint
// to be reachable without a session.
//
// Authorisation lives in the resolvers, not in the routing, because logging in
// has to happen at the same endpoint as everything else. That is only sound
// while every resolver actually checks. This reads the source and fails when
// one does not, so adding an operation that forgets is a failing test rather
// than a quiet hole through which anybody on the internet reads the mail.
func TestEveryOperationAuthorises(t *testing.T) {
	t.Parallel()

	// Every .go file in this directory, read directly. ParseDir is deprecated
	// for not honouring build tags; there are none here, and reading the files
	// is clearer than pulling in the packages loader for one test.
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("cannot list the package: %s", err)
	}

	fileSet := token.NewFileSet()
	checked := 0
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("cannot parse %s: %s", name, err)
		}

		{
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || !isGraphResolver(function) {
					continue
				}
				// Not part of the schema: it registers the routes.
				if function.Name.Name == "AddRoutes" {
					continue
				}
				checked++

				operation := function.Name.Name
				if reason, ok := unauthenticated[operation]; ok {
					if callsAuthorizing(function) && operation != "ChangePassword" {
						t.Errorf("%s is listed as unauthenticated (%s) but does authorise; "+
							"remove it from the list", operation, reason)
					}
					continue
				}
				if !callsAuthorizing(function) {
					t.Errorf("%s does not authorise the caller. Every resolver must call "+
						"one of the require helpers, because the GraphQL endpoint is "+
						"reachable without a session. If it is genuinely safe to leave open, "+
						"add it to unauthenticated with the reason.", operation)
				}
			}
		}
	}

	// A parser that silently matched nothing would make this test pass while
	// checking nothing at all.
	if checked < 40 {
		t.Errorf("only found %d resolvers, which is too few; the test is not looking where it thinks", checked)
	}
	t.Logf("checked %d resolvers", checked)
}

// isGraphResolver reports whether a function is an exported method on *graph,
// which is what the schema is built from.
func isGraphResolver(function *ast.FuncDecl) bool {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	pointer, ok := function.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	identifier, ok := pointer.X.(*ast.Ident)
	if !ok || identifier.Name != "graph" {
		return false
	}
	return function.Name.IsExported()
}

// callsAuthorizing reports whether a function body calls one of the helpers,
// anywhere, including inside a nested closure.
func callsAuthorizing(function *ast.FuncDecl) bool {
	found := false
	ast.Inspect(function, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if authorizing[selector.Sel.Name] {
			found = true
			return false
		}
		return true
	})
	return found
}
