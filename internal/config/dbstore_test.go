package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

// TestEverySectionIsStored is the guard the comment above sections() asks
// for: a section of the configuration that is written by the API and not
// listed here is a setting that silently resets on the next restart. That is
// what happened to the IMAP settings — a mail program was told to connect to
// the port the process happens to bind, every time the server came back.
//
// The database connection is the one exception. It cannot be read out of the
// database it names.
func TestEverySectionIsStored(t *testing.T) {
	stored := (&Configuration{}).sections()
	value := reflect.TypeOf(Configuration{})

	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" || name == "database" {
			continue
		}
		if _, ok := stored[name]; !ok {
			t.Errorf("the %q section is not in sections(), so changing it does not survive a restart", name)
		}
	}

	for name := range stored {
		found := false
		for index := 0; index < value.NumField(); index++ {
			if strings.Split(value.Field(index).Tag.Get("yaml"), ",")[0] == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("sections() stores %q, which is not a section of the configuration", name)
		}
	}
}

// TestSectionsPointAtTheirOwnField catches the copy-and-paste that gives two
// section names the same target, which would store one section's settings
// under another's name.
func TestSectionsPointAtTheirOwnField(t *testing.T) {
	configuration := &Configuration{}
	seen := map[uintptr]string{}
	for name, target := range configuration.sections() {
		address := reflect.ValueOf(target).Pointer()
		if previous, ok := seen[address]; ok {
			t.Errorf("sections() maps both %q and %q to the same field", previous, name)
		}
		seen[address] = name
	}
}

// TestSectionNamesAreDeclaredOnce keeps the constants and the map from
// drifting: every settingX constant should appear in sections().
func TestSectionNamesAreDeclaredOnce(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "dbstore.go", nil, 0)
	if err != nil {
		t.Fatalf("cannot read dbstore.go: %s", err)
	}
	stored := (&Configuration{}).sections()

	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for index, name := range declaration.Names {
			if !strings.HasPrefix(name.Name, "setting") || index >= len(declaration.Values) {
				continue
			}
			literal, ok := declaration.Values[index].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			section := strings.Trim(literal.Value, `"`)
			if _, ok := stored[section]; !ok {
				t.Errorf("%s names the %q section, which sections() does not store", name.Name, section)
			}
		}
		return true
	})
}
