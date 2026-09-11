package config

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/secretbox"
)

// A provider key goes into the rows sealed and comes back out opened; a
// key stored before sealing existed still reads; and without a server
// secret nothing is sealed rather than something being lost.
func TestAgentSecretsAreSealedInRows(t *testing.T) {
	configuration := Default()
	configuration.Server.Secret = strings.Repeat("s", 32)
	configuration.Agent.Providers = []AgentProvider{{Name: "openai", Kind: "openai", APIKey: "sk-live-1"}}
	configuration.Agent.Search.APIKey = "brave-1"
	configuration.Agent.MCP.Servers = []AgentMCPServer{{Name: "tracker", URL: "https://tracker.example", Env: []AgentMCPEnvironment{{Name: "TOKEN", Value: "t-1"}}}}

	rows, err := ToRows(configuration, 1)
	if err != nil {
		t.Fatal(err)
	}
	stored := rows.Settings[settingAgent]
	for _, plain := range []string{"sk-live-1", "brave-1", "t-1"} {
		if strings.Contains(stored, plain) {
			t.Fatalf("the rows hold %q in the clear:\n%s", plain, stored)
		}
	}
	// The live configuration is untouched: sealing happens on the copy
	// that is written.
	if configuration.Agent.Providers[0].APIKey != "sk-live-1" {
		t.Fatal("ToRows must not seal the configuration it was given")
	}

	read, err := FromRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	if read.Agent.Providers[0].APIKey != "sk-live-1" || read.Agent.Search.APIKey != "brave-1" || read.Agent.MCP.Servers[0].Env[0].Value != "t-1" {
		t.Fatalf("the secrets did not come back: %+v", read.Agent)
	}

	// Written before sealing: a plain value reads as itself.
	rows.Settings[settingAgent] = "providers:\n  - name: openai\n    kind: openai\n    apiKey: sk-plain\n"
	read, err = FromRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	if read.Agent.Providers[0].APIKey != "sk-plain" {
		t.Fatalf("a plain key should read as itself, got %q", read.Agent.Providers[0].APIKey)
	}

	// No server secret: stored as given, and said so by the value itself.
	configuration.Server.Secret = ""
	rows, err = ToRows(configuration, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rows.Settings[settingAgent], "sk-live-1") {
		t.Fatal("without a secret there is nothing to seal with")
	}
	if secretbox.Sealed("sk-live-1") {
		t.Fatal("a plain key must not look sealed")
	}
}
