package computer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	deviceComputer "github.com/ziyan/teanode/internal/computer"
)

// The person's herdr sessions: the Claude Code and Codex sessions they keep
// open in herdr's panes, on each of their attached computers.
//
// The agent works in them beside the person rather than starting a coding
// agent of its own: it reads what a session said and did, looks at its
// screen, types an instruction in where the person can see it, and is told
// when one it watches finishes. A question a session asks is the person's
// to answer; the agent passes on what they chose, and the card it raises
// says exactly what will be pressed.
//
// The program on the computer decides and refuses (internal/computer/
// herdr.go), so this tool, the dashboard and the command line behave the
// same.

// HerdrAction is one thing that can be done with herdr sessions, by its
// name on each surface: the tool's action, the GraphQL operation the
// dashboard and the command line call, and the command.
type HerdrAction struct {
	ToolAction string
	Operation  string
	Command    string
}

// HerdrActions are every one, on every surface. A test fails when a surface
// lacks one, or has one this does not list.
var HerdrActions = []HerdrAction{
	{ToolAction: "list", Operation: "ListAgentHerdrSessions", Command: "list"},
	{ToolAction: "read", Operation: "ReadAgentHerdrSession", Command: "read"},
	{ToolAction: "screen", Operation: "ReadAgentHerdrScreen", Command: "screen"},
	{ToolAction: "send", Operation: "SendAgentHerdrSession", Command: "send"},
	{ToolAction: "wait", Operation: "WaitAgentHerdrSession", Command: "wait"},
	{ToolAction: "answer", Operation: "AnswerAgentHerdrQuestion", Command: "answer"},
	{ToolAction: "watch", Operation: "WatchAgentHerdrSession", Command: "watch"},
	{ToolAction: "setup", Operation: "SetUpAgentHerdrHooks", Command: "setup"},
}

func herdrToolActions() []string {
	actions := make([]string, 0, len(HerdrActions))
	for _, action := range HerdrActions {
		actions = append(actions, action.ToolAction)
	}
	return actions
}

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "herdr", Family: tools.FamilyComputer, Risk: tools.RiskGranting,
				Description: "The person's own Claude Code and Codex sessions, in herdr's panes on their attached computers: work in them beside the person. list is every session on every computer, with the state each is in (idle, working, asking, unknown) and the question it waits on; read is a session's last turns from its history; screen is what its pane shows now, or its last lines; send types text into the pane and presses enter, in front of the person, refused while it asks a question, and while it works unless should_queue; wait waits for it to stop working; answer answers the question it waits on with the options the person chose, by number and label, or with free_text, and only while question_fingerprint is still the question on screen; watch has you woken in this conversation when it next finishes its turn; setup puts TeaNode's reporting hooks into Claude Code on that computer (is_removal takes them out). A pane is named by computer and pane together.",
				Parameters: tools.Object(map[string]any{
					"action":               tools.EnumProperty("what to do", herdrToolActions()...),
					"computer":             tools.StringProperty("which computer, by name; list covers every one, and the others need it when more than one runs herdr"),
					"pane":                 tools.StringProperty("the pane, as list gives it, such as w1:p2; every action but list and setup needs it"),
					"turn_count":           tools.IntegerProperty("read: how many of the last turns, 10 by default, 100 at most"),
					"line_count":           tools.IntegerProperty("screen: the last this many lines rather than the screen as it stands"),
					"text":                 tools.StringProperty("send: what to type; enter is pressed after it"),
					"should_queue":         tools.BooleanProperty("send: type it even while the session works, for it to read when its turn ends"),
					"wait_seconds":         tools.IntegerProperty("wait: how long to wait at most, 30 by default, 600 at most"),
					"question_fingerprint": tools.StringProperty("answer: the question's fingerprint, as list gave it"),
					"option_numbers":       map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "answer: the numbers of the options the person chose"},
					"option_labels":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "answer: the labels of those options, exactly as list gave them, one for each of option_numbers"},
					"free_text":            tools.StringProperty("answer: what the person said to type, for an option that takes text"),
					"is_removal":           tools.BooleanProperty("setup: take the hooks out rather than putting them in"),
				}, "action"),
				Guidance: "herdr: list first, and read before you act on a session; never start a coding agent of your own for work the person keeps in herdr. A question a session waits on is the person's: show it with its options, and answer only with what they chose, never your own pick. Before you send, say what you will type: the person may be typing in the same pane. After a send you will not wait on, watch the pane, end your turn, and you are woken when it finishes.",
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call herdrArguments
					if err := json.Unmarshal(arguments, &call); err != nil {
						return tools.RiskGranting
					}
					switch strings.ToLower(strings.TrimSpace(call.Action)) {
					case "list", "read", "screen", "wait", "watch":
						return tools.RiskRead
					case "send", "setup":
						return tools.RiskWrite
					}
					// An answer approves what a coding agent does as the
					// person on their machine.
					return tools.RiskGranting
				},
				Preview: func(arguments json.RawMessage) string {
					var call herdrArguments
					_ = json.Unmarshal(arguments, &call)
					where := "pane " + call.Pane
					if strings.TrimSpace(call.Computer) != "" {
						where += " on " + call.Computer
					}
					switch strings.ToLower(strings.TrimSpace(call.Action)) {
					case "send":
						return fmt.Sprintf("Type into the coding session in %s: %s", where, shorten(call.Text, 120))
					case "answer":
						var chosen []string
						for index, number := range call.OptionNumbers {
							label := ""
							if index < len(call.OptionLabels) {
								label = " " + call.OptionLabels[index]
							}
							chosen = append(chosen, strconv.Itoa(number)+"."+label)
						}
						if call.FreeText != "" {
							chosen = append(chosen, strconv.Quote(call.FreeText))
						}
						return fmt.Sprintf("Answer the question in %s with: %s", where, strings.Join(chosen, ", "))
					case "watch":
						return "Be told when the coding session in " + where + " finishes"
					case "setup":
						if call.IsRemoval {
							return "Take TeaNode's hooks out of Claude Code on " + call.Computer
						}
						return "Put TeaNode's reporting hooks into Claude Code on " + call.Computer
					}
					return ""
				},
				Run: runHerdr,
			},
		}
	})
}

