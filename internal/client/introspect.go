package client

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Schema is as much of the server's GraphQL schema as is needed to call an
// operation without knowing it in advance.
//
// It exists so the command line tool covers the whole API without a hand
// written command per operation. Hand written ones drift: an operation added
// to the schema is reachable the moment the server supports it, with no
// second place to remember to update.
type Schema struct {
	QueryType    string
	MutationType string
	Operations   map[string]*Operation
	Types        map[string]*Type
}

// Operation is one query or mutation.
type Operation struct {
	Name        string
	Kind        string // "query" or "mutation"
	Description string
	Arguments   []*Argument
	Type        *TypeReference
}

// Argument is one argument of an operation or input object field.
type Argument struct {
	Name        string
	Description string
	Type        *TypeReference
}

// Type is a named type in the schema.
type Type struct {
	Name        string
	Kind        string
	Description string
	Fields      []*Field
	InputFields []*Argument
	EnumValues  []string
}

// Field is one field of an object type.
type Field struct {
	Name        string
	Description string
	Arguments   []*Argument
	Type        *TypeReference
}

// TypeReference is a type as used, which may wrap a named type in non-null and list
// markers.
type TypeReference struct {
	Kind   string
	Name   string
	OfType *TypeReference
}

// Named returns the underlying named type, unwrapping non-null and list.
func (self *TypeReference) Named() string {
	for reference := self; reference != nil; reference = reference.OfType {
		if reference.Name != "" {
			return reference.Name
		}
	}
	return ""
}

// String renders the type the way it is written in a query, for example
// "[Domain!]!".
func (self *TypeReference) String() string {
	if self == nil {
		return ""
	}
	switch self.Kind {
	case "NON_NULL":
		return self.OfType.String() + "!"
	case "LIST":
		return "[" + self.OfType.String() + "]"
	default:
		return self.Name
	}
}

// Required reports whether a value has to be given.
func (self *TypeReference) Required() bool {
	return self != nil && self.Kind == "NON_NULL"
}

const introspectionQuery = `
query {
  __schema {
    queryType { name }
    mutationType { name }
    types {
      name
      kind
      description
      fields(includeDeprecated: false) {
        name
        description
        args { name description type { ...ref } }
        type { ...ref }
      }
      inputFields { name description type { ...ref } }
      enumValues(includeDeprecated: false) { name }
    }
  }
}
fragment ref on __Type {
  kind name
  ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } }
}
`

// Introspect reads the schema from the server.
func Introspect(ctx context.Context, connection *Client) (*Schema, error) {
	var result struct {
		Schema struct {
			QueryType    struct{ Name string } `json:"queryType"`
			MutationType struct{ Name string } `json:"mutationType"`
			Types        []struct {
				Name        string `json:"name"`
				Kind        string `json:"kind"`
				Description string `json:"description"`
				Fields      []struct {
					Name        string         `json:"name"`
					Description string         `json:"description"`
					Args        []*Argument    `json:"args"`
					Type        *TypeReference `json:"type"`
				} `json:"fields"`
				InputFields []*Argument `json:"inputFields"`
				EnumValues  []struct {
					Name string `json:"name"`
				} `json:"enumValues"`
			} `json:"types"`
		} `json:"__schema"`
	}
	if err := connection.Execute(ctx, introspectionQuery, nil, &result); err != nil {
		return nil, err
	}

	schema := &Schema{
		QueryType:    result.Schema.QueryType.Name,
		MutationType: result.Schema.MutationType.Name,
		Operations:   make(map[string]*Operation),
		Types:        make(map[string]*Type),
	}

	for _, described := range result.Schema.Types {
		typed := &Type{
			Name:        described.Name,
			Kind:        described.Kind,
			Description: described.Description,
			InputFields: described.InputFields,
		}
		for _, field := range described.Fields {
			typed.Fields = append(typed.Fields, &Field{
				Name:        field.Name,
				Description: field.Description,
				Arguments:   field.Args,
				Type:        field.Type,
			})
		}
		for _, value := range described.EnumValues {
			typed.EnumValues = append(typed.EnumValues, value.Name)
		}
		schema.Types[typed.Name] = typed
	}

	for _, root := range []struct{ name, kind string }{
		{schema.QueryType, "query"},
		{schema.MutationType, "mutation"},
	} {
		rootType := schema.Types[root.name]
		if rootType == nil {
			continue
		}
		for _, field := range rootType.Fields {
			schema.Operations[field.Name] = &Operation{
				Name:        field.Name,
				Kind:        root.kind,
				Description: field.Description,
				Arguments:   field.Arguments,
				Type:        field.Type,
			}
		}
	}
	return schema, nil
}

