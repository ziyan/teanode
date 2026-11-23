package cmd

import (
	"context"
	"io"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// What the agent keeps: memories, the corrections it learned from, and
// its schedules, from a terminal.

// newAgentMemoryCommand is the graph: pages addressed by path, with the
// facts on them.
//
// The flat list of memories this replaced is gone from here. It still
// exists in the database and in the API for one release, and an agent's
// own migration moves what was in it onto the graph the first time the
// graph is touched -- but showing two different lists of "what your agent
// remembers" from one command is how a person ends up editing the one
// nothing reads.
func newAgentMemoryCommand() *cli.Command {
	return &cli.Command{
		Name:     "memory",
		Usage:    "what your agent knows about you: pages, each with a path, and the facts on them",
		Commands: append(newAgentGraphCommands(), newAgentPageShareCommands()...),
	}
}

func newAgentFeedbackCommand() *cli.Command {
	return &cli.Command{
		Name:   "feedback",
		Usage:  "the corrections recorded from what you did: what the agent is shown as examples",
		Flags:  []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 50}},
		Action: runAgentFeedbackList,
	}
}

func readValue(command *cli.Command, value string) (string, error) {
	if value == "-" {
		content, err := io.ReadAll(command.Reader)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(content)), nil
	}
	return value, nil
}

func runAgentFeedbackList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	corrections, err := client.ListAgentCorrections(ctx, connection, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(corrections)
	}
	rows := make([][]string, 0, len(corrections))
	for _, correction := range corrections {
		rows = append(rows, []string{correction.CreatedAt.Local().Format("2006-01-02 15:04"), correction.Kind, correction.Said})
	}
	return printTable([]string{"when", "kind", "what"}, rows)
}
