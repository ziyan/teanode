package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/skills"
)

// A judgement that cannot be read asks; one that can is taken as said.
func TestTheCallJudgementIsReadOrAsks(t *testing.T) {
	for text, expectedRisk := range map[string]string{
		`{"callRisk": "read", "riskReason": "shows a thread"}`:  callRiskRead,
		`{"callRisk": "Change", "riskReason": "writes a file"}`: callRiskChange,
		`{"callRisk": "outward", "riskReason": "posts"}`:        callRiskOutward,
		`{"callRisk": "harmless"}`:                              callRiskDestructive,
		`{"riskReason": "no risk given"}`:                       callRiskDestructive,
		`not an answer`:                                         callRiskDestructive,
	} {
		if callRisk, _ := readCallRisk(text); callRisk != expectedRisk {
			t.Errorf("%s was read as %q, not %q", text, callRisk, expectedRisk)
		}
	}
}

// The judge is shown the commands of the action chosen, as written, and
// the arguments that fill them, and never a secret's value.
func TestTheJudgeSeesTheChosenCommands(t *testing.T) {
	declared := &skills.Tool{
		Name: "notes", Description: "the person's notes", Type: skills.KindWorkflow, ActionField: "action",
		Actions: map[string][]*skills.Step{
			"show":  {{Name: "show", Type: skills.KindShell, Command: []string{"notes", "show", "{{name}}", "--token", "{{secret:NOTES_TOKEN}}"}}},
			"share": {{Name: "share", Type: skills.KindShell, Command: []string{"notes", "share", "{{name}}"}}},
		},
		Parameters: map[string]any{"type": "object"},
	}
	tool := (&Agent{}).skillTool(&skills.Skill{Name: "notebook"}, "server", declared)
	if tool.JudgedCall == nil {
		t.Fatal("a skill that runs commands is not judged")
	}
	call := tool.JudgedCall(json.RawMessage(`{"action":"show","name":"groceries"}`))
	for _, expected := range []string{"skill: notebook", "action: show", `"notes","show","{{name}}"`, "{{secret:NOTES_TOKEN}}", `"name":"groceries"`} {
		if !strings.Contains(call, expected) {
			t.Errorf("the judge is not shown %s in:\n%s", expected, call)
		}
	}
	if strings.Contains(call, "share") {
		t.Errorf("the judge is shown an action not chosen:\n%s", call)
	}
	fetching := &skills.Tool{Name: "weather", Type: skills.KindHTTP, URL: "https://weather.example", Parameters: map[string]any{"type": "object"}}
	if tool := (&Agent{}).skillTool(&skills.Skill{Name: "weather"}, "server", fetching); tool.JudgedCall != nil {
		t.Error("a skill that runs no command is judged")
	}
}

// A call already judged in the turn is not judged again, and a tool with
// nothing to judge never asks by judgement.
func TestACallIsJudgedOncePerTurn(t *testing.T) {
	run := &AskRun{judgedCalls: map[string]bool{"posts a message": true, "shows a thread": false}}
	judged := func(said string) *Tool {
		return &Tool{Name: "notes", JudgedCall: func(json.RawMessage) string { return said }}
	}
	if !run.judgedToAsk(context.Background(), judged("posts a message"), nil) {
		t.Error("a call judged to ask did not")
	}
	if run.judgedToAsk(context.Background(), judged("shows a thread"), nil) {
		t.Error("a call judged to run asked")
	}
	if run.judgedToAsk(context.Background(), &Tool{Name: "plain", Risk: tools.RiskWrite}, nil) {
		t.Error("a tool with nothing to judge asked")
	}
}

// What the judgements cost is carried once, by the next answer, so that the
// turn's cost includes them and no answer counts them twice.
func TestJudgementsAreCarriedByTheNextAnswer(t *testing.T) {
	run := &AskRun{judgementUsage: models.AgentUsageNote{PromptTokens: 300, CompletionTokens: 40, Cost: 0.002}}
	first := &models.AgentUsageNote{PromptTokens: 1000, CompletionTokens: 100, Cost: 0.01}
	run.carryJudgements(first)
	if first.PromptTokens != 1300 || first.CompletionTokens != 140 || first.Cost < 0.0119 || first.Cost > 0.0121 {
		t.Errorf("the first answer carries %+v", first)
	}
	second := &models.AgentUsageNote{PromptTokens: 1000}
	run.carryJudgements(second)
	if second.PromptTokens != 1000 || second.Cost != 0 {
		t.Errorf("the second answer counted them again: %+v", second)
	}
}
