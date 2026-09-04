package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"
)

// PrintJSON writes a value to standard output as indented JSON, which is what
// every command prints with --json and what "teanode api" prints always.
func PrintJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// JSONFlag is offered by every command that prints a table, so that a script
// or a language model driving the tool gets structured output from the same
// command a person reads.
func JSONFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:  "json",
		Usage: "print the result as JSON",
	}
}

// printTable writes rows under a header, columns aligned. An empty cell is
// left empty rather than printed as a placeholder, because a column of dashes
// draws the eye to what is not there.
func printTable(headers []string, rows [][]string) error {
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, strings.Join(headers, "\t"))
	for _, row := range rows {
		_, _ = fmt.Fprintln(writer, strings.Join(row, "\t"))
	}
	return writer.Flush()
}

// confirm asks before something irreversible, unless --force was given.
func confirm(command *cli.Command, warning string) error {
	if command.Bool("force") {
		return nil
	}
	fmt.Printf("%s Type 'yes' to continue: ", warning)
	var answer string
	_, _ = fmt.Scanln(&answer)
	if answer != "yes" {
		return fmt.Errorf("cancelled")
	}
	return nil
}
