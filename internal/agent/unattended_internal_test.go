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
// goes ahead, confirmed and marked as confirmed by the allowance, when the
// person allowed every reason it needs the word for, and is refused
// otherwise, saying where they allow it. A tool on the operator's list is
// never let through, and a run held to reading or to a few tools never uses
// the allowance.
func TestUnattendedCallsRunWhenThePersonAllowedThem(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Tools.Confirm = []string{"guarded"}
	worker := &Agent{settings: &Settings{Configuration: func() *config.Configuration { return configuration }}}
	var ran []*Call
	run := func(_ context.Context, call *Call) (*tools.Result, error) {
		ran = append(ran, call)
		return &tools.Result{Content: `{"sent": true}`}, nil
	}
	sending := &Tool{Name: "send", Risk: tools.RiskOutward, Run: run}
	// Outward, and on the person's own list: two reasons.
	listedSending := &Tool{Name: "notes", Risk: tools.RiskOutward, Run: run}
	// On the operator's list, which nobody can allow.
	guarded := &Tool{Name: "guarded", Risk: tools.RiskWrite, Run: run}
	attempt := func(allowed []models.UnattendedRisk, tool *Tool, change func(*AskSettings)) string {
		t.Helper()
		settings := &AskSettings{
			Agent:        &models.Agent{ID: "agent01", Confirm: []string{"notes"}, UnattendedAllowedRisks: allowed},
			Owner:        &models.User{Username: "robin"},
			Conversation: &models.AgentConversation{ID: "conversation01"},
			Headless:     true,
		}
		if change != nil {
			change(settings)
		}
		turn := &AskRun{agent: worker, settings: settings}
		return turn.runTool(context.Background(), configuration, []*Tool{tool}, nil, llm.ToolCall{ID: "call01", Name: tool.Name, Arguments: `{}`}).content
	}

	if said := attempt(nil, sending, nil); !strings.Contains(said, "needs_confirmation") || !strings.Contains(said, "outward") || !strings.Contains(said, "When you are not there") || len(ran) != 0 {
		t.Fatalf("nothing allowed: refused, naming the reason and the setting: %q, %d ran", said, len(ran))
	}
	if said := attempt([]models.UnattendedRisk{models.UnattendedRiskDestructive}, sending, nil); !strings.Contains(said, "needs_confirmation") || len(ran) != 0 {
		t.Fatalf("another kind allowed: still refused: %q", said)
	}
	if said := attempt([]models.UnattendedRisk{models.UnattendedRiskOutward}, sending, nil); !strings.Contains(said, `"sent": true`) || len(ran) != 1 || !ran[0].Confirmed || !ran[0].IsConfirmedUnattended {
		t.Fatalf("outward allowed: runs, confirmed by the allowance: %q, %+v", said, ran)
	}
	if said := attempt([]models.UnattendedRisk{models.UnattendedRiskOutward}, listedSending, nil); !strings.Contains(said, "needs_confirmation") || !strings.Contains(said, "listed") || len(ran) != 1 {
		t.Fatalf("one of two reasons allowed: refused: %q", said)
	}
	if said := attempt([]models.UnattendedRisk{models.UnattendedRiskOutward, models.UnattendedRiskListed}, listedSending, nil); !strings.Contains(said, `"sent": true`) || len(ran) != 2 {
		t.Fatalf("both reasons allowed: runs: %q", said)
	}
	everything := append([]models.UnattendedRisk{}, models.UnattendedRisks...)
	if said := attempt(everything, guarded, nil); !strings.Contains(said, "needs_confirmation") || !strings.Contains(said, "operatorListed") || len(ran) != 2 {
		t.Fatalf("the operator's list is never let through: %q", said)
	}
	if said := attempt(everything, sending, func(settings *AskSettings) { settings.Allow = map[string]bool{"send": true} }); !strings.Contains(said, "needs_confirmation") || len(ran) != 2 {
		t.Fatalf("a run held to a few tools never uses the allowance: %q", said)
	}
}

// The judge may answer money and access as kinds of their own, and a
// subagent asks only when the turn that started it can.
func TestJudgedKindsAndSubagentsFollowTheirTurn(t *testing.T) {
	for _, callRisk := range []string{callRiskMoney, callRiskGranting, callRiskOutward} {
		if judged, _ := readCallRisk(`{"callRisk":"` + callRisk + `","riskReason":"x"}`); judged != callRisk {
			t.Errorf("%s is read as %s", callRisk, judged)
		}
	}
	parent := &AskRun{settings: &AskSettings{Headless: true}}
	child := &AskRun{settings: &AskSettings{confirmVia: parent}}
	if child.CanAsk() {
		t.Error("a subagent of a turn with nobody present cannot ask")
	}
	present := &AskRun{settings: &AskSettings{}}
	if !(&AskRun{settings: &AskSettings{confirmVia: present}}).CanAsk() {
		t.Error("a subagent of a turn with somebody there asks through it")
	}
}

// The setting holds only the kinds a person may allow.
func TestUnattendedAllowedRisksAreValidated(t *testing.T) {
	agent := &models.Agent{UnattendedAllowedRisks: []models.UnattendedRisk{models.UnattendedRiskOutward, "everything"}}
	if err := agent.Validate(); err == nil || !strings.Contains(err.Error(), "everything") {
		t.Fatalf("an unknown kind is refused: %v", err)
	}
	agent = &models.Agent{UnattendedAllowedRisks: []models.UnattendedRisk{models.UnattendedRiskOperatorListed}}
	if err := agent.Validate(); err == nil {
		t.Fatal("the operator's list is not a kind a person may allow")
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