// OperationNames returns every operation, sorted, so that a listing and an
// error message can both suggest what there is.
func (self *Schema) OperationNames() []string {
	names := make([]string, 0, len(self.Operations))
	for name := range self.Operations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// FindOperation resolves an operation name, matching case insensitively so
// that a name typed from memory still works.
func (self *Schema) FindOperation(name string) *Operation {
	if operation, ok := self.Operations[name]; ok {
		return operation
	}
	for candidate, operation := range self.Operations {
		if strings.EqualFold(candidate, name) {
			return operation
		}
	}
	return nil
}

// FindArgument resolves one of an operation's arguments, case insensitively.
func (self *Operation) FindArgument(name string) *Argument {
	for _, argument := range self.Arguments {
		if strings.EqualFold(argument.Name, name) {
			return argument
		}
	}
	return nil
}

// Selection builds the selection set for an operation's return type: every
// field that can be asked for without arguments, to a bounded depth.
//
// Bounded because the schema is cyclic — a Domain has Credentials, and a
// Delivery has a Mail that has a Domain — so an unbounded walk does not
// terminate. Three levels is enough for everything the API returns to be
// legible without asking for the whole graph.
func (self *Schema) Selection(reference *TypeReference, depth int) string {
	named := self.Types[reference.Named()]
	if named == nil {
		return ""
	}
	switch named.Kind {
	case "SCALAR", "ENUM":
		return ""
	}
	if depth <= 0 {
		return ""
	}

	var fields []string
	for _, field := range named.Fields {
		// A field with a required argument is a query of its own, not part of
		// this reply.
		required := false
		for _, argument := range field.Arguments {
			if argument.Type.Required() {
				required = true
				break
			}
		}
		if required {
			continue
		}

		nested := self.Selection(field.Type, depth-1)
		if nested == "" {
			// An object whose own selection came back empty cannot be asked
			// for at all: GraphQL requires a selection set for it.
			inner := self.Types[field.Type.Named()]
			if inner != nil && inner.Kind != "SCALAR" && inner.Kind != "ENUM" {
				continue
			}
			fields = append(fields, field.Name)
			continue
		}
		fields = append(fields, field.Name+" "+nested)
	}
	if len(fields) == 0 {
		return ""
	}
	return "{ " + strings.Join(fields, " ") + " }"
}

// BuildQuery assembles a query for an operation and its arguments, declaring a
// variable for each so that input objects and lists work without having to
// write GraphQL literal syntax.
func (self *Schema) BuildQuery(operation *Operation, arguments map[string]any, depth int) (string, error) {
	names := make([]string, 0, len(arguments))
	for name := range arguments {
		names = append(names, name)
	}
	sort.Strings(names)

	declarations := make([]string, 0, len(names))
	passed := make([]string, 0, len(names))
	for _, name := range names {
		argument := operation.FindArgument(name)
		if argument == nil {
			return "", fmt.Errorf("client: %s has no argument %q; it takes %s",
				operation.Name, name, describeArguments(operation))
		}
		declarations = append(declarations, "$"+argument.Name+": "+argument.Type.String())
		passed = append(passed, argument.Name+": $"+argument.Name)
	}

	for _, argument := range operation.Arguments {
		if argument.Type.Required() && arguments[argument.Name] == nil {
			return "", fmt.Errorf("client: %s needs %s, which is %s",
				operation.Name, argument.Name, argument.Type.String())
		}
	}

	var builder strings.Builder
	builder.WriteString(operation.Kind)
	if len(declarations) > 0 {
		builder.WriteString(" (" + strings.Join(declarations, ", ") + ")")
	}
	builder.WriteString(" { " + operation.Name)
	if len(passed) > 0 {
		builder.WriteString("(" + strings.Join(passed, ", ") + ")")
	}
	if selection := self.Selection(operation.Type, depth); selection != "" {
		builder.WriteString(" " + selection)
	}
	builder.WriteString(" }")
	return builder.String(), nil
}

func describeArguments(operation *Operation) string {
	if len(operation.Arguments) == 0 {
		return "none"
	}
	described := make([]string, 0, len(operation.Arguments))
	for _, argument := range operation.Arguments {
		described = append(described, argument.Name+": "+argument.Type.String())
	}
	return strings.Join(described, ", ")
}
