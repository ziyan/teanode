package computer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TeaNode's own hooks in Claude Code, which report what a session is doing
// as it does it.
//
// They only report: the script appends the hook's input to a file and exits
// at once, printing nothing, so Claude Code carries on as if it were not
// there. What it reports is read here before anything else when the state
// of a session is decided. They are put in only when the person asks
// (setup), beside whatever is there already, and taken out again the same
// way (setup with removal); before the settings are first changed, a copy
// is kept beside them.
//
// Codex has none: its history file says when a turn starts and ends and
// what it asked, as it happens, and each hook there must be trusted by its
// hash besides.

// The files the hooks use, under the person's home directory.
var (
	hookScriptPath   = filepath.Join(".local", "share", "teanode", "teanode-herdr-hook")
	hookEventsPath   = filepath.Join(".local", "state", "teanode", "herdr-events.jsonl")
	claudeSettings   = filepath.Join(".claude", "settings.json")
	hookBackupSuffix = ".before-teanode"
)

// The bounds of the events file.
const (
	// hookEventsReadBytes is how much of its end is read each look: the
	// latest event of every live session is in it.
	hookEventsReadBytes = 256 << 10
	// hookEventsMostBytes is how large it grows before it is cut to its
	// end.
	hookEventsMostBytes = 4 << 20
	// hookWorkingFresh is how long a report that a turn runs is believed
	// without another: a session killed in the middle of a turn never says
	// it stopped.
	hookWorkingFresh = 30 * time.Minute
)

// hookEventNames are the events the hooks report, and the arguments the
// script is given for each.
var hookEventNames = []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "Notification", "Stop"}

// What a session's latest report says.
const (
	hookStateNone    = ""
	hookStateWorking = "working"
	hookStateIdle    = "idle"
)

const hookScript = `#!/bin/sh
# TeaNode's herdr hook: it reports what Claude Code does, and decides
# nothing. Installed by "teanode computer herdr setup".
directory="$HOME/.local/state/teanode"
mkdir -p "$directory" 2>/dev/null
{ printf '{"hookEventName":"%s","paneId":"%s","reportedAt":%s,"input":' "$1" "${HERDR_PANE_ID:-}" "$(date +%s)"; tr -d '\n'; printf '}\n'; } >> "$directory/herdr-events.jsonl" 2>/dev/null
exit 0
`

// HerdrSetupResult says what setup did.
type HerdrSetupResult struct {
	IsInstalled    bool     `json:"isInstalled"`
	SettingsPath   string   `json:"settingsPath"`
	ScriptPath     string   `json:"scriptPath"`
	BackupPath     string   `json:"backupPath,omitempty"`
	HookEventNames []string `json:"hookEventNames"`
}

// hookReport is one line of the events file.
type hookReport struct {
	HookEventName string `json:"hookEventName"`
	ReportedAt    int64  `json:"reportedAt"`
	Input         struct {
		SessionID string `json:"session_id"`
		ToolName  string `json:"tool_name"`
	} `json:"input"`
}

// readHookState is what the latest report of a session says, if the hooks
// are installed and it reported anything.
func readHookState(home, codingSessionId string) string {
	if codingSessionId == "" {
		return hookStateNone
	}
	file, err := os.Open(filepath.Join(home, hookEventsPath))
	if err != nil {
		return hookStateNone
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return hookStateNone
	}
	lines, _, err := tailLines(file, info.Size(), hookEventsReadBytes)
	if err != nil {
		return hookStateNone
	}
	for index := len(lines) - 1; index >= 0; index-- {
		var report hookReport
		if json.Unmarshal(lines[index], &report) != nil || report.Input.SessionID != codingSessionId {
			continue
		}
		switch report.HookEventName {
		case "Stop":
			return hookStateIdle
		case "UserPromptSubmit", "PreToolUse", "PostToolUse":
			if time.Since(time.Unix(report.ReportedAt, 0)) > hookWorkingFresh {
				return hookStateNone
			}
			return hookStateWorking
		}
		// A permission prompt or a notification says the screen has a
		// form, which the screen says better.
		return hookStateNone
	}
	return hookStateNone
}

