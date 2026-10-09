package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// The hooks a coding tool runs: Claude Code and Codex call `teanode hook
// <tool>` at points in a session's life with the event as JSON on
// standard input, and print what the command prints into the model's
// context. A session is shown the memory of the checkout it starts in,
// and what each prompt recalls; each answer has the tool's transcript
// read in again. See docs/planning/coding-session-memory-execplan.md.

// hookTools are the coding tools hooks are installed in, by the name of
// the source type that reads their transcripts.
var hookTools = []string{"claude-code", "codex"}

// hookEvents are the events a hook is installed for, and how long each
// may take, in seconds. Codex gives SessionEnd three seconds at most,
// which is why capture never waits for the server.
var hookEvents = []struct {
	name           string
	timeoutSeconds int
}{
	{"SessionStart", 30},
	{"UserPromptSubmit", 15},
	{"Stop", 10},
	{"PreCompact", 10},
	{"SessionEnd", 3},
}

const (
	// hookShownPrompts is for how many prompts a page shown to a session
	// is not shown to it again: it is still in the context the tool
	// keeps, and showing it again spends the budget on what is there.
	hookShownPrompts = 5

	// hookPromptWait and hookStartWait are how long a hook waits for the
	// server before it gives up and shows nothing. A prompt the person is
	// waiting on is not held up for memory.
	hookPromptWait = 10 * time.Second
	hookStartWait  = 20 * time.Second

	// hookGitWait is how long asking git about the checkout may take: a
	// checkout on a slow disk is not worth holding a prompt for.
	hookGitWait = 2 * time.Second
)

// NewHookCommand is `teanode hook`.
func NewHookCommand() *cli.Command {
	computerFlag := &cli.StringFlag{Name: "computer", Usage: "the name this computer is attached under, where it is not the host name", Sources: cli.EnvVars("TEANODE_COMPUTER_NAME")}
	everywhereFlag := &cli.BoolFlag{Name: "everywhere", Usage: "recall from all of memory, not only the checkout's project and what it links to"}
	commands := []*cli.Command{}
	for _, tool := range hookTools {
		commands = append(commands, &cli.Command{
			Name:  tool,
			Usage: "what " + toolName(tool) + " runs at each point of a session: reads the event on standard input and prints what the session is shown",
			Flags: []cli.Flag{computerFlag, everywhereFlag},
			Action: func(ctx context.Context, command *cli.Command) error {
				return runHook(ctx, command, tool)
			},
		})
	}
	commands = append(commands,
		&cli.Command{
			Name:      "install",
			Usage:     "add the hooks to Claude Code's settings or Codex's hooks file, keeping every other hook there",
			ArgsUsage: "<claude-code|codex>",
			Flags:     []cli.Flag{computerFlag, everywhereFlag},
			Action:    runHookInstall,
		},
		&cli.Command{
			Name:      "uninstall",
			Usage:     "take TeaNode's hooks out of the tool's file, leaving every other hook",
			ArgsUsage: "<claude-code|codex>",
			Action:    runHookUninstall,
		},
		&cli.Command{
			Name:   "capture",
			Usage:  "ask the sources reading a coding tool's transcripts on this computer to read again now; what a hook starts after an answer",
			Hidden: true,
			Flags: []cli.Flag{computerFlag,
				&cli.StringFlag{Name: "assistant", Usage: "claude-code or codex"},
			},
			Action: runHookCapture,
		},
	)
	return &cli.Command{
		Name:  "hook",
		Usage: "show your coding sessions what your agent remembers: the hooks Claude Code and Codex run",
		Description: "Once installed (teanode hook install claude-code, or codex), a session started in a checkout your agent has read " +
			"is shown the checkout's page and where the last session there stopped; each prompt is shown what it " +
			"recalls from that project; and each answer is read in within a minute. A hook that cannot reach the server shows nothing " +
			"and never stops the tool. 'teanode agent memory checkout' prints what a session would be shown.",
		Commands: commands,
	}
}

func toolName(tool string) string {
	if tool == "codex" {
		return "Codex"
	}
	return "Claude Code"
}

// hookEvent is what both tools write on a hook's standard input; each
// event fills some of it.
type hookEvent struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Directory      string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Prompt         string `json:"prompt"`
	Source         string `json:"source"`
}

