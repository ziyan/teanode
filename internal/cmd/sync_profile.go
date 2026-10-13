package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
)

func syncProfileCommands() []*cli.Command {
	return []*cli.Command{
		{Name: "capabilities", Usage: "show the services and owned mailboxes available for device setup", Flags: []cli.Flag{JSONFlag()}, Action: func(ctx context.Context, command *cli.Command) error {
			connection, err := openClient(command)
			if err != nil {
				return err
			}
			capabilities, err := connection.DeviceSetupCapabilities(ctx)
			if err != nil {
				return describeError(command, err)
			}
			return PrintJSON(capabilities)
		}},
		{Name: "profile", Usage: "create one setup profile for explicitly selected Mail, Calendar, Reminders and Contacts", Flags: []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "name", Required: true, Usage: "the device name"}, &cli.BoolFlag{Name: "mail", Usage: "include Mail, using the explicit owned mailbox ID"}, &cli.StringFlag{Name: "mailbox-id", Usage: "owned mailbox ID from sync capabilities; requires --mail"}, &cli.BoolFlag{Name: "calendar"}, &cli.BoolFlag{Name: "reminders"}, &cli.BoolFlag{Name: "contacts"}, &cli.BoolFlag{Name: "download-only", Usage: "allow a local profile download when no device-reachable server name is configured"}}, Action: func(ctx context.Context, command *cli.Command) error {
			selectedServices, err := syncProfileSelections(command)
			if err != nil {
				return usage(err.Error())
			}
			connection, err := openClient(command)
			if err != nil {
				return err
			}
			profile, err := connection.CreateDeviceSetupProfile(ctx, selectedServices)
			if err != nil {
				return describeError(command, err)
			}
			return PrintJSON(profile)
		}},
	}
}

func syncProfileSelections(command *cli.Command) (map[string]any, error) {
	deviceName := strings.TrimSpace(command.String("name"))
	hasMailAccess, hasCalendarAccess, hasRemindersAccess, hasContactsAccess := command.Bool("mail"), command.Bool("calendar"), command.Bool("reminders"), command.Bool("contacts")
	mailboxID := strings.TrimSpace(command.String("mailbox-id"))
	if deviceName == "" || len(deviceName) > 128 || (!hasMailAccess && !hasCalendarAccess && !hasRemindersAccess && !hasContactsAccess) {
		return nil, fmt.Errorf("give a device name and select at least one service")
	}
	if hasMailAccess && mailboxID == "" {
		return nil, fmt.Errorf("--mail requires --mailbox-id from sync capabilities")
	}
	if !hasMailAccess && mailboxID != "" {
		return nil, fmt.Errorf("--mailbox-id requires selecting --mail explicitly")
	}
	var selectedMailboxID any
	if hasMailAccess {
		selectedMailboxID = mailboxID
	}
	return map[string]any{"deviceName": deviceName, "mailboxId": selectedMailboxID, "hasMailAccess": hasMailAccess, "hasCalendarAccess": hasCalendarAccess, "hasRemindersAccess": hasRemindersAccess, "hasContactsAccess": hasContactsAccess, "isDownloadOnly": command.Bool("download-only")}, nil
}
