package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// newAgentTabCommand builds "teanode agent tab": the person's browser as
// the agent reaches it through the extension, and handing back a tab taken
// over on the dashboard's Browser page. Taking a tab over needs its picture,
// so that is the dashboard's.
func newAgentTabCommand() *cli.Command {
	return &cli.Command{
		Name:   "tab",
		Usage:  "your browser as your agent reaches it: its tabs, and whether you have taken one over",
		Flags:  []cli.Flag{JSONFlag()},
		Action: runAgentTabShow,
		Commands: []*cli.Command{
			{
				Name:      "hand-back",
				Usage:     "give your agent back the tab you took over on the dashboard's Browser page",
				ArgsUsage: "[note]",
				Description: "Anything after hand-back is a note for the agent: what you did, or what it\n" +
					"should know. Its requests to take the tab over are answered with it.",
				Action: runAgentTabHandBack,
			},
		},
	}
}

func runAgentTabShow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	tab, err := client.ReadAgentTab(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(tab)
	}
	if !tab.Allowed {
		fmt.Println("attaching a tab is off on this server")
		return nil
	}
	if !tab.Connected {
		fmt.Println("your browser is not connected; sign the TeaNode extension in")
		return nil
	}
	if tab.IsTakenOver {
		fmt.Println("you have taken the tab over: your agent can read it but not act in it until you hand it back")
	} else {
		fmt.Println("your agent holds the tab")
	}
	if len(tab.Tabs) == 0 {
		fmt.Println("no tab is open for your agent yet")
		return nil
	}
	rows := make([][]string, 0, len(tab.Tabs))
	for _, listing := range tab.Tabs {
		var marks []string
		if listing.IsOwn {
			marks = append(marks, "yours")
		}
		if listing.IsCurrent {
			marks = append(marks, "in use")
		}
		rows = append(rows, []string{forTerminal(listing.Title), forTerminal(listing.URL), strings.Join(marks, ", ")})
	}
	return printTable([]string{"TITLE", "ADDRESS", ""}, rows)
}

func runAgentTabHandBack(ctx context.Context, command *cli.Command) error {
	note := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	isHandedBack, err := client.HandBackAgentTab(ctx, connection, note, "", "")
	if err != nil {
		return describeError(command, err)
	}
	if isHandedBack {
		fmt.Println("handed back")
	} else {
		fmt.Println("you had not taken the tab over, and nothing asked for it")
	}
	return nil
}
