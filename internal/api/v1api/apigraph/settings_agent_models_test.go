package apigraph

import (
	"slices"
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// The synthesize model is saved and read back like every other slot, a
// save that leaves it out keeps it, and it is listed among the kinds of
// work, after the scan.
func TestTheSynthesizeModelIsSavedAndReadBack(t *testing.T) {
	configuration := &config.Configuration{}
	synthesizeModel := "local:judge"
	if err := applyAgentSettings(configuration, &AgentParameters{Models: &AgentModelsParameters{Synthesize: &synthesizeModel}}); err != nil {
		t.Fatal(err)
	}
	if configuration.Agent.Models.Synthesize != synthesizeModel {
		t.Fatalf("the saved model is %q", configuration.Agent.Models.Synthesize)
	}
	scanModel := "local:scanner"
	if err := applyAgentSettings(configuration, &AgentParameters{Models: &AgentModelsParameters{Scan: &scanModel}}); err != nil {
		t.Fatal(err)
	}
	settings := describeAgentSettings(configuration)
	if settings.Models.Synthesize != synthesizeModel || settings.Models.Scan != scanModel {
		t.Errorf("read back synthesize %q and scan %q", settings.Models.Synthesize, settings.Models.Scan)
	}
	scanAt := slices.Index(settings.Works, string(config.AgentWorkScan))
	if scanAt < 0 || slices.Index(settings.Works, string(config.AgentWorkSynthesize)) != scanAt+1 {
		t.Errorf("the kinds of work are %v", settings.Works)
	}
}
