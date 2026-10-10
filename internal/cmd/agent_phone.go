package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// newAgentPhoneCommand builds "teanode agent phone": the phones you talk to
// your agent from, in the TeaNode app.
func newAgentPhoneCommand() *cli.Command {
	return &cli.Command{
		Name:  "phone",
		Usage: "the phones you talk to your agent from, in the TeaNode app",
		Description: "The app connects to this server by a pairing code: it shows one, such as KQ7M-4TXD, and you\n" +
			"approve it here, on the dashboard's Connect page, or by telling your agent the code. 'approve'\n" +
			"signs the phone in as you; 'deny' turns it away.",
		Commands: []*cli.Command{
			{
				Name:      "approve",
				Usage:     "sign in the phone showing this pairing code, as you",
				ArgsUsage: "<pairing-code>",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "force", Usage: "approve without asking first"},
					JSONFlag(),
				},
				Action: func(ctx context.Context, command *cli.Command) error {
					return runAgentPhoneDecide(ctx, command, true)
				},
			},
			{
				Name:      "deny",
				Usage:     "turn away the phone showing this pairing code",
				ArgsUsage: "<pairing-code>",
				Flags:     []cli.Flag{JSONFlag()},
				Action: func(ctx context.Context, command *cli.Command) error {
					return runAgentPhoneDecide(ctx, command, false)
				},
			},
		},
	}
}

func runAgentPhoneDecide(ctx context.Context, command *cli.Command, isApproved bool) error {
	if command.Args().Len() != 1 {
		return usage("which code: teanode agent phone " + command.Name + " <pairing-code>")
	}
	userCode := command.Args().First()
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if isApproved {
		// Said before it is done, because approving signs a device in as
		// you, and the name it gives is only what it claims.
		pairing, err := client.ReadOAuthDevicePairing(ctx, connection, userCode)
		if err != nil {
			return describeError(command, err)
		}
		if err := confirm(command, describePairing(pairing)); err != nil {
			return err
		}
	}
	pairing, err := client.DecideOAuthDevicePairing(ctx, connection, userCode, isApproved)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(pairing)
	}
	if pairing.IsApproved {
		fmt.Printf("%s is signed in as %s.\n", deviceOf(pairing), pairing.Username)
	} else {
		fmt.Printf("%s was turned away.\n", deviceOf(pairing))
	}
	return nil
}

// describePairing says what approving would do.
func describePairing(pairing *client.OAuthDevicePairing) string {
	access := "use your agent's tools as " + pairing.Username
	if pairing.IsFullAccess {
		access = "do everything you can do on this server, as " + pairing.Username
	}
	return fmt.Sprintf("%s (calling itself %q; the name is not verified) asks to %s.", deviceOf(pairing), pairing.ClientName, access)
}

// deviceOf names the device a pairing is for.
func deviceOf(pairing *client.OAuthDevicePairing) string {
	if pairing.DeviceName != "" {
		return fmt.Sprintf("%q", pairing.DeviceName)
	}
	return "The device"
}
