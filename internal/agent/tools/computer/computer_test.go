package computer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
)

// fakeRun is a turn with the person present and a computer attached, or
// not; only what the tools ask of it is there.
type fakeRun struct {
	tools.Run
	headless bool
	computer tools.Computer
	config   *config.Configuration
}

func (self *fakeRun) AttachedComputers() []tools.Computer {
	if self.computer == nil {
		return nil
	}
	return []tools.Computer{self.computer}
}

func (self *fakeRun) Headless() bool                       { return self.headless }
func (self *fakeRun) Configuration() *config.Configuration { return self.config }
func (self *fakeRun) ComputersAllowed() bool               { return true }
func (self *fakeRun) Offered() []*tools.Tool               { return nil }

type fakeComputer struct {
	asked []string
}

func (self *fakeComputer) Ask(_ context.Context, action string, args any, _ time.Duration) (json.RawMessage, error) {
	encoded, _ := json.Marshal(args)
	self.asked = append(self.asked, action+" "+string(encoded))
	if action == "shell" {
		return json.RawMessage(`{"stdout":"hi\n","stderr":"","exitCode":0}`), nil
	}
	return json.RawMessage(`{"entries":[{"name":"notes.txt","size":12}]}`), nil
}
func (self *fakeComputer) Name() string   { return "laptop" }
func (self *fakeComputer) System() string { return "linux" }
func (self *fakeComputer) Home() string   { return "/home/alice" }

func find(t *testing.T, name string) *tools.Tool {
	t.Helper()
	for _, tool := range tools.Build().All() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %q", name)
	return nil
}

func TestShellReachesTheComputerAndRefusesWhatWouldDestroyIt(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	shell := find(t, "shell")
	attached := &fakeComputer{}
	run := &fakeRun{computer: attached, config: configuration}
	ctx := tools.WithRun(context.Background(), run)

	result, err := shell.Run(ctx, &tools.Call{Arguments: json.RawMessage(`{"command":"echo hi"}`)})
	if err != nil || !strings.Contains(result.Content, "hi") || !result.Untrusted {
		t.Fatalf("echo: %+v %v", result, err)
	}
	if len(attached.asked) != 1 || !strings.HasPrefix(attached.asked[0], "shell ") {
		t.Fatalf("asked %v", attached.asked)
	}
	if shell.RiskOf(json.RawMessage(`{"command":"ls"}`)) != tools.RiskWrite || shell.RiskOf(json.RawMessage(`{"command":"rm x"}`)) != tools.RiskDestructive || shell.RiskOf(json.RawMessage(`{"command":"rm -rf /"}`)) != tools.RiskDestructive {
		t.Fatal("a listing runs, a removal asks, and so does the gravest")
	}
	if preview := shell.Preview(json.RawMessage(`{"command":"apt-get install jq"}`)); !strings.Contains(preview, "installs or removes software") {
		t.Fatalf("the card says why: %s", preview)
	}

	// Nobody present, or nothing attached: said, not tried.
	if _, err := shell.Run(tools.WithRun(context.Background(), &fakeRun{headless: true, computer: attached, config: configuration}), &tools.Call{Arguments: json.RawMessage(`{"command":"ls"}`)}); err == nil || !strings.Contains(err.Error(), "nobody present") {
		t.Fatalf("headless: %v", err)
	}
	if _, err := shell.Run(tools.WithRun(context.Background(), &fakeRun{config: configuration}), &tools.Call{Arguments: json.RawMessage(`{"command":"ls"}`)}); err == nil || !strings.Contains(err.Error(), "teanode computer start") {
		t.Fatalf("none attached: %v", err)
	}
	if overlay := shell.Overlay(ctx); !strings.Contains(overlay, `"laptop" (linux)`) || !strings.Contains(overlay, "/home/alice") {
		t.Fatalf("overlay %q", overlay)
	}
}

func TestFilesystemRisksByAction(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	filesystem := find(t, "filesystem")
	for action, want := range map[string]tools.Risk{"read": tools.RiskRead, "list": tools.RiskRead, "search": tools.RiskRead, "grep": tools.RiskRead, "write": tools.RiskWrite, "edit": tools.RiskWrite, "append": tools.RiskWrite, "copy": tools.RiskWrite, "mkdir": tools.RiskWrite, "move": tools.RiskDestructive, "delete": tools.RiskDestructive} {
		if got := filesystem.RiskOf(json.RawMessage(`{"action":"` + action + `","path":"x"}`)); got != want {
			t.Errorf("%s: %s, want %s", action, got, want)
		}
	}
	if filesystem.RiskOf(json.RawMessage(`{"action":"append","path":"~/.bashrc"}`)) != tools.RiskDestructive || filesystem.RiskOf(json.RawMessage(`{"action":"write","path":"~/.ssh/authorized_keys"}`)) != tools.RiskDestructive {
		t.Fatal("a write into what the machine runs on its own asks first")
	}
	attached := &fakeComputer{}
	ctx := tools.WithRun(context.Background(), &fakeRun{computer: attached, config: configuration})
	result, err := filesystem.Run(ctx, &tools.Call{Arguments: json.RawMessage(`{"action":"list","path":"~/Documents"}`)})
	if err != nil || !strings.Contains(result.Content, "notes.txt") {
		t.Fatalf("list: %+v %v", result, err)
	}
	if _, err := filesystem.Run(ctx, &tools.Call{Arguments: json.RawMessage(`{"action":"move","path":"a"}`)}); err == nil {
		t.Fatal("a move needs a destination")
	}
	if _, err := filesystem.Run(ctx, &tools.Call{Arguments: json.RawMessage(`{"action":"burn","path":"a"}`)}); err == nil {
		t.Fatal("an unknown action is refused")
	}
}
