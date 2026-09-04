package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewAPICommand builds "teanode api", which reaches every operation the server
// offers rather than only the ones with a command of their own.
//
// It works off the schema the server reports, so an operation added to the API
// is callable the moment the server supports it, with no second place to
// update and nothing to drift. Output is JSON, which is also what makes it
// usable from a script or by a language model driving the tool.
func NewAPICommand() *cli.Command {
	return &cli.Command{
		Name:  "api",
		Usage: "call the API directly; covers every operation, and prints JSON",
		Description: "The typed commands cover the common tasks. This covers everything else,\n" +
			"including anything added since. Start with 'teanode api list'.",
		Commands: []*cli.Command{
			{
				Name:      "list",
				Usage:     "list every operation the server offers",
				ArgsUsage: "[substring]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the listing as JSON",
					},
				},
				Action: runAPIList,
			},
			{
				Name:      "describe",
				Usage:     "describe an operation, its arguments and what it returns",
				ArgsUsage: "<operation>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "json",
						Usage: "print the description as JSON",
					},
				},
				Action: runAPIDescribe,
			},
			{
				Name:      "call",
				Usage:     "call an operation with name=value arguments",
				ArgsUsage: "<operation> [name=value ...]",
				Description: "Values are read as text. Use name:=<json> to pass a number, a boolean,\n" +
					"a list or an object literally:\n\n" +
					"  teanode api call ListDomains\n" +
					"  teanode api call GetDomain domainId=01ABC...\n" +
					"  teanode api call CreateDomain domainParameters:='{\"domain\":\"example.com\"}'\n" +
					"  teanode api call ListMails pagination:='{\"first\":10}'\n\n" +
					"The reply carries every field that can be asked for without arguments,\n" +
					"to --depth levels.",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "depth",
						Value: 3,
						Usage: "how far to follow nested objects in the reply",
					},
					&cli.StringFlag{
						Name:  "select",
						Usage: "GraphQL selection set to use instead of the generated one, for example \"{ id domain }\"",
					},
				},
				Action: runAPICall,
			},
			{
				Name:      "graphql",
				Usage:     "run a GraphQL query written by hand",
				ArgsUsage: "[query]",
				Description: "Reads the query from the argument, from --file, or from standard input.\n\n" +
					"  teanode api graphql '{ ListDomains { id domain } }'\n" +
					"  echo '{ ListDomains { id } }' | teanode api graphql",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "file",
						Usage: "read the query from a file",
					},
					&cli.StringFlag{
						Name:  "variables",
						Usage: "variables as a JSON object",
					},
				},
				Action: runAPIGraphQL,
			},
		},
	}
}

func runAPIList(ctx context.Context, command *cli.Command) error {
	schema, err := introspect(ctx, command)
	if err != nil {
		return err
	}

	filter := strings.ToLower(command.Args().First())
	type listed struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Returns   string `json:"returns"`
		Arguments string `json:"arguments"`
		Summary   string `json:"summary,omitempty"`
	}
	operations := make([]listed, 0, len(schema.Operations))
	for _, name := range schema.OperationNames() {
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		operation := schema.Operations[name]
		operations = append(operations, listed{
			Name:      operation.Name,
			Kind:      operation.Kind,
			Returns:   operation.Type.String(),
			Arguments: argumentSignature(operation),
			Summary:   firstLine(operation.Description),
		})
	}

	if command.Bool("json") {
		return PrintJSON(operations)
	}
	if len(operations) == 0 {
		fmt.Printf("no operation matches %q\n", command.Args().First())
		return nil
	}
	for _, operation := range operations {
		fmt.Printf("%-9s %s(%s) -> %s\n", operation.Kind, operation.Name, operation.Arguments, operation.Returns)
		if operation.Summary != "" {
			fmt.Printf("          %s\n", operation.Summary)
		}
	}
	fmt.Printf("\n%d operations. 'teanode api describe <name>' for the details.\n", len(operations))
	return nil
}

