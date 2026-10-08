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
	hookScriptPath     = filepath.Join(".local", "share", "teanode", "teanode-herdr-hook")
	hookEventsPath     = filepath.Join(".local", "state", "teanode", "herdr-events.jsonl")
	claudeSettingsPath = filepath.Join(".claude", "settings.json")
	hookBackupSuffix   = ".before-teanode"
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
	// without another. A turn reports at every tool call; one the person
	// stopped with Esc, or one killed, never says it stopped, and after
	// this the screen and herdr decide again.
	hookWorkingFresh = 2 * time.Minute
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

// hookScript keeps only the event, the session and the time: what a hook
// is given holds the person's prompts and their tools' output, which is
// nobody's business here. The line is written in one write, so lines from
// sessions reporting at once do not interleave, to a file only the person
// can read.
const hookScript = `#!/bin/sh
# TeaNode's herdr hook: it reports what Claude Code does, and decides
# nothing. Installed by "teanode computer herdr setup".
umask 077
directory="$HOME/.local/state/teanode"
mkdir -p "$directory" 2>/dev/null
session=$(tr -d '\n' | sed -n 's/.*"session_id" *: *"\([A-Za-z0-9-]*\)".*/\1/p')
line=$(printf '{"hookEventName":"%s","sessionId":"%s","reportedAt":%s}' "$1" "$session" "$(date +%s)")
printf '%s\n' "$line" >> "$directory/herdr-events.jsonl" 2>/dev/null
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
	SessionID     string `json:"sessionId"`
	ReportedAt    int64  `json:"reportedAt"`
}

// readHookReports is the latest report of each session, read from the end
// of the events file once for every pane; empty when the hooks are not
// installed.
func readHookReports(home string) map[string]*hookReport {
	latest := map[string]*hookReport{}
	file, err := os.Open(filepath.Join(home, hookEventsPath))
	if err != nil {
		return latest
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return latest
	}
	lines, _, err := tailLines(file, info.Size(), hookEventsReadBytes)
	if err != nil {
		return latest
	}
	for _, line := range lines {
		var report hookReport
		if json.Unmarshal(line, &report) != nil || report.SessionID == "" {
			continue
		}
		latest[report.SessionID] = &report
	}
	return latest
}

// hookStateOf is what a session's latest report says.
func hookStateOf(reports map[string]*hookReport, codingSessionId string, now time.Time) string {
	report := reports[codingSessionId]
	if codingSessionId == "" || report == nil {
		return hookStateNone
	}
	switch report.HookEventName {
	case "Stop":
		return hookStateIdle
	case "UserPromptSubmit", "PreToolUse", "PostToolUse":
		if now.Sub(time.Unix(report.ReportedAt, 0)) > hookWorkingFresh {
			return hookStateNone
		}
		return hookStateWorking
	}
	// A permission prompt or a notification says the screen has a form,
	// which the screen says better.
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
	// Into a file beside it, then over it: a hook that appends meanwhile
	// loses its line, which the next one makes up for, rather than the
	// file being left half written.
	_ = writeFileAtomically(path, bytes.Join(lines, []byte("\n")))
}

// setUpHooks puts TeaNode's hooks into Claude Code's settings, or takes
// them out, leaving every other entry as it was.
func setUpHooks(home string, isRemoval bool) (*HerdrSetupResult, error) {
	scriptPath := filepath.Join(home, hookScriptPath)
	settingsPath := filepath.Join(home, claudeSettingsPath)
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
		var groups []json.RawMessage
		if raw, ok := hooks.values[eventName]; ok {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return nil, fmt.Errorf("the %s hooks in %s are not JSON this can change safely: %w", eventName, settingsPath, err)
			}
		}
		groups, err = withoutOurHooks(groups, scriptPath)
		if err != nil {
			return nil, fmt.Errorf("the %s hooks in %s are not JSON this can change safely: %w", eventName, settingsPath, err)
		}
		if !isRemoval {
			ours, err := marshalPlain(map[string]any{"hooks": []any{map[string]any{
				"type": "command", "command": "sh '" + scriptPath + "' " + eventName, "timeout": 5,
			}}})
			if err != nil {
				return nil, err
			}
			groups = append(groups, ours)
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
// and any group that held only TeaNode's dropped. A group with none of
// TeaNode's is kept as it was written.
func withoutOurHooks(groups []json.RawMessage, scriptPath string) ([]json.RawMessage, error) {
	kept := []json.RawMessage{}
	for _, raw := range groups {
		group := &orderedObject{values: map[string]json.RawMessage{}}
		if err := group.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		var listed []json.RawMessage
		if rawHooks, ok := group.values["hooks"]; ok {
			if err := json.Unmarshal(rawHooks, &listed); err != nil {
				return nil, err
			}
		}
		others := []json.RawMessage{}
		for _, hook := range listed {
			var entry struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(hook, &entry) == nil && strings.Contains(entry.Command, scriptPath) {
				continue
			}
			others = append(others, hook)
		}
		switch {
		case len(others) == len(listed):
			kept = append(kept, raw)
			continue
		case len(others) == 0:
			continue
		}
		encoded, err := marshalPlain(others)
		if err != nil {
			return nil, err
		}
		group.set("hooks", encoded)
		changed, err := group.MarshalJSON()
		if err != nil {
			return nil, err
		}
		kept = append(kept, changed)
	}
	return kept, nil
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
