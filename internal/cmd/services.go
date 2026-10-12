package cmd

import (
	"context"
	"github.com/urfave/cli/v3"
)

// NewServicesCommand manages deployment-wide hosted service choices.
func NewServicesCommand() *cli.Command {
	return &cli.Command{Name: "services", Usage: "read or change the hosted services offered by this server", Commands: []*cli.Command{
		{Name: "get", Usage: "show running services and any pending restart", Flags: []cli.Flag{JSONFlag()}, Action: func(ctx context.Context, command *cli.Command) error {
			connection, err := openClient(command)
			if err != nil {
				return err
			}
			capabilities, err := connection.ServiceCapabilities(ctx)
			if err != nil {
				return err
			}
			return PrintJSON(capabilities)
		}},
		{Name: "set", Usage: "change service choices (enabling requires a restart)", Flags: []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "mail-routing"}, &cli.BoolFlag{Name: "mailboxes"}, &cli.BoolFlag{Name: "calendar"}, &cli.BoolFlag{Name: "reminders"}, &cli.BoolFlag{Name: "contacts"}}, Action: func(ctx context.Context, command *cli.Command) error {
			connection, err := openClient(command)
			if err != nil {
				return err
			}
			selected := map[string]any{}
			for _, field := range []struct{ flagName, fieldName string }{{"mail-routing", "isMailRoutingEnabled"}, {"mailboxes", "isMailboxEnabled"}, {"calendar", "isCalendarEnabled"}, {"reminders", "isRemindersEnabled"}, {"contacts", "isContactsEnabled"}} {
				if command.IsSet(field.flagName) {
					selected[field.fieldName] = command.Bool(field.flagName)
				}
			}
			capabilities, err := connection.UpdateServices(ctx, selected)
			if err != nil {
				return err
			}
			return PrintJSON(capabilities)
		}},
	}}
}
