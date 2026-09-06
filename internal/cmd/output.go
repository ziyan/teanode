package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

// PrintJSON writes a value to standard output as indented JSON, which is what
// every command prints with --json and what "teanode api" prints always.
func PrintJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// jsonRequested records that the command that ran asked for JSON, so that
// when it fails the error can be printed the same way. Set by the flag's own
// action, which runs once the flags are parsed and before the command does,
// and by the commands that print JSON without being asked.
var jsonRequested bool

// JSONRequested says whether the command that ran wanted JSON.
func JSONRequested() bool {
	return jsonRequested
}

// JSONFlag is offered by every command that prints a table, so that a script
// or a language model driving the tool gets structured output from the same
// command a person reads.
func JSONFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:  "json",
		Usage: "print the result as JSON",
		Action: func(ctx context.Context, command *cli.Command, value bool) error {
			jsonRequested = value
			return nil
		},
	}
}

// alwaysJSON is the Before of a command that prints JSON whether or not it
// was asked to, so that its errors are JSON as well.
func alwaysJSON(ctx context.Context, command *cli.Command) (context.Context, error) {
	jsonRequested = true
	return ctx, nil
}

// PrintError writes a failed command's error the way its output would have
// been written: as JSON when JSON was asked for, so a script parses failure
// the same way it parses success, and as text otherwise. Always to standard
// error, so that a failure is never mistaken for a result.
func PrintError(err error) {
	if !jsonRequested {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return
	}
	encoded, encodeError := json.Marshal(map[string]any{"error": err.Error(), "exitCode": ExitCode(err)})
	if encodeError != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", encoded)
}

// ForceFlag is offered by every command that would otherwise ask before doing
// something that cannot be undone. TEANODE_FORCE sets it for every command
// in a shell, for a script that has already decided.
func ForceFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    "force",
		Usage:   "do not ask for confirmation",
		Sources: cli.EnvVars("TEANODE_FORCE"),
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
//
// A question is only asked of somebody who can answer it. When standard
// input is not a terminal — a script, a pipe, an agent — the command refuses
// at once and says what to pass, rather than printing a prompt nobody sees
// and failing with "cancelled" after standard input runs dry.
func confirm(command *cli.Command, warning string) error {
	if command.Bool("force") {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return usage(fmt.Sprintf("%s\nRefusing without confirmation, and standard input is not a terminal to ask on; pass --force to proceed", warning))
	}
	fmt.Printf("%s Type 'yes' to continue: ", warning)
	var answer string
	_, _ = fmt.Scanln(&answer)
	if answer != "yes" {
		return fmt.Errorf("cancelled")
	}
	return nil
}

// noteCapped says on standard error when a list stopped at --first, so that
// a page is never mistaken for the whole. Nothing is said when the list is
// shorter, and nothing when --first was 0, which asked for everything.
func noteCapped(shown, first int, command string) {
	if first <= 0 || shown < first {
		return
	}
	fmt.Fprintf(os.Stderr, "note: showing the first %d; '%s --first 0' shows every one\n", shown, command)
}

// oneOf checks a flag against the values it accepts, so that a typo in a
// filter is an error rather than an empty list that looks like a quiet
// server.
func oneOf(flag, value string, choices ...string) error {
	if value == "" {
		return nil
	}
	for _, choice := range choices {
		if value == choice {
			return nil
		}
	}
	return usage(fmt.Sprintf("--%s must be one of %s, not %q", flag, strings.Join(choices, ", "), value))
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
