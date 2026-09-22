package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
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