// hookOutput is how a hook adds to the model's context, in the form both
// tools read.
type hookOutput struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// runHook answers one event. It never fails the tool: whatever goes
// wrong is written to the hook log and the session is shown nothing.
func runHook(ctx context.Context, command *cli.Command, tool string) error {
	// A panic exits 2, which Claude Code reads as "block this prompt" (or,
	// on Stop, "do not stop"): never that, whatever went wrong.
	defer func() {
		if recovered := recover(); recovered != nil {
			logHook("%s: %v", tool, recovered)
		}
	}()
	if err := answerHook(ctx, command, tool, os.Stdin, command.Root().Writer); err != nil {
		logHook("%s: %s", tool, err)
	}
	return nil
}

func answerHook(ctx context.Context, command *cli.Command, tool string, input io.Reader, output io.Writer) error {
	var event hookEvent
	if err := json.NewDecoder(input).Decode(&event); err != nil {
		return fmt.Errorf("cannot read the event: %w", err)
	}
	switch event.HookEventName {
	case "SessionStart":
		return showSessionStart(ctx, command, &event, output)
	case "UserPromptSubmit":
		return showPromptRecall(ctx, command, &event, output)
	case "Stop", "PreCompact":
		return startCapture(command, tool)
	case "SessionEnd":
		forgetHookState(event.SessionID)
		return startCapture(command, tool)
	}
	return nil
}

func codingPlaceOf(ctx context.Context, command *cli.Command, event *hookEvent) *client.AgentCodingPlace {
	return codingPlaceAt(ctx, command, event.Directory, event.SessionID)
}

// codingPlaceAt is a directory on this computer and what git says about
// the checkout it is in: its remotes, its commit and its top directory.
// The server finds a checkout this computer never profiled by its remote,
// since another computer's checkout at the same path may be another
// repository, or this one at another commit.
func codingPlaceAt(ctx context.Context, command *cli.Command, directory, sessionId string) *client.AgentCodingPlace {
	home, _ := os.UserHomeDir()
	place := &client.AgentCodingPlace{Directory: directory, ComputerName: computerNameOf(command), HomeDirectory: home, SessionID: sessionId}
	gitContext, cancel := context.WithTimeout(ctx, hookGitWait)
	defer cancel()
	git := func(arguments ...string) string {
		output, err := exec.CommandContext(gitContext, "git", append([]string{"-C", directory}, arguments...)...).Output()
		if err != nil {
			return ""
		}
		return string(output)
	}
	// A line each, not a word each: a checkout's path may have a space in it.
	if lines := strings.Split(strings.TrimRight(git("rev-parse", "--show-toplevel", "HEAD"), "\n"), "\n"); len(lines) == 2 {
		place.CheckoutRoot, place.Head = lines[0], lines[1]
	}
	// With -z each entry is the key, a newline and the value, ended by a
	// NUL, so a value is read whole whatever it holds.
	for _, entry := range strings.Split(git("config", "-z", "--get-regexp", `^remote\..*\.url$`), "\x00") {
		if _, remote, isEntry := strings.Cut(entry, "\n"); isEntry && remote != "" {
			place.RemoteURLs = append(place.RemoteURLs, remote)
		}
	}
	return place
}

func computerNameOf(command *cli.Command) string {
	if name := strings.TrimSpace(command.String("computer")); name != "" {
		return name
	}
	name, _ := os.Hostname()
	return name
}

func showSessionStart(ctx context.Context, command *cli.Command, event *hookEvent, output io.Writer) error {
	// A new start, a resume or a compaction: in every case what was shown
	// before is no longer in the context, so nothing is held back, even
	// when the server cannot be reached this time.
	state := &hookState{ShownAtPrompt: map[string]int{}}
	saveHookState(event.SessionID, state)
	pruneHookStates(time.Now())
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	connection.SetTimeout(hookStartWait)
	shown, err := client.ReadAgentCodingContext(ctx, connection, codingPlaceOf(ctx, command, event))
	if err != nil {
		return err
	}
	state.remember(shown.ShownPaths)
	saveHookState(event.SessionID, state)
	return writeHookOutput(output, event.HookEventName, shown.Text)
}

