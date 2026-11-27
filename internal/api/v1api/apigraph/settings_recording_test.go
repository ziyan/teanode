package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// The limits that stopped being settings are still accepted, so an older
// app's page saves, and are ignored; they answer zero or empty. The budget
// beside them round-trips and is not reset by them.
func TestRemovedLimitsAreIgnoredAndAnswerEmptyWhileTheBudgetRoundTrips(test *testing.T) {
	configuration := config.Default()
	err := applyAgentSettings(configuration, &AgentParameters{Limits: &AgentLimitsParameters{
		MaxRecordingMediaBytes: new("3GB"), MaxRecordingAccountBytes: new("not-a-size"), MaxBodyCharacters: new(5),
		RequestTimeout: new("1s"), Concurrency: new(9), IngestChunksPerRun: new(1), ScanConcurrency: new(8), RewriteConcurrency: new(8),
		DailyCostPerAgent: new(30.0), MonthlyCostPerServer: new(500.0), DailyTokensPerAgent: new(int64(7)), MonthlyTokensPerServer: new(int64(11)),
	}})
	if err != nil {
		test.Fatalf("an older app's save was refused: %s", err)
	}
	limits := describeAgentSettings(configuration).Limits
	if limits.DailyCostPerAgent != 30 || limits.MonthlyCostPerServer != 500 || limits.DailyTokensPerAgent != 7 || limits.MonthlyTokensPerServer != 11 {
		test.Fatalf("the budget did not round-trip: %+v", limits)
	}
	if limits.MaxRecordingMediaBytes != "" || limits.MaxRecordingAccountBytes != "" || limits.RequestTimeout != "" ||
		limits.MaxBodyCharacters != 0 || limits.Concurrency != 0 || limits.IngestChunksPerRun != 0 || limits.ScanConcurrency != 0 || limits.RewriteConcurrency != 0 {
		test.Fatalf("a removed limit answers something: %+v", limits)
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
