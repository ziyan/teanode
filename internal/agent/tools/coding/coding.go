// Package coding hands a piece of work to a coding agent on the person's
// own computer. The agent here writes the brief and reads the answer; the
// program that does the work runs where their files are, as them, through
// the same attached computer the shell and filesystem tools use.
package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/computer"
	device "github.com/ziyan/teanode/internal/computer"
)

const (
	// defaultSeconds is how long a run is given when the call says
	// nothing, and longestSeconds the most the attached computer will
	// hold a command for. A longer piece of work is done by telling the
	// coding agent to write its progress to a file and calling again.
	defaultSeconds = 300
	longestSeconds = 600

	// waitOver is added to the command's own timeout before the answer is
	// given up on, so that the computer's own report of a timeout is what
	// comes back rather than this end guessing.
	waitOver = 30 * time.Second
)

// allowedTools are the ones the coding agent may use without stopping to
// ask, since nobody is at its end of the run to answer. The person's
// confirmation of this call is what stands for all of them.
var allowedTools = []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep", "WebFetch", "WebSearch"}

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			define("claude_code", "Claude Code", "Hand a piece of coding work to Claude Code on the person's own computer: it reads and edits their files, runs commands and works on its own until it is done, then answers. Say what to do and where, in the prompt, as you would brief somebody. One call is one run with no memory of the last, so for work that spans calls, tell it to keep a plan or a progress file and to read that file first."),
			define("codex", "Codex", "Hand a piece of coding work to Codex on the person's own computer: it reads and edits their files, runs commands and works on its own until it is done, then answers. Say what to do and where, in the prompt, as you would brief somebody. One call is one run with no memory of the last, so for work that spans calls, tell it to keep a plan or a progress file and to read that file first."),
		}
	})
}

func define(name, label, description string) *tools.Tool {
	return &tools.Tool{
		Name: name, Family: tools.FamilyComputer, Risk: tools.RiskDestructive,
		Description: description,
		Parameters: tools.Object(map[string]any{
			"prompt":        tools.StringProperty("what to do, in as much detail as you would give a person"),
			"directory":     tools.StringProperty("where to work, on their computer; their home directory by default"),
			"system_prompt": tools.StringProperty("standing instructions for how to work, added to its own"),
			"model":         tools.StringProperty("a model to use, its own default otherwise"),
			"timeout":       tools.IntegerProperty(fmt.Sprintf("seconds before it is stopped, %d by default, %d at most", defaultSeconds, longestSeconds)),
			"computer":      tools.StringProperty("which attached computer, when several are"),
		}, "prompt"),
		Guidance: label + ": it works on its own and answers once, so the brief is the whole of what it knows -- name the files or the directory, say what done looks like, and say what not to touch. It costs the person money at their own account with " + label + ", not through this server, so one considered call beats several tries. Read what it answers before telling them it is done.",
		Preview: func(arguments json.RawMessage) string {
			var call request
			if err := json.Unmarshal(arguments, &call); err != nil {
				return "Run " + label + " on your computer"
			}
			where := strings.TrimSpace(call.Directory)
			if where == "" {
				where = "your home directory"
			}
			return fmt.Sprintf("Run %s on your computer in %s, which may change files and run commands there: %s", label, where, tools.FirstWords(call.Prompt, 20))
		},
		Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
			return run(ctx, call, name, label)
		},
	}
}

type request struct {
	Prompt       string `json:"prompt"`
	Directory    string `json:"directory"`
	SystemPrompt string `json:"system_prompt"`
	Model        string `json:"model"`
	Timeout      int    `json:"timeout"`
	Computer     string `json:"computer"`
}

func run(ctx context.Context, call *tools.Call, name, label string) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[request](call)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.Prompt) == "" {
		return nil, fmt.Errorf("a prompt is needed: say what %s should do", label)
	}
	attached, err := computer.Of(tools.MustRun(ctx), arguments.Computer)
	if err != nil {
		return nil, err
	}
	seconds := arguments.Timeout
	if seconds <= 0 {
		seconds = defaultSeconds
	}
	if seconds > longestSeconds {
		seconds = longestSeconds
	}
	command := commandFor(name, &arguments)
	answer, err := attached.Ask(ctx, "shell", &device.ShellArguments{
		Command:   command,
		Directory: arguments.Directory,
		Timeout:   seconds,
	}, time.Duration(seconds)*time.Second+waitOver)
	if err != nil {
		return nil, err
	}
	return describe(answer, name, label, attached.Name())
}

