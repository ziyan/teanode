package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewLinkCommand builds "teanode link", what the server knows about web links:
// the same previews as the card under a message in the dashboard and the app,
// for links in a conversation.
func NewLinkCommand() *cli.Command {
	return &cli.Command{
		Name:  "link",
		Usage: "web links as the dashboard and the app show them",
		Commands: []*cli.Command{
			{
				Name:      "preview",
				Usage:     "show what the page behind each link says about itself, with the link's tracking parameters taken off",
				ArgsUsage: fmt.Sprintf("<url>... (at most %d)", client.MostLinkPreviewURLs),
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "conversation", Usage: "the conversation the links are in (required): a link gets a preview only if somebody other than the agent wrote it there"},
				},
				Action: runLinkPreview,
			},
		},
	}
}

func runLinkPreview(ctx context.Context, command *cli.Command) error {
	urls := command.Args().Slice()
	if len(urls) == 0 {
		return usage("which links? usage: teanode link preview --conversation <id> <url>...")
	}
	conversationId := strings.TrimSpace(command.String("conversation"))
	if conversationId == "" {
		return usage("which conversation are the links in? give --conversation <id>, as teanode agent conversation list shows them")
	}
	if len(urls) > client.MostLinkPreviewURLs {
		return usage(fmt.Sprintf("at most %d links at a time", client.MostLinkPreviewURLs))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	previews, err := client.ReadLinkPreviews(ctx, connection, conversationId, urls)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(previews)
	}
	for index, preview := range previews {
		if index > 0 {
			_, _ = fmt.Fprintln(command.Writer)
		}
		if err := printLinkPreview(preview); err != nil {
			return err
		}
	}
	return nil
}

// printLinkPreview writes a link's cleaned address, then what its page said,
// or that the page could not be read.
func printLinkPreview(preview *client.LinkPreview) error {
	fields := [][2]string{{"link", preview.CleanedURL}}
	if !preview.IsAvailable {
		fields = append(fields, [2]string{"preview", "none: the page could not be read, or is not a web page"})
		return printFields(fields)
	}
	fields = append(fields, [2]string{"site", preview.SiteName}, [2]string{"title", preview.Title})
	if description := strings.TrimSpace(preview.Description); description != "" {
		fields = append(fields, [2]string{"description", description})
	}
	return printFields(fields)
}
