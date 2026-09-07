package server

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/cmd"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// NewUserCommand builds "teanode-server user", the accounts on this server,
// edited in the database directly.
//
// These exist for a server that will not start, or that nobody can log into.
// Day to day the accounts are managed through the API — "teanode user" from
// the client, or the web UI — which checks who is asking. Nothing here does:
// whoever can run this on the host holds the database anyway, which is why
// every change made here is recorded in the audit log as a rescue.
func NewUserCommand() *cli.Command {
	return &cli.Command{
		Name:  "user",
		Usage: "recover the accounts that administer this server, without going through it",
		Description: "Edits the user table directly, for when the server cannot be started or\n" +
			"nobody can log in. Day to day, use 'teanode user' instead, which goes\n" +
			"through the running server like the web UI does.",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list the accounts",
				Flags:  []cli.Flag{cmd.JSONFlag()},
				Action: runUserList,
			},
			{
				Name:      "rescue",
				Usage:     "make an account an administrator again, recreating the role and group if they were deleted",
				ArgsUsage: "<username>",
				Description: "The way back for an administrator who edited themselves out. Puts the\n" +
					"account into a group that holds every permission, creating the\n" +
					"Administrator role and the Administrators group again if they are gone.\n" +
					"Recorded in the audit log as a rescue.",
				Action: runUserRescue,
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
	var users []*models.User
	if err := withLocalTransaction(func(tx db.Transaction) error {
		var err error
		users, err = tx.ListUsers()
		return err
	}); err != nil {
		return err
	}

	if command.Bool("json") {
		type listed struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Name     string `json:"name,omitempty"`
			Email    string `json:"email,omitempty"`
			Disabled bool   `json:"disabled"`
		}
		listing := make([]listed, 0, len(users))
		for _, user := range users {
			listing = append(listing, listed{ID: user.ID, Username: user.Username, Name: user.Name, Email: user.Email, Disabled: user.Disabled()})
		}
		return cmd.PrintJSON(listing)
	}

	if len(users) == 0 {
		fmt.Println("no accounts; this server has not been claimed yet, and the next")
		fmt.Println("person to open the web UI will be asked to create one")
		return nil
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "USERNAME\tNAME\tEMAIL\tSTATE")
	for _, user := range users {
		state := "enabled"
		if user.Disabled() {
			state = "disabled"
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", user.Username, user.Name, user.Email, state)
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
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}
	return withLocalTransaction(func(tx db.Transaction) error {
		if existing, err := tx.GetUserByUsername(username); err != nil {
			return err
		} else if existing != nil {
			return fmt.Errorf("%q already exists; use 'teanode-server user password' to change it", username)
		}
		if _, err := access.EnsureSeeded(tx); err != nil {
			return err
		}
		user, err := tx.CreateUser(&models.User{Username: username, PasswordHash: string(hash), Email: command.String("email")})
		if err != nil {
			return err
		}
		// An account made on the host is an administrator: the one reason to
		// make one here is that nobody can sign in.
		if err := access.AddUserToGroups(tx, user.ID, models.GroupNameAdministrators, models.GroupNameMembers); err != nil {
			return err
		}
		fmt.Printf("added %s as an administrator\n", username)
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
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}
	return withLocalTransaction(func(tx db.Transaction) error {
		user, err := tx.GetUserByUsername(username)
		if err != nil {
			return err
		}
		if user == nil {
			return fmt.Errorf("no account called %q", username)
		}
		if _, err := tx.UpdateUser(user.ID, func(user *models.User) error {
			user.PasswordHash = string(hash)
			return nil
		}); err != nil {
			return err
		}
		fmt.Printf("changed the password for %s\n", username)
		return nil
	})
}

func runUserRemove(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode-server user remove <username>")
	}
	return withLocalTransaction(func(tx db.Transaction) error {
		user, err := tx.GetUserByUsername(username)
		if err != nil {
			return err
		}
		if user == nil {
			return fmt.Errorf("no account called %q", username)
		}
		// Its sessions, tokens, passkeys and memberships go with it.
		if err := tx.DeleteUser(user.ID); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", username)
		if count, err := tx.CountUsers(); err == nil && count == 0 {
			printUnclaimedWarning()
		}
		return nil
	})
}

func runUserReset(ctx context.Context, command *cli.Command) error {
	return withLocalTransaction(func(tx db.Transaction) error {
		users, err := tx.ListUsers()
		if err != nil {
			return err
		}
		if len(users) == 0 {
			fmt.Println("there are no accounts; the server is already unclaimed")
			return nil
		}
		if !command.Bool("force") {
			fmt.Printf("This removes %d account(s) and leaves the server open for anyone who can\n", len(users))
			fmt.Printf("reach the web UI to claim. Type 'yes' to continue: ")
			var answer string
			_, _ = fmt.Scanln(&answer)
			if answer != "yes" {
				return fmt.Errorf("cancelled")
			}
		}
		for _, user := range users {
			if err := tx.DeleteUser(user.ID); err != nil {
				return err
			}
		}
		fmt.Printf("removed %d account(s)\n\n", len(users))
		fmt.Println("Open the web UI to create a new one. A running server picks this up")
		fmt.Println("without a restart.")
		return nil
	})
}

func runUserRescue(ctx context.Context, command *cli.Command) error {
	username := command.Args().First()
	if username == "" {
		return fmt.Errorf("which username? usage: teanode-server user rescue <username>")
	}
	return withLocalTransaction(func(tx db.Transaction) error {
		if err := access.Rescue(tx, username); err != nil {
			return err
		}
		fmt.Printf("%s is an administrator again\n", username)
		return nil
	})
}

// withLocalTransaction runs a change against the database the environment
// names, as the rescue actor: the one path that checks no permission, and so
// the one most visibly recorded.
func withLocalTransaction(function func(db.Transaction) error) error {
	store, closeStore, err := cmd.OpenLocalStore()
	if err != nil {
		return err
	}
	defer closeStore()
	defer func() {
		_ = store.Close()
	}()
	database, closeDatabase, err := cmd.OpenLocalDatabase()
	if err != nil {
		return err
	}
	defer closeDatabase()
	if err := database.SetSecret(store.Current().Secret()); err != nil {
		return err
	}
	ctx := db.ContextWithAuditPrincipal(context.Background(), db.AuditPrincipal{ActorKind: models.AuditActorRescue})
	return database.TransactionContext(ctx, function)
}

func printUnclaimedWarning() {
	fmt.Println("\nThat was the last account. The server is now unclaimed: the next person")
	fmt.Println("to open the web UI will be asked to create one.")
}
