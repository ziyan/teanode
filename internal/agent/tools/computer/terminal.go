package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/computer"
)

// A terminal the agent drives.
//
// The shell tool runs one command and returns when it ends. A great deal of
// what is worth running does not end on its own: it prompts, it draws, it
// waits to be told what to do next. A coding agent is the case in point --
// asked a question, it puts the question on the screen and waits, and the
// only way to answer is to be in the terminal with it.
//
// So this opens a terminal on the person's own computer, runs a program in
// it, and reads what the program shows: the screen as it stands, not the
// stream of bytes that drew it. Typing goes in; keys go in by name, because
// most of what driving a program needs is Enter, an arrow, or a control
// character, and asking a model to write escape sequences is asking for
// mistakes. And there is a wait for the screen to settle, so that a model
// need not guess how long a program takes to answer.

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "terminal", Family: tools.FamilyComputer, Risk: tools.RiskWrite,
				Description: "A terminal on the person's own computer, which you drive. start opens one and runs a program in it (a shell if you name none), and answers with a session id and the first screen; attached is the terminal the person is sitting in themselves, when they ran `teanode terminal` — they see everything you type there; type sends text; keys sends named keys — enter, tab, escape, up, down, left, right, backspace, home, end, pageup, pagedown, space, ctrl-c, ctrl-d, ctrl-z, ctrl-l, f1 through f12 — and either answers with the screen after it; read is the screen now; wait watches the screen until it stops changing, or the program ends, or the seconds run out; resize changes the size; signal sends int, term, kill or hup; close ends it. You read the screen, not a log: what the program is showing at this moment, with the cursor's position. A program that asks something puts the question on the screen and waits; answer it with type and keys enter. Close what you opened when you are done.",
				Parameters: tools.Object(map[string]any{
					"action":    tools.EnumProperty("what to do", "start", "attached", "type", "keys", "read", "wait", "resize", "signal", "close"),
					"computer":  tools.StringProperty("which of their computers, by name, when more than one is attached"),
					"session":   tools.StringProperty("the session id start answered with; every action but start needs it"),
					"command":   tools.StringProperty("start: the program to run, their shell by default"),
					"arguments": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "start: the program's arguments, one per item"},
					"directory": tools.StringProperty("start: where to run it; their home directory by default"),
					"columns":   tools.IntegerProperty("start, resize: the terminal's width, 120 by default"),
					"rows":      tools.IntegerProperty("start, resize: its height, 40 by default"),
					"text":      tools.StringProperty("type: what to type, exactly; it is not followed by enter unless you send that key"),
					"keys":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "keys: the keys to press, in order, by name"},
					"seconds":   tools.IntegerProperty("wait: how long to watch for at most, 30 by default, 600 at most; type, keys, start: how long to let the screen settle before answering, 2 by default"),
					"signal":    tools.StringProperty("signal: int, term, kill or hup"),
				}, "action"),

				Guidance: "terminal: read the screen after each step and answer what it asks; wait rather than guessing how long a program takes; close the session when you are done. attached is the terminal the person is sitting in, when they attached one: they see what you type.",
				// The terminal the person is sitting in, when they attached one: said
				// every round, because it changes what typing means.
				Overlay: func(ctx context.Context) string {
					run := tools.MustRun(ctx)
					if run.Headless() {
						return ""
					}
					computing, ok := run.(tools.Computing)
					if !ok || !computing.ComputersAllowed() {
						return ""
					}
					for _, attached := range computing.AttachedComputers() {
						holder, ok := attached.(tools.SessionHolder)
						if !ok || holder.AttachedTerminal() == "" {
							continue
						}
						return fmt.Sprintf("<terminal>\nThe person has attached the terminal they are sitting in, on %s, with `teanode terminal`. terminal with action attached (computer: %q) gives you its session; read shows what they see, and type types where their cursor is. You are looking at the same screen: say what you are about to type before you type it, and do not close it -- it is theirs.\n</terminal>", attached.Name(), attached.Name())
					}
					return ""
				},
				// Opening a terminal runs a program on the person's machine, and
				// that is the moment they are asked -- not every keystroke after.
				// A card in front of each key is a card nobody reads, and the
				// program they said yes to is what the keys go to.
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call terminalArguments
					_ = json.Unmarshal(arguments, &call)
					if strings.EqualFold(strings.TrimSpace(call.Action), "start") {
						return startRisk(&call)
					}
					return tools.RiskWrite
				},
				Preview: func(arguments json.RawMessage) string {
					var call terminalArguments
					_ = json.Unmarshal(arguments, &call)
					where := "their computer"
					if strings.TrimSpace(call.Computer) != "" {
						where = call.Computer
					}
					switch strings.ToLower(strings.TrimSpace(call.Action)) {
					case "start":
						program := strings.TrimSpace(call.Command)
						if program == "" {
							program = "their shell"
						} else if len(call.Arguments) > 0 {
							program += " " + strings.Join(call.Arguments, " ")
						}
						return fmt.Sprintf("Open a terminal on %s and run %s", where, program)
					case "type":
						return fmt.Sprintf("Type into the terminal on %s: %s", where, shorten(call.Text, 80))
					case "keys":
						return fmt.Sprintf("Press %s in the terminal on %s", strings.Join(call.Keys, ", "), where)
					case "signal":
						return fmt.Sprintf("Send %s to the program in the terminal on %s", call.Signal, where)
					case "close":
						return fmt.Sprintf("Close the terminal on %s", where)
					}
					return ""
				},
				Run: runTerminal,
			},
		}
	})
}

