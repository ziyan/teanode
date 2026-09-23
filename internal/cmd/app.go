package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewAppCommand builds "teanode app", the programs authorized to act as
// somebody. Each holds a token it renews by itself, so it is the app that
// is named and disconnected, not the token of the moment.
func NewAppCommand() *cli.Command {
	userFlag := func() cli.Flag {
		return &cli.StringFlag{
			Name:  "user",
			Usage: "whose apps; required on the server's own console, where the caller is not an account",
		}
	}
	return &cli.Command{
		Name:  "app",
		Usage: "the apps authorized to act as you",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list the apps holding a token of yours",
				Flags:  []cli.Flag{userFlag(), JSONFlag()},
				Action: runAppList,
			},
			{
				Name:      "rename",
				Usage:     "rename an app; the name stays when it renews",
				ArgsUsage: "<client-id> <name>",
				Flags:     []cli.Flag{userFlag()},
				Action:    runAppRename,
			},
			{
				Name:      "disconnect",
				Usage:     "revoke every token an app holds, so it has to be authorized again",
				ArgsUsage: "<client-id>",
				Flags:     []cli.Flag{userFlag()},
				Action:    runAppDisconnect,
			},
		},
	}
}

func runAppList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	apps, err := client.ListApps(ctx, connection, command.String("user"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(apps)
	}
	if len(apps) == 0 {
		fmt.Println("no apps; an app appears here once you authorize it")
		return nil
	}
	rows := make([][]string, 0, len(apps))
	for _, app := range apps {
		rows = append(rows, []string{
			app.ClientID, app.Name, strings.Join(app.RedirectHosts, ", "),
			formatTime(app.LastUsed), formatTime(app.RenewableUntil),
		})
	}
	return printTable([]string{"CLIENT", "NAME", "RETURNS TO", "LAST USED", "RENEWS UNTIL"}, rows)
}

func runAppRename(ctx context.Context, command *cli.Command) error {
	clientId, name := command.Args().Get(0), strings.TrimSpace(strings.Join(command.Args().Slice()[min(1, command.Args().Len()):], " "))
	if clientId == "" || name == "" {
		return usage("which app, and what to call it? usage: teanode app rename <client-id> <name>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.RenameApp(ctx, connection, clientId, command.String("user"), name); err != nil {
		return describeNotFound(command, err, "app "+clientId+" holding a token of this account")
	}
	fmt.Printf("%s is now %q\n", clientId, name)
	return nil
}

func runAppDisconnect(ctx context.Context, command *cli.Command) error {
	clientId := command.Args().First()
	if clientId == "" {
		return usage("which app? usage: teanode app disconnect <client-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DisconnectApp(ctx, connection, clientId, command.String("user")); err != nil {
		return describeNotFound(command, err, "app "+clientId+" holding a token of this account")
	}
	fmt.Printf("disconnected %s\n", clientId)
	return nil
}
