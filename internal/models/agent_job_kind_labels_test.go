package models_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Every kind of agent job has a label in the dashboard's English catalogue
// (agent.runKinds.<kind>), which the other catalogues are checked against:
// a run of a kind without one is listed by its raw value, so a new kind
// added here without a label read as an untranslated word in the runs list.
func TestEveryAgentJobKindHasALabel(t *testing.T) {
	kinds := agentJobKinds(t)
	if len(kinds) == 0 {
		t.Fatal("no agent job kinds found in agent.go")
	}
	catalogue, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "i18n", "en.ts"))
	if err != nil {
		t.Fatalf("reading the English catalogue: %s", err)
	}
	for _, kind := range kinds {
		if !strings.Contains(string(catalogue), "'agent.runKinds."+kind+"':") {
			t.Errorf("the agent job kind %q has no agent.runKinds.%s label in web/src/i18n/en.ts", kind, kind)
		}
	}
}

// agentJobKinds is the value of every AgentJobKind constant in agent.go,
// read from the source so a kind added there is checked without a list
// here to keep up to date.
func agentJobKinds(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "agent.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing agent.go: %s", err)
	}
	kinds := []string{}
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue || len(value.Values) != 1 {
				continue
			}
			typeName, isTypeName := value.Type.(*ast.Ident)
			literal, isLiteral := value.Values[0].(*ast.BasicLit)
			if !isTypeName || typeName.Name != "AgentJobKind" || !isLiteral || literal.Kind != token.STRING {
				continue
			}
			kind, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatalf("reading %s: %s", literal.Value, err)
			}
			kinds = append(kinds, kind)
		}
	}
	return kinds
}
