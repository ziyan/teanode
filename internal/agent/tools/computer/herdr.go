package computer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	{ToolAction: "open", Operation: "OpenAgentHerdrSession", Command: "open"},
	{ToolAction: "close", Operation: "CloseAgentHerdrSession", Command: "close"},
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
				Name: "herdr", Family: tools.FamilyComputer, Risk: tools.RiskWrite,
				// Said to MCP clients to change nothing, though send and answer
				// type into the person's sessions, at the person's word: a
				// client such as ChatGPT asks before every call of a tool
				// that is not read-only and would not let them always allow
				// it, and they wanted to talk to their own coding sessions
				// from it without a prompt each time. Inside TeaNode the
				// risk is still read or write by action, and a session that
				// asks, or an answer to a question that changed, is refused
				// whoever calls.
				Annotations: tools.LocalReadOnly(),
				Description: "The person's own Claude Code and Codex sessions, in herdr's panes on their attached computers: work in them beside the person. list is the sessions on every computer (or the one named), those asking first, then those working, a page at a time with totalCount and nextOffset, each with the state it is in (idle, working, asking, unknown) and the question it waits on; read is a session's last turns from its history; screen is what its pane shows now, or its last lines; send types text into the pane and presses enter, in front of the person; a session at work reads it as a message in its turn, and one that asks a question refuses it until the question is answered; wait waits for it to stop working; answer answers the question it waits on with the options the person chose, by number and label, or with free_text, and only while question_fingerprint is still the question on screen; watch has you woken in this conversation when it next finishes its turn; open starts a new Claude Code or Codex session in a directory, in a herdr pane of its own (a new tab of the workspace named after the directory, or a new workspace), and close ends one and its pane, refused while it works; setup puts TeaNode's reporting hooks into Claude Code on that computer (is_removal takes them out). A pane is named by computer and pane together; call it by its paneName when you talk to the person, since its id means nothing to them.",
				Parameters: tools.Object(map[string]any{
					"action":                  tools.EnumProperty("what to do", herdrToolActions()...),
					"computer":                tools.StringProperty("which computer, by name; list covers every one, and the others need it when more than one runs herdr"),
					"pane":                    tools.StringProperty("the pane, by its paneName as list gives it (workspace, tab and agent) or its paneId; every action but list and setup needs it"),
					"limit":                   tools.IntegerProperty("list: how many sessions a page gives, 20 by default, 50 at most"),
					"offset":                  tools.IntegerProperty("list: how many to skip, for the next page (the nextOffset the page before gave)"),
					"turn_count":              tools.IntegerProperty("read: how many of the last turns, 10 by default, 100 at most"),
					"line_count":              tools.IntegerProperty("screen: the last this many lines rather than the screen as it stands"),
					"text":                    tools.StringProperty("send: what to type; enter is pressed after it"),
					"wait_seconds":            tools.IntegerProperty("wait: how long to wait at most, 30 by default, 600 at most"),
					"question_fingerprint":    tools.StringProperty("answer: the question's fingerprint, as list gave it"),
					"option_numbers":          map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "answer: the numbers of the options the person chose"},
					"option_labels":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "answer: the labels of those options, exactly as list gave them, one for each of option_numbers"},
					"free_text":               tools.StringProperty("answer: what the person said to type, for an option that takes text"),
					"is_removal":              tools.BooleanProperty("setup: take the hooks out rather than putting them in"),
					"directory":               tools.StringProperty("open: the directory to start the coding agent in, on that computer; ~ is the person's home"),
					"coding_agent":            tools.EnumProperty("open: which coding agent to start", "claude", "codex"),
					"agent_name":              tools.StringProperty("open: what to call the session in herdr; the directory's name by default"),
					"should_skip_permissions": tools.BooleanProperty("open: start it without asking before it acts (Claude Code's --dangerously-skip-permissions, Codex's --yolo); only when the person asks for it"),
				}, "action"),
				Guidance: "herdr: list first, and read before you act on a session; start one with open only when the person wants a new session, and close only one they are done with. A question a session waits on is the person's: show it with its options, and answer only with what they chose, never your own pick. Before you send, say what you will type: the person may be typing in the same pane. After a send you will not wait on, watch the pane, end your turn, and you are woken when it finishes.",
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call herdrArguments
					if err := json.Unmarshal(arguments, &call); err != nil {
						return tools.RiskWrite
					}
					switch strings.ToLower(strings.TrimSpace(call.Action)) {
					case "list", "read", "screen", "wait", "watch":
						return tools.RiskRead
					}
					// A send and an answer go to the person's own sessions
					// without a card each time, at their asking: they found
					// one per answer too many. What keeps an answer theirs is
					// the guidance, the option labels it must carry, and the
					// fingerprint of the question they were shown.
					return tools.RiskWrite
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
					case "open":
						return fmt.Sprintf("Start %s in %s on %s", call.CodingAgent, call.Directory, call.Computer)
					case "close":
						return "Close the coding session in " + where
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
	Action                string   `json:"action"`
	Computer              string   `json:"computer"`
	Pane                  string   `json:"pane"`
	TurnCount             int      `json:"turn_count"`
	LineCount             int      `json:"line_count"`
	Text                  string   `json:"text"`
	WaitSeconds           int      `json:"wait_seconds"`
	QuestionFingerprint   string   `json:"question_fingerprint"`
	OptionNumbers         []int    `json:"option_numbers"`
	OptionLabels          []string `json:"option_labels"`
	FreeText              string   `json:"free_text"`
	IsRemoval             bool     `json:"is_removal"`
	Directory             string   `json:"directory"`
	CodingAgent           string   `json:"coding_agent"`
	AgentName             string   `json:"agent_name"`
	ShouldSkipPermissions bool     `json:"should_skip_permissions"`
	Limit                 int      `json:"limit"`
	Offset                int      `json:"offset"`
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
		return listHerdr(ctx, run, arguments.Computer, arguments.Limit, arguments.Offset)
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
		asked.Text = arguments.Text
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
	case "open":
		asked.Directory, asked.CodingAgentKind, asked.AgentName = arguments.Directory, arguments.CodingAgent, arguments.AgentName
		asked.ShouldSkipPermissions = arguments.ShouldSkipPermissions
		wait = 90 * time.Second
		note = "opened " + arguments.CodingAgent + " in " + arguments.Directory
	case "close":
		note = "closed " + asked.PaneID
	case "setup":
		asked.IsRemoval = arguments.IsRemoval
		note = "set up the hooks on " + attached.Name()
	default:
		return nil, fmt.Errorf("%q is not one of %s", arguments.Action, strings.Join(herdrToolActions(), ", "))
	}
	return carry(ctx, attached, "herdr_"+action, asked, wait, note)
}