// startRisk is what opening a terminal is worth asking about: the same
// judgment the shell tool makes of a command line. A program named with
// its arguments is classified as the shell tool would classify it, and a
// shell handed a script with -c is judged by the script, so a listing or
// a build opens without a card; a bare shell, which the model then types
// anything into, asks, because the keys after the card never do.
func startRisk(call *terminalArguments) tools.Risk {
	program := strings.TrimSpace(call.Command)
	if program == "" {
		return tools.RiskDestructive
	}
	line := program
	if len(call.Arguments) > 0 {
		line += " " + strings.Join(call.Arguments, " ")
	}
	if script, ok := shellScriptOf(program, call.Arguments); ok {
		line = script
	} else if isShell(program) && len(call.Arguments) == 0 {
		return tools.RiskDestructive
	}
	if computer.Classify(line).Action == computer.ActionAllow {
		return tools.RiskWrite
	}
	return tools.RiskDestructive
}

// shellScriptOf is the script a shell was handed with -c, when the
// program is a shell and one of its arguments is -c (or -lc and the like)
// followed by the script.
func shellScriptOf(program string, arguments []string) (string, bool) {
	if !isShell(program) {
		return "", false
	}
	for index, argument := range arguments {
		if strings.HasPrefix(argument, "-") && strings.Contains(argument, "c") && index+1 < len(arguments) {
			return arguments[index+1], true
		}
	}
	return "", false
}

func isShell(program string) bool {
	switch strings.ToLower(filepath.Base(program)) {
	case "sh", "bash", "zsh", "dash", "fish", "ksh", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh":
		return true
	}
	return false
}

type terminalArguments struct {
	Action    string   `json:"action"`
	Computer  string   `json:"computer"`
	Session   string   `json:"session"`
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
	Directory string   `json:"directory"`
	Columns   int      `json:"columns"`
	Rows      int      `json:"rows"`
	Text      string   `json:"text"`
	Keys      []string `json:"keys"`
	Seconds   int      `json:"seconds"`
	Signal    string   `json:"signal"`
}

// The waits.
const (
	// settleQuiet is how long the screen has to go without changing to be
	// called settled: long enough for a program that answers in bursts,
	// short enough that a wait is not mostly waiting.
	settleQuiet = 1200 * time.Millisecond
	// settlePoll is how often the screen is read while waiting.
	settlePoll = 300 * time.Millisecond
	// settleAfterInput is how long the screen is given after something was
	// typed, before it is read back, when the call did not say.
	settleAfterInput = 2 * time.Second
	// waitAtMost bounds a wait, however long it asked for.
	waitAtMost = 600 * time.Second
)

