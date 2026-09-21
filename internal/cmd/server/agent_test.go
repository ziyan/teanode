package server

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// A disabled agent constructs nothing: no registry, no client, nothing that
// could dial a model service. This is the rule every optional integration
// follows, and the one a mail server must keep — an operator who did not
// turn the agent on has not agreed to anybody's mail leaving the machine.
func TestRegistryIsNilWhenDisabled(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Providers = []config.AgentProvider{{Name: "p", Kind: "openai", APIKey: "k"}}
	configuration.Agent.Models.Default = "p:m"
	registry, err := openAgent(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if registry != nil {
		t.Fatal("the agent is off, but a registry was built")
	}

	configuration.Agent.Enabled = true
	registry, err = openAgent(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if registry == nil {
		t.Fatal("the agent is on, but no registry was built")
	}
}
