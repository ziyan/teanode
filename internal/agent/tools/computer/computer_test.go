package computer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// fakeRun is a turn with the person present and a computer attached, or
// not; only what the tools ask of it is there.
type fakeRun struct {
	tools.Run
	headless bool
	// unattended is a run with nobody present that may reach the machine
	// anyway: the night, which the owner decided should have it.
	unattended bool
	computer   tools.Computer
	config     *config.Configuration
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
func (self *fakeRun) ComputersUnattended() bool            { return self.unattended }
func (self *fakeRun) Offered() []*tools.Tool               { return nil }
func (self *fakeRun) Agent() *models.Agent                 { return &models.Agent{ID: "agent01"} }
func (self *fakeRun) Conversation() *models.AgentConversation {
	return &models.AgentConversation{ID: "c1"}
}

// Database is none: these tests are about the computer, and a reach is read
// only to mention it.
func (self *fakeRun) Database() db.Database { return nil }

type fakeComputer struct {
	asked       []string
	name        string
	description string
	// answers are what an action answers, when a test says.
	answers map[string]string
}

func (self *fakeComputer) Ask(_ context.Context, action string, args any, _ time.Duration) (json.RawMessage, error) {
	encoded, _ := json.Marshal(args)
	self.asked = append(self.asked, action+" "+string(encoded))
	if answer, found := self.answers[action]; found {
		return json.RawMessage(answer), nil
	}
	if action == "shell" {
		return json.RawMessage(`{"stdout":"hi\n","stderr":"","exitCode":0}`), nil
	}
	return json.RawMessage(`{"entries":[{"name":"notes.txt","size":12}]}`), nil
}
func (self *fakeComputer) Name() string {
	if self.name == "" {
		return "laptop"
	}
	return self.name
}
func (self *fakeComputer) System() string      { return "linux" }
func (self *fakeComputer) Home() string        { return "/home/alice" }
func (self *fakeComputer) Description() string { return self.description }

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

func TestShellReachesTheComputerAndRunsWhatItIsGiven(t *testing.T) {
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
	// No call is judged by what the command looks like: every command is
	// a write, and only reading about background commands is a read.
	for _, command := range []string{"ls", "rm -rf ~", "cat notes.txt"} {
		if risk := shell.RiskFor(json.RawMessage(`{"command":"` + command + `"}`)); risk != tools.RiskWrite {
			t.Fatalf("%s is judged %s: a command is not classified by its shape", command, risk)
		}
	}
	for action, want := range map[string]tools.Risk{"read": tools.RiskRead, "list": tools.RiskRead, "stop": tools.RiskWrite, "run": tools.RiskWrite} {
		if risk := shell.RiskFor(json.RawMessage(`{"action":"` + action + `","id":"x"}`)); risk != want {
			t.Fatalf("%s is judged %s, want %s", action, risk, want)
		}
	}
	if preview := shell.Preview(json.RawMessage(`{"command":"apt-get install jq"}`)); !strings.Contains(preview, "apt-get install jq") {
		t.Fatalf("the card says what will run: %s", preview)
	}

	// Nobody present, or nothing attached: said, not tried.
	if _, err := shell.Run(tools.WithRun(context.Background(), &fakeRun{headless: true, computer: attached, config: configuration}), &tools.Call{Arguments: json.RawMessage(`{"command":"ls"}`)}); err == nil || !strings.Contains(err.Error(), "nobody present") {
		t.Fatalf("headless: %v", err)
	}
	if _, err := shell.Run(tools.WithRun(context.Background(), &fakeRun{config: configuration}), &tools.Call{Arguments: json.RawMessage(`{"command":"ls"}`)}); err == nil || !strings.Contains(err.Error(), "teanode computer start") {
		t.Fatalf("none attached: %v", err)
	}
	// Except for the run the owner said may: the night runs with nobody
	// present and reaches the machine anyway, and the overlay tells it
	// which machine it has.
	night := &fakeRun{headless: true, unattended: true, computer: attached, config: configuration}
	nightly := tools.WithRun(context.Background(), night)
	if result, err := shell.Run(nightly, &tools.Call{Arguments: json.RawMessage(`{"command":"echo hi"}`)}); err != nil || !strings.Contains(result.Content, "hi") {
		t.Fatalf("the night reaches it: %+v %v", result, err)
	}
	if overlay := shell.Overlay(nightly); !strings.Contains(overlay, `"laptop" (linux)`) {
		t.Fatalf("and is told what is attached: %q", overlay)
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
	// The action decides, and the path never does. A list of paths worth
	// asking about was tried and removed: naming some of the dangerous
	// places reads as though it names them all.
	for _, path := range []string{"~/.bashrc", "~/.ssh/authorized_keys", "/etc/passwd"} {
		if got := filesystem.RiskOf(json.RawMessage(`{"action":"write","path":"` + path + `"}`)); got != tools.RiskWrite {
			t.Errorf("writing %s is an ordinary write, got %s", path, got)
		}
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

// Several computers are named with what each is for, when the person said.
//
// A caller asking without naming one is told which are attached; told only
// two host names, it still had to guess which was the one it wanted.
func TestSeveralComputersAreNamedWithWhatEachIsFor(t *testing.T) {
	listed := names([]tools.Computer{
		&fakeComputer{name: "desk", description: "the computer at home, with the family photos"},
		&fakeComputer{name: "travel"},
	})
	if listed != "desk, the computer at home, with the family photos; travel" {
		t.Errorf("listed as %q", listed)
	}
}

// A read that finds a file that is not text says how to get the file.
func TestABinaryReadSaysHowToGetTheFile(t *testing.T) {
	binary := withBinaryHint(&tools.Result{Content: `{"path":"/tmp/shot.png","bytes":5000,"binary":true}`}, "desk", "/tmp/shot.png")
	if !strings.Contains(binary.Content, "share_file") || !strings.Contains(binary.Content, `\"desk\"`) {
		t.Errorf("a binary read answered %s", binary.Content)
	}
	text := withBinaryHint(&tools.Result{Content: `{"path":"/tmp/notes.txt","content":"hello"}`}, "desk", "/tmp/notes.txt")
	if strings.Contains(text.Content, "share_file") {
		t.Errorf("a text read was given the hint: %s", text.Content)
	}
}

// backgroundComputer is a computer whose program keeps background commands.
type backgroundComputer struct {
	fakeComputer
}

func (self *backgroundComputer) HasBackground() bool { return true }

// A command past its wait goes on in the background where the program can
// keep it, and the answer says so; a program that cannot is asked for
// nothing it does not know.
func TestACommandPastItsWaitGoesOnWhereTheProgramCanKeepIt(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	shell := find(t, "shell")

	attached := &backgroundComputer{fakeComputer{answers: map[string]string{
		"shell": `{"stdout":"compiling\n","stderr":"","exitCode":0,"seconds":120,"backgroundId":"01BUILD"}`,
	}}}
	run := &fakeRun{computer: attached, config: configuration}
	result, err := shell.Run(tools.WithRun(context.Background(), run), &tools.Call{Arguments: json.RawMessage(`{"command":"make"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(attached.asked[0], `"shouldKeepOnTimeout":true`) || !strings.Contains(attached.asked[0], `"origin":`) {
		t.Fatalf("the program is asked to keep it past its wait, and told whom to tell: %v", attached.asked)
	}
	if !strings.Contains(result.Content, "goes on in the background") || !strings.Contains(result.Content, "01BUILD") || strings.Contains(result.Content, "exitCode") {
		t.Fatalf("the answer says it is still running, without an exit code it does not have: %s", result.Content)
	}

	// Nobody present: past its wait it is killed as it always was, and it
	// may not be started in the background at all.
	night := &fakeRun{headless: true, unattended: true, computer: attached, config: configuration}
	if _, err := shell.Run(tools.WithRun(context.Background(), night), &tools.Call{Arguments: json.RawMessage(`{"command":"make"}`)}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(attached.asked[1], "shouldKeepOnTimeout") {
		t.Fatalf("a run with nobody present does not leave a command running: %s", attached.asked[1])
	}
	if _, err := shell.Run(tools.WithRun(context.Background(), night), &tools.Call{Arguments: json.RawMessage(`{"command":"make","isBackground":true}`)}); err == nil || !strings.Contains(err.Error(), "nobody present") {
		t.Fatalf("nor start one in the background: %v", err)
	}

	// A program that predates background commands is asked for none.
	old := &fakeComputer{}
	oldRun := tools.WithRun(context.Background(), &fakeRun{computer: old, config: configuration})
	if _, err := shell.Run(oldRun, &tools.Call{Arguments: json.RawMessage(`{"command":"make"}`)}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(old.asked[0], "shouldKeepOnTimeout") || strings.Contains(old.asked[0], "origin") {
		t.Fatalf("an old program is sent what it always was: %s", old.asked[0])
	}
	if _, err := shell.Run(oldRun, &tools.Call{Arguments: json.RawMessage(`{"command":"make","isBackground":true}`)}); err == nil || !strings.Contains(err.Error(), "update") {
		t.Fatalf("and asking it for the background says to update it: %v", err)
	}
}

// The agent stopping its own command is not woken to hear that it ended.
func TestStoppingACommandAcknowledgesItsEnding(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	shell := find(t, "shell")
	attached := &backgroundComputer{fakeComputer{answers: map[string]string{
		"background_stop": `{"id":"01LOOP","isRunning":false,"stopReason":"stopped","origin":{"agentId":"a"}}`,
		"background_list": `[{"id":"01LOOP","command":"sleep 60","isRunning":true,"origin":{"conversationId":"c1"}}]`,
	}}}
	ctx := tools.WithRun(context.Background(), &fakeRun{computer: attached, config: configuration})
	result, err := shell.Run(ctx, &tools.Call{Arguments: json.RawMessage(`{"action":"stop","id":"01LOOP"}`)})
	if err != nil || !strings.Contains(attached.asked[0], `"isAcknowledged":true`) || strings.Contains(result.Content, "origin") {
		t.Fatalf("stop: %v %v %+v", attached.asked, err, result)
	}
	if _, err := shell.Run(ctx, &tools.Call{Arguments: json.RawMessage(`{"action":"read"}`)}); err == nil {
		t.Fatal("read needs an id")
	}
	result, err = shell.Run(ctx, &tools.Call{Arguments: json.RawMessage(`{"action":"list"}`)})
	if err != nil || !strings.Contains(result.Content, "01LOOP") || strings.Contains(result.Content, "origin") || !result.Untrusted {
		t.Fatalf("list: %+v %v", result, err)
	}
}

// A command that printed more than an answer holds still says it is running
// and under which id: the cut is made after the note, not before it.
func TestALongOutputKeepsTheBackgroundId(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	shell := find(t, "shell")
	long := strings.Repeat("compiling a file\n", 5000)
	encoded, _ := json.Marshal(map[string]any{"stdout": long, "stderr": "", "exitCode": 0, "seconds": 120, "backgroundId": "01LONG"})
	attached := &backgroundComputer{fakeComputer{answers: map[string]string{"shell": string(encoded)}}}
	result, err := shell.Run(tools.WithRun(context.Background(), &fakeRun{computer: attached, config: configuration}), &tools.Call{Arguments: json.RawMessage(`{"command":"make"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) > tools.ResultCharacters+100 || !strings.Contains(result.Content, "01LONG") || !strings.Contains(result.Content, "woken when it ends") {
		t.Fatalf("the id and the note survive the cut: %d characters, %q", len(result.Content), result.Content[:200])
	}
}