func runAPIDescribe(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which operation? usage: teanode api describe <operation>")
	}

	schema, err := introspect(ctx, command)
	if err != nil {
		return err
	}
	operation := schema.FindOperation(name)
	if operation == nil {
		return unknownOperation(schema, name)
	}

	if command.Bool("json") {
		return PrintJSON(describeOperation(schema, operation))
	}

	fmt.Printf("%s %s\n", operation.Kind, operation.Name)
	if operation.Description != "" {
		fmt.Printf("\n%s\n", indent(operation.Description, "  "))
	}

	fmt.Printf("\narguments:\n")
	if len(operation.Arguments) == 0 {
		fmt.Printf("  none\n")
	}
	for _, argument := range operation.Arguments {
		required := ""
		if argument.Type.Required() {
			required = "  (required)"
		}
		fmt.Printf("  %s: %s%s\n", argument.Name, argument.Type.String(), required)
		if argument.Description != "" {
			fmt.Printf("      %s\n", firstLine(argument.Description))
		}
		describeInputType(schema, argument.Type.Named(), "      ")
	}

	fmt.Printf("\nreturns: %s\n", operation.Type.String())
	if returned := schema.Types[operation.Type.Named()]; returned != nil {
		for _, field := range returned.Fields {
			fmt.Printf("  %s: %s\n", field.Name, field.Type.String())
		}
	}
	return nil
}

func runAPICall(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which operation? usage: teanode api call <operation> [name=value ...]")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	schema, err := client.Introspect(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	operation := schema.FindOperation(name)
	if operation == nil {
		return unknownOperation(schema, name)
	}

	arguments, err := parseArguments(operation, command.Args().Tail())
	if err != nil {
		return err
	}

	query, err := schema.BuildQuery(operation, arguments, int(command.Int("depth")))
	if err != nil {
		return err
	}
	if selection := command.String("select"); selection != "" {
		query, err = replaceSelection(schema, operation, arguments, selection)
		if err != nil {
			return err
		}
	}

	var result map[string]any
	if err := connection.Execute(ctx, query, arguments, &result); err != nil {
		return err
	}
	return PrintJSON(result[operation.Name])
}

func runAPIGraphQL(ctx context.Context, command *cli.Command) error {
	query, err := readQuery(command)
	if err != nil {
		return err
	}

	variables := map[string]any{}
	if encoded := command.String("variables"); encoded != "" {
		if err := json.Unmarshal([]byte(encoded), &variables); err != nil {
			return fmt.Errorf("--variables is not a JSON object: %w", err)
		}
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var result any
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return describeConnectionError(command, err)
	}
	return PrintJSON(result)
}

func readQuery(command *cli.Command) (string, error) {
	if filename := command.String("file"); filename != "" {
		content, err := os.ReadFile(filename)
		if err != nil {
			return "", err
		}
		return string(content), nil
	}
	if query := strings.TrimSpace(command.Args().First()); query != "" {
		return query, nil
	}

	content, err := readAllStdin()
	if err != nil {
		return "", err
	}
	query := strings.TrimSpace(content)
	if query == "" {
		return "", fmt.Errorf("no query; pass one as an argument, with --file, or on standard input")
	}
	return query, nil
}

// parseArguments turns "name=value" and "name:=json" pairs into values of the
// types the operation declares.
func parseArguments(operation *client.Operation, pairs []string) (map[string]any, error) {
	arguments := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		name, value, literal, err := splitArgument(pair)
		if err != nil {
			return nil, err
		}

		argument := operation.FindArgument(name)
		if argument == nil {
			return nil, fmt.Errorf("%s has no argument %q; it takes %s",
				operation.Name, name, argumentSignature(operation))
		}

		if literal {
			var decoded any
			if err := json.Unmarshal([]byte(value), &decoded); err != nil {
				return nil, fmt.Errorf("the value of %s is not valid JSON: %w", name, err)
			}
			arguments[argument.Name] = decoded
			continue
		}

		coerced, err := coerce(argument, value)
		if err != nil {
			return nil, err
		}
		arguments[argument.Name] = coerced
	}
	return arguments, nil
}

func splitArgument(pair string) (string, string, bool, error) {
	if name, value, ok := strings.Cut(pair, ":="); ok {
		return name, value, true, nil
	}
	name, value, ok := strings.Cut(pair, "=")
	if !ok {
		return "", "", false, fmt.Errorf("%q is not name=value; use name:=<json> for a number, list or object", pair)
	}
	return name, value, false, nil
}

// coerce reads a value as the type the schema declares, so that "first=10"
// arrives as a number rather than as the string "10".
func coerce(argument *client.Argument, value string) (any, error) {
	switch argument.Type.Named() {
	case "Int":
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s is %s, and %q is not a whole number", argument.Name, argument.Type.String(), value)
		}
		return number, nil
	case "Float":
		number, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("%s is %s, and %q is not a number", argument.Name, argument.Type.String(), value)
		}
		return number, nil
	case "Boolean":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("%s is %s, and %q is not true or false", argument.Name, argument.Type.String(), value)
		}
		return parsed, nil
	}

	// An input object or a list cannot be written as plain text, and saying so
	// is more useful than sending a string the server will reject.
	if argument.Type.Kind == "LIST" || (argument.Type.Kind == "NON_NULL" && argument.Type.OfType != nil && argument.Type.OfType.Kind == "LIST") {
		return nil, fmt.Errorf("%s is %s; pass it as %s:=<json>", argument.Name, argument.Type.String(), argument.Name)
	}
	return value, nil
}