func showPromptRecall(ctx context.Context, command *cli.Command, event *hookEvent, output io.Writer) error {
	state := loadHookState(event.SessionID)
	state.PromptCount++
	defer saveHookState(event.SessionID, state)
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	connection.SetTimeout(hookPromptWait)
	shown, err := client.RecallAgentCodingMemory(ctx, connection, codingPlaceOf(ctx, command, event), event.Prompt, state.recentlyShown(), command.Bool("everywhere"))
	if err != nil {
		return err
	}
	state.remember(shown.ShownPaths)
	return writeHookOutput(output, event.HookEventName, shown.Text)
}

func writeHookOutput(output io.Writer, eventName, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var answer hookOutput
	answer.HookSpecificOutput.HookEventName = eventName
	answer.HookSpecificOutput.AdditionalContext = text
	return json.NewEncoder(output).Encode(answer)
}

// startCapture starts `teanode hook capture` on its own and returns at
// once: the tool waits for a hook, and Codex ends a session's hooks after
// three seconds, so the server is asked by a process nobody waits for.
func startCapture(command *cli.Command, tool string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	arguments := append(globalArguments(command), "hook", "capture", "--assistant", tool)
	if name := command.String("computer"); name != "" {
		arguments = append(arguments, "--computer", name)
	}
	child := exec.Command(executable, arguments...)
	child.Stdin, child.Stdout, child.Stderr = nil, nil, nil
	detach(child)
	if err := child.Start(); err != nil {
		return err
	}
	return child.Process.Release()
}

// globalArguments is the root flags this command was run with that say
// which server to talk to, for a command started from it.
func globalArguments(command *cli.Command) []string {
	var arguments []string
	for _, name := range []string{"profile", "url"} {
		if value := command.String(name); value != "" && command.IsSet(name) {
			arguments = append(arguments, "--"+name, value)
		}
	}
	if command.IsSet("insecure") && command.Bool("insecure") {
		arguments = append(arguments, "--insecure")
	}
	return arguments
}

func runHookCapture(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		logHook("capture: %s", err)
		return nil
	}
	connection.SetTimeout(hookStartWait)
	if _, err := client.CaptureAgentCodingSession(ctx, connection, computerNameOf(command), command.String("assistant")); err != nil {
		logHook("capture: %s", err)
	}
	return nil
}

// hookState is what a session's hooks remember between prompts: how many
// prompts there have been, and at which one each page was last shown.
type hookState struct {
	PromptCount   int            `json:"promptCount"`
	ShownAtPrompt map[string]int `json:"shownAtPrompt"`
}

func (self *hookState) remember(paths []string) {
	for _, shownPath := range paths {
		self.ShownAtPrompt[shownPath] = self.PromptCount
	}
}

func (self *hookState) recentlyShown() []string {
	var paths []string
	for shownPath, prompt := range self.ShownAtPrompt {
		if self.PromptCount-prompt <= hookShownPrompts {
			paths = append(paths, shownPath)
		}
	}
	slices.Sort(paths)
	return paths
}

