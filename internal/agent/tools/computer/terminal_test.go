package computer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
)

// A computer that can hold a terminal open, with the person sitting in one.
type fakeHolder struct {
	*fakeComputer
	closed []string
}

func (self *fakeHolder) StartSession(context.Context, string, string, []string, string, map[string]string, int, int) (string, error) {
	return "opened", nil
}
func (self *fakeHolder) WriteSession(context.Context, string, []byte) error { return nil }
func (self *fakeHolder) ReadScreen(_ context.Context, id string) (*tools.Screen, error) {
	return &tools.Screen{Columns: 80, Rows: 24, Text: "ziyan@gen7:~ $ ", CursorX: 15, CursorY: 0}, nil
}
func (self *fakeHolder) ResizeSession(context.Context, string, int, int) error { return nil }
func (self *fakeHolder) SignalSession(context.Context, string, string) error   { return nil }
func (self *fakeHolder) CloseSession(_ context.Context, id string) error {
	self.closed = append(self.closed, id)
	return nil
}
func (self *fakeHolder) AttachedTerminal() string { return "attached" }

func runTerminalWith(t *testing.T, holder *fakeHolder, arguments string) (*tools.Result, error) {
	t.Helper()
	run := &fakeRun{computer: holder, config: config.Default()}
	run.config.Agent.Enabled = true
	ctx := tools.WithRun(context.Background(), run)
	return find(t, "terminal").Run(ctx, &tools.Call{Arguments: json.RawMessage(arguments)})
}

// The terminal the person is sitting in is theirs: the agent may read it and
// type into it, and may not close it, whatever the prompt said that round.
func TestTheAttachedTerminalIsReadAndNotClosed(t *testing.T) {
	holder := &fakeHolder{fakeComputer: &fakeComputer{}}

	result, err := runTerminalWith(t, holder, `{"action":"attached"}`)
	if err != nil {
		t.Fatalf("attached: %v", err)
	}
	if !strings.Contains(result.Content, `"attached"`) || !strings.Contains(result.Content, "ziyan@gen7") {
		t.Fatalf("its session and its screen: %s", result.Content)
	}

	if _, err := runTerminalWith(t, holder, `{"action":"close","session":"attached"}`); err == nil || !strings.Contains(err.Error(), "theirs") {
		t.Fatalf("closing it is refused, saying whose it is: %v", err)
	}
	if len(holder.closed) != 0 {
		t.Fatalf("and nothing was closed: %v", holder.closed)
	}

	// One the agent opened itself is its own to close.
	if _, err := runTerminalWith(t, holder, `{"action":"close","session":"opened"}`); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(holder.closed) != 1 || holder.closed[0] != "opened" {
		t.Fatalf("that one was closed: %v", holder.closed)
	}
}

// A key is sent by name, and a name this does not know is refused rather
// than sent as letters.
func TestKeysAreNamedAndUnknownOnesRefused(t *testing.T) {
	for _, each := range []struct {
		name  string
		bytes string
	}{
		{"enter", "\r"}, {"ctrl-c", "\x03"}, {"up", "\x1b[A"}, {"f5", "\x1b[15~"}, {"backspace", "\x7f"},
	} {
		got, ok := keyBytes(each.name)
		if !ok || string(got) != each.bytes {
			t.Errorf("%s sends %q, not %q (%v)", each.name, each.bytes, got, ok)
		}
	}
	if _, ok := keyBytes("banana"); ok {
		t.Fatalf("an unknown key is not a key")
	}
	holder := &fakeHolder{fakeComputer: &fakeComputer{}}
	if _, err := runTerminalWith(t, holder, `{"action":"keys","session":"opened","keys":["banana"]}`); err == nil || !strings.Contains(err.Error(), "not a key") {
		t.Fatalf("refused, naming the keys it knows: %v", err)
	}
}

// Opening a terminal asks only for what the shell tool would ask about: a
// listing opens without a card, a shell handed a harmless script with -c
// too, and a bare shell -- which the keys after it could type anything
// into -- or a removal still asks.
func TestStartingATerminalAsksOnlyForWhatTheShellWould(t *testing.T) {
	cases := []struct {
		name      string
		command   string
		arguments []string
		want      tools.Risk
	}{
		{"a listing", "ls", []string{"-la", "~/projects"}, tools.RiskWrite},
		{"a harmless script", "bash", []string{"-lc", "pwd; find ~/archive -type f | head"}, tools.RiskWrite},
		{"a removal in a script", "bash", []string{"-lc", "rm -rf ~/archive"}, tools.RiskDestructive},
		{"a bare shell", "bash", nil, tools.RiskDestructive},
		{"nothing named", "", nil, tools.RiskDestructive},
		{"sudo", "sudo", []string{"apt", "install", "x"}, tools.RiskDestructive},
	}
	for _, testCase := range cases {
		got := startRisk(&terminalArguments{Action: "start", Command: testCase.command, Arguments: testCase.arguments})
		if got != testCase.want {
			t.Errorf("%s: got %s, want %s", testCase.name, got, testCase.want)
		}
	}
}
