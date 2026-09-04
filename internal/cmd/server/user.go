package server

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/cmd"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/security"
)

// NewUserCommand builds "teanode-server user", the accounts that administer
// this server, edited in the stored configuration directly.
//
// These exist for a server that will not start, or that nobody can log into.
// Day to day the accounts are managed through the API — "teanode user" from
// the client, or the dashboard — which validates a change the same way
// whichever way it arrives. Writing the configuration underneath a running
// server is safe now: the write is checked against the version it was based
// on, and every running instance notices the new version within seconds.
func NewUserCommand() *cli.Command {
	return &cli.Command{
		Name:  "user",
		Usage: "recover the accounts that administer this server, without going through it",
		Description: "Edits the stored configuration directly, for when the server cannot be\n" +
			"started or nobody can log in. Day to day, use 'teanode user' instead,\n" +
			"which goes through the running server like the dashboard does.",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list the accounts",
				Flags:  []cli.Flag{cmd.JSONFlag()},
				Action: runUserList,
			},
			{
				Name:      "add",
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
				Action: runUserAdd,
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
				Name:      "remove",
				Usage:     "remove an account, along with the API tokens issued to it",
				ArgsUsage: "<username>",
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
	configuration, err := cmd.LoadLocalConfiguration()
	if err != nil {
		return err
	}

	if command.Bool("json") {
		type listed struct {
			Username string `json:"username"`
			Name     string `json:"name,omitempty"`
			Email    string `json:"email,omitempty"`
		}
		users := make([]listed, 0, len(configuration.Users))
		for _, user := range configuration.Users {
			users = append(users, listed{Username: user.Username, Name: user.Name, Email: user.Email})
		}
		return cmd.PrintJSON(users)
	}

	if len(configuration.Users) == 0 {
		fmt.Println("no accounts; this server has not been claimed yet, and the next")
		fmt.Println("person to open the dashboard will be asked to create one")
		return nil
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "USERNAME\tNAME\tEMAIL")
	for _, user := range configuration.Users {
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\n", user.Username, user.Name, user.Email)
	}
	return writer.Flush()
}

func runUserAdd(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode-server user add <username>")
	}
	password, err := cmd.ReadPassword(command.Bool("stdin"))
	if err != nil {
		return err
	}

	return cmd.UpdateLocalConfiguration(func(configuration *config.Configuration) error {
		if configuration.FindUser(username) != nil {
			return fmt.Errorf("%q already exists; use 'teanode-server user password' to change it", username)
		}
		hash, err := security.HashPassword(password)
		if err != nil {
			return err
		}
		configuration.Users = append(configuration.Users, &config.User{
			ID:           config.NewID(),
			Username:     username,
			PasswordHash: string(hash),
			Email:        command.String("email"),
		})
		fmt.Printf("added %s\n", username)
		return nil
	})
}

func runUserPassword(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode-server user password <username>")
	}
	password, err := cmd.ReadPassword(command.Bool("stdin"))
	if err != nil {
		return err
	}

	return cmd.UpdateLocalConfiguration(func(configuration *config.Configuration) error {
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

func runUserRemove(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode-server user remove <username>")
	}

	return cmd.UpdateLocalConfiguration(func(configuration *config.Configuration) error {
		if configuration.FindUser(username) == nil {
			return fmt.Errorf("no account called %q", username)
		}
		// Its tokens go with it, because they live inside it.
		users := make([]*config.User, 0, len(configuration.Users))
		for _, user := range configuration.Users {
			if user != nil && user.Username != username {
				users = append(users, user)
			}
		}
		configuration.Users = users
		fmt.Printf("removed %s\n", username)
		if len(configuration.Users) == 0 {
			printUnclaimedWarning()
		}
		return nil
	})
}

func runUserReset(ctx context.Context, command *cli.Command) error {
	return cmd.UpdateLocalConfiguration(func(configuration *config.Configuration) error {
		existing := len(configuration.Users)
		if existing == 0 {
			fmt.Println("there are no accounts; the server is already unclaimed")
			return nil
		}
		if !command.Bool("force") {
			fmt.Printf("This removes %d account(s) and leaves the server open for anyone who can\n", existing)
			fmt.Printf("reach the dashboard to claim. Type 'yes' to continue: ")
			var answer string
			_, _ = fmt.Scanln(&answer)
			if answer != "yes" {
				return fmt.Errorf("cancelled")
			}
		}
		configuration.Users = nil
		fmt.Printf("removed %d account(s)\n\n", existing)
		fmt.Println("Open the dashboard to create a new one. A running server picks this up")
		fmt.Println("without a restart.")
		return nil
	})
}

func printUnclaimedWarning() {
	fmt.Println("\nThat was the last account. The server is now unclaimed: the next person")
	fmt.Println("to open the dashboard will be asked to create one.")
}
