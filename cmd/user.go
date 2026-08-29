package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/security"
)

// NewUserCommand builds "teanode user", for the accounts that administer this
// server. Normally the first account is created in the browser on first run;
// these exist for when a browser is not an option, and for recovering from
// being locked out.
func NewUserCommand() *cli.Command {
	offline := &cli.BoolFlag{
		Name: "offline",
		Usage: "edit the configuration file directly instead of going through the server; " +
			"only for when the server cannot be started or nobody can log in",
	}

	return &cli.Command{
		Name:  "user",
		Usage: "manage the accounts that administer this server",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list the accounts",
				Flags:  []cli.Flag{offline, JSONFlag()},
				Action: runUserList,
			},
			{
				Name:      "add",
				Usage:     "add an account, prompting for its password",
				ArgsUsage: "<username>",
				Flags: []cli.Flag{
					offline,
					&cli.StringFlag{
						Name:  "email",
						Usage: "address that receives notifications, such as a domain whose DNS records have stopped resolving",
					},
					&cli.BoolFlag{
						Name:  "stdin",
						Usage: "read the password from standard input instead of prompting",
					},
				},
				Action: runUserAdd,
			},
			{
				Name:      "password",
				Usage:     "set an account's password",
				ArgsUsage: "<username>",
				Flags: []cli.Flag{
					offline,
					&cli.BoolFlag{
						Name:  "stdin",
						Usage: "read the password from standard input instead of prompting",
					},
				},
				Action: runUserPassword,
			},
			{
				Name:      "remove",
				Usage:     "remove an account, along with the API tokens issued to it",
				ArgsUsage: "<username>",
				Flags:     []cli.Flag{offline},
				Action:    runUserRemove,
			},
			{
				Name:  "reset",
				Usage: "remove every account, so the server asks for a new one on next visit",
				Description: "Leaves the server unclaimed: the next person to open the dashboard is\n" +
					"asked to create an account. Useful for trying the first-run flow, and for\n" +
					"recovering when nobody can log in. Anyone who can reach the dashboard can\n" +
					"claim it until somebody does, so do not leave it in that state.",
				Flags: []cli.Flag{
					offline,
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
	if command.Bool("offline") {
		configuration, err := loadOfflineConfiguration(command)
		if err != nil {
			return err
		}
		users := make([]*client.User, 0, len(configuration.Users))
		for _, user := range configuration.Users {
			users = append(users, &client.User{Username: user.Username, Email: user.Email})
		}
		return printUsers(command, users)
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	users, err := client.ListUsers(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	return printUsers(command, users)
}

func printUsers(command *cli.Command, users []*client.User) error {
	if command.Bool("json") {
		return printJSON(users)
	}
	if len(users) == 0 {
		fmt.Println("no accounts; this server has not been claimed yet, and the next")
		fmt.Println("person to open the dashboard will be asked to create one")
		return nil
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "USERNAME\tEMAIL")
	for _, user := range users {
		_, _ = fmt.Fprintf(writer, "%s\t%s\n", user.Username, user.Email)
	}
	return writer.Flush()
}

func runUserAdd(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode user add <username>")
	}

	password, err := readPassword(command.Bool("stdin"))
	if err != nil {
		return err
	}
	email := command.String("email")

	if command.Bool("offline") {
		return updateOffline(command, func(configuration *config.Configuration) error {
			if configuration.FindUser(username) != nil {
				return fmt.Errorf("%q already exists; use 'teanode user password' to change it", username)
			}
			hash, err := security.HashPassword(password)
			if err != nil {
				return err
			}
			configuration.Users = append(configuration.Users, &config.User{
				ID:           config.NewID(),
				Username:     username,
				PasswordHash: string(hash),
				Email:        email,
			})
			fmt.Printf("added %s\n", username)
			return nil
		})
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if _, err := client.CreateUser(ctx, connection, username, password, email); err != nil {
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

	password, err := readPassword(command.Bool("stdin"))
	if err != nil {
		return err
	}

	if command.Bool("offline") {
		return updateOffline(command, func(configuration *config.Configuration) error {
			user := configuration.FindUser(username)
			if user == nil {
				return fmt.Errorf("no account called %q", username)
			}
			hash, err := security.HashPassword(password)
			if err != nil {
				return err
			}
			user.PasswordHash = string(hash)
			fmt.Printf("changed the password for %s\n", username)
			return nil
		})
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

func runUserRemove(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode user remove <username>")
	}

	if command.Bool("offline") {
		return updateOffline(command, func(configuration *config.Configuration) error {
			if configuration.FindUser(username) == nil {
				return fmt.Errorf("no account called %q", username)
			}
			removeUser(configuration, username)
			fmt.Printf("removed %s\n", username)
			if len(configuration.Users) == 0 {
				printUnclaimedWarning()
			}
			return nil
		})
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
		printUnclaimedWarning()
	}
	return nil
}

func runUserReset(ctx context.Context, command *cli.Command) error {
	if command.Bool("offline") {
		return updateOffline(command, func(configuration *config.Configuration) error {
			existing := len(configuration.Users)
			if existing == 0 {
				fmt.Println("there are no accounts; the server is already unclaimed")
				return nil
			}
			if err := confirmReset(command, existing); err != nil {
				return err
			}
			configuration.Users = nil
			printReset(existing)
			return nil
		})
	}

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
	if err := confirmReset(command, len(users)); err != nil {
		return err
	}
	for _, user := range users {
		if err := client.DeleteUser(ctx, connection, user.Username); err != nil {
			return err
		}
	}
	printReset(len(users))
	return nil
}

func confirmReset(command *cli.Command, existing int) error {
	if command.Bool("force") {
		return nil
	}
	fmt.Printf("This removes %d account(s) and leaves the server open for anyone who can\n", existing)
	fmt.Printf("reach the dashboard to claim. Type 'yes' to continue: ")
	var answer string
	_, _ = fmt.Scanln(&answer)
	if answer != "yes" {
		return fmt.Errorf("cancelled")
	}
	return nil
}

func printReset(existing int) {
	fmt.Printf("removed %d account(s)\n\n", existing)
	fmt.Println("Open the dashboard to create a new one. A running server picks this up")
	fmt.Println("without a restart.")
}

func printUnclaimedWarning() {
	fmt.Println("\nThat was the last account. The server is now unclaimed: the next person")
	fmt.Println("to open the dashboard will be asked to create one.")
}

// removeUser drops an account. Its tokens go with it, because they live
// inside it.
func removeUser(configuration *config.Configuration, username string) {
	users := make([]*config.User, 0, len(configuration.Users))
	for _, user := range configuration.Users {
		if user != nil && user.Username != username {
			users = append(users, user)
		}
	}
	configuration.Users = users
}