// trimHookEvents cuts the events file to its end once it has grown large.
func trimHookEvents(home string) {
	path := filepath.Join(home, hookEventsPath)
	info, err := os.Stat(path)
	if err != nil || info.Size() < hookEventsMostBytes {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	lines, _, err := tailLines(file, info.Size(), hookEventsReadBytes)
	_ = file.Close()
	if err != nil {
		return
	}
	_ = os.WriteFile(path, bytes.Join(lines, []byte("\n")), 0o600)
}

// setUpHooks puts TeaNode's hooks into Claude Code's settings, or takes
// them out, leaving every other entry as it was.
func setUpHooks(home string, isRemoval bool) (*HerdrSetupResult, error) {
	scriptPath := filepath.Join(home, hookScriptPath)
	settingsPath := filepath.Join(home, claudeSettings)
	result := &HerdrSetupResult{SettingsPath: settingsPath, ScriptPath: scriptPath, HookEventNames: []string{}}

	settings := &orderedObject{values: map[string]json.RawMessage{}}
	original, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		if err := settings.UnmarshalJSON(original); err != nil {
			return nil, fmt.Errorf("%s is not JSON this can change safely: %w", settingsPath, err)
		}
	case errors.Is(err, os.ErrNotExist):
		if isRemoval {
			return result, nil
		}
	default:
		return nil, err
	}
	hooks := &orderedObject{values: map[string]json.RawMessage{}}
	if raw, ok := settings.values["hooks"]; ok {
		if err := hooks.UnmarshalJSON(raw); err != nil {
			return nil, fmt.Errorf("the hooks in %s are not JSON this can change safely: %w", settingsPath, err)
		}
	}
	for _, eventName := range hookEventNames {
		var groups []map[string]any
		if raw, ok := hooks.values[eventName]; ok {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return nil, fmt.Errorf("the %s hooks in %s are not JSON this can change safely: %w", eventName, settingsPath, err)
			}
		}
		groups = withoutOurHooks(groups, scriptPath)
		if !isRemoval {
			groups = append(groups, map[string]any{"hooks": []any{map[string]any{
				"type": "command", "command": "sh '" + scriptPath + "' " + eventName, "timeout": 5,
			}}})
			result.HookEventNames = append(result.HookEventNames, eventName)
		}
		if len(groups) == 0 {
			hooks.remove(eventName)
			continue
		}
		raw, err := marshalPlain(groups)
		if err != nil {
			return nil, err
		}
		hooks.set(eventName, raw)
	}
	if len(hooks.keys) == 0 {
		settings.remove("hooks")
	} else {
		raw, err := hooks.MarshalJSON()
		if err != nil {
			return nil, err
		}
		settings.set("hooks", raw)
	}
	changed, err := settings.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, changed, "", "  "); err != nil {
		return nil, err
	}
	indented.WriteByte('\n')

	if !isRemoval {
		if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(scriptPath, []byte(hookScript), 0o700); err != nil {
			return nil, err
		}
	}
	if original != nil && !bytes.Equal(original, indented.Bytes()) {
		backupPath := settingsPath + hookBackupSuffix
		if _, err := os.Stat(backupPath); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(backupPath, original, 0o600); err != nil {
				return nil, err
			}
			result.BackupPath = backupPath
		}
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		return nil, err
	}
	if err := writeFileAtomically(settingsPath, indented.Bytes()); err != nil {
		return nil, err
	}
	if isRemoval {
		_ = os.Remove(scriptPath)
	}
	result.IsInstalled = !isRemoval
	return result, nil
}

// marshalPlain is JSON as written, without the escaping of <, > and & that
// is meant for HTML and would change a command line's look in the file.
func marshalPlain(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// withoutOurHooks is a list of hook groups with TeaNode's own taken out,
// and any group that held only TeaNode's dropped.
func withoutOurHooks(groups []map[string]any, scriptPath string) []map[string]any {
	kept := []map[string]any{}
	for _, group := range groups {
		listed, _ := group["hooks"].([]any)
		var others []any
		for _, hook := range listed {
			entry, _ := hook.(map[string]any)
			command, _ := entry["command"].(string)
			if strings.Contains(command, scriptPath) {
				continue
			}
			others = append(others, hook)
		}
		if len(others) == 0 && len(listed) > 0 {
			continue
		}
		group["hooks"] = others
		kept = append(kept, group)
	}
	return kept
}

// writeFileAtomically writes a file whole or not at all, keeping its mode.
func writeFileAtomically(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if _, err := temporary.Write(data); err != nil {
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
	return os.Rename(temporary.Name(), path)
}

// orderedObject is a JSON object whose keys keep their order, so a
// person's settings file reads as they wrote it after a change.
type orderedObject struct {
	keys   []string
	values map[string]json.RawMessage
}

func (self *orderedObject) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return errors.New("not an object")
	}
	self.keys, self.values = nil, map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, _ := token.(string)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		self.set(key, value)
	}
	if _, err := decoder.Token(); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func (self *orderedObject) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, key := range self.keys {
		if index > 0 {
			buffer.WriteByte(',')
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buffer.Write(encoded)
		buffer.WriteByte(':')
		buffer.Write(self.values[key])
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func (self *orderedObject) set(key string, value json.RawMessage) {
	if _, ok := self.values[key]; !ok {
		self.keys = append(self.keys, key)
	}
	self.values[key] = value
}

func (self *orderedObject) remove(key string) {
	if _, ok := self.values[key]; !ok {
		return
	}
	delete(self.values, key)
	for index, each := range self.keys {
		if each == key {
			self.keys = append(self.keys[:index], self.keys[index+1:]...)
			break
		}
	}
}
