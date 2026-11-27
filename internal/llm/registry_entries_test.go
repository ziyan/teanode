package llm

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// A model added to the configuration after the registry opened, the way a
// save of a model entry does, resolves once the registry is told of the
// configuration, with no restart; and an id with an "@" of its own resolves.
func TestRegistrySeesEntriesAddedAfterItOpened(test *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{
		Name: "fixture", Kind: "openai", BaseURL: "http://127.0.0.1:1", APIKey: "fixture",
		ModelEntries: []config.AgentModelEntry{{Model: "original"}, {Model: "@cf/meta/llama-3-8b"}},
	}}
	configuration.Agent.Models.Default = "fixture:original"
	registry, err := Open(&configuration.Agent)
	if err != nil {
		test.Fatal(err)
	}
	if _, _, err := registry.ForModel("fixture:@cf/meta/llama-3-8b"); err != nil {
		test.Fatalf("an id with an @ resolves: %s", err)
	}
	if _, _, err := registry.ForModel("fixture:added"); err == nil {
		test.Fatal("a model with no entry resolved")
	}

	// What a save does to the current configuration.
	configuration.Agent.Providers[0].ModelEntries = append(configuration.Agent.Providers[0].ModelEntries, config.AgentModelEntry{Model: "added", ContextWindow: 64000})
	if _, _, err := registry.ForModel("fixture:added"); err == nil {
		test.Fatal("the registry saw the entry before it was told")
	}
	registry.RefreshModelEntries(&configuration.Agent)
	if _, model, err := registry.ForModel("fixture:added"); err != nil || model != "added" {
		test.Fatalf("the added model resolves: %q %v", model, err)
	}
	if length := registry.ContextLength(context.Background(), "fixture:added"); length != 64000 {
		test.Fatalf("the entry's window is read: %d", length)
	}

	// An entry that is switched off or removed stops resolving.
	isOff := false
	configuration.Agent.Providers[0].ModelEntries[2].Enabled = &isOff
	registry.RefreshModelEntries(&configuration.Agent)
	if _, _, err := registry.ForModel("fixture:added"); err == nil {
		test.Fatal("a switched off model resolved")
	}
}
