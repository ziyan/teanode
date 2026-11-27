package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// The models a provider offers: "teanode settings agent model". The same
// operations as the provider page's "Add model" and a model's own page, and
// they need server:manage like the rest of the settings.

func newSettingsAgentCommand() *cli.Command {
	return &cli.Command{
		Name:  "agent",
		Usage: "the agent's providers and the models they offer, for an operator; needs server:manage",
		Commands: []*cli.Command{
			{
				Name:  "model",
				Usage: "the models a provider offers, each with its own name, context window and prices",
				Description: "A role (default, quick, embedding, ...) names a model as provider:model, and\n" +
					"only a model listed here can be named. 'teanode settings set agent' changes the\n" +
					"roles.\n\n" +
					"  teanode settings agent model list\n" +
					"  teanode settings agent model add openai gpt-6.1-sol --name Sol --input 2.5 --output 10\n" +
					"  teanode settings agent model set openai gpt-6.1-sol --context-window 200000\n" +
					"  teanode settings agent model remove openai gpt-5-old",
				Commands: []*cli.Command{
					{
						Name:   "list",
						Usage:  "every model of every provider, with its window, prices and what uses it",
						Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "provider", Usage: "only this provider's models"}},
						Action: runSettingsAgentModelList,
					},
					{
						Name:      "add",
						Usage:     "offer a model on a provider",
						ArgsUsage: "PROVIDER MODEL",
						Flags:     settingsAgentModelFlags(),
						Action:    runSettingsAgentModelAdd,
					},
					{
						Name:      "set",
						Usage:     "change a model; what is not given stays as it is",
						ArgsUsage: "PROVIDER MODEL",
						Flags:     settingsAgentModelFlags(),
						Action:    runSettingsAgentModelSet,
					},
					{
						Name:      "remove",
						Usage:     "stop offering a model; refused while a role uses it",
						ArgsUsage: "PROVIDER MODEL",
						Flags:     []cli.Flag{JSONFlag()},
						Action:    runSettingsAgentModelRemove,
					},
				},
			},
		},
	}
}

func settingsAgentModelFlags() []cli.Flag {
	return []cli.Flag{
		JSONFlag(),
		&cli.StringFlag{Name: "name", Usage: "what people read instead of the id; empty for the id"},
		&cli.IntFlag{Name: "context-window", Usage: "the window in tokens, correcting the provider's figure; 0 takes the provider's"},
		&cli.Float64Flag{Name: "input", Usage: "price per million input tokens; 0 is unknown"},
		&cli.Float64Flag{Name: "output", Usage: "price per million output tokens"},
		&cli.Float64Flag{Name: "cache-read", Usage: "price per million cached input tokens"},
		&cli.Float64Flag{Name: "cache-write", Usage: "price per million tokens written to the cache"},
		&cli.Float64Flag{Name: "audio-input", Usage: "price per million audio input tokens"},
		&cli.BoolFlag{Name: "enabled", Usage: "whether the model is offered; --enabled=false keeps it listed but off"},
	}
}

func settingsAgentModelArguments(command *cli.Command) (providerName, model string, err error) {
	if command.Args().Len() != 2 {
		return "", "", usage("usage: teanode settings agent model " + command.Name + " PROVIDER MODEL")
	}
	return strings.TrimSpace(command.Args().Get(0)), strings.TrimSpace(command.Args().Get(1)), nil
}

func runSettingsAgentModelList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	providers, err := client.AgentProviderModelList(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	only := command.String("provider")
	selected := []*client.AgentProviderModels{}
	for _, provider := range providers {
		if only == "" || provider.ProviderName == only {
			selected = append(selected, provider)
		}
	}
	if only != "" && len(selected) == 0 {
		return fmt.Errorf("no provider called %q is declared", only)
	}
	if command.Bool("json") {
		return PrintJSON(selected)
	}
	rows := [][]string{}
	for _, provider := range selected {
		for _, entry := range provider.Models {
			name := entry.Name
			if name == "" {
				name = "-"
			}
			state := "on"
			if !entry.IsEnabled {
				state = "off"
			}
			rows = append(rows, []string{
				provider.ProviderName, forTerminal(name), forTerminal(entry.Model), formatContextWindow(entry),
				formatPricePerMillion(entry.Pricing.Input), formatPricePerMillion(entry.Pricing.Output), state, strings.Join(entry.UsedFor, ", "),
			})
		}
	}
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no provider offers a model; add one with 'teanode settings agent model add PROVIDER MODEL'")
		return nil
	}
	return printTable([]string{"provider", "name", "id", "window", "input", "output", "state", "used for"}, rows)
}