// hookStatePath is where a session's state is kept, under the user's
// cache directory: losing it costs a page shown twice, nothing more.
func hookStatePath(sessionId string) string {
	cache, err := os.UserCacheDir()
	if err != nil || sessionId == "" || strings.ContainsAny(sessionId, `/\`) {
		return ""
	}
	return filepath.Join(cache, "teanode", "hooks", sessionId+".json")
}

// hookStateKept is how long a session's state outlives its last use: a
// session that ended without its SessionEnd (a killed terminal) leaves
// its file behind, and nothing else would ever remove it.
const hookStateKept = 7 * 24 * time.Hour

// pruneHookStates removes the state of sessions not heard from for a week.
func pruneHookStates(now time.Time) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(filepath.Join(cache, "teanode", "hooks"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !strings.HasSuffix(entry.Name(), ".json") || now.Sub(info.ModTime()) < hookStateKept {
			continue
		}
		_ = os.Remove(filepath.Join(cache, "teanode", "hooks", entry.Name()))
	}
}

// forgetHookState removes an ended session's state.
func forgetHookState(sessionId string) {
	if statePath := hookStatePath(sessionId); statePath != "" {
		_ = os.Remove(statePath)
	}
}

func loadHookState(sessionId string) *hookState {
	state := &hookState{ShownAtPrompt: map[string]int{}}
	if statePath := hookStatePath(sessionId); statePath != "" {
		if encoded, err := os.ReadFile(statePath); err == nil {
			_ = json.Unmarshal(encoded, state)
		}
	}
	if state.ShownAtPrompt == nil {
		state.ShownAtPrompt = map[string]int{}
	}
	return state
}

func saveHookState(sessionId string, state *hookState) {
	statePath := hookStatePath(sessionId)
	if statePath == "" {
		return
	}
	encoded, err := json.Marshal(state)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(statePath), 0o700)
	}
	if err == nil {
		err = os.WriteFile(statePath, encoded, 0o600)
	}
	if err != nil {
		logHook("cannot keep the session's state: %s", err)
	}
}

// logHook writes what went wrong where the person can look for it: a
// hook's standard error is shown to them by Claude Code, and a server
// that is down is not worth a warning at every prompt.
func logHook(format string, arguments ...any) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return
	}
	logPath := filepath.Join(cache, "teanode", "hooks", "hook.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, arguments...))
}

// hookFileOf is the file a tool keeps its hooks in.
func hookFileOf(tool string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch tool {
	case "claude-code":
		return filepath.Join(home, ".claude", "settings.json"), nil
	case "codex":
		return filepath.Join(home, ".codex", "hooks.json"), nil
	}
	return "", fmt.Errorf("the tool is one of %s", strings.Join(hookTools, ", "))
}

func toolOf(command *cli.Command) (string, error) {
	tool := command.Args().First()
	if !slices.Contains(hookTools, tool) {
		return "", usage("name the tool: " + strings.Join(hookTools, " or "))
	}
	return tool, nil
}

func runHookInstall(ctx context.Context, command *cli.Command) error {
	tool, err := toolOf(command)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// The server is named in the hook, so a later sign-in to another
	// server does not send this computer's prompts there, and so the hook
	// does not depend on a token in the environment the tool runs in.
	resolved, err := resolveCommandTarget(command)
	if err != nil {
		return err
	}
	profile := resolved.Profile
	if resolved.Local {
		profile = LocalProfileName
	}
	if profile == "" {
		return usage("the hooks name a saved profile, and " + resolved.URL + " is not one: sign in with teanode auth login --url " + resolved.URL + " first")
	}
	words := []string{executable, "--profile", profile, "hook", tool}
	if name := command.String("computer"); name != "" {
		words = append(words, "--computer", name)
	}
	if command.Bool("everywhere") {
		words = append(words, "--everywhere")
	}
	// Exit 2 from a hook blocks the prompt in Claude Code, and the binary
	// named here may be replaced one day by one with no `hook` command,
	// which exits 2 for a command it does not know: so the hook's own exit
	// status is never the tool's business.
	hookCommand := shellQuoteWords(words) + " 2>/dev/null || true"
	file, err := hookFileOf(tool)
	if err != nil {
		return err
	}
	if err := editHookFile(file, func(hooks map[string]any) {
		removeTeaNodeHooks(hooks, tool)
		for _, event := range hookEvents {
			entries, _ := hooks[event.name].([]any)
			hooks[event.name] = append(entries, map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": hookCommand, "timeout": event.timeoutSeconds}},
			})
		}
	}); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(command.Root().Writer, "Installed in %s: %s\n", file, hookCommand)
	if tool == "codex" {
		_, _ = fmt.Fprintln(command.Root().Writer, "Codex runs hooks only with `hooks = true` under [features] in ~/.codex/config.toml, and asks you to trust a new hook before it first runs: answer it, or review it with /hooks.")
	}
	return nil
}

func runHookUninstall(ctx context.Context, command *cli.Command) error {
	tool, err := toolOf(command)
	if err != nil {
		return err
	}
	file, err := hookFileOf(tool)
	if err != nil {
		return err
	}
	if err := editHookFile(file, func(hooks map[string]any) { removeTeaNodeHooks(hooks, tool) }); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(command.Root().Writer, "Removed TeaNode's hooks from %s\n", file)
	return nil
}

// isTeaNodeHook says whether a hook command is the one install wrote for
// a tool: it runs `hook <tool>` of a program named teanode.
func isTeaNodeHook(hookCommand, tool string) bool {
	words := shellWords(hookCommand)
	if len(words) < 3 || !strings.HasPrefix(filepath.Base(words[0]), "teanode") {
		return false
	}
	for index := 1; index+1 < len(words); index++ {
		if words[index] == "hook" && words[index+1] == tool {
			return true
		}
	}
	return false
}

// removeTeaNodeHooks takes the tool's TeaNode hooks out of a hooks map,
// and every entry and event left empty by it, leaving every other hook.
func removeTeaNodeHooks(hooks map[string]any, tool string) {
	for eventName, value := range hooks {
		entries, isList := value.([]any)
		if !isList {
			continue
		}
		var kept []any
		for _, entry := range entries {
			group, isGroup := entry.(map[string]any)
			if !isGroup {
				kept = append(kept, entry)
				continue
			}
			commands, _ := group["hooks"].([]any)
			var keptCommands []any
			for _, hook := range commands {
				fields, _ := hook.(map[string]any)
				hookCommand, _ := fields["command"].(string)
				if !isTeaNodeHook(hookCommand, tool) {
					keptCommands = append(keptCommands, hook)
				}
			}
			if len(keptCommands) < len(commands) {
				if len(keptCommands) == 0 {
					continue
				}
				group["hooks"] = keptCommands
			}
			kept = append(kept, group)
		}
		if len(kept) == 0 {
			delete(hooks, eventName)
		} else {
			hooks[eventName] = kept
		}
	}
}

// editHookFile changes the "hooks" object of a tool's JSON file and writes
// the file back with everything else in it as it was. A file that is not
// JSON is left alone rather than replaced.
func editHookFile(file string, change func(hooks map[string]any)) error {
	// The file a link points at, so that a settings file kept in a
	// dotfiles repository stays a link rather than being replaced by a copy.
	if target, err := filepath.EvalSymlinks(file); err == nil {
		file = target
	}
	settings := map[string]any{}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(file); err == nil {
		mode = info.Mode().Perm()
	}
	encoded, err := os.ReadFile(file)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		// Numbers as written, not through a float.
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&settings); err != nil {
			return fmt.Errorf("%s is not JSON this can edit safely: %w", file, err)
		}
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	change(hooks)
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	// Without escaping < > and &, which other hooks' commands are full
	// of: the file is one people read and edit by hand.
	var written bytes.Buffer
	encoder := json.NewEncoder(&written)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	// Written beside it and moved over it, so a tool reading the file at
	// that moment never sees half of it; a name of its own, so two
	// installs at once do not write the same temporary file.
	temporary, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".*.teanode")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if _, err := temporary.Write(written.Bytes()); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), file)
}

// shellQuoteWords joins words into a command line a shell reads back as
// the same words.
func shellQuoteWords(words []string) string {
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		if word != "" && strings.Trim(word, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:@") == "" {
			quoted = append(quoted, word)
		} else {
			quoted = append(quoted, "'"+strings.ReplaceAll(word, "'", `'\''`)+"'")
		}
	}
	return strings.Join(quoted, " ")
}

// shellWords splits a command line shellQuoteWords wrote back into its
// words: blanks separate them, and single quotes keep a blank (and the
// '\” that stands for a quote) inside one.
func shellWords(line string) []string {
	var words []string
	var word strings.Builder
	isInWord, isQuoted, isEscaped := false, false, false
	for _, character := range line {
		switch {
		case isEscaped:
			word.WriteRune(character)
			isEscaped = false
		case character == '\'':
			isQuoted, isInWord = !isQuoted, true
		case !isQuoted && character == '\\':
			isEscaped, isInWord = true, true
		case !isQuoted && (character == ' ' || character == '\t'):
			if isInWord {
				words = append(words, word.String())
				word.Reset()
				isInWord = false
			}
		default:
			word.WriteRune(character)
			isInWord = true
		}
	}
	if isInWord {
		words = append(words, word.String())
	}
	return words
}
