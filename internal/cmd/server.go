package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewServerCommand builds "teanode server": the running instance, as opposed
// to its configuration.
func NewServerCommand() *cli.Command {
	return &cli.Command{
		Name:  "server",
		Usage: "the running instance: its status, its addresses, and restarting it",
		Commands: []*cli.Command{
			{
				Name:   "status",
				Usage:  "which build is running, for how long, and whether a restart is needed",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runServerStatus,
			},
			{
				Name:  "restart",
				Usage: "restart the instance, which is how a change to the listeners, TLS, storage or the integrations takes effect",
				Description: "The process exits and whatever supervises it — a container's restart\n" +
					"policy, systemd — is expected to start a new one. The server refuses when\n" +
					"it was started in a way that has no supervisor, rather than going down and\n" +
					"staying down.",
				Flags:  []cli.Flag{ForceFlag(), JSONFlag()},
				Action: runServerRestart,
			},
			{
				Name:   "addresses",
				Usage:  "the addresses the outside world reaches this server on, which its DNS records must point at",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runServerAddresses,
			},
			{
				Name:  "identity",
				Usage: "how outgoing mail identifies itself, and whether a strict receiver would believe it",
				Description: "The address mail leaves from, whether its reverse name resolves back to\n" +
					"it, and whether the SMTP greeting agrees. Large receivers refuse mail\n" +
					"outright when these disagree, which is a different question from whether\n" +
					"a domain's records are published.",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runServerIdentity,
			},
		},
	}
}

func runServerStatus(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	status, err := client.GetServerStatus(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(status)
	}

	pending := "nothing; every setting in use is current"
	if len(status.PendingRestart) > 0 {
		pending = strings.Join(status.PendingRestart, ", ") + " changed since it started"
	}
	restarting := ""
	if status.Restarting {
		restarting = " (restart under way)"
	}
	return printFields([][2]string{
		{"instance", status.Instance},
		{"version", status.Version},
		{"commit", status.Commit},
		{"started", status.StartedAt.Local().Format(time.RFC3339)},
		{"uptime", (time.Duration(status.UptimeSeconds) * time.Second).String()},
		{"supervision", status.Supervision + restarting},
		{"needs restart for", pending},
	})
}

func runServerRestart(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	status, err := client.GetServerStatus(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if err := confirm(command, fmt.Sprintf("This restarts instance %s; mail is refused for the few seconds it takes.", status.Instance)); err != nil {
		return err
	}
	result, err := client.RestartServer(ctx, connection)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(result)
	}
	if !result.Started {
		fmt.Printf("instance %s is already restarting\n", result.Instance)
		return nil
	}
	fmt.Printf("instance %s is restarting; %s is expected to bring it back\n", result.Instance, result.Supervision)
	return nil
}

func runServerAddresses(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	addresses, err := client.GetServerAddresses(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(addresses)
	}
	fields := [][2]string{}
	if addresses.IPv4 != "" {
		fields = append(fields, [2]string{"ipv4", addresses.IPv4})
	}
	if addresses.IPv6 != "" {
		fields = append(fields, [2]string{"ipv6", addresses.IPv6})
	}
	if addresses.Error != "" {
		fields = append(fields, [2]string{"problem", addresses.Error})
	}
	if len(fields) == 0 {
		fmt.Println("the server has not worked out its external addresses yet")
		return nil
	}
	return printFields(fields)
}

func runServerIdentity(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	identity, err := client.GetOutgoingIdentity(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(identity)
	}
	confirmed := "no: the reverse name does not resolve back to the address"
	if identity.Confirmed {
		confirmed = "yes"
	}
	helloMatches := "no: the greeting name does not resolve to the address"
	if identity.HelloMatches {
		helloMatches = "yes"
	}
	return printFields([][2]string{
		{"leaves from", identity.Address},
		{"via", identity.Via},
		{"reverse name", identity.ReverseName},
		{"resolves back", strings.Join(identity.ForwardAddresses, ", ")},
		{"confirmed", confirmed},
		{"greeting", identity.HelloName},
		{"greeting resolves to", strings.Join(identity.HelloAddresses, ", ")},
		{"greeting matches", helloMatches},
	})
}