type herdrArguments struct {
	Action              string   `json:"action"`
	Computer            string   `json:"computer"`
	Pane                string   `json:"pane"`
	TurnCount           int      `json:"turn_count"`
	LineCount           int      `json:"line_count"`
	Text                string   `json:"text"`
	ShouldQueue         bool     `json:"should_queue"`
	WaitSeconds         int      `json:"wait_seconds"`
	QuestionFingerprint string   `json:"question_fingerprint"`
	OptionNumbers       []int    `json:"option_numbers"`
	OptionLabels        []string `json:"option_labels"`
	FreeText            string   `json:"free_text"`
	IsRemoval           bool     `json:"is_removal"`
}

// herdrWait is how long an action is waited for, beyond its own wait.
const herdrWait = 30 * time.Second

// herdrComputers are the attached computers whose program watches herdr.
func herdrComputers(run tools.Run) ([]tools.Computer, error) {
	computing, ok := run.(tools.Computing)
	if !ok || !computing.ComputersAllowed() || !tools.FeatureAllowed(run.Configuration(), "computer") {
		return nil, errors.New("attaching a computer is off on this server")
	}
	attached := computing.AttachedComputers()
	if len(attached) == 0 {
		return nil, errors.New("no computer is attached; ask the person to run `teanode computer start` on it")
	}
	var watching []tools.Computer
	for _, one := range attached {
		if holder, ok := one.(tools.FeatureHolder); ok && holder.HasFeature(deviceComputer.FeatureHerdr) {
			watching = append(watching, one)
		}
	}
	if len(watching) == 0 {
		return nil, fmt.Errorf("the program on %s is too old for herdr; ask the person to update teanode there and restart it", names(attached))
	}
	return watching, nil
}

