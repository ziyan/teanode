package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewPasskeyCommand builds "teanode passkey": the authenticators registered
// to the account this command acts as. Registering one needs a browser with
// an authenticator to talk to, so that happens in the dashboard; the rest is
// here.
func NewPasskeyCommand() *cli.Command {
	return &cli.Command{
		Name:  "passkey",
		Usage: "the passkeys registered to your account; register one in the dashboard",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list your passkeys, and whether this server offers them",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runPasskeyList,
			},
			{
				Name:      "rename",
				Usage:     "change what an authenticator is called",
				ArgsUsage: "<passkey-id> <name>",
				Action:    runPasskeyRename,
			},
			{
				Name:      "delete",
				Aliases:   []string{"remove"},
				Usage:     "remove a passkey, so that authenticator can no longer sign in",
				ArgsUsage: "<passkey-id>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runPasskeyDelete,
			},
		},
	}
}

func runPasskeyList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	policy, err := client.GetPasskeyPolicy(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	passkeys, err := client.ListPasskeys(ctx, connection)
	if err != nil {
		return describeAccountError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"policy": policy, "passkeys": passkeys})
	}
	if !policy.Enabled {
		fmt.Println("this server does not offer passkeys")
	}
	if len(passkeys) == 0 {
		fmt.Printf("no passkeys; register one under the dashboard's account settings (up to %d)\n", policy.MaximumPerUser)
		return nil
	}
	rows := make([][]string, 0, len(passkeys))
	for _, passkey := range passkeys {
		rows = append(rows, []string{
			passkey.ID, passkey.Name, formatTime(&passkey.CreatedAt), formatTime(&passkey.UsedAt),
			passkey.IP, strings.Join(passkey.Transports, ","), yesNo(passkey.BackupState),
		})
	}
	return printTable([]string{"ID", "NAME", "REGISTERED", "LAST USED", "FROM", "TRANSPORTS", "BACKED UP"}, rows)
}

// describeAccountError explains a refusal that is about who is asking rather
// than what was asked: passkeys belong to an account, and the console — a
// token minted from the server secret — is not one.
func describeAccountError(command *cli.Command, err error) error {
	if !errors.Is(err, client.ErrUnauthorized) {
		return describeError(command, err)
	}
	resolved, resolveError := resolveCommandTarget(command)
	if resolveError == nil && resolved.Local {
		return &describedError{fmt.Errorf("%w; passkeys belong to an account, and the console is not one. "+
			"Sign in as somebody with 'teanode auth login', or pass --url and a token", err)}
	}
	return describeError(command, err)
}

// describePasskeyError is describeAccountError for a command that named one
// passkey, so that a missing one is named too.
func describePasskeyError(command *cli.Command, err error, passkeyId string) error {
	if errors.Is(err, client.ErrNotFound) {
		return describeNotFound(command, err, "passkey "+passkeyId)
	}
	return describeAccountError(command, err)
}

func runPasskeyRename(ctx context.Context, command *cli.Command) error {
	passkeyId, name := command.Args().Get(0), command.Args().Get(1)
	if passkeyId == "" || name == "" {
		return usage("usage: teanode passkey rename <passkey-id> <name>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	passkey, err := client.RenamePasskey(ctx, connection, passkeyId, name)
	if err != nil {
		return describePasskeyError(command, err, passkeyId)
	}
	fmt.Printf("renamed %s to %q\n", passkey.ID, passkey.Name)
	return nil
}

func runPasskeyDelete(ctx context.Context, command *cli.Command) error {
	passkeyId := command.Args().First()
	if passkeyId == "" {
		return usage("which passkey? usage: teanode passkey delete <passkey-id>")
	}
	if err := confirm(command, fmt.Sprintf("This removes passkey %s; the authenticator it names can no longer sign in.", passkeyId)); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeletePasskey(ctx, connection, passkeyId); err != nil {
		return describePasskeyError(command, err, passkeyId)
	}
	fmt.Printf("removed passkey %s\n", passkeyId)
	return nil
}
