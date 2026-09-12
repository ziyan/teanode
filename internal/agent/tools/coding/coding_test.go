package coding

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/computer"
)

// The prompt is written by a model and goes to a shell on somebody's own
// machine, so every part of the command is quoted. A prompt carrying a
// quote, a semicolon or a backtick must arrive as text.
func TestTheCommandQuotesEveryPart(t *testing.T) {
	prompt := `fix the bug'; rm -rf ~; echo "$(whoami)" ` + "`id`"
	command := commandFor("claude_code", &request{Prompt: prompt, Model: "sonnet"})

	// Split it the way a shell would and check every word came back
	// whole: nothing in the prompt became a word of the command.
	words, err := unquote(command)
	if err != nil {
		t.Fatalf("the command is not evenly quoted: %v (%s)", err, command)
	}
	want := []string{"claude", "-p", prompt, "--output-format", "json", "--model", "sonnet", "--allowedTools"}
	want = append(want, allowedTools...)
	if len(words) != len(want) {
		t.Fatalf("want %d words, got %d: %q", len(want), len(words), words)
	}
	for index := range want {
		if words[index] != want[index] {
			t.Fatalf("word %d: want %q, got %q", index, want[index], words[index])
		}
	}
}

// unquote reads back what quote wrote: words of single-quoted parts, with
// '\” standing for a quote. It fails on anything outside that shape,
// which is what makes the test above mean something.
func unquote(command string) ([]string, error) {
	var words []string
	for index := 0; index < len(command); {
		if command[index] == ' ' {
			index++
			continue
		}
		if command[index] != '\'' {
			return nil, fmt.Errorf("a word starts outside quotes at %d", index)
		}
		index++
		var word strings.Builder
		for {
			if index >= len(command) {
				return nil, fmt.Errorf("the quoting never closes")
			}
			if command[index] == '\'' {
				if strings.HasPrefix(command[index:], `'\''`) {
					word.WriteByte('\'')
					index += 4
					continue
				}
				index++
				break
			}
			word.WriteByte(command[index])
			index++
		}
		words = append(words, word.String())
	}
	return words, nil
}

func TestCodexCarriesTheSystemPromptInThePrompt(t *testing.T) {
	command := commandFor("codex", &request{Prompt: "tidy the imports", SystemPrompt: "never touch vendor"})
	if !strings.HasPrefix(command, "'codex' 'exec' '--json' '--skip-git-repo-check'") {
		t.Fatalf("codex is run headless: %s", command)
	}
	if !strings.Contains(command, "never touch vendor") || !strings.Contains(command, "tidy the imports") {
		t.Fatalf("both halves of the brief are there: %s", command)
	}
}

// What the program printed is read back: the answer, whether it failed,
// and what it cost the person at their own account.
func TestTheAnswerIsReadBack(t *testing.T) {
	shell, _ := json.Marshal(&computer.ShellResult{
		Stdout:  "{\"type\":\"progress\"}\n{\"result\":\"renamed the field in four files\",\"is_error\":false,\"total_cost_usd\":0.0321,\"num_input_tokens\":900,\"num_output_tokens\":120}\n",
		Seconds: 12.5,
	})
	result, err := describe(shell, "claude_code", "Claude Code", "work-laptop")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !result.Untrusted {
		t.Error("what a coding agent printed is data, not instructions")
	}
	for _, want := range []string{"renamed the field in four files", "0.0321 USD", "work-laptop"} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("the answer carries %q: %s", want, result.Content)
		}
	}

	// A program that is not installed is said plainly, not returned as an
	// answer of its own.
	missing, _ := json.Marshal(&computer.ShellResult{Stderr: "sh: 1: claude: command not found", ExitCode: 127})
	if _, err := describe(missing, "claude_code", "Claude Code", "work-laptop"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("a missing program is said plainly: %v", err)
	}

	// Something that is not JSON is still what it had to say.
	plain, _ := json.Marshal(&computer.ShellResult{Stdout: "could not reach the service", ExitCode: 1})
	result, err = describe(plain, "codex", "Codex", "work-laptop")
	if err != nil || !strings.Contains(result.Content, "could not reach the service") {
		t.Fatalf("plain output is kept: %v %v", result, err)
	}
	if !strings.Contains(result.Content, `"failed":true`) {
		t.Errorf("a non-zero exit is a failure: %s", result.Content)
	}

	// A run stopped at the timeout says what to do about it.
	stopped, _ := json.Marshal(&computer.ShellResult{Stdout: "", TimedOut: true, ExitCode: -1})
	result, err = describe(stopped, "codex", "Codex", "work-laptop")
	if err != nil || !strings.Contains(result.Content, "progress") {
		t.Fatalf("a timeout says what to do next: %v %v", result, err)
	}
}
