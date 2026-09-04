package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewUserCommand builds "teanode user", for the accounts that administer this
// server. Normally the first account is created in the browser on first run;
// these exist for when a browser is not an option.
//
// Everything here goes through the running server. A server that will not
// start, or that nobody can log into, is what "teanode-server user" is for:
// it edits the stored configuration directly.
func NewUserCommand() *cli.Command {
	return &cli.Command{
		Name:  "user",
		Usage: "manage the accounts that administer this server",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list the accounts",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runUserList,
			},
			{
				Name:      "create",
				Aliases:   []string{"add"},
				Usage:     "add an account, prompting for its password",
				ArgsUsage: "<username>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "email",
						Usage: "address that receives notifications, such as a domain whose DNS records have stopped resolving",
					},
					&cli.BoolFlag{
						Name:  "stdin",
						Usage: "read the password from standard input instead of prompting",
					},
				},
				Action: runUserCreate,
			},
			{
				Name:      "update",
				Usage:     "change an account's name, address, or the username it signs in with",
				ArgsUsage: "<username>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "name",
						Usage: "what to call this person; empty clears it",
					},
					&cli.StringFlag{
						Name:  "email",
						Usage: "address that receives notifications; empty clears it",
					},
					&cli.StringFlag{
						Name:  "rename",
						Usage: "the username to sign in with from now on; sessions and tokens move with the account",
					},
					JSONFlag(),
				},
				Action: runUserUpdate,
			},
			{
				Name:      "password",
				Usage:     "set an account's password",
				ArgsUsage: "<username>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "stdin",
						Usage: "read the password from standard input instead of prompting",
					},
				},
				Action: runUserPassword,
			},
			{
				Name:      "delete",
				Aliases:   []string{"remove"},
				Usage:     "remove an account, along with the API tokens issued to it",
				ArgsUsage: "<username>",
				Action:    runUserDelete,
			},
			{
				Name:  "reset",
				Usage: "remove every account, so the server asks for a new one on next visit",
				Description: "Leaves the server unclaimed: the next person to open the dashboard is\n" +
					"asked to create an account. Useful for trying the first-run flow. Anyone\n" +
					"who can reach the dashboard can claim it until somebody does, so do not\n" +
					"leave it in that state.",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "force",
						Usage: "do not ask for confirmation",
					},
				},
				Action: runUserReset,
			},
		},
	}
}

func runUserList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	users, err := client.ListUsers(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(users)
	}
	if len(users) == 0 {
		fmt.Println("no accounts; this server has not been claimed yet, and the next")
		fmt.Println("person to open the dashboard will be asked to create one")
		return nil
	}

	rows := make([][]string, 0, len(users))
	for _, user := range users {
		rows = append(rows, []string{user.Username, user.Name, user.Email})
	}
	return printTable([]string{"USERNAME", "NAME", "EMAIL"}, rows)
}

func runUserCreate(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode user create <username>")
	}
	password, err := ReadPassword(command.Bool("stdin"))
	if err != nil {
		return err
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if _, err := client.CreateUser(ctx, connection, username, password, command.String("email")); err != nil {
		return describeConnectionError(command, err)
	}
	fmt.Printf("added %s\n", username)
	return nil
}

func runUserPassword(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode user password <username>")
	}
	password, err := ReadPassword(command.Bool("stdin"))
	if err != nil {
		return err
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if _, err := client.SetUserPassword(ctx, connection, username, password); err != nil {
		return describeConnectionError(command, err)
	}
	fmt.Printf("changed the password for %s\n", username)
	return nil
}

func runUserDelete(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode user delete <username>")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteUser(ctx, connection, username); err != nil {
		return describeConnectionError(command, err)
	}
	fmt.Printf("removed %s\n", username)

	remaining, err := client.ListUsers(ctx, connection)
	if err == nil && len(remaining) == 0 {
		fmt.Println("\nThat was the last account. The server is now unclaimed: the next person")
		fmt.Println("to open the dashboard will be asked to create one.")
	}
	return nil
}

func runUserReset(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	users, err := client.ListUsers(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if len(users) == 0 {
		fmt.Println("there are no accounts; the server is already unclaimed")
		return nil
	}
	if err := confirm(command, fmt.Sprintf("This removes %d account(s) and leaves the server open for anyone who can\n"+
		"reach the dashboard to claim.", len(users))); err != nil {
		return err
	}
	for _, user := range users {
		if err := client.DeleteUser(ctx, connection, user.Username); err != nil {
			return err
		}
	}
	fmt.Printf("removed %d account(s)\n\n", len(users))
	fmt.Println("Open the dashboard to create a new one. A running server picks this up")
	fmt.Println("without a restart.")
	return nil
}

func runUserUpdate(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode user update <username> [--name ...] [--email ...] [--rename ...]")
	}
	parameters := &client.UserParameters{}
	if command.IsSet("name") {
		value := command.String("name")
		parameters.Name = &value
	}
	if command.IsSet("email") {
		value := command.String("email")
		parameters.Email = &value
	}
	if command.IsSet("rename") {
		value := command.String("rename")
		parameters.NewUsername = &value
	}
	if *parameters == (client.UserParameters{}) {
		return fmt.Errorf("nothing to change; pass --name, --email or --rename")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	user, err := client.UpdateUser(ctx, connection, username, parameters)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(user)
	}
	fmt.Printf("changed %s\n", user.Username)
	return nil
}
