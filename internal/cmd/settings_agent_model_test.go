package cmd

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
)

// A provider's models are on the API and the command line under the same
// names: SaveAgentModelEntry is "model add" and "model set", and
// DeleteAgentModelEntry is "model remove". The listing is the settings.
func TestAgentModelEntryParity(test *testing.T) {
	test.Parallel()

	table := map[string][]string{
		"SaveAgentModelEntry":   {"add", "set"},
		"DeleteAgentModelEntry": {"remove"},
	}
	kind := reflect.TypeFor[apigraph.AgentModelEntryMutation]()
	for index := 0; index < kind.NumMethod(); index++ {
		if _, isListed := table[kind.Method(index).Name]; !isListed {
			test.Errorf("the table does not list the operation %s", kind.Method(index).Name)
		}
	}

	var model *cli.Command
	for _, subcommand := range newSettingsAgentCommand().Commands {
		if subcommand.Name == "model" {
			model = subcommand
		}
	}
	if model == nil {
		test.Fatal("`teanode settings agent model` does not exist")
	}
	commands := map[string]*cli.Command{}
	for _, subcommand := range model.Commands {
		commands[subcommand.Name] = subcommand
	}
	if commands["list"] == nil {
		test.Error("there is no `model list`")
	}
	for operation, names := range table {
		for _, name := range names {
			if commands[name] == nil {
				test.Errorf("%s has no `teanode settings agent model %s`", operation, name)
			}
		}
	}
	// Every field of the entry the API takes, the command line sets.
	flags := []string{}
	for _, flag := range commands["set"].Flags {
		flags = append(flags, flag.Names()...)
	}
	for _, name := range []string{"name", "context-window", "input", "output", "cache-read", "cache-write", "audio-input", "enabled"} {
		if !slices.Contains(flags, name) {
			test.Errorf("`model set` has no --%s", name)
		}
	}
}

// The window is the override, else the provider's own figure marked as its,
// else unknown.
func TestTheWindowColumnSaysWhereTheFigureCameFrom(test *testing.T) {
	test.Parallel()

	for entry, want := range map[*client.AgentModelEntry]string{
		{ContextWindow: 64000, ReportedContextWindow: 128000}: "64000",
		{ReportedContextWindow: 128000}:                       "128000 (provider's)",
		{}:                                                    "unknown",
	} {
		if got := formatContextWindow(entry); got != want {
			test.Errorf("%+v read %q, want %q", entry, got, want)
		}
	}
}

// Giving a price sets the model's own, and --input 0 is a price of zero; the
// entry keeps its prices when none is given, and --provider-price drops them.
func TestModelFlagsSetOrDropTheModelsOwnPrice(test *testing.T) {
	test.Parallel()

	run := func(entry *client.AgentModelEntry, arguments ...string) *client.AgentModelEntry {
		test.Helper()
		command := &cli.Command{Name: "set", Flags: settingsAgentModelFlags(), Action: func(ctx context.Context, command *cli.Command) error {
			applySettingsAgentModelFlags(command, entry)
			return nil
		}}
		if err := command.Run(context.Background(), append([]string{"set"}, arguments...)); err != nil {
			test.Fatal(err)
		}
		return entry
	}
	if entry := run(&client.AgentModelEntry{Model: "alpha"}, "--name", "Alpha"); entry.Pricing != nil {
		test.Fatalf("no price flag leaves the price unset: %+v", entry.Pricing)
	}
	if entry := run(&client.AgentModelEntry{Model: "alpha"}, "--input", "0"); entry.Pricing == nil || *entry.Pricing != (client.AgentPricing{}) {
		test.Fatalf("--input 0 is a price of zero: %+v", entry.Pricing)
	}
	kept := &client.AgentPricing{Input: 1, Output: 2}
	if entry := run(&client.AgentModelEntry{Model: "alpha", Pricing: kept}, "--output", "5"); entry.Pricing == nil || entry.Pricing.Input != 1 || entry.Pricing.Output != 5 {
		test.Fatalf("a price flag changes only its own field: %+v", entry.Pricing)
	}
	if entry := run(&client.AgentModelEntry{Model: "alpha", Pricing: &client.AgentPricing{Input: 1}}, "--provider-price"); entry.Pricing != nil {
		test.Fatalf("--provider-price drops the prices: %+v", entry.Pricing)
	}
}
