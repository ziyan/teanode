package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewSessionCommand builds "teanode session": the browsers an account is
// signed in on.
func NewSessionCommand() *cli.Command {
	return &cli.Command{
		Name:  "session",
		Usage: "the browsers signed in to the dashboard",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list signed-in browsers, newest first",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "user",
						Usage: "whose sessions; required on the server's own console, where the caller is not an account",
					},
					&cli.BoolFlag{
						Name:  "revoked",
						Usage: "include sessions that have been ended",
					},
					JSONFlag(),
				},
				Action: runSessionList,
			},
			{
				Name:      "revoke",
				Usage:     "sign one browser out",
				ArgsUsage: "<session-id>",
				Action:    runSessionRevoke,
			},
			{
				Name:   "revoke-all",
				Usage:  "sign every browser out of the account this command acts as",
				Flags:  []cli.Flag{ForceFlag()},
				Action: runSessionRevokeAll,
			},
		},
	}
}

func runSessionList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	sessions, err := client.ListSessions(ctx, connection, command.String("user"), command.Bool("revoked"))
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(sessions)
	}
	if len(sessions) == 0 {
		fmt.Println("no sessions; nobody is signed in to the dashboard")
		return nil
	}
	rows := make([][]string, 0, len(sessions))
	for _, session := range sessions {
		state := "active"
		if session.Revoked != nil {
			state = "ended"
		}
		rows = append(rows, []string{
			session.ID, formatTime(&session.Created), formatTime(session.LastUsed), session.IP,
			truncate(session.UserAgent, 40), state,
		})
	}
	return printTable([]string{"ID", "SIGNED IN", "LAST USED", "FROM", "BROWSER", "STATE"}, rows)
}

func runSessionRevoke(ctx context.Context, command *cli.Command) error {
	sessionId := command.Args().First()
	if sessionId == "" {
		return fmt.Errorf("which session? usage: teanode session revoke <session-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.RevokeSession(ctx, connection, sessionId); err != nil {
		return describeConnectionError(command, err)
	}
	fmt.Printf("ended session %s\n", sessionId)
	return nil
}

func runSessionRevokeAll(ctx context.Context, command *cli.Command) error {
	if err := confirm(command, "This signs every browser out of the account; API tokens are unaffected."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.RevokeAllSessions(ctx, connection); err != nil {
		return describeConnectionError(command, err)
	}
	fmt.Println("ended every session")
	return nil
}
