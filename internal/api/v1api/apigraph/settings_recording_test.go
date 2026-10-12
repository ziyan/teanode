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

func TestRecordingTranscriptionModelIsExplicitAndCanBeDisabled(test *testing.T) {
	configuration := config.Default()
	if configuration.Agent.Models.Transcription != "" {
		test.Fatal("file transcription enabled by default")
	}
	if err := applyAgentSettings(configuration, &AgentParameters{Models: &AgentModelsParameters{Transcription: new("fixture:diarized")}}); err != nil {
		test.Fatal(err)
	}
	if describeAgentSettings(configuration).Models.Transcription != "fixture:diarized" {
		test.Fatal("explicit transcription setting lost")
	}
	if err := applyAgentSettings(configuration, &AgentParameters{Models: &AgentModelsParameters{Transcription: new("")}}); err != nil {
		test.Fatal(err)
	}
	if describeAgentSettings(configuration).Models.Transcription != "" {
		test.Fatal("disabling transcription fell back to another model")
	}
}
