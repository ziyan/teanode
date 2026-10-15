package cmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Prices: the model prices checked against what the provider billed. The
// same operations as the provider editor's "Check prices now" and the
// settings tool's prices_show and prices_check actions. Both need
// server:manage.

func newAgentPricesCommand() *cli.Command {
	return &cli.Command{
		Name:  "prices",
		Usage: "the model prices checked against the provider's bill, for an operator; needs server:manage",
		Commands: []*cli.Command{
			{
				Name:   "show",
				Usage:  "what the last check of each provider with a billing key compared and changed",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentPricesShow,
			},
			{
				Name:   "check",
				Usage:  "compare the prices with the bill now instead of at the daily check, change the ones that differ, and tell the operators",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentPricesCheck,
			},
		},
	}
}

func runAgentPricesShow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	checks, err := client.AgentBillingChecks(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(checks)
	}
	return printAgentBillingChecks(command, checks, false)
}

func runAgentPricesCheck(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	checks, err := client.CheckAgentProviderPrices(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(checks)
	}
	return printAgentBillingChecks(command, checks, true)
}

// printAgentBillingChecks writes each provider's check: when it ran, why it
// could not read the bill, the prices compared and, after a check run now,
// the ones that were changed.
func printAgentBillingChecks(command *cli.Command, checks []*client.AgentBillingCheck, isShowingChanges bool) error {
	if len(checks) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no openai provider has a billing key; set one on the provider in the agent settings")
		return nil
	}
	for index, check := range checks {
		if index > 0 {
			_, _ = fmt.Fprintln(command.Writer)
		}
		if check.CheckedAt.IsZero() {
			_, _ = fmt.Fprintf(command.Writer, "%s: not checked yet\n", check.ProviderName)
			continue
		}
		_, _ = fmt.Fprintf(command.Writer, "%s: checked %s\n", check.ProviderName, formatTime(&check.CheckedAt))
		if check.ErrorMessage != "" {
			_, _ = fmt.Fprintf(command.Writer, "could not read the bill: %s\n", forTerminal(check.ErrorMessage))
			continue
		}
		rows := make([][]string, 0, len(check.Compared))
		for _, compared := range check.Compared {
			note := ""
			if compared.IsPattern {
				note = "pattern, not changed"
			}
			rows = append(rows, []string{
				compared.Model,
				compared.TokenKind,
				formatPricePerMillion(compared.ConfiguredPerMillion),
				formatPricePerMillion(compared.BilledPerMillion),
				strconv.Itoa(compared.DayCount),
				note,
			})
		}
		if len(rows) == 0 {
			_, _ = fmt.Fprintln(command.Writer, "no price to compare: none of the models the agent used was billed in the last two weeks")
		} else {
			_, _ = fmt.Fprintln(command.Writer, "prices per million tokens")
			if err := printTable([]string{"model", "kind", "configured", "billed", "days", ""}, rows); err != nil {
				return err
			}
		}
		if !isShowingChanges {
			_, _ = fmt.Fprintf(command.Writer, "%d changed\n", len(check.Changed))
			continue
		}
		if len(check.Changed) == 0 {
			_, _ = fmt.Fprintln(command.Writer, "nothing changed")
			continue
		}
		changedRows := make([][]string, 0, len(check.Changed))
		for _, changed := range check.Changed {
			changedRows = append(changedRows, []string{
				changed.Model,
				changed.TokenKind,
				formatPricePerMillion(changed.BeforePerMillion),
				formatPricePerMillion(changed.AfterPerMillion),
			})
		}
		_, _ = fmt.Fprintln(command.Writer, "changed")
		if err := printTable([]string{"model", "kind", "before", "after"}, changedRows); err != nil {
			return err
		}
	}
	return nil
}

// formatPricePerMillion renders a price without trailing zeros. A zero is
// "not set", because the configuration reads zero as unknown, not as free.
func formatPricePerMillion(price float64) string {
	if price == 0 {
		return "not set"
	}
	return strconv.FormatFloat(price, 'f', -1, 64)
}
