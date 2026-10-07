package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A call that needs the person's word, in a run with nobody present: it
// goes ahead, confirmed, when the person allowed every reason it needs the
// word for, and is refused otherwise, saying where they allow it.
func TestUnattendedCallsRunWhenThePersonAllowedThem(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Tools.Confirm = []string{"notes"}
	worker := &Agent{settings: &Settings{Configuration: func() *config.Configuration { return configuration }}}
	var ran []*Call
	sending := &Tool{Name: "send", Risk: tools.RiskOutward, Run: func(_ context.Context, call *Call) (*tools.Result, error) {
		ran = append(ran, call)
		return &tools.Result{Content: `{"sent": true}`}, nil
	}}
	// Outward, and on the operator's list: two reasons.
	listedSending := &Tool{Name: "notes", Risk: tools.RiskOutward, Run: sending.Run}
	attempt := func(allowed []models.UnattendedRisk, tool *Tool) string {
		t.Helper()
		run := &AskRun{agent: worker, settings: &AskSettings{
			Agent:        &models.Agent{ID: "agent01", UnattendedAllowedRisks: allowed},
			Owner:        &models.User{Username: "robin"},
			Conversation: &models.AgentConversation{ID: "conversation01"},
			Headless:     true,
		}}
		return run.runTool(context.Background(), configuration, []*Tool{tool}, nil, llm.ToolCall{ID: "call01", Name: tool.Name, Arguments: `{}`}).content
	}

	if said := attempt(nil, sending); !strings.Contains(said, "needs_confirmation") || !strings.Contains(said, "outward") || !strings.Contains(said, "When you are not there") || len(ran) != 0 {
		t.Fatalf("nothing allowed: refused, naming the reason and the setting: %q, %d ran", said, len(ran))
	}
	if said := attempt([]models.UnattendedRisk{models.UnattendedRiskDestructive}, sending); !strings.Contains(said, "needs_confirmation") || len(ran) != 0 {
		t.Fatalf("another kind allowed: still refused: %q", said)
	}
	if said := attempt([]models.UnattendedRisk{models.UnattendedRiskOutward}, sending); !strings.Contains(said, `"sent": true`) || len(ran) != 1 || !ran[0].Confirmed {
		t.Fatalf("outward allowed: runs, confirmed as if the person had: %q, %+v", said, ran)
	}
	if said := attempt([]models.UnattendedRisk{models.UnattendedRiskOutward}, listedSending); !strings.Contains(said, "needs_confirmation") || !strings.Contains(said, "listed") || len(ran) != 1 {
		t.Fatalf("one of two reasons allowed: refused: %q", said)
	}
	if said := attempt([]models.UnattendedRisk{models.UnattendedRiskOutward, models.UnattendedRiskListed}, listedSending); !strings.Contains(said, `"sent": true`) || len(ran) != 2 {
		t.Fatalf("both reasons allowed: runs: %q", said)
	}
}

// The setting holds only the four kinds.
func TestUnattendedAllowedRisksAreValidated(t *testing.T) {
	agent := &models.Agent{UnattendedAllowedRisks: []models.UnattendedRisk{models.UnattendedRiskOutward, "everything"}}
	if err := agent.Validate(); err == nil || !strings.Contains(err.Error(), "everything") {
		t.Fatalf("an unknown kind is refused: %v", err)
	}
	if !(&models.Agent{}).IsAllowedUnattended(nil) || (&models.Agent{}).IsAllowedUnattended([]models.UnattendedRisk{models.UnattendedRiskGranting}) {
		t.Fatal("no reasons is allowed; a reason not allowed is not")
	}
}

// Every bound on unattended work is the operator's, and zero is its
// default.
func TestUnattendedLimitsResolveTheirDefaults(t *testing.T) {
	limits := &config.AgentLimits{}
	if limits.EffectiveGoalTurnsPerDay() != 48 || limits.EffectiveGoalTurnsAlone() != 24 || limits.EffectiveGoalsInProgress() != 20 ||
		limits.EffectiveBackgroundWakesAlone() != 20 || limits.EffectiveMaxRoundsPerSubagent() != 20 {
		t.Fatalf("the defaults: %+v", limits)
	}
	limits = &config.AgentLimits{GoalTurnsPerDay: 200, GoalTurnsAlone: 500, GoalsInProgress: 100, BackgroundWakesAlone: 80, MaxRoundsPerSubagent: 60}
	if limits.EffectiveGoalTurnsPerDay() != 200 || limits.EffectiveGoalTurnsAlone() != 500 || limits.EffectiveGoalsInProgress() != 100 ||
		limits.EffectiveBackgroundWakesAlone() != 80 || limits.EffectiveMaxRoundsPerSubagent() != 60 {
		t.Fatalf("what the operator set: %+v", limits)
	}
	if config.Default().Agent.Limits.MaxRoundsPerAsk != 150 {
		t.Fatalf("a turn's rounds: %d", config.Default().Agent.Limits.MaxRoundsPerAsk)
	}
}