// formatContextWindow is the override where there is one, else the
// provider's own figure marked as such, else unknown.
func formatContextWindow(entry *client.AgentModelEntry) string {
	switch {
	case entry.ContextWindow > 0:
		return strconv.Itoa(entry.ContextWindow)
	case entry.ReportedContextWindow > 0:
		return strconv.Itoa(entry.ReportedContextWindow) + " (provider's)"
	}
	return "unknown"
}

// settingsAgentModelFind is a provider's entry for a model, or nil.
func settingsAgentModelFind(ctx context.Context, connection *client.Client, providerName, model string) (*client.AgentModelEntry, error) {
	providers, err := client.AgentProviderModelList(ctx, connection)
	if err != nil {
		return nil, err
	}
	for _, provider := range providers {
		if provider.ProviderName != providerName {
			continue
		}
		for _, entry := range provider.Models {
			if entry.Model == model {
				return entry, nil
			}
		}
		return nil, nil
	}
	return nil, fmt.Errorf("no provider called %q is declared", providerName)
}

// applySettingsAgentModelFlags changes an entry by the flags that were given.
func applySettingsAgentModelFlags(command *cli.Command, entry *client.AgentModelEntry) {
	if command.IsSet("name") {
		entry.Name = strings.TrimSpace(command.String("name"))
	}
	if command.IsSet("context-window") {
		entry.ContextWindow = int(command.Int("context-window"))
	}
	for flag, price := range map[string]*float64{
		"input": &entry.Pricing.Input, "output": &entry.Pricing.Output, "cache-read": &entry.Pricing.CacheRead,
		"cache-write": &entry.Pricing.CacheWrite, "audio-input": &entry.Pricing.AudioInput,
	} {
		if command.IsSet(flag) {
			*price = command.Float64(flag)
		}
	}
	if command.IsSet("enabled") {
		entry.IsEnabled = command.Bool("enabled")
	}
}

func saveSettingsAgentModel(ctx context.Context, command *cli.Command, connection *client.Client, providerName string, entry *client.AgentModelEntry, verb string) error {
	saved, err := client.SaveAgentModelEntry(ctx, connection, providerName, entry)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s %s:%s\n", verb, providerName, saved.Model)
	return nil
}

func runSettingsAgentModelAdd(ctx context.Context, command *cli.Command) error {
	providerName, model, err := settingsAgentModelArguments(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	existing, err := settingsAgentModelFind(ctx, connection, providerName, model)
	if err != nil {
		return describeError(command, err)
	}
	if existing != nil {
		return fmt.Errorf("%s already offers %s; change it with 'teanode settings agent model set %s %s'", providerName, model, providerName, model)
	}
	entry := &client.AgentModelEntry{Model: model, IsEnabled: true}
	applySettingsAgentModelFlags(command, entry)
	return saveSettingsAgentModel(ctx, command, connection, providerName, entry, "added")
}

func runSettingsAgentModelSet(ctx context.Context, command *cli.Command) error {
	providerName, model, err := settingsAgentModelArguments(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	entry, err := settingsAgentModelFind(ctx, connection, providerName, model)
	if err != nil {
		return describeError(command, err)
	}
	if entry == nil {
		return fmt.Errorf("%s does not offer %s; add it with 'teanode settings agent model add %s %s'", providerName, model, providerName, model)
	}
	applySettingsAgentModelFlags(command, entry)
	return saveSettingsAgentModel(ctx, command, connection, providerName, entry, "changed")
}

func runSettingsAgentModelRemove(ctx context.Context, command *cli.Command) error {
	providerName, model, err := settingsAgentModelArguments(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteAgentModelEntry(ctx, connection, providerName, model); err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"providerName": providerName, "model": model, "isRemoved": true})
	}
	_, _ = fmt.Fprintf(command.Writer, "removed %s:%s\n", providerName, model)
	return nil
}
