package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/skills"
)

// The computer argument is this server's, and comes off before a connected
// server sees the call.
func TestTheComputerArgumentIsTakenOffACall(t *testing.T) {
	named, rest := takeComputer(json.RawMessage(`{"query":"open issues","computer":"desk"}`))
	if named != "desk" {
		t.Errorf("named %q", named)
	}
	if strings.Contains(string(rest), "computer") || !strings.Contains(string(rest), "open issues") {
		t.Errorf("the server would see %s", rest)
	}
	untouched := json.RawMessage(`{"query":"open issues"}`)
	if named, rest := takeComputer(untouched); named != "" || string(rest) != string(untouched) {
		t.Errorf("a call naming no computer came out as %q, %s", named, rest)
	}
}

// Naming a computer raises a read to asking first; a write already asks.
func TestNamingAComputerAsksFirst(t *testing.T) {
	read := asksWhenAComputerIsNamed(tools.RiskRead)
	if risk := read(json.RawMessage(`{}`)); risk != tools.RiskRead {
		t.Errorf("a read naming no computer is %v", risk)
	}
	if risk := read(json.RawMessage(`{"computer":"desk"}`)); risk != tools.RiskWrite {
		t.Errorf("a read naming a computer is %v", risk)
	}
	outward := asksWhenAComputerIsNamed(tools.RiskOutward)
	if risk := outward(json.RawMessage(`{"computer":"desk"}`)); risk != tools.RiskOutward {
		t.Errorf("an outward call naming a computer became %v", risk)
	}
}

// A server that runs on the person's computer runs on the one named, or the
// only one; with several and none named it says so rather than choosing.
func TestAServerOnTheComputerRunsWhereItIsSet(t *testing.T) {
	worker := &Agent{}
	worker.AttachComputer("a1", nil, ComputerIdentity{Name: "alpha", System: "linux"})
	if on, err := worker.computerForServer("a1", "tracker", ""); err != nil || on.name != "alpha" {
		t.Errorf("with one attached: %v, %v", on, err)
	}
	worker.AttachComputer("a1", nil, ComputerIdentity{Name: "beta", System: "linux"})
	if _, err := worker.computerForServer("a1", "tracker", ""); err == nil || !strings.Contains(err.Error(), "set its reach") {
		t.Errorf("with two attached and none set: %v", err)
	}
	if on, err := worker.computerForServer("a1", "tracker", "beta"); err != nil || on.name != "beta" {
		t.Errorf("with beta set: %v, %v", on, err)
	}
	if _, err := worker.computerForServer("a1", "tracker", "gamma"); err == nil {
		t.Error("a computer that is not attached was used")
	}
}

// A skill with a computer argument of its own keeps it, unraised.
//
// That argument is the skill's and means something of its own; this server
// must not read it as a computer to go through, nor ask first because of it.
func TestASkillsOwnComputerArgumentIsLeftAlone(t *testing.T) {
	declared := &skills.Tool{Name: "status", Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"computer": map[string]any{"type": "string", "description": "the host to look up"},
		},
	}}
	tool := (&Agent{}).skillTool(&skills.Skill{Name: "inventory"}, "server", declared)
	if tool.RiskOf != nil {
		t.Error("naming the skill's own computer argument would ask first")
	}
	properties := tool.Parameters["properties"].(map[string]any)
	if description := properties["computer"].(map[string]any)["description"]; description != "the host to look up" {
		t.Errorf("the skill's own argument was replaced: %v", description)
	}
}

// A skill's command is held to the shell tool's rule, a write that does not
// ask by its class; whether a call asks is judged (see judgedToAsk).
func TestASkillsCommandIsHeldToTheShellRule(t *testing.T) {
	declared := &skills.Tool{Name: "notes", Type: skills.KindShell, Command: []string{"notes"}, Parameters: map[string]any{"type": "object"}}
	tool := (&Agent{}).skillTool(&skills.Skill{Name: "notebook"}, "server", declared)
	if tool.Risk != tools.RiskWrite || tools.NeedsConfirmation(tool, json.RawMessage(`{}`), nil, nil) {
		t.Errorf("the tool is %v and asks by its class", tool.Risk)
	}
	if tool.JudgedCall == nil {
		t.Error("its calls are not judged")
	}
}
