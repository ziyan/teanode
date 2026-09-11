package connected

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// Declaring one server rewrites the whole list, so every other server has
// to come through it whole. An omitted enabled reads as false at the other
// end, which would quietly switch off every server the person did not
// mention.
func TestCarriedOverKeepsEveryOtherServerWhole(t *testing.T) {
	off := false
	configuration := &config.Configuration{}
	configuration.Agent.MCP.Servers = []config.AgentMCPServer{
		{
			Name: "tracker", Transport: "stdio", Command: "/usr/bin/tracker",
			Args: []string{"--serve"}, WorkingDir: "/srv",
			Env:      []config.AgentMCPEnvironment{{Name: "TRACKER_TOKEN", Value: "a secret"}},
			Auth:     "user",
			Headless: true, ReadOnly: []string{"tracker_list"}, Disabled: []string{"tracker_delete"},
			Timeout: config.Duration(45 * time.Second), Enabled: &off,
		},
		{Name: "robinhood", Transport: "http", URL: "https://agent.example.com/mcp"},
	}

	carried := carriedOver(configuration, "robinhood")
	if len(carried) != 1 {
		t.Fatalf("the one being declared is left out: %d", len(carried))
	}
	kept := carried[0]
	if kept["enabled"] != false {
		t.Errorf("a switched-off server stays switched off: %v", kept["enabled"])
	}
	if kept["timeout"] != "45s" {
		t.Errorf("its timeout is kept: %v", kept["timeout"])
	}
	for field, want := range map[string]any{"name": "tracker", "transport": "stdio", "command": "/usr/bin/tracker", "workingDir": "/srv", "auth": "user", "headless": true, "previousName": "tracker"} {
		if kept[field] != want {
			t.Errorf("%s: want %v, got %v", field, want, kept[field])
		}
	}
	// The value is never carried in the clear; an empty one keeps what is
	// stored, which is how the secret survives the trip.
	environment, _ := kept["env"].([]string)
	if len(environment) != 1 || environment[0] != "TRACKER_TOKEN=" {
		t.Errorf("the environment goes by name with no value: %v", environment)
	}
	if names, _ := kept["readOnly"].([]string); len(names) != 1 || names[0] != "tracker_list" {
		t.Errorf("its read-only list is kept: %v", kept["readOnly"])
	}

	// Nothing is left out when the name matches nothing.
	if carried := carriedOver(configuration, "absent"); len(carried) != 2 {
		t.Fatalf("both are carried: %d", len(carried))
	}
}
