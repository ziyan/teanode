package cmd

import (
	"context"
	"fmt"
	"github.com/urfave/cli/v3"
)

// NewSyncCommand manages account-owned collection passwords without a mailbox.
func NewSyncCommand() *cli.Command {
	credentialFields := "id credentialName hasCalendarAccess hasRemindersAccess hasContactsAccess createdAt lastUsedAt"
	scopeFlags := []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "name", Required: true, Usage: "the device name"}, &cli.BoolFlag{Name: "calendar"}, &cli.BoolFlag{Name: "reminders"}, &cli.BoolFlag{Name: "contacts"}, &cli.BoolFlag{Name: "profile", Usage: "create an HTTPS setup profile instead of displaying a password"}}
	return &cli.Command{Name: "sync", Usage: "set up or revoke device calendar and contact sync", Commands: []*cli.Command{
		{Name: "list", Flags: []cli.Flag{JSONFlag()}, Action: func(ctx context.Context, command *cli.Command) error {
			connection, err := openClient(command)
			if err != nil {
				return err
			}
			var response map[string]any
			if err := connection.Execute(ctx, "query { ListSyncCredentials { "+credentialFields+" } }", nil, &response); err != nil {
				return describeError(command, err)
			}
			return PrintJSON(response["ListSyncCredentials"])
		}},
		{Name: "create", Flags: scopeFlags, Action: func(ctx context.Context, command *cli.Command) error {
			connection, err := openClient(command)
			if err != nil {
				return err
			}
			operationName := "CreateSyncCredential"
			selectedFields := "username password principalPath credential { " + credentialFields + " }"
			if command.Bool("profile") {
				operationName = "CreateSyncSetupProfile"
				selectedFields = "downloadLink expiresAt credential { " + credentialFields + " }"
			}
			document := "mutation ($credentialName: String!, $hasCalendarAccess: Boolean!, $hasRemindersAccess: Boolean!, $hasContactsAccess: Boolean!) { " + operationName + "(credentialName: $credentialName, hasCalendarAccess: $hasCalendarAccess, hasRemindersAccess: $hasRemindersAccess, hasContactsAccess: $hasContactsAccess) { " + selectedFields + " } }"
			var response map[string]any
			if err := connection.Execute(ctx, document, map[string]any{"credentialName": command.String("name"), "hasCalendarAccess": command.Bool("calendar"), "hasRemindersAccess": command.Bool("reminders"), "hasContactsAccess": command.Bool("contacts")}, &response); err != nil {
				return describeError(command, err)
			}
			return PrintJSON(response[operationName])
		}},
		{Name: "revoke", ArgsUsage: "<credential-id>", Action: func(ctx context.Context, command *cli.Command) error {
			credentialId := command.Args().First()
			if credentialId == "" {
				return fmt.Errorf("give the device credential identifier")
			}
			connection, err := openClient(command)
			if err != nil {
				return err
			}
			var response map[string]any
			if err := connection.Execute(ctx, "mutation ($credentialId: String!) { RevokeSyncCredential(credentialId: $credentialId) }", map[string]any{"credentialId": credentialId}, &response); err != nil {
				return describeError(command, err)
			}
			return PrintJSON(response["RevokeSyncCredential"])
		}},
	}}
}
