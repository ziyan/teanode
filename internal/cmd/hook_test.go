package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Installing keeps every hook another program put in the file, and
// installing twice leaves one set of TeaNode's; uninstalling takes out
// only TeaNode's.
func TestInstallingHooksKeepsEveryOtherHook(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	original := `{"model": "opus", "hooks": {"SessionStart": [{"matcher": "*", "hooks": [{"type": "command", "command": "bash ~/.claude/hooks/other-tool session", "timeout": 10}]}]}}`
	if err := os.WriteFile(file, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	install := func(hooks map[string]any) {
		removeTeaNodeHooks(hooks, "claude-code")
		for _, event := range hookEvents {
			entries, _ := hooks[event.name].([]any)
			hooks[event.name] = append(entries, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/opt/bin/teanode hook claude-code", "timeout": event.timeoutSeconds}}})
		}
	}
	for range 2 {
		if err := editHookFile(file, install); err != nil {
			t.Fatalf("editHookFile: %s", err)
		}
	}
	settings := readSettings(t, file)
	if settings["model"] != "opus" {
		t.Fatalf("the rest of the file is kept: %v", settings)
	}
	commands := hookCommandsOf(settings)
	if strings.Count(commands, "teanode hook claude-code") != len(hookEvents) {
		t.Fatalf("one TeaNode hook an event however often it is installed:\n%s", commands)
	}
	if !strings.Contains(commands, "other-tool") {
		t.Fatalf("the other program's hook is kept:\n%s", commands)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("the file keeps its permissions: %v %v", info.Mode(), err)
	}

	if err := editHookFile(file, func(hooks map[string]any) { removeTeaNodeHooks(hooks, "claude-code") }); err != nil {
		t.Fatalf("editHookFile: %s", err)
	}
	commands = hookCommandsOf(readSettings(t, file))
	if strings.Contains(commands, "teanode") || !strings.Contains(commands, "other-tool") {
		t.Fatalf("uninstalling takes out only TeaNode's:\n%s", commands)
	}
}

func TestATeaNodeHookIsKnownByItsCommand(t *testing.T) {
	for hookCommand, isTeaNode := range map[string]bool{
		"/opt/bin/teanode hook claude-code":                      true,
		"'/opt/my tools/teanode' hook claude-code":               true,
		"/opt/bin/teanode --profile work hook claude-code":       true,
		"/opt/bin/teanode hook codex":                            false,
		"bash ~/.claude/hooks/other-tool hook claude-code":       false,
		"/usr/local/bin/teanode-dev hook claude-code --computer": true,
	} {
		if got := isTeaNodeHook(hookCommand, "claude-code"); got != isTeaNode {
			t.Fatalf("isTeaNodeHook(%q) = %v", hookCommand, got)
		}
	}
}

// A page shown to a session is held back for a few prompts, then may be
// shown again: by then the tool may have compacted it away.
func TestAShownPageIsHeldBackForAFewPrompts(t *testing.T) {
	state := &hookState{ShownAtPrompt: map[string]int{}}
	state.remember([]string{"projects/seedling"})
	for prompt := 1; prompt <= hookShownPrompts; prompt++ {
		state.PromptCount = prompt
		if shown := state.recentlyShown(); len(shown) != 1 {
			t.Fatalf("at prompt %d the page is still held back: %v", prompt, shown)
		}
	}
	state.PromptCount = hookShownPrompts + 1
	if shown := state.recentlyShown(); len(shown) != 0 {
		t.Fatalf("after %d prompts it may be shown again: %v", hookShownPrompts, shown)
	}
}

// What a hook prints is the form both tools read additional context in,
// and nothing at all where there is nothing to show.
func TestAHookPrintsAdditionalContext(t *testing.T) {
	var output bytes.Buffer
	if err := writeHookOutput(&output, "UserPromptSubmit", ""); err != nil || output.Len() != 0 {
		t.Fatalf("nothing to show prints nothing: %q %v", output.String(), err)
	}
	if err := writeHookOutput(&output, "UserPromptSubmit", "<teanode-memory>\nx\n</teanode-memory>"); err != nil {
		t.Fatal(err)
	}
	var printed hookOutput
	if err := json.Unmarshal(output.Bytes(), &printed); err != nil {
		t.Fatal(err)
	}
	if printed.HookSpecificOutput.HookEventName != "UserPromptSubmit" || !strings.Contains(printed.HookSpecificOutput.AdditionalContext, "teanode-memory") {
		t.Fatalf("printed %s", output.String())
	}
}

func TestShellQuoteWordsReadsBackAsTheSameWords(t *testing.T) {
	words := []string{"/opt/my tools/teanode", "--profile", "it's", "hook", "codex"}
	got := shellQuoteWords(words)
	want := `'/opt/my tools/teanode' --profile 'it'\''s' hook codex`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if back := shellWords(got); strings.Join(back, "|") != strings.Join(words, "|") {
		t.Fatalf("read back as %q", back)
	}
}

func readSettings(t *testing.T, file string) map[string]any {
	t.Helper()
	encoded, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string]any{}
	if err := json.Unmarshal(encoded, &settings); err != nil {
		t.Fatalf("the file is still JSON: %s", err)
	}
	return settings
}

func hookCommandsOf(settings map[string]any) string {
	var commands []string
	hooks, _ := settings["hooks"].(map[string]any)
	for _, value := range hooks {
		entries, _ := value.([]any)
		for _, entry := range entries {
			group, _ := entry.(map[string]any)
			list, _ := group["hooks"].([]any)
			for _, hook := range list {
				fields, _ := hook.(map[string]any)
				hookCommand, _ := fields["command"].(string)
				commands = append(commands, hookCommand)
			}
		}
	}
	return strings.Join(commands, "\n")
}
