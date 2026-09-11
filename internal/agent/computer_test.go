package agent

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeComputer is the daemon's side of the relay: it answers every request
// with what the test says.
type fakeComputer struct {
	agent   *Agent
	agentId string
	answers func(action string, args json.RawMessage) (bool, string)
}

func (self *fakeComputer) Send(message []byte) error {
	var decoded deviceMessage
	_ = json.Unmarshal(message, &decoded)
	go func() {
		ok, data := self.answers(decoded.Action, decoded.Args)
		if ok {
			self.agent.ComputerAnswered(self.agentId, self, decoded.ID, true, json.RawMessage(data), "")
		} else {
			self.agent.ComputerAnswered(self.agentId, self, decoded.ID, false, nil, data)
		}
	}()
	return nil
}

func TestComputerRelayCarriesRequestsAndAnswers(t *testing.T) {
	worker := &Agent{}
	computer := &fakeComputer{agent: worker, agentId: "a1", answers: func(action string, args json.RawMessage) (bool, string) {
		if action == "shell" {
			var call struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(args, &call)
			if call.Command == "rm -rf /" {
				return false, "the program refused: removing the root of the filesystem"
			}
			return true, `{"stdout":"hi\n","stderr":"","exitCode":0}`
		}
		return true, `{"entries":[]}`
	}}
	worker.AttachComputer("a1", computer, "laptop", "linux", "/home/alice")
	listed := worker.ComputersAttached("a1")
	if len(listed) != 1 || listed[0].Name != "laptop" || listed[0].System != "linux" || listed[0].Since.IsZero() {
		t.Fatalf("attached %+v", listed)
	}
	laptop := worker.computersFor("a1")[0]
	answer, err := laptop.Ask(context.Background(), "shell", map[string]any{"command": "echo hi"}, 0)
	if err != nil || !json.Valid(answer) {
		t.Fatalf("shell %s %v", answer, err)
	}
	if _, err := laptop.Ask(context.Background(), "shell", map[string]any{"command": "rm -rf /"}, 0); err == nil {
		t.Fatal("the program's refusal should come back as an error")
	}
	// A second computer under another name sits beside the first; the same
	// name again replaces it.
	other := &fakeComputer{agent: worker, agentId: "a1", answers: computer.answers}
	worker.AttachComputer("a1", other, "desktop", "darwin", "/Users/alice")
	if names := worker.ComputersAttached("a1"); len(names) != 2 || names[0].Name != "desktop" || names[1].Name != "laptop" {
		t.Fatalf("two computers by name: %+v", names)
	}
	again := &fakeComputer{agent: worker, agentId: "a1", answers: computer.answers}
	worker.AttachComputer("a1", again, "laptop", "linux", "/home/alice")
	worker.DetachComputer("a1", computer)
	if names := worker.ComputersAttached("a1"); len(names) != 2 {
		t.Fatalf("detaching a stale connection must not drop the current ones: %+v", names)
	}
	worker.DetachComputer("a1", other)
	worker.DetachComputer("a1", again)
	if names := worker.ComputersAttached("a1"); len(names) != 0 {
		t.Fatalf("detached: %+v", names)
	}
	// The tab and the computer are separate devices of one agent.
	worker.AttachTab("a1", &fakeTab{agent: worker, agentId: "a1", answers: func(string, json.RawMessage) (bool, string) { return true, "{}" }}, "Portal", "https://portal.example/")
	if names := worker.ComputersAttached("a1"); len(names) != 0 {
		t.Fatal("a tab is not a computer")
	}
}