func runTerminal(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[terminalArguments](call)
	if err != nil {
		return nil, err
	}
	run := tools.MustRun(ctx)
	attached, err := computerOf(run, arguments.Computer)
	if err != nil {
		return nil, err
	}
	holder, ok := attached.(tools.SessionHolder)
	if !ok {
		return nil, fmt.Errorf("%s cannot hold a terminal open", attached.Name())
	}

	action := strings.ToLower(strings.TrimSpace(arguments.Action))
	if action == "attached" {
		id := holder.AttachedTerminal()
		if id == "" {
			return nil, fmt.Errorf("%s has no terminal attached; the person attaches one with `teanode terminal`", attached.Name())
		}
		screen, err := holder.ReadScreen(ctx, id)
		if err != nil {
			return nil, err
		}
		return screenResult(id, screen, "the terminal "+attached.Name()+" is sitting in")
	}
	if action != "start" && strings.TrimSpace(arguments.Session) == "" {
		return nil, fmt.Errorf("%s needs the session id that start answered with", action)
	}
	id := strings.TrimSpace(arguments.Session)

	switch action {
	case "start":
		command := strings.TrimSpace(arguments.Command)
		if command == "" {
			command = "/bin/sh"
			if strings.HasPrefix(strings.ToLower(attached.System()), "windows") {
				return nil, fmt.Errorf("a terminal cannot be opened on a Windows computer yet")
			}
		}
		id, err = holder.StartSession(ctx, "pty", command, arguments.Arguments, arguments.Directory, nil, arguments.Columns, arguments.Rows)
		if err != nil {
			return nil, err
		}
		screen, err := settle(ctx, holder, id, secondsOr(arguments.Seconds, settleAfterInput))
		if err != nil {
			return nil, err
		}
		return screenResult(id, screen, "opened a terminal on "+attached.Name())

	case "type":
		if arguments.Text == "" {
			return nil, fmt.Errorf("type needs text")
		}
		if err := holder.WriteSession(ctx, id, []byte(arguments.Text)); err != nil {
			return nil, err
		}
		screen, err := settle(ctx, holder, id, secondsOr(arguments.Seconds, settleAfterInput))
		if err != nil {
			return nil, err
		}
		return screenResult(id, screen, "typed "+shorten(arguments.Text, 40))

	case "keys":
		if len(arguments.Keys) == 0 {
			return nil, fmt.Errorf("keys needs at least one key")
		}
		var pressed []byte
		for _, name := range arguments.Keys {
			bytes, known := keyBytes(name)
			if !known {
				return nil, fmt.Errorf("%q is not a key this sends; the names are enter, tab, escape, up, down, left, right, backspace, delete, home, end, pageup, pagedown, space, ctrl-a through ctrl-z, f1 through f12", name)
			}
			pressed = append(pressed, bytes...)
		}
		if err := holder.WriteSession(ctx, id, pressed); err != nil {
			return nil, err
		}
		screen, err := settle(ctx, holder, id, secondsOr(arguments.Seconds, settleAfterInput))
		if err != nil {
			return nil, err
		}
		return screenResult(id, screen, "pressed "+strings.Join(arguments.Keys, " "))

	case "read":
		screen, err := holder.ReadScreen(ctx, id)
		if err != nil {
			return nil, err
		}
		return screenResult(id, screen, "read the screen")

	case "wait":
		wait := secondsOr(arguments.Seconds, 30*time.Second)
		if wait > waitAtMost {
			wait = waitAtMost
		}
		screen, err := settle(ctx, holder, id, wait)
		if err != nil {
			return nil, err
		}
		return screenResult(id, screen, "waited for the screen to settle")

	case "resize":
		if arguments.Columns <= 0 || arguments.Rows <= 0 {
			return nil, fmt.Errorf("resize needs columns and rows")
		}
		if err := holder.ResizeSession(ctx, id, arguments.Columns, arguments.Rows); err != nil {
			return nil, err
		}
		screen, err := settle(ctx, holder, id, settleQuiet)
		if err != nil {
			return nil, err
		}
		return screenResult(id, screen, fmt.Sprintf("resized to %d by %d", arguments.Columns, arguments.Rows))

	case "signal":
		if err := holder.SignalSession(ctx, id, arguments.Signal); err != nil {
			return nil, err
		}
		screen, err := settle(ctx, holder, id, settleQuiet)
		if err != nil {
			return nil, err
		}
		return screenResult(id, screen, "sent "+arguments.Signal)

	case "close":
		if id == holder.AttachedTerminal() {
			// Theirs, not the agent's: they are sitting in it, and leaving
			// the shell is how they end it. The prompt says as much; this
			// is for the round where the prompt was not enough.
			return nil, fmt.Errorf("that is the terminal the person is sitting in; it is theirs to leave, not yours to close")
		}
		if err := holder.CloseSession(ctx, id); err != nil {
			return nil, err
		}
		result, err := tools.JSONResult(map[string]any{"session": id, "closed": true})
		if err != nil {
			return nil, err
		}
		result.Note = "closed the terminal"
		return result, nil
	}
	return nil, fmt.Errorf("%q is not start, attached, type, keys, read, wait, resize, signal or close", arguments.Action)
}

