package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// The chat apps, from a terminal: the bot you made in Telegram or Discord,
// its token handed to your agent, and the code a chat sends to link
// itself. The bots run on the server.

func newAgentChannelCommand() *cli.Command {
	return &cli.Command{
		Name:  "channel",
		Usage: "the chat apps you talk to your agent from: your own Telegram or Discord bot",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "your bots, whether each runs, and the code a chat sends to link itself",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentChannelList,
			},
			{
				Name:      "set",
				Usage:     "hand your agent a bot's token (- reads it from stdin), or switch the bot on or off",
				ArgsUsage: "<telegram|discord>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "token", Usage: "the bot's token, from BotFather or the Discord developer portal; - reads it from stdin"},
					&cli.StringFlag{Name: "enabled", Usage: "true or false: whether the bot runs"},
				},
				Action: runAgentChannelSet,
			},
			{
				Name:      "unlink",
				Usage:     "drop the linked chat and draw a new code",
				ArgsUsage: "<telegram|discord>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentChannelUnlink,
			},
			{
				Name:      "remove",
				Usage:     "forget a bot altogether",
				ArgsUsage: "<telegram|discord>",
				Action:    runAgentChannelRemove,
			},
		},
	}
}

func runAgentChannelList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	channels, err := client.ListAgentChannels(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(channels)
	}
	rows := make([][]string, 0, len(channels))
	for _, channel := range channels {
		state := "stopped"
		switch {
		case !channel.Enabled:
			state = "off"
		case channel.Running:
			state = "running"
		case channel.LastError != "":
			state = "failing"
		}
		linked := "not linked; send /link " + channel.LinkCode + " to the bot"
		if channel.Linked {
			linked = "linked to " + channel.LinkedName
		}
		rows = append(rows, []string{channel.Kind, channel.BotName, state, linked, channel.LastError})
	}
	return printTable([]string{"app", "bot", "state", "chat", "error"}, rows)
}

func runAgentChannelSet(ctx context.Context, command *cli.Command) error {
	kind := command.Args().First()
	if kind == "" {
		return fmt.Errorf("which app? telegram or discord")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	token, err := readValue(command, command.String("token"))
	if err != nil {
		return err
	}
	var enabled *bool
	switch command.String("enabled") {
	case "":
	case "true":
		value := true
		enabled = &value
	case "false":
		value := false
		enabled = &value
	default:
		return fmt.Errorf("--enabled takes true or false")
	}
	view, err := client.SetAgentChannel(ctx, connection, kind, token, enabled)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	if view.Linked {
		_, _ = fmt.Fprintf(command.Writer, "%s: set, linked to %s\n", view.Kind, view.LinkedName)
	} else {
		_, _ = fmt.Fprintf(command.Writer, "%s: set; in the app, send the bot: /link %s\n", view.Kind, view.LinkCode)
	}
	return nil
}

func runAgentChannelUnlink(ctx context.Context, command *cli.Command) error {
	kind := command.Args().First()
	if kind == "" {
		return fmt.Errorf("which app? telegram or discord")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.UnlinkAgentChannel(ctx, connection, kind)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: unlinked; in the app, send the bot: /link %s\n", view.Kind, view.LinkCode)
	return nil
}

func runAgentChannelRemove(ctx context.Context, command *cli.Command) error {
	kind := command.Args().First()
	if kind == "" {
		return fmt.Errorf("which app? telegram or discord")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.RemoveAgentChannel(ctx, connection, kind); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: removed\n", kind)
	return nil
}
