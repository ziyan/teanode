package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewUpgradeCommand builds "teanode upgrade": what the server is running,
// what has been released since, and installing a release from here.
func NewUpgradeCommand() *cli.Command {
	return &cli.Command{
		Name:  "upgrade",
		Usage: "the server's version, the newest release, and installing it",
		Commands: []*cli.Command{
			{
				Name:    "status",
				Aliases: []string{"get", "show"},
				Usage:   "what is running, what is available, and whether this deployment can install it",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "check",
						Usage: "ask the release list again first, rather than reporting the last answer",
					},
					&cli.BoolFlag{
						Name:  "notes",
						Usage: "print the newest release's notes",
					},
					JSONFlag(),
				},
				Action: runUpgradeStatus,
			},
			{
				Name:  "apply",
				Usage: "install the newest release, or a named version, and restart",
				Description: "The server downloads the release, checks it against the release's\n" +
					"checksums, replaces its binary and restarts. This answers as soon as that\n" +
					"has started; 'teanode upgrade status' says when it has finished. A\n" +
					"container is upgraded by replacing its image instead, and the server says\n" +
					"so rather than trying.",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "version",
						Usage: "the release to install, for example 0.3.0; the newest unless given",
					},
					ForceFlag(),
					JSONFlag(),
				},
				Action: runUpgradeApply,
			},
		},
	}
}

func runUpgradeStatus(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	upgrade, err := client.GetUpgrade(ctx, connection, command.Bool("check"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(upgrade)
	}
	if err := printFields(upgradeFields(upgrade)); err != nil {
		return err
	}
	if command.Bool("notes") && upgrade.Notes != "" {
		fmt.Printf("\n%s\n", strings.TrimSpace(upgrade.Notes))
	}
	return nil
}

func upgradeFields(upgrade *client.Upgrade) [][2]string {
	latest := upgrade.Latest
	switch {
	case latest == "" && upgrade.Enabled:
		latest = "not known yet; 'teanode upgrade status --check' asks now"
	case latest == "":
		latest = "not checked; upgrades are turned off in the settings"
	case upgrade.Available:
		latest += " (newer)"
	default:
		latest += " (up to date)"
	}
	installable := "yes"
	if !upgrade.Applicable {
		installable = "no"
		if upgrade.Reason != "" {
			installable += ": " + upgrade.Reason
		}
	}
	fields := [][2]string{
		{"running", upgrade.Current},
		{"latest", latest},
		{"last checked", formatTime(upgrade.CheckedAt)},
		{"installable from here", installable},
		{"automatic", yesNo(upgrade.Automatic)},
	}
	if upgrade.Window != "" {
		fields = append(fields, [2]string{"window", upgrade.Window})
	}
	if upgrade.Upgrading {
		fields = append(fields, [2]string{"upgrading", "now"})
	}
	if upgrade.Error != "" {
		fields = append(fields, [2]string{"problem", upgrade.Error})
	}
	if upgrade.CheckError != "" {
		fields = append(fields, [2]string{"last check failed", upgrade.CheckError})
	}
	if upgrade.URL != "" {
		fields = append(fields, [2]string{"release", upgrade.URL})
	}
	return fields
}

func runUpgradeApply(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	current, err := client.GetUpgrade(ctx, connection, false)
	if err != nil {
		return describeError(command, err)
	}
	if !current.Applicable {
		reason := current.Reason
		if reason == "" {
			reason = "this deployment cannot install an upgrade from here"
		}
		return fmt.Errorf("%s", reason)
	}
	target := command.String("version")
	if target == "" {
		target = current.Latest
	}
	if target == "" {
		return fmt.Errorf("no release is known yet; 'teanode upgrade status --check' asks the release list")
	}
	if err := confirm(command, fmt.Sprintf("This installs %s over %s and restarts the server; mail is refused for the few seconds it takes.",
		target, current.Current)); err != nil {
		return err
	}

	upgrade, err := client.ApplyUpgrade(ctx, connection, command.String("version"))
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(upgrade)
	}
	if upgrade.Error != "" {
		return fmt.Errorf("the upgrade did not start: %s", upgrade.Error)
	}
	fmt.Printf("installing %s; the server restarts when the download is checked. 'teanode upgrade status' says when it is done.\n", target)
	return nil
}
