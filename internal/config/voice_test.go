package config

import (
	"strings"
	"testing"
)

// The model a call is answered with names a declared provider, like every
// other model, and may be left out.
func TestTheCallModelIsChecked(t *testing.T) {
	configuration := Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []AgentProvider{{Name: "spoken", Kind: AgentProviderKindOpenAI, APIKey: "test-key"}}
	configuration.Agent.Models.Default = "spoken:large"
	configuration.Agent.Voice = AgentVoice{Enabled: true}
	if err := configuration.Validate(); err != nil && strings.Contains(err.Error(), "agent.voice.askModel") {
		t.Fatalf("left out: %s", err)
	}
	configuration.Agent.Voice.AskModel = "spoken:small"
	if err := configuration.Validate(); err != nil && strings.Contains(err.Error(), "agent.voice.askModel") {
		t.Fatalf("a declared provider: %s", err)
	}
	configuration.Agent.Voice.AskModel = "elsewhere:small"
	if err := configuration.Validate(); err == nil || !strings.Contains(err.Error(), "agent.voice.askModel") {
		t.Fatalf("a provider not declared is refused: %v", err)
	}
}
