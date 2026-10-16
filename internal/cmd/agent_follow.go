package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Follow-ups: the bills, parcels, deadlines and coming events your agent
// keeps track of from your mail. The same operations as the Following list
// on the Alerts card and the agent's own agent_profile tool.

func newAgentFollowCommand() *cli.Command {
	return &cli.Command{
		Name:  "follow",
		Usage: "the bills, parcels, deadlines and events your agent is keeping track of",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "what your agent is following, the nearest day first; --all adds what is over",
				Flags:  []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "all", Usage: "the newest of every state, including closed, expired and stopped"}},
				Action: runAgentFollowList,
			},
			{
				Name:  "add",
				Usage: "have your agent follow something you name",
				Description: "A bill needs --amount and --due, a deadline or an event needs --due, and a parcel\n" +
					"needs --tracking or a --title. Naming what your agent already found keeps one\n" +
					"follow-up. Days are written YYYY-MM-DD.\n\n" +
					"  teanode agent follow add --kind bill --title \"Water bill\" --payee \"Example Water\" --amount 84.20 --currency USD --due 2026-11-03\n" +
					"  teanode agent follow add --kind parcel --title \"Garden shears\" --carrier \"Example Post\" --tracking EX123456789 --due 2026-10-24\n" +
					"  teanode agent follow add --kind event --title \"Street fair\" --place \"Elm Street\" --due 2026-10-31",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "kind", Usage: "bill, parcel, deadline or event", Required: true},
					&cli.StringFlag{Name: "title", Usage: "one line saying what it is", Required: true},
					&cli.StringFlag{Name: "due", Usage: "the day it is due, arrives or happens, YYYY-MM-DD; a parcel may leave it out"},
					&cli.StringFlag{Name: "payee", Usage: "for a bill: who is to be paid"},
					&cli.StringFlag{Name: "amount", Usage: "for a bill: how much, such as 84.20"},
					&cli.StringFlag{Name: "currency", Usage: "for a bill: the currency code, such as USD"},
					&cli.StringFlag{Name: "carrier", Usage: "for a parcel: who carries it"},
					&cli.StringFlag{Name: "tracking", Usage: "for a parcel: its tracking number"},
					&cli.StringFlag{Name: "place", Usage: "for an event: where"},
				},
				Action: runAgentFollowAdd,
			},
			{
				Name:      "stop",
				Usage:     "stop following one; it is kept, and nothing more is said about it",
				ArgsUsage: "<follow-up-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentFollowStop,
			},
		},
	}
}

func runAgentFollowList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	followUps, err := client.ListAgentFollowUps(ctx, connection, !command.Bool("all"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(followUps)
	}
	if len(followUps) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "your agent is not following anything yet")
		return nil
	}
	rows := make([][]string, 0, len(followUps))
	for _, followUp := range followUps {
		rows = append(rows, []string{
			followUp.ID, followUp.FollowUpKind, followUp.DueOn, followUpAmountWords(followUp), followUp.FollowUpState,
			followUp.FollowUpTitle, followUpDetailWords(followUp),
		})
	}
	return printTable([]string{"id", "kind", "due", "amount", "state", "title", "about"}, rows)
}

// followUpAmountWords is a bill's amount with its currency, and nothing for
// the kinds that have none.
func followUpAmountWords(followUp *client.AgentFollowUp) string {
	return strings.TrimSpace(followUp.Amount + " " + followUp.CurrencyCode)
}

// followUpDetailWords is who a follow-up is with or where: the payee of a
// bill, the carrier and tracking number of a parcel, the place of an event,
// and the page one found on the web came from.
func followUpDetailWords(followUp *client.AgentFollowUp) string {
	parts := []string{}
	for _, part := range []string{followUp.Payee, followUp.Carrier, followUp.TrackingNumber, followUp.Place, followUp.SourceURL} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}

func runAgentFollowAdd(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	followUp, err := client.AddAgentFollowUp(ctx, connection, client.NewAgentFollowUp{
		FollowUpKind: strings.TrimSpace(command.String("kind")), FollowUpTitle: strings.TrimSpace(command.String("title")),
		DueOn: strings.TrimSpace(command.String("due")), Payee: strings.TrimSpace(command.String("payee")),
		Amount: strings.TrimSpace(command.String("amount")), CurrencyCode: strings.TrimSpace(command.String("currency")),
		Carrier: strings.TrimSpace(command.String("carrier")), TrackingNumber: strings.TrimSpace(command.String("tracking")),
		Place: strings.TrimSpace(command.String("place")),
	})
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(followUp)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: following %s %q\n", followUp.ID, followUp.FollowUpKind, followUp.FollowUpTitle)
	return nil
}

func runAgentFollowStop(ctx context.Context, command *cli.Command) error {
	followUpId := strings.TrimSpace(command.Args().First())
	if followUpId == "" {
		return fmt.Errorf("which follow-up? give its id; agent follow list shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	followUp, err := client.StopAgentFollowUp(ctx, connection, followUpId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(followUp)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s %q\n", followUp.ID, followUpStopWords(followUp), followUp.FollowUpTitle)
	return nil
}

// followUpStopWords says what became of a follow-up asked to stop: stopped,
// or, when it was already over, how it ended.
func followUpStopWords(followUp *client.AgentFollowUp) string {
	if followUp.FollowUpState == "stopped" {
		return "stopped following"
	}
	return "was already " + followUp.FollowUpState + ", not following"
}
