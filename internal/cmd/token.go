package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewTokenCommand builds "teanode token", which issues the tokens that let
// this tool administer a server from somewhere else.
//
// On the server itself no token is needed: the tool authenticates with one it
// mints from the server secret in the configuration file. Tokens are for
// running "teanode --url https://mail.example.com ..." from a laptop, or from
// a deployment script.
func NewTokenCommand() *cli.Command {
	return &cli.Command{
		Name:  "token",
		Usage: "manage API tokens for administering this server remotely",
		Commands: []*cli.Command{
			{
				Name:      "create",
				Usage:     "issue a token and print it",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "user",
						Usage: "account the token belongs to and acts as; required on the server's own console, where the caller is not an account",
					},
					&cli.StringFlag{
						Name:  "lifetime",
						Usage: "how long it lasts, for example 720h; omit for a token that does not expire",
					},
					JSONFlag(),
				},
				Action: runTokenCreate,
			},
			{
				Name:  "list",
				Usage: "list the issued tokens",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "user",
						Usage: "whose tokens; required on the server's own console, where the caller is not an account",
					},
					&cli.BoolFlag{
						Name:  "revoked",
						Usage: "include tokens that have been revoked",
					},
					JSONFlag(),
				},
				Action: runTokenList,
			},
			{
				Name:      "revoke",
				Usage:     "revoke a token",
				ArgsUsage: "<id>",
				Action:    runTokenRevoke,
			},
		},
	}
}

func runTokenCreate(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("what will hold it? usage: teanode token create <name>")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	username, err := tokenOwner(ctx, command, connection)
	if err != nil {
		return err
	}

	token, secret, err := client.CreateToken(ctx, connection, name, username, command.String("lifetime"))
	if err != nil {
		return describeConnectionError(command, err)
	}

	if command.Bool("json") {
		return PrintJSON(map[string]any{"token": token, "secret": secret})
	}

	fmt.Printf("Issued %s for %s. Only its hash is stored, so this is the only time\n", token.ID, token.Username)
	fmt.Printf("it is shown.\n\n")
	fmt.Printf("  %s\n\n", secret)
	fmt.Printf("Use it from another machine with:\n\n")
	fmt.Printf("  export TEANODE_URL=https://%s\n", "your-server")
	fmt.Printf("  export TEANODE_TOKEN=%s\n\n", secret)
	fmt.Printf("Or keep it in %s, which is read when TEANODE_TOKEN is not set.\n", tokenFilePath())
	return nil
}

func runTokenList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	tokens, err := client.ListTokens(ctx, connection, command.String("user"), command.Bool("revoked"))
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(tokens)
	}
	if len(tokens) == 0 {
		fmt.Println("no tokens; issue one with 'teanode token create <name>'")
		return nil
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "ID\tNAME\tACTS AS\tCREATED\tEXPIRES\tLAST USED\tSTATE")
	for _, token := range tokens {
		expires := "never"
		if token.Expires != nil {
			expires = token.Expires.Local().Format(time.RFC3339)
		}
		lastUsed := "never"
		if token.LastUsed != nil {
			lastUsed = token.LastUsed.Local().Format(time.RFC3339)
		}
		state := "enabled"
		switch {
		case token.Revoked != nil:
			state = "revoked"
		case token.Expires != nil && token.Expires.Before(time.Now()):
			state = "expired"
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			token.ID, token.Name, token.Username,
			token.Created.Local().Format(time.RFC3339), expires, lastUsed, state)
	}
	return writer.Flush()
}

func runTokenRevoke(ctx context.Context, command *cli.Command) error {
	id := command.Args().First()
	if id == "" {
		return fmt.Errorf("which token? usage: teanode token revoke <id>")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteToken(ctx, connection, id); err != nil {
		return describeConnectionError(command, err)
	}
	fmt.Printf("revoked %s\n", id)
	return nil
}

// tokenOwner works out which account a new token should belong to.
//
// The dashboard and a client already holding a token both act as somebody, and
// that is the answer. On the server's own console the caller is not an account
// at all, so --user is required — unless there is exactly one account, which
// is the common case and leaves nothing to guess.
func tokenOwner(ctx context.Context, command *cli.Command, connection *client.Client) (string, error) {
	if username := command.String("user"); username != "" {
		return username, nil
	}

	current, err := client.GetCurrentUser(ctx, connection)
	if err != nil {
		return "", describeConnectionError(command, err)
	}
	if current != nil {
		return current.Username, nil
	}

	users, err := client.ListUsers(ctx, connection)
	if err != nil {
		return "", err
	}
	switch len(users) {
	case 0:
		return "", fmt.Errorf("a token belongs to an account, and this server has none yet; " +
			"create one with 'teanode user add <username>', or open the dashboard")
	case 1:
		return users[0].Username, nil
	default:
		names := make([]string, 0, len(users))
		for _, user := range users {
			names = append(names, user.Username)
		}
		return "", fmt.Errorf("which account should hold it? pass --user with one of: %s",
			strings.Join(names, ", "))
	}
}
