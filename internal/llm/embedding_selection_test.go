package llm

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

func TestEmbeddingSelectionUsesRegistrySnapshot(test *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fixture", Kind: "openai", BaseURL: "http://127.0.0.1:1", APIKey: "fixture"}}
	configuration.Agent.Models.Embedding = "fixture:original"
	configuration.Agent.Models.EmbeddingDimensions = 3
	registry, err := Open(&configuration.Agent)
	if err != nil {
		test.Fatal(err)
	}
	configuration.Agent.Models.Embedding = "fixture:replacement"
	configuration.Agent.Models.EmbeddingDimensions = 5
	selection, err := registry.Embedding()
	if err != nil || selection.Model != "original" || selection.Name != "fixture:original@3" || selection.Dimensions != 3 {
		test.Fatalf("running selection=%+v: %v", selection, err)
	}
	restarted, err := Open(&configuration.Agent)
	if err != nil {
		test.Fatal(err)
	}
	selection, err = restarted.Embedding()
	if err != nil || selection.Model != "replacement" || selection.Name != "fixture:replacement@5" || selection.Dimensions != 5 {
		test.Fatalf("restarted selection=%+v: %v", selection, err)
	}
	configuration.Agent.Models.Embedding = ""
	disabled, err := Open(&configuration.Agent)
	if err != nil {
		test.Fatal(err)
	}
	if disabled.HasEmbedding() {
		test.Fatal("disabled registry has embeddings")
	}
	configuration.Agent.Models.Embedding = "fixture:replacement"
	if disabled.HasEmbedding() {
		test.Fatal("pending change enabled the running registry")
	}
	if _, err := disabled.Embedding(); err == nil {
		test.Fatal("disabled registry selected an embedding model")
	}
	var absent *Registry
	if absent.HasEmbedding() {
		test.Fatal("absent registry has embeddings")
	}
}
