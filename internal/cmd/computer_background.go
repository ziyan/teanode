package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// newComputerBackgroundCommand builds "teanode computer background": the
// commands the agent's shell left running on your attached computers, to
// list, read and stop.
func newComputerBackgroundCommand() *cli.Command {
	return &cli.Command{
		Name:  "background",
		Usage: "the commands your agent left running in the background on your computers",
		Description: "The agent's shell can leave a command running on an attached computer and come\n" +
			"back to it. 'list' says what runs or ended lately, 'read' prints the last of what\n" +
			"one wrote, and 'stop' ends one; the agent that started it hears that it ended.",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "the commands running, or ended lately, newest first",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "conversation", Usage: "only the ones this conversation started, by id"},
					JSONFlag(),
				},
				Action: runComputerBackgroundList,
			},
			{
				Name:      "read",
				Usage:     "print the last of what one command wrote: its output, then its errors",
				ArgsUsage: "<computer> <id>",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "tail", Usage: "how many bytes of the end of each stream to print; 64 KiB by default, 256 KiB at most"},
					JSONFlag(),
				},
				Action: runComputerBackgroundRead,
			},
			{
				Name:      "stop",
				Usage:     "end one command",
				ArgsUsage: "<computer> <id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runComputerBackgroundStop,
			},
		},
	}
}

func runComputerBackgroundList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	commands, err := client.ListAgentBackgroundCommands(ctx, connection, command.String("conversation"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(commands)
	}
	if len(commands) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing runs in the background")
		return nil
	}
	for _, background := range commands {
		_, _ = fmt.Fprintf(command.Writer, "%s  %s  %s  %s  %s\n",
			background.ID,
			background.Computer,
			backgroundCommandState(background),
			background.StartedAt.Local().Format("2006-01-02 15:04"),
			forTerminal(background.Command))
	}
	return nil
}

func runComputerBackgroundRead(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return usage("which command? usage: teanode computer background read <computer> <id>")
	}
	tailBytes := int(command.Int("tail"))
	if tailBytes < 0 {
		return usage("--tail is a number of bytes, and not below zero")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	background, err := client.ReadAgentBackgroundCommand(ctx, connection, command.Args().Get(0), command.Args().Get(1), tailBytes)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(background)
	}
	// The command's output goes to standard output as it was written, and
	// its errors and what this program says about them to standard error,
	// so that the output alone can be piped on.
	_, _ = fmt.Fprint(command.Writer, background.Stdout)
	if background.IsStdoutTruncated {
		_, _ = fmt.Fprintf(command.ErrWriter, "(output cut: the last %d of %d bytes)\n", len(background.Stdout), background.StdoutByteCount)
	}
	_, _ = fmt.Fprint(command.ErrWriter, background.Stderr)
	if background.IsStderrTruncated {
		_, _ = fmt.Fprintf(command.ErrWriter, "(errors cut: the last %d of %d bytes)\n", len(background.Stderr), background.StderrByteCount)
	}
	_, _ = fmt.Fprintf(command.ErrWriter, "%s on %s: %s\n", background.ID, background.Computer, backgroundCommandState(background))
	return nil
}

func runComputerBackgroundStop(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return usage("which command? usage: teanode computer background stop <computer> <id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	background, err := client.StopAgentBackgroundCommand(ctx, connection, command.Args().Get(0), command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(background)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s on %s: %s\n", background.ID, background.Computer, backgroundCommandState(background))
	return nil
}

// backgroundCommandState says in a few words where a background command
// stands: running, the code it exited with, or why it was ended.
func backgroundCommandState(background *client.AgentBackgroundCommand) string {
	switch {
	case background.IsRunning:
		return "running"
	case background.StopReason == "stopped":
		return "stopped"
	case background.StopReason == "lifetime":
		return "stopped after 24 hours"
	case strings.TrimSpace(background.StopReason) != "":
		return "stopped (" + background.StopReason + ")"
	default:
		return fmt.Sprintf("exit %d", background.ExitCode)
	}
}
