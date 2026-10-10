package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/util/qrcode"
)

// newAgentPhoneCommand builds "teanode agent phone": the phones you talk to
// your agent from, in the TeaNode app.
func newAgentPhoneCommand() *cli.Command {
	return &cli.Command{
		Name:  "phone",
		Usage: "the phones you talk to your agent from, in the TeaNode app",
		Description: "A phone connects by scanning a one-time code made where you are signed in: 'pair' draws one\n" +
			"here, and the dashboard's Connect page shows one. The code signs the phone in as you, so show it\n" +
			"to nobody else.",
		Commands: []*cli.Command{
			{
				Name:   "pair",
				Usage:  "draw a one-time code for the TeaNode app on your phone to scan, connecting it as you",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentPhonePair,
			},
		},
	}
}

func runAgentPhonePair(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	code, err := client.CreateOAuthConnectCode(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(code)
	}
	lines, err := qrcode.TerminalLines(code.ConnectLink)
	if err != nil {
		return err
	}
	fmt.Println("Scan this with your phone's camera to connect the TeaNode app as you:")
	fmt.Println()
	for _, line := range lines {
		fmt.Println("  " + line)
	}
	fmt.Println()
	fmt.Println("Or open this link on the phone (it signs the phone in as you; give it to nobody else):")
	fmt.Println("  " + code.ConnectLink)
	fmt.Printf("It works once, until %s.\n", code.ExpiresAt.Local().Format(time.Kitchen))
	return nil
}