// commandFor is the command line the coding agent is started with. Every
// part of it is quoted for the shell on the other end, because the prompt
// is written by a model and a stray quote in it must end up in the prompt
// and not in the command.
func commandFor(name string, arguments *request) string {
	parts := []string{name}
	switch name {
	case "claude_code":
		parts = []string{"claude", "-p", arguments.Prompt, "--output-format", "json"}
		if model := strings.TrimSpace(arguments.Model); model != "" {
			parts = append(parts, "--model", model)
		}
		// Always given, so that it never stops to ask a person who is not
		// there and simply hangs until the timeout.
		parts = append(parts, "--allowedTools")
		parts = append(parts, allowedTools...)
		if system := strings.TrimSpace(arguments.SystemPrompt); system != "" {
			parts = append(parts, "--append-system-prompt", system)
		}
	case "codex":
		prompt := arguments.Prompt
		if system := strings.TrimSpace(arguments.SystemPrompt); system != "" {
			prompt = "Additional instructions:\n" + system + "\n\nThe work:\n" + arguments.Prompt
		}
		parts = []string{"codex", "exec", "--json", "--skip-git-repo-check"}
		if model := strings.TrimSpace(arguments.Model); model != "" {
			parts = append(parts, "--model", model)
		}
		parts = append(parts, prompt)
	}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, quote(part))
	}
	return strings.Join(quoted, " ")
}

// quote wraps a word for /bin/sh, which is what the attached computer runs
// a command through. Single quotes take everything literally, and the one
// character they cannot hold is closed, escaped and opened again.
func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// codingAnswer is what these programs print with their JSON option. They
// do not agree on the names, so both spellings are read.
type codingAnswer struct {
	Result       string  `json:"result"`
	IsError      bool    `json:"is_error"`
	CostUSD      float64 `json:"cost_usd"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	InputTokens  int     `json:"num_input_tokens"`
	OutputTokens int     `json:"num_output_tokens"`
}

func describe(answer json.RawMessage, name, label, on string) (*tools.Result, error) {
	var shell device.ShellResult
	if err := json.Unmarshal(answer, &shell); err != nil {
		return nil, fmt.Errorf("the computer answered something %s cannot read: %w", name, err)
	}
	described := map[string]any{"ran_on": on, "seconds": shell.Seconds}
	if shell.TimedOut {
		described["timed_out"] = true
		described["do_this_next"] = fmt.Sprintf("%s was stopped at the timeout. Tell it to write its progress to a file in the working directory, then call again asking it to read that file first.", label)
	}
	if shell.ExitCode == 127 || strings.Contains(shell.Stderr, "command not found") {
		return nil, fmt.Errorf("%s is not installed on %s, or is not on the path of a non-interactive shell; the person can check with the shell tool", label, on)
	}
	var parsed codingAnswer
	if err := json.Unmarshal([]byte(strings.TrimSpace(lastLine(shell.Stdout))), &parsed); err != nil || parsed.Result == "" {
		// It printed something else, which is still what it had to say.
		text := strings.TrimSpace(shell.Stdout)
		if text == "" {
			text = strings.TrimSpace(shell.Stderr)
		}
		described["answer"] = text
		described["failed"] = shell.ExitCode != 0
	} else {
		described["answer"] = parsed.Result
		described["failed"] = parsed.IsError || shell.ExitCode != 0
		if cost := parsed.CostUSD + parsed.TotalCostUSD; cost > 0 {
			// Their own account with the service, not this server's.
			described["cost_to_the_person"] = fmt.Sprintf("%.4f USD", cost)
		}
		if parsed.InputTokens+parsed.OutputTokens > 0 {
			described["tokens"] = parsed.InputTokens + parsed.OutputTokens
		}
	}
	if shell.StdoutTruncated {
		described["cut"] = "the computer cut what it printed"
	}
	result, err := tools.JSONResult(described)
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	result.Note = fmt.Sprintf("ran %s on %s", label, on)
	return result, nil
}

// lastLine is the final non-empty line, because these programs print a
// stream of events and the whole answer is the last of them.
func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return ""
}
