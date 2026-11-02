package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Sending yourself a file in your main chat, the conversation the dashboard
// and the phone open on: a chart a script drew, a recording of the screen, a
// report. The phone is told, as it is of anything the agent says there.

func newAgentSendFileCommand() *cli.Command {
	return &cli.Command{
		Name:      "send-file",
		Usage:     "send yourself a file in your main chat, where the dashboard and the phone open, and have the phone tell you; a page or a PDF opens in place, a picture or a video plays",
		ArgsUsage: "<file> [file...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "caption", Usage: "a line to send with the file; with several files, with each"},
			&cli.StringFlag{Name: "type", Usage: "the file's type, such as text/html; the name's extension by default"},
		},
		Action: runAgentSendFile,
	}
}

func runAgentSendFile(ctx context.Context, command *cli.Command) error {
	paths := command.Args().Slice()
	if len(paths) == 0 {
		return fmt.Errorf("which file? give its path")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name := filepath.Base(path)
		if _, err := client.SendAgentFileToMainChat(ctx, connection, name, strings.TrimSpace(command.String("type")), strings.TrimSpace(command.String("caption")), content); err != nil {
			return describeError(command, err)
		}
		_, _ = fmt.Fprintf(command.Writer, "%s: sent to your main chat\n", name)
	}
	return nil
}
