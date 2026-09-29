package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Alerts: what your agent told you unasked, and what you asked it not to
// tell you about. The same operations as the Alerts card on the agent page
// and the agent's own agent_profile tool. The switch and the night are
// keys of "agent settings set".

func newAgentAlertCommand() *cli.Command {
	return &cli.Command{
		Name:  "alert",
		Usage: "what your agent told you unasked, and what you asked it not to tell you about",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "the latest alerts, newest first, with the messages each was about",
				Flags:  []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 20}},
				Action: runAgentAlertList,
			},
			{
				Name:      "mute",
				Usage:     "stop being told about something: an alert's subject, sender, domain or kind, or one you name",
				ArgsUsage: "[<alert-id>]",
				Description: "With an alert id, what is muted is taken from the alert: the burst it was about,\n" +
					"or the sender of a single message, or with --scope its subject, senders, their\n" +
					"domains or its kind. Without one, give --target; its scope is read from it (an\n" +
					"address, a domain, else a subject) unless --scope says.\n\n" +
					"  teanode agent alert mute 0f3c9a --scope domain\n" +
					"  teanode agent alert mute --scope sender --target alerts@shop.example.com\n" +
					"  teanode agent alert mute --scope kind --target burst",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "scope", Usage: "sender, domain, subjectKey or kind; by default what the alert was about, or what the target reads as"},
					&cli.StringFlag{Name: "target", Usage: "the address, domain, subject key or kind (burst, or a category such as notification)"},
				},
				Action: runAgentAlertMute,
			},
			{
				Name:   "mutes",
				Usage:  "what you asked not to be told about",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentAlertMutes,
			},
			{
				Name:      "unmute",
				Usage:     "take a mute back",
				ArgsUsage: "<mute-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentAlertUnmute,
			},
		},
	}
}

func runAgentAlertList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	alerts, err := client.ListAgentAlerts(ctx, connection, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(alerts)
	}
	if len(alerts) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "your agent has not told you anything unasked yet")
		return nil
	}
	rows := make([][]string, 0, len(alerts))
	for _, alert := range alerts {
		about := make([]string, 0, len(alert.Covered))
		for _, covered := range alert.Covered {
			about = append(about, fmt.Sprintf("%s (%s)", covered.Subject, covered.FromAddress))
		}
		urgent := ""
		if alert.IsUrgent {
			urgent = "urgent"
		}
		rows = append(rows, []string{alert.ID, formatTime(&alert.SentAt), urgent, alert.SubjectKey, alert.AlertText, strings.Join(about, "; ")})
	}
	return printTable([]string{"id", "sent", "", "subject key", "said", "about"}, rows)
}

func runAgentAlertMute(ctx context.Context, command *cli.Command) error {
	alertId := strings.TrimSpace(command.Args().First())
	muteScope, muteTarget := strings.TrimSpace(command.String("scope")), strings.TrimSpace(command.String("target"))
	if alertId == "" && muteTarget == "" {
		return fmt.Errorf("give an alert id (agent alert list shows them), or --target")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	mute, err := client.MuteAgentAlert(ctx, connection, alertId, muteScope, muteTarget)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(mute)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: muted %s %q\n", mute.ID, mute.MuteScope, mute.MuteTarget)
	return nil
}

func runAgentAlertMutes(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	mutes, err := client.ListAgentAlertMutes(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(mutes)
	}
	if len(mutes) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing is muted")
		return nil
	}
	rows := make([][]string, 0, len(mutes))
	for _, mute := range mutes {
		rows = append(rows, []string{mute.ID, mute.MuteScope, mute.MuteTarget, formatTime(&mute.CreatedAt)})
	}
	return printTable([]string{"id", "scope", "target", "since"}, rows)
}

func runAgentAlertUnmute(ctx context.Context, command *cli.Command) error {
	muteId := strings.TrimSpace(command.Args().First())
	if muteId == "" {
		return fmt.Errorf("which mute? give its id; agent alert mutes shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.UnmuteAgentAlert(ctx, connection, muteId); err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"unmuted": muteId})
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: unmuted\n", muteId)
	return nil
}