// herdrComputerOf is the computer a call means: the one named, or the only
// one that runs herdr.
func herdrComputerOf(run tools.Run, name string) (tools.Computer, error) {
	watching, err := herdrComputers(run)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if len(watching) == 1 {
			return watching[0], nil
		}
		return nil, fmt.Errorf("several computers run herdr (%s); say which in computer", names(watching))
	}
	attached, err := computerOf(run, name)
	if err != nil {
		return nil, err
	}
	if holder, ok := attached.(tools.FeatureHolder); !ok || !holder.HasFeature(deviceComputer.FeatureHerdr) {
		return nil, fmt.Errorf("the program on %s is too old for herdr; ask the person to update teanode there and restart it", attached.Name())
	}
	return attached, nil
}

func runHerdr(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[herdrArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(arguments.Action))
	if action == "list" {
		return listHerdr(ctx, run, arguments.Computer)
	}
	attached, err := herdrComputerOf(run, arguments.Computer)
	if err != nil {
		return nil, err
	}
	asked := &deviceComputer.HerdrArguments{PaneID: strings.TrimSpace(arguments.Pane)}
	wait := herdrWait
	note := ""
	switch action {
	case "read":
		asked.TurnCount = arguments.TurnCount
		note = "read the coding session in " + asked.PaneID
	case "screen":
		asked.LineCount = arguments.LineCount
		note = "looked at the screen of " + asked.PaneID
	case "send":
		asked.Text, asked.ShouldQueue = arguments.Text, arguments.ShouldQueue
		note = "typed into " + asked.PaneID + ": " + shorten(arguments.Text, 40)
	case "wait":
		asked.WaitSeconds = arguments.WaitSeconds
		wait += time.Duration(max(arguments.WaitSeconds, 30)) * time.Second
		note = "waited for " + asked.PaneID
	case "answer":
		asked.QuestionFingerprint, asked.OptionNumbers, asked.OptionLabels, asked.FreeText =
			arguments.QuestionFingerprint, arguments.OptionNumbers, arguments.OptionLabels, arguments.FreeText
		if len(arguments.OptionNumbers) > 0 && len(arguments.OptionLabels) != len(arguments.OptionNumbers) {
			return nil, errors.New("answer needs option_labels, one for each of option_numbers, exactly as list gave them")
		}
		note = "answered the question in " + asked.PaneID
	case "watch":
		if !tools.CanLeaveRunning(run) || tools.ConversationIDOf(run) == "" {
			return nil, errors.New("this run has no conversation to wake when the session finishes; use wait instead")
		}
		origin, err := json.Marshal(tools.BackgroundOrigin{AgentID: run.Agent().ID, ConversationID: tools.ConversationIDOf(run)})
		if err != nil {
			return nil, err
		}
		asked.Origin = origin
		note = "watching " + asked.PaneID
	case "setup":
		asked.IsRemoval = arguments.IsRemoval
		note = "set up the hooks on " + attached.Name()
	default:
		return nil, fmt.Errorf("%q is not one of %s", arguments.Action, strings.Join(herdrToolActions(), ", "))
	}
	return carry(ctx, attached, "herdr_"+action, asked, wait, note)
}

// listHerdr lists the sessions on every computer that runs herdr, or the
// one named, each under its computer's name.
func listHerdr(ctx context.Context, run tools.Run, name string) (*tools.Result, error) {
	var asked []tools.Computer
	if strings.TrimSpace(name) != "" {
		one, err := herdrComputerOf(run, name)
		if err != nil {
			return nil, err
		}
		asked = []tools.Computer{one}
	} else {
		watching, err := herdrComputers(run)
		if err != nil {
			return nil, err
		}
		asked = watching
	}
	listed := map[string]any{}
	for _, one := range asked {
		answer, err := one.Ask(ctx, "herdr_list", &deviceComputer.HerdrArguments{}, herdrWait)
		if err != nil {
			listed[one.Name()] = map[string]string{"error": err.Error()}
			continue
		}
		listed[one.Name()] = json.RawMessage(answer)
	}
	result, err := tools.JSONResult(map[string]any{"computers": listed})
	if err != nil {
		return nil, err
	}
	if len(result.Content) > tools.ResultCharacters {
		result.Content = result.Content[:tools.ResultCharacters] + "\n[cut here: the answer goes on]"
	}
	// What a coding session says and asks is data from outside.
	result.Untrusted = true
	result.Note = "listed the herdr sessions"
	return result, nil
}