// herdrListLimit is how many sessions a page of list gives by default, and
// herdrListMost the most it gives: a listing of every pane on several
// computers runs past what some clients of the tool read whole.
const (
	herdrListLimit = 20
	herdrListMost  = 50
)

// herdrListedSession is a session as list gives it: what the agent needs
// to name it, judge it and answer it. Its history file and herdr's own
// status are left out; read and screen go further.
type herdrListedSession struct {
	Computer          string                        `json:"computer"`
	PaneID            string                        `json:"paneId"`
	PaneName          string                        `json:"paneName"`
	CodingAgentKind   string                        `json:"codingAgentKind"`
	HerdrSessionState string                        `json:"herdrSessionState"`
	PaneTitle         string                        `json:"paneTitle,omitempty"`
	WorkingDirectory  string                        `json:"workingDirectory,omitempty"`
	IsWatched         bool                          `json:"isWatched,omitempty"`
	Question          *deviceComputer.HerdrQuestion `json:"question,omitempty"`
}

// herdrStateOrder puts the sessions that want the person first.
var herdrStateOrder = map[string]int{
	deviceComputer.HerdrSessionStateAsking:  0,
	deviceComputer.HerdrSessionStateWorking: 1,
	deviceComputer.HerdrSessionStateIdle:    2,
}

// listHerdr lists the sessions on every computer that runs herdr, or the
// one named: those asking first, then those working, then the rest, a page
// at a time, saying how many there are and how to read on.
func listHerdr(ctx context.Context, run tools.Run, name string, limit, offset int) (*tools.Result, error) {
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
	if limit <= 0 {
		limit = herdrListLimit
	}
	limit = min(limit, herdrListMost)
	offset = max(offset, 0)
	var sessions []herdrListedSession
	failures := map[string]string{}
	for _, one := range asked {
		answer, err := one.Ask(ctx, "herdr_list", &deviceComputer.HerdrArguments{}, herdrWait)
		if err != nil {
			failures[one.Name()] = err.Error()
			continue
		}
		var listed []*deviceComputer.HerdrSession
		if err := json.Unmarshal(answer, &listed); err != nil {
			failures[one.Name()] = "its program answered what this server cannot read: " + err.Error()
			continue
		}
		for _, session := range listed {
			sessions = append(sessions, herdrListedSession{
				Computer: one.Name(), PaneID: session.PaneID, PaneName: session.PaneName,
				CodingAgentKind: session.CodingAgentKind, HerdrSessionState: session.HerdrSessionState,
				PaneTitle: session.PaneTitle, WorkingDirectory: session.WorkingDirectory,
				IsWatched: session.IsWatched, Question: session.Question,
			})
		}
	}
	sort.SliceStable(sessions, func(left, right int) bool {
		leftOrder, isLeftKnown := herdrStateOrder[sessions[left].HerdrSessionState]
		rightOrder, isRightKnown := herdrStateOrder[sessions[right].HerdrSessionState]
		if !isLeftKnown {
			leftOrder = len(herdrStateOrder)
		}
		if !isRightKnown {
			rightOrder = len(herdrStateOrder)
		}
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if sessions[left].Computer != sessions[right].Computer {
			return sessions[left].Computer < sessions[right].Computer
		}
		return sessions[left].PaneName < sessions[right].PaneName
	})
	totalCount := len(sessions)
	page := sessions[min(offset, totalCount):min(offset+limit, totalCount)]
	answer := map[string]any{"herdrSessions": page, "totalCount": totalCount, "offset": offset}
	if len(failures) > 0 {
		answer["unreachableComputers"] = failures
	}
	if remainingCount := totalCount - offset - len(page); remainingCount > 0 {
		answer["nextOffset"] = offset + len(page)
		answer["moreNote"] = fmt.Sprintf("%d more sessions; list again with offset %d to read on", remainingCount, offset+len(page))
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	// What a coding session says and asks is data from outside.
	result.Untrusted = true
	result.Note = "listed the herdr sessions"
	return result, nil
}