// replaceSelection rebuilds a query with a selection set the caller wrote,
// for when the generated one asks for more than is wanted.
func replaceSelection(schema *client.Schema, operation *client.Operation, arguments map[string]any, selection string) (string, error) {
	generated, err := schema.BuildQuery(operation, arguments, 0)
	if err != nil {
		return "", err
	}
	selection = strings.TrimSpace(selection)
	if !strings.HasPrefix(selection, "{") {
		selection = "{ " + selection + " }"
	}
	// BuildQuery with depth zero emits no selection, so the operation call is
	// the last thing before the closing brace.
	index := strings.LastIndex(generated, "}")
	if index < 0 {
		return "", fmt.Errorf("cannot build a query for %s", operation.Name)
	}
	return strings.TrimSpace(generated[:index]) + " " + selection + " }", nil
}

func introspect(ctx context.Context, command *cli.Command) (*client.Schema, error) {
	connection, err := openClient(command)
	if err != nil {
		return nil, err
	}
	schema, err := client.Introspect(ctx, connection)
	if err != nil {
		return nil, describeConnectionError(command, err)
	}
	return schema, nil
}

func unknownOperation(schema *client.Schema, name string) error {
	lower := strings.ToLower(name)
	near := make([]string, 0, 4)
	for _, candidate := range schema.OperationNames() {
		if strings.Contains(strings.ToLower(candidate), lower) {
			near = append(near, candidate)
			if len(near) == 4 {
				break
			}
		}
	}
	if len(near) > 0 {
		return fmt.Errorf("no operation called %q; did you mean %s?", name, strings.Join(near, ", "))
	}
	return fmt.Errorf("no operation called %q; run 'teanode api list' to see them", name)
}

func describeOperation(schema *client.Schema, operation *client.Operation) any {
	type describedArgument struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Required    bool   `json:"required"`
		Description string `json:"description,omitempty"`
	}
	type describedField struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description,omitempty"`
	}

	arguments := make([]describedArgument, 0, len(operation.Arguments))
	for _, argument := range operation.Arguments {
		arguments = append(arguments, describedArgument{
			Name:        argument.Name,
			Type:        argument.Type.String(),
			Required:    argument.Type.Required(),
			Description: argument.Description,
		})
	}

	fields := make([]describedField, 0)
	if returned := schema.Types[operation.Type.Named()]; returned != nil {
		for _, field := range returned.Fields {
			fields = append(fields, describedField{
				Name:        field.Name,
				Type:        field.Type.String(),
				Description: field.Description,
			})
		}
	}

	return map[string]any{
		"name":        operation.Name,
		"kind":        operation.Kind,
		"description": operation.Description,
		"arguments":   arguments,
		"returns":     operation.Type.String(),
		"fields":      fields,
	}
}

// describeInputType lists the fields of an input object, so that the JSON to
// pass for it does not have to be guessed.
func describeInputType(schema *client.Schema, name, prefix string) {
	inputType := schema.Types[name]
	if inputType == nil || inputType.Kind != "INPUT_OBJECT" {
		if inputType != nil && inputType.Kind == "ENUM" {
			fmt.Printf("%sone of: %s\n", prefix, strings.Join(inputType.EnumValues, ", "))
		}
		return
	}
	fmt.Printf("%s%s is an object with:\n", prefix, name)
	for _, field := range inputType.InputFields {
		fmt.Printf("%s  %s: %s\n", prefix, field.Name, field.Type.String())
	}
}

func argumentSignature(operation *client.Operation) string {
	if len(operation.Arguments) == 0 {
		return ""
	}
	described := make([]string, 0, len(operation.Arguments))
	for _, argument := range operation.Arguments {
		described = append(described, argument.Name+": "+argument.Type.String())
	}
	return strings.Join(described, ", ")
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for index, line := range lines {
		lines[index] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// readAllStdin reads a query piped in, which is how a longer one is passed
// without fighting the shell over quoting.
func readAllStdin() (string, error) {
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
