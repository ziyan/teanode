package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewSettingsCommand builds "teanode settings": the optional integrations.
//
// "set" is generic on purpose. Each section's keys and their types come from
// the server's own schema — the <Section>ParametersInput object — so a
// setting added to the API is settable the day it appears, and a value is
// converted to the type the schema declares the way "teanode api call"
// converts an argument. Eight sections of five to eight fields each would
// otherwise be forty flags to write and keep in step.
func NewSettingsCommand() *cli.Command {
	return &cli.Command{
		Name:  "settings",
		Usage: "the optional integrations: object storage, scanners, the relay, certificates",
		Commands: []*cli.Command{
			{
				Name:      "show",
				Aliases:   []string{"get", "list"},
				Usage:     "show every section, or one",
				ArgsUsage: "[section]",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runSettingsShow,
			},
			{
				Name:      "set",
				Aliases:   []string{"update"},
				Usage:     "change settings in one section",
				ArgsUsage: "<section> key=value [key=value ...]",
				Description: "Keys are the section's fields, as 'teanode settings show' prints them;\n" +
					"'teanode settings describe <section>' lists them with their types.\n" +
					"Secrets are write only: a key left out keeps its value, and an empty\n" +
					"value clears it. Most of these are read once at startup, so the change\n" +
					"takes effect at the next 'teanode server restart'.\n\n" +
					"  teanode settings set antispam enabled=true host=127.0.0.1 port=783\n" +
					"  teanode settings set relay enabled=true host=smtp.example.org port=587 security=starttls username=me password=-\n" +
					"  teanode settings set s3 enabled=false\n\n" +
					"A value of \"-\" is read from the terminal without echoing, for a secret.",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runSettingsSet,
			},
			{
				Name:      "describe",
				Usage:     "list the keys a section accepts, with their types",
				ArgsUsage: "<section>",
				Action:    runSettingsDescribe,
			},
		},
	}
}

func runSettingsShow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	settings, err := client.GetSettings(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}

	section := command.Args().First()
	if section != "" {
		if _, ok := settings[section]; !ok {
			return fmt.Errorf("no section called %q; there is %s", section, strings.Join(sectionNames(settings), ", "))
		}
	}

	if command.Bool("json") {
		if section != "" {
			return PrintJSON(settings[section])
		}
		return PrintJSON(settings)
	}

	fields := [][2]string{}
	for _, name := range sectionNames(settings) {
		if section != "" && name != section {
			continue
		}
		var values map[string]any
		if err := json.Unmarshal(settings[name], &values); err != nil {
			return fmt.Errorf("the %s section is not an object: %w", name, err)
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fields = append(fields, [2]string{name + "." + key, formatSettingValue(values[key])})
		}
	}
	return printFields(fields)
}

func sectionNames(settings client.Settings) []string {
	names := make([]string, 0, len(settings))
	for name := range settings {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func formatSettingValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, formatSettingValue(item))
		}
		return strings.Join(parts, ", ")
	case float64:
		return fmt.Sprintf("%g", typed)
	default:
		return fmt.Sprint(typed)
	}
}

// sectionInput finds the input object the schema declares for a section, by
// asking the UpdateSettings mutation what type its argument of that name has.
func sectionInput(schema *client.Schema, section string) (*client.Type, error) {
	operation := schema.FindOperation("UpdateSettings")
	if operation == nil {
		return nil, fmt.Errorf("this server has no UpdateSettings operation")
	}
	argument := operation.FindArgument(section)
	if argument == nil {
		names := make([]string, 0, len(operation.Arguments))
		for _, candidate := range operation.Arguments {
			names = append(names, candidate.Name)
		}
		return nil, fmt.Errorf("no section called %q; there is %s", section, strings.Join(names, ", "))
	}
	inputType := schema.Types[argument.Type.Named()]
	if inputType == nil || inputType.Kind != "INPUT_OBJECT" {
		return nil, fmt.Errorf("the %s section is not something that can be set key by key", section)
	}
	return inputType, nil
}

func runSettingsSet(ctx context.Context, command *cli.Command) error {
	section := command.Args().First()
	pairs := command.Args().Tail()
	if section == "" || len(pairs) == 0 {
		return fmt.Errorf("usage: teanode settings set <section> key=value [key=value ...]")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	schema, err := client.Introspect(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	inputType, err := sectionInput(schema, section)
	if err != nil {
		return err
	}

	values := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		key, value, literal, err := splitArgument(pair)
		if err != nil {
			return err
		}
		var field *client.Argument
		for _, candidate := range inputType.InputFields {
			if candidate.Name == key {
				field = candidate
				break
			}
		}
		if field == nil {
			return fmt.Errorf("%s has no setting %q; 'teanode settings describe %s' lists them", section, key, section)
		}
		if literal {
			var decoded any
			if err := json.Unmarshal([]byte(value), &decoded); err != nil {
				return fmt.Errorf("the value of %s is not valid JSON: %w", key, err)
			}
			values[key] = decoded
			continue
		}
		if value == "-" {
			value, err = ReadSecret(key + ": ")
			if err != nil {
				return err
			}
		}
		coerced, err := coerce(field, value)
		if err != nil {
			return err
		}
		values[key] = coerced
	}

	settings, err := client.UpdateSettings(ctx, connection, map[string]any{section: values})
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(settings[section])
	}
	fmt.Printf("changed %s; 'teanode server status' says whether a restart is needed for it to take effect\n", section)
	return nil
}

func runSettingsDescribe(ctx context.Context, command *cli.Command) error {
	section := command.Args().First()
	if section == "" {
		return fmt.Errorf("which section? usage: teanode settings describe <section>")
	}
	schema, err := introspect(ctx, command)
	if err != nil {
		return err
	}
	inputType, err := sectionInput(schema, section)
	if err != nil {
		return err
	}
	// The description column only when the schema has descriptions to put in
	// it; an empty column is a question the reader cannot answer.
	described := false
	for _, field := range inputType.InputFields {
		described = described || field.Description != ""
	}
	rows := make([][]string, 0, len(inputType.InputFields))
	for _, field := range inputType.InputFields {
		row := []string{field.Name, field.Type.String()}
		if described {
			row = append(row, firstLine(field.Description))
		}
		rows = append(rows, row)
	}
	headers := []string{"KEY", "TYPE"}
	if described {
		headers = append(headers, "MEANING")
	}
	return printTable(headers, rows)
}
