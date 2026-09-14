package server

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/commentparse"
)

// What a setting means, rather than what it is set to.
//
// settings_get answers with values, which is enough to report what a server
// is configured to do and not enough to change anything safely: a field
// invented by a model is rejected or, worse, quietly ignored, and the
// settings a mail server keeps are the ones that stop it receiving mail when
// they are wrong. The names, the types and the documentation are all in the
// configuration structs already -- they are what docs/configuration.md is
// checked against -- so this reads them out rather than asking anybody to
// keep a second copy in a prompt.

// describeDepth is how far into a section this goes. Three levels covers
// every section the configuration has, and stops a cycle being followed if
// one is ever introduced.
const describeDepth = 3

// settingsDescribeTool is declared here and registered in server.go with
// the rest of the section's tools: a member of a group has to be in the
// same registration, or the group names a tool that is not there yet.
func settingsDescribeTool() *tools.Tool {
	return &tools.Tool{
		Name: "settings_describe", Family: tools.FamilyServer, Risk: tools.RiskRead,
		Permissions: []models.Permission{models.PermissionServerManage},
		Description: "What the fields of a settings section are, what type each takes, and what it means -- the documentation the configuration carries. Read it before settings update: the names have to be exact, and a field that is not one is refused.",
		Parameters: tools.Object(map[string]any{
			"section": tools.StringProperty("one section, as settings get names them; all of them when absent"),
		}),
		Preview: tools.PreviewOf(func(struct{}) string {
			return "Read what the server's settings mean"
		}),
		Run: runSettingsDescribe,
	}
}

func runSettingsDescribe(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[struct {
		Section string `json:"section"`
	}](call)
	if err != nil {
		return nil, err
	}
	whole := reflect.TypeOf(config.Configuration{})
	wanted := strings.TrimSpace(strings.ToLower(arguments.Section))
	sections := map[string]any{}
	for index := 0; index < whole.NumField(); index++ {
		field := whole.Field(index)
		name := yamlName(field)
		// The database is not the agent's business: it holds the password
		// this server connects with, and settings_get leaves it out too.
		if name == "" || name == "database" {
			continue
		}
		if wanted == "" {
			// Every section at once is thirty-odd thousand characters,
			// which is past what a result may be -- and what came back was
			// cut in the middle of the JSON, so it was not readable at all.
			// Without a section this is the list to choose from.
			sections[name] = collapse(commentparse.GetStructFieldComment(whole.PkgPath(), whole.Name(), field.Name))
			continue
		}
		if name == wanted {
			return tools.JSONResult(map[string]any{name: describeType(field.Type, describeDepth)})
		}
	}
	if wanted != "" {
		return nil, fmt.Errorf("there is no settings section %q", arguments.Section)
	}
	return tools.JSONResult(map[string]any{
		"sections": sections,
		"note":     "give one section to see its fields, their types and what each means",
	})
}

// describeType is a struct as a map of field name to what it is, or the name
// of the type when there is nothing to look inside.
func describeType(structType reflect.Type, depth int) any {
	for structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct || depth <= 0 {
		return typeName(structType)
	}
	described := map[string]any{}
	for index := 0; index < structType.NumField(); index++ {
		field := structType.Field(index)
		name := yamlName(field)
		if name == "" {
			continue
		}
		entry := map[string]any{"type": typeName(field.Type)}
		if said := commentparse.GetStructFieldComment(structType.PkgPath(), structType.Name(), field.Name); said != "" {
			entry["means"] = collapse(said)
		}
		// A secret is named and described, never read: what it is for is
		// the useful part, and settings_get redacts the value anyway.
		if field.Tag.Get("secret") == "true" {
			entry["secret"] = true
		}
		inner := field.Type
		for inner.Kind() == reflect.Pointer || inner.Kind() == reflect.Slice {
			inner = inner.Elem()
		}
		if inner.Kind() == reflect.Struct && inner.PkgPath() != "" && !isPlainValue(inner) {
			entry["fields"] = describeType(inner, depth-1)
		}
		described[name] = entry
	}
	return described
}

// yamlName is the name a field has in the configuration document, or empty
// for one the document does not carry.
func yamlName(field reflect.StructField) string {
	tag := field.Tag.Get("yaml")
	if tag == "-" {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	if !field.IsExported() {
		return ""
	}
	return strings.ToLower(field.Name)
}

// typeName is what a value of this type looks like in the document.
func typeName(fieldType reflect.Type) string {
	switch fieldType.Kind() {
	case reflect.Pointer:
		return typeName(fieldType.Elem())
	case reflect.Slice:
		return "list of " + typeName(fieldType.Elem())
	case reflect.Map:
		return "map of " + typeName(fieldType.Elem())
	case reflect.Bool:
		return "true or false"
	case reflect.String:
		if fieldType.Name() != "" && fieldType.Name() != "string" {
			return fieldType.Name()
		}
		return "text"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if fieldType.Name() != "" && !strings.HasPrefix(fieldType.Name(), "int") && !strings.HasPrefix(fieldType.Name(), "uint") {
			return fieldType.Name()
		}
		return "a number"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Struct:
		if fieldType.Name() != "" {
			return fieldType.Name()
		}
	}
	return fieldType.String()
}

// isPlainValue says whether a struct is a value written as one thing -- a
// duration, a size -- rather than a group of fields to look inside.
func isPlainValue(structType reflect.Type) bool {
	for index := 0; index < structType.NumField(); index++ {
		if yamlName(structType.Field(index)) != "" {
			return false
		}
	}
	return true
}

// collapse puts a doc comment on one line: a result is read by a model, and
// the line breaks of a comment written for a screen are not information.
func collapse(said string) string {
	return strings.Join(strings.Fields(said), " ")
}
