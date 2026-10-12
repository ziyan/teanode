package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

func TestRecordingLimitsUseTheSharedSettingsReadAndUpdatePath(test *testing.T) {
	configuration := config.Default()
	dailyTokenLimit := configuration.Agent.Limits.DailyTokensPerAgent
	if err := applyAgentSettings(configuration, &AgentParameters{Limits: &AgentLimitsParameters{MaxRecordingMediaBytes: new("3GB"), MaxRecordingAccountBytes: new("60GB")}}); err != nil {
		test.Fatal(err)
	}
	described := describeAgentSettings(configuration)
	if described.Limits.MaxRecordingMediaBytes != "3GB" || described.Limits.MaxRecordingAccountBytes != "60GB" || configuration.Agent.Limits.DailyTokensPerAgent != dailyTokenLimit {
		test.Fatal("recording limits did not round-trip or reset unrelated settings")
	}
	if err := applyAgentSettings(configuration, &AgentParameters{Limits: &AgentLimitsParameters{MaxRecordingMediaBytes: new("not-a-size")}}); err == nil {
		test.Fatal("invalid recording size was accepted")
	}
}
