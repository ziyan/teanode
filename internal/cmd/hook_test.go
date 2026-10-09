package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
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

// A group of another program's hooks is left exactly as it was, even
// one with no commands in it, and the file keeps its characters and stays
// the link it was.
func TestEditingHooksLeavesOtherGroupsAndTheFileAlone(t *testing.T) {
	directory := t.TempDir()
	real := filepath.Join(directory, "dotfiles-settings.json")
	link := filepath.Join(directory, "settings.json")
	original := `{"timeout": 600, "hooks": {"Stop": [{"matcher": "", "hooks": []}, {"matcher": "*"}], "SessionStart": [{"hooks": [{"type": "command", "command": "run-other-tool > log 2>&1 && true"}]}]}}`
	if err := os.WriteFile(real, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := editHookFile(link, func(hooks map[string]any) { removeTeaNodeHooks(hooks, "claude-code") }); err != nil {
		t.Fatalf("editHookFile: %s", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the settings file is still a link: %v", err)
	}
	written, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"timeout": 600`, `run-other-tool > log 2>&1 && true`, `"hooks": []`, `"matcher": "*"`} {
		if !strings.Contains(string(written), want) {
			t.Fatalf("the file keeps %q:\n%s", want, written)
		}
	}
	if strings.Contains(string(written), "null") {
		t.Fatalf("no group's hooks become null:\n%s", written)
	}
}

// What git says about a checkout is read whole, a space in its path
// included, remotes and all.
func TestTheHookReadsTheCheckoutFromGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "my checkouts", "seedling")
	run := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=A", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=A", "GIT_COMMITTER_EMAIL=a@example.com")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %s", arguments, err, output)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o700); err != nil {
		t.Fatal(err)
	}
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "first")
	run("remote", "add", "origin", "git@git.example.com:garden/seedling.git")
	run("remote", "add", "mirror", "https://git.example.net/garden/seedling")

	command := &cli.Command{Flags: []cli.Flag{&cli.StringFlag{Name: "computer", Value: "workbench"}}}
	place := codingPlaceAt(context.Background(), command, filepath.Join(root, "cmd"), "session")
	if resolved, _ := filepath.EvalSymlinks(root); place.CheckoutRoot != root && place.CheckoutRoot != resolved {
		t.Fatalf("the checkout's top directory, space and all: %q", place.CheckoutRoot)
	}
	if len(place.Head) != 40 {
		t.Fatalf("the commit: %q", place.Head)
	}
	slices.Sort(place.RemoteURLs)
	if strings.Join(place.RemoteURLs, " ") != "git@git.example.com:garden/seedling.git https://git.example.net/garden/seedling" {
		t.Fatalf("every remote: %q", place.RemoteURLs)
	}
}

// The hook talks to the server its command names, whatever URL the coding
// tool's environment carries: a shell that loaded a development server's
// variables would otherwise send every prompt there.
func TestTheHookTalksToTheProfileItNames(t *testing.T) {
	root := &cli.Command{Flags: []cli.Flag{
		&cli.StringFlag{Name: "url", Value: "http://127.0.0.1:9"},
		&cli.StringFlag{Name: "token", Value: "from-the-environment"},
		&cli.StringFlag{Name: "profile", Value: LocalProfileName},
		&cli.BoolFlag{Name: "insecure"}, &cli.BoolFlag{Name: "read-only"},
	}}
	resolved, err := hookTarget(root)
	if err != nil {
		t.Fatalf("hookTarget: %s", err)
	}
	if !resolved.Local || resolved.URL != "" {
		t.Fatalf("the named profile, not the environment's URL: %+v", resolved)
	}
	arguments := globalArguments(root)
	if slices.Contains(arguments, "--url") {
		t.Fatalf("the capture is not handed the environment's URL: %v", arguments)
	}
}
