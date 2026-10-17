package cmd

import (
	"context"
	"fmt"
	"strings"
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
			"to nobody else. A phone connected with a code drawn here works for everything but approving a\n" +
			"call that spends your money, or lending it: for that, connect it with the dashboard's Connect\n" +
			"page. 'list' shows your phones, 'notify' chooses what each is told about (message,\n" +
			"approval, question, answer, circle, chat, skill_share_call, location), 'test' sends one a notification, and 'remove' signs it out.\n" +
			"skill_share_call is 'Skill calls waiting for your approval', as the dashboard and the app word it; approval is\n" +
			"for what your own agent asks. location is 'People who share their location arriving at and leaving places'.",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "your phones, and whether this server can send them notifications",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentPhoneList,
			},
			{
				Name:      "rename",
				Usage:     "give a phone another name",
				ArgsUsage: "<phone-id> <name>",
				Action:    runAgentPhoneRename,
			},
			{
				Name:      "notify",
				Usage:     "choose what a phone is told about: any of message, approval, question, answer, circle, chat, skill_share_call (skill calls waiting for your approval), location (people who share their location arriving at and leaving places); 'all' for every kind",
				ArgsUsage: "<phone-id> <kind>...",
				Action:    runAgentPhoneNotify,
			},
			{
				Name:      "test",
				Usage:     "send a phone a notification that says it can be reached",
				ArgsUsage: "<phone-id>",
				Action:    runAgentPhoneTest,
			},
			{
				Name:      "remove",
				Usage:     "remove a phone: it is signed out and told nothing more",
				ArgsUsage: "<phone-id>",
				Flags:     []cli.Flag{&cli.BoolFlag{Name: "force", Usage: "remove without asking first"}},
				Action:    runAgentPhoneRemove,
			},
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

func runAgentPhoneList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := client.ListAgentPhones(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(list)
	}
	if !list.IsPushEnabled {
		fmt.Println("notifications to phones are off on this server (agent.push.enabled)")
	}
	if len(list.Phones) == 0 {
		fmt.Println("no phones; connect one with 'teanode agent phone pair' and the TeaNode app")
		return nil
	}
	rows := make([][]string, 0, len(list.Phones))
	for _, phone := range list.Phones {
		reach := "notified"
		switch {
		case !phone.CanBeNotified:
			reach = "not registered for notifications"
		case phone.LastDeliveryError != "":
			reach = "last failed: " + forTerminal(phone.LastDeliveryError)
		}
		rows = append(rows, []string{
			phone.ID, forTerminal(phone.PhoneName), forTerminal(phone.PhoneModelName), strings.Join(phone.NotificationKinds, ","),
			reach, formatTime(phone.LastSeenAt),
		})
	}
	return printTable([]string{"ID", "NAME", "MODEL", "TOLD ABOUT", "NOTIFICATIONS", "LAST SEEN"}, rows)
}

func runAgentPhoneRename(ctx context.Context, command *cli.Command) error {
	phoneId, name := command.Args().Get(0), strings.TrimSpace(strings.Join(command.Args().Slice()[min(1, command.Args().Len()):], " "))
	if phoneId == "" || name == "" {
		return usage("which phone, and what to call it? usage: teanode agent phone rename <phone-id> <name>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	phone, err := client.UpdateAgentPhone(ctx, connection, phoneId, &name, nil)
	if err != nil {
		return describeNotFound(command, err, "phone "+phoneId)
	}
	fmt.Printf("%s is now %q\n", phone.ID, phone.PhoneName)
	return nil
}

func runAgentPhoneNotify(ctx context.Context, command *cli.Command) error {
	phoneId := command.Args().First()
	kinds := command.Args().Slice()[min(1, command.Args().Len()):]
	if phoneId == "" || len(kinds) == 0 {
		return usage("which phone, and told about what? usage: teanode agent phone notify <phone-id> message approval question answer, or all")
	}
	if len(kinds) == 1 && kinds[0] == "all" {
		kinds = []string{}
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	phone, err := client.UpdateAgentPhone(ctx, connection, phoneId, nil, &kinds)
	if err != nil {
		return describeNotFound(command, err, "phone "+phoneId)
	}
	fmt.Printf("%s is told about: %s\n", forTerminal(phone.PhoneName), strings.Join(phone.NotificationKinds, ", "))
	return nil
}

func runAgentPhoneTest(ctx context.Context, command *cli.Command) error {
	phoneId := command.Args().First()
	if phoneId == "" {
		return usage("which phone? usage: teanode agent phone test <phone-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	phone, err := client.TestAgentPhone(ctx, connection, phoneId)
	if err != nil {
		return describeNotFound(command, err, "phone "+phoneId)
	}
	fmt.Printf("sent %s a notification; it should arrive in a few seconds\n", forTerminal(phone.PhoneName))
	return nil
}

func runAgentPhoneRemove(ctx context.Context, command *cli.Command) error {
	phoneId := command.Args().First()
	if phoneId == "" {
		return usage("which phone? usage: teanode agent phone remove <phone-id>")
	}
	if err := confirm(command, "The phone "+phoneId+" will be signed out and told nothing more."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.RemoveAgentPhone(ctx, connection, phoneId); err != nil {
		return describeNotFound(command, err, "phone "+phoneId)
	}
	fmt.Printf("removed %s\n", phoneId)
	return nil
}
