package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

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

// ForceFlag is offered by every command that would otherwise ask before doing
// something that cannot be undone.
func ForceFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:  "force",
		Usage: "do not ask for confirmation",
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

// printFields writes one record as aligned "name: value" lines, for the
// commands that show a single thing rather than a list.
func printFields(fields [][2]string) error {
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	for _, field := range fields {
		_, _ = fmt.Fprintf(writer, "%s:\t%s\n", field[0], field[1])
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

// formatTime renders a time for a table, or "never" for one that has not
// happened.
func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "never"
	}
	return value.Local().Format(time.RFC3339)
}

// yesNo renders a boolean for a table.
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// truncate shortens a cell so that a subject line does not push every other
// column off the right of the terminal. Counted in characters, not bytes, so
// a subject in Japanese is cut between letters rather than inside one.
func truncate(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}