// settle reads the screen until it has stopped changing for a while, the
// program has ended, or the time is up -- and answers with the screen as it
// stands then.
func settle(ctx context.Context, holder tools.SessionHolder, id string, most time.Duration) (*tools.Screen, error) {
	deadline := time.Now().Add(most)
	var last *tools.Screen
	quietSince := time.Time{}
	for {
		screen, err := holder.ReadScreen(ctx, id)
		if err != nil {
			return nil, err
		}
		last = screen
		if screen.Ended {
			return screen, nil
		}
		if screen.Changed {
			quietSince = time.Now()
		} else if quietSince.IsZero() {
			quietSince = time.Now()
		}
		if !screen.Changed && time.Since(quietSince) >= settleQuiet {
			return screen, nil
		}
		if time.Now().After(deadline) {
			return last, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(settlePoll):
		}
	}
}

// screenResult is the screen as the model reads it.
func screenResult(id string, screen *tools.Screen, note string) (*tools.Result, error) {
	answer := map[string]any{
		"session": id,
		"screen":  screen.Text,
		"cursor":  map[string]int{"x": screen.CursorX, "y": screen.CursorY},
		"size":    map[string]int{"columns": screen.Columns, "rows": screen.Rows},
	}
	if screen.Ended {
		answer["ended"] = true
		answer["exit_code"] = screen.Code
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	// What a program draws is data from outside, whatever it says.
	result.Untrusted = true
	result.Note = note
	return result, nil
}

func secondsOr(seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func shorten(text string, most int) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\n", " ")
	if letters := []rune(text); len(letters) > most {
		return string(letters[:most]) + "…"
	}
	return text
}

// keyBytes is what a named key sends to a terminal.
func keyBytes(name string) ([]byte, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	switch key {
	case "enter", "return":
		return []byte("\r"), true
	case "tab":
		return []byte("\t"), true
	case "escape", "esc":
		return []byte("\x1b"), true
	case "backspace":
		return []byte("\x7f"), true
	case "delete", "del":
		return []byte("\x1b[3~"), true
	case "space":
		return []byte(" "), true
	case "up":
		return []byte("\x1b[A"), true
	case "down":
		return []byte("\x1b[B"), true
	case "right":
		return []byte("\x1b[C"), true
	case "left":
		return []byte("\x1b[D"), true
	case "home":
		return []byte("\x1b[H"), true
	case "end":
		return []byte("\x1b[F"), true
	case "pageup":
		return []byte("\x1b[5~"), true
	case "pagedown":
		return []byte("\x1b[6~"), true
	}
	// ctrl-a through ctrl-z are the control characters 1 through 26.
	if strings.HasPrefix(key, "ctrl-") && len(key) == 6 {
		letter := key[5]
		if letter >= 'a' && letter <= 'z' {
			return []byte{letter - 'a' + 1}, true
		}
	}
	// f1 through f12.
	functions := map[string]string{
		"f1": "\x1bOP", "f2": "\x1bOQ", "f3": "\x1bOR", "f4": "\x1bOS",
		"f5": "\x1b[15~", "f6": "\x1b[17~", "f7": "\x1b[18~", "f8": "\x1b[19~",
		"f9": "\x1b[20~", "f10": "\x1b[21~", "f11": "\x1b[23~", "f12": "\x1b[24~",
	}
	if sequence, found := functions[key]; found {
		return []byte(sequence), true
	}
	return nil, false
}
