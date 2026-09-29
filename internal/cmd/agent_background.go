package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// newAgentBackgroundCommand builds "teanode agent background": the surveys
// and subagents your agent started and did not wait for, and the surveys
// 'teanode agent survey' starts, to list, read and stop.
func newAgentBackgroundCommand() *cli.Command {
	return &cli.Command{
		Name:  "background",
		Usage: "the surveys and subagents your agent runs in the background",
		Description: "The agent can start a survey or a subagent and not wait for it; the conversation that started\n" +
			"it is woken when it finishes. 'list' says what runs or finished lately, 'show' prints one's result,\n" +
			"and 'stop' ends one; stopped work wakes nothing. The commands left running on your computers are\n" +
			"'teanode computer background'.",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "the work running, queued or finished lately, newest first",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "first", Usage: "how many; 100 at most"},
					JSONFlag(),
				},
				Action: runAgentBackgroundList,
			},
			{
				Name:      "show",
				Usage:     "print one's result: a survey's report or a subagent's answer, and the runs it made",
				ArgsUsage: "<id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentBackgroundShow,
			},
			{
				Name:      "stop",
				Usage:     "stop one that is queued or running",
				ArgsUsage: "<id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentBackgroundStop,
			},
		},
	}
}

func runAgentBackgroundList(ctx context.Context, command *cli.Command) error {
	first := int(command.Int("first"))
	if first < 0 {
		return usage("--first is a count, and not below zero")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	works, err := client.ListAgentBackgroundWork(ctx, connection, first)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(works)
	}
	if len(works) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no background work")
		return nil
	}
	for _, work := range works {
		_, _ = fmt.Fprintf(command.Writer, "%s  %s  %s  %s  %s\n",
			work.ID,
			work.WorkKind,
			backgroundWorkState(work),
			work.CreatedAt.Local().Format("2006-01-02 15:04"),
			forTerminal(work.Title))
	}
	return nil
}

func runAgentBackgroundShow(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return usage("which? usage: teanode agent background show <id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	work, err := client.GetAgentBackgroundWork(ctx, connection, command.Args().Get(0))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(work)
	}
	// The result to standard output as it is, and what this program says
	// about it to standard error, so that a report can be piped on.
	if work.ResultText != "" {
		_, _ = fmt.Fprintln(command.Writer, strings.TrimSpace(forTerminal(work.ResultText)))
	}
	_, _ = fmt.Fprintf(command.ErrWriter, "%s: %s, %s\n", work.ID, forTerminal(work.Title), backgroundWorkState(work))
	if work.ErrorMessage != "" {
		_, _ = fmt.Fprintf(command.ErrWriter, "%s\n", forTerminal(work.ErrorMessage))
	}
	if len(work.RunIDs) > 0 {
		_, _ = fmt.Fprintf(command.ErrWriter, "The runs, each openable with 'teanode agent run show': %s\n", strings.Join(work.RunIDs, ", "))
	}
	return nil
}

func runAgentBackgroundStop(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return usage("which? usage: teanode agent background stop <id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	work, err := client.StopAgentBackgroundWork(ctx, connection, command.Args().Get(0))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(work)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", work.ID, backgroundWorkState(work))
	return nil
}

// backgroundWorkState says in a word or two where a piece of background
// work stands.
func backgroundWorkState(work *client.AgentBackgroundWork) string {
	switch work.WorkStatus {
	case "done":
		return "finished"
	case "":
		return "unknown"
	}
	return work.WorkStatus
}
