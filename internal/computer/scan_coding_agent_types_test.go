package computer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The coding agent types in the registry are read end to end: the
// daemon lists a tool's folder, runs the type's jq program over each file
// and cuts what it prints into chat units. What they keep and what they
// leave out is the point of them, so it is checked against invented
// sessions holding one of every kind of line.

// scanRegistryType reads a folder with one of the registry's types, as a
// source of it would.
func scanRegistryType(t *testing.T, name, folder string, known map[string]string, key string) *ScanResult {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("the coding agent types run jq")
	}
	kind, err := os.ReadFile(filepath.Join("..", "sources", "testdata", "registry", name+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if known == nil {
		known = map[string]string{}
	}
	result, err := RunScan(context.Background(), &Options{Home: filepath.Dir(folder)}, &ScanArguments{
		Format: FormatTyped, SourceType: string(kind), SourceKey: key,
		Settings: map[string]any{"path": folder}, Known: known, KnownID: key + "-" + filepath.Base(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	return result
}

// writeFiles puts each file under root, making its folders.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for relative, content := range files {
		full := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// entriesByKind splits a page into its memory pages and its conversation
// units.
func entriesByKind(result *ScanResult) (pages, units []ScanEntry) {
	for _, entry := range result.Entries {
		if entry.Kind == "chat" {
			units = append(units, entry)
		} else {
			pages = append(pages, entry)
		}
	}
	return pages, units
}

// A Claude Code session as the tool writes it, with one of every kind of
// line the reader leaves out beside the two it keeps.
var claudeCodeSession = strings.Join([]string{
	`{"type":"permission-mode","permissionMode":"default"}`,
	`{"type":"user","uuid":"u1","timestamp":"2026-08-14T09:00:00Z","cwd":"/work/garden-app","gitBranch":"retry-upload","message":{"role":"user","content":"Add a retry to the upload step."}}`,
	`{"type":"assistant","uuid":"a1","timestamp":"2026-08-14T09:00:05Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"THINKING the upload helper is in one file"},{"type":"text","text":"I will wrap the upload in a retry with backoff."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"TOOLCALL upload.go"}}]}}`,
	`{"type":"user","uuid":"u2","timestamp":"2026-08-14T09:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"TOOLRESULT package upload"}]}}`,
	`{"type":"user","uuid":"u3","timestamp":"2026-08-14T09:00:07Z","isMeta":true,"message":{"role":"user","content":"METALINE expanded skill text"}}`,
	`{"type":"user","uuid":"u4","timestamp":"2026-08-14T09:00:08Z","message":{"role":"user","content":"<command-name>/clear</command-name><command-args></command-args>"}}`,
	`{"type":"assistant","uuid":"s1","timestamp":"2026-08-14T09:00:09Z","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"SIDECHAIN a subagent's answer"}]}}`,
	`{"type":"user","uuid":"c1","timestamp":"2026-08-14T09:00:10Z","isCompactSummary":true,"message":{"role":"user","content":"COMPACTSUMMARY of the session so far"}}`,
	`{"type":"system","uuid":"y1","timestamp":"2026-08-14T09:00:11Z","content":"SYSTEMLINE hook ran"}`,
	`{"type":"ai-title","aiTitle":"Retrying failed uploads","sessionId":"session-one"}`,
	`{"type":"custom-title","customTitle":"Upload retries","sessionId":"session-one"}`,
	`{"type":"pr-link","prUrl":"https://example.com/garden-app/pull/7","sessionId":"session-one"}`,
	`{"type":"attachment","uuid":"q1","timestamp":"2026-08-14T09:02:00Z","attachment":{"type":"queued_command","commandMode":"prompt","origin":{"kind":"human"},"prompt":"Also log each retry, please."}}`,
	`{"type":"attachment","uuid":"q2","timestamp":"2026-08-14T09:02:10Z","attachment":{"type":"queued_command","commandMode":"task-notification","prompt":"TASKNOTICE the build finished"}}`,
	`{"type":"attachment","uuid":"q3","timestamp":"2026-08-14T09:02:20Z","attachment":{"type":"queued_command","commandMode":"prompt","origin":{"kind":"peer","from":"another-session"},"prompt":"PEERMESSAGE from another session"}}`,
	`{"type":"attachment","uuid":"q4","timestamp":"2026-08-14T09:02:30Z","attachment":{"type":"queued_command","commandMode":"prompt","origin":{"kind":"auto-continuation"},"prompt":"AUTOCONTINUE keep going"}}`,
	`{"type":"attachment","uuid":"q5","timestamp":"2026-08-14T09:02:40Z","attachment":{"type":"hook_success","content":"HOOKOUTPUT ok"}}`,
	`{"type":"assistant","uuid":"a2","timestamp":"2026-08-14T09:03:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Done: three tries, doubling the wait."}]}}`,
	`{"type":"user","uuid":"u5","timestamp":"2026-08-14T09:04:00Z","message":{"role":"user","content":[{"type":"text","text":"<system-reminder>REMINDER injected</system-reminder>Thanks, ship it."},{"type":"image","source":{"type":"base64","data":"IMAGEDATA"}}]}}`,
}, "\n") + "\n"

// A Codex session as the tool writes it: its own context put into the
// person's turn, the developer's instructions, reasoning and tool traffic
// beside what was said.
var codexSession = strings.Join([]string{
	`{"timestamp":"2026-08-15T10:00:00Z","type":"session_meta","payload":{"id":"session-three","cwd":"/work/garden-app","git":{"branch":"faster-build"}}}`,
	`{"timestamp":"2026-08-15T10:00:00Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"DEVELOPER sandbox rules"}]}}`,
	`{"timestamp":"2026-08-15T10:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions for /work/garden-app\n\nINSTRUCTIONS from the repository"}]}}`,
	`{"timestamp":"2026-08-15T10:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>ENVIRONMENT shell zsh</environment_context>"}]}}`,
	`{"timestamp":"2026-08-15T10:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Why is the build slow?"}]}}`,
	`{"timestamp":"2026-08-15T10:00:02Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"REASONING look at the cache"}]}}`,
	`{"timestamp":"2026-08-15T10:00:03Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"FUNCTIONCALL ls"}}`,
	`{"timestamp":"2026-08-15T10:00:04Z","type":"response_item","payload":{"type":"function_call_output","output":"FUNCTIONOUTPUT build.log"}}`,
	`{"timestamp":"2026-08-15T10:00:05Z","type":"event_msg","payload":{"type":"agent_message","message":"EVENTMESSAGE duplicate of the answer"}}`,
	`{"timestamp":"2026-08-15T10:00:06Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"The cache is cleared on every run."}]}}`,
}, "\n") + "\n"

func TestTheClaudeCodeTypeKeepsOnlyWhatWasSaid(t *testing.T) {
	folder := filepath.Join(t.TempDir(), ".claude")
	writeFiles(t, folder, map[string]string{
		"CLAUDE.md": "Prefer small commits.\n",
		"projects/-work-garden-app/memory/testing.md":                 "Run the tests before pushing.\n",
		"projects/-work-garden-app/session-one.jsonl":                 claudeCodeSession,
		"projects/-work-garden-app/session-one/subagents/agent.jsonl": `{"type":"assistant","uuid":"x1","timestamp":"2026-08-14T09:00:00Z","message":{"content":[{"type":"text","text":"SUBAGENTFILE"}]}}`,
		// Titled only by the tool.
		"projects/-work-garden-app/session-two.jsonl": `{"type":"user","uuid":"v1","timestamp":"2026-08-16T09:00:00Z","cwd":"/work/garden-app","message":{"role":"user","content":"Rename the config file."}}` + "\n" +
			`{"type":"ai-title","aiTitle":"Renaming the config file","sessionId":"session-two"}` + "\n",
		"settings.json": `{"theme":"dark"}`,
	})
	pages, units := entriesByKind(scanRegistryType(t, "claude-code", folder, nil, "claude01"))
	if len(units) != 2 {
		t.Fatalf("two conversations, one unit each: %+v", units)
	}
	var unit ScanEntry
	for _, each := range units {
		if strings.HasPrefix(each.ExternalID, "projects/-work-garden-app/session-two.jsonl#") {
			if each.Metadata["channel"] != "Renaming the config file" {
				t.Fatalf("a session the person did not title is named by the tool's title: %+v", each.Metadata)
			}
		} else {
			unit = each
		}
	}
	if len(pages) != 2 {
		t.Fatalf("the standing instructions and the project memory are pages: %+v", pages)
	}
	for _, page := range pages {
		if !strings.HasPrefix(page.Title, "Claude Code memory: ") {
			t.Fatalf("a memory page says what it is: %+v", page)
		}
	}
	for _, said := range []string{"Add a retry to the upload step.", "I will wrap the upload in a retry with backoff.", "Also log each retry, please.", "Done: three tries, doubling the wait.", "Thanks, ship it."} {
		if !strings.Contains(unit.Text, said) {
			t.Fatalf("what was said is kept (%q): %q", said, unit.Text)
		}
	}
	for _, dropped := range []string{"THINKING", "TOOLCALL", "TOOLRESULT", "METALINE", "/clear", "SIDECHAIN", "COMPACTSUMMARY", "SYSTEMLINE", "REMINDER", "IMAGEDATA", "SUBAGENTFILE", "TASKNOTICE", "PEERMESSAGE", "AUTOCONTINUE", "HOOKOUTPUT"} {
		if strings.Contains(unit.Text, dropped) {
			t.Fatalf("%s is left out: %q", dropped, unit.Text)
		}
	}
	if !strings.Contains(unit.Text, PersonAuthor+": Add a retry") || !strings.Contains(unit.Text, PersonAuthor+": Also log each retry") || !strings.Contains(unit.Text, "Claude Code: Done") {
		t.Fatalf("with who said what: %q", unit.Text)
	}
	participants, _ := unit.Metadata["participants"].([]string)
	if len(participants) != 2 || participants[0] != PersonAuthor {
		t.Fatalf("the person is among who was in it, for the server to name: %+v", unit.Metadata)
	}
	if unit.Metadata["channel"] != "Upload retries" || unit.Metadata["branch"] != "retry-upload" || unit.Metadata["directory"] != "/work/garden-app" {
		t.Fatalf("the title, branch and directory go with it: %+v", unit.Metadata)
	}
	if unit.Metadata["pullRequests"] != "https://example.com/garden-app/pull/7" {
		t.Fatalf("and the pull request it opened: %+v", unit.Metadata)
	}
}

// A session grows while it is read: the unit already sent is sent as
// unchanged, even though the session was retitled since, and only the new
// talk is sent in full.
func TestTheClaudeCodeTypeSendsOnlyWhatASessionAdded(t *testing.T) {
	folder := filepath.Join(t.TempDir(), ".claude")
	session := "projects/-work-garden-app/session-one.jsonl"
	writeFiles(t, folder, map[string]string{session: claudeCodeSession})
	_, first := entriesByKind(scanRegistryType(t, "claude-code", folder, nil, "claude02"))
	if len(first) != 1 {
		t.Fatalf("one unit to start with: %+v", first)
	}
	grown := claudeCodeSession + strings.Join([]string{
		`{"type":"custom-title","customTitle":"Upload retries and timeouts","sessionId":"session-one"}`,
		`{"type":"user","uuid":"u6","timestamp":"2026-08-14T13:00:00Z","message":{"role":"user","content":"Now add a timeout as well."}}`,
		`{"type":"assistant","uuid":"a3","timestamp":"2026-08-14T13:00:30Z","message":{"role":"assistant","content":[{"type":"text","text":"Thirty seconds, set from the config."}]}}`,
	}, "\n") + "\n"
	writeFiles(t, folder, map[string]string{session: grown})
	_, second := entriesByKind(scanRegistryType(t, "claude-code", folder, map[string]string{first[0].ExternalID: first[0].Hash}, "claude02"))
	if len(second) != 2 {
		t.Fatalf("the old unit and one after the silence: %+v", second)
	}
	for _, unit := range second {
		isOld := unit.ExternalID == first[0].ExternalID
		if isOld != unit.Unchanged {
			t.Fatalf("only the unit sent before is unchanged: %+v", unit)
		}
		if !isOld && !strings.Contains(unit.Text, "Now add a timeout as well.") {
			t.Fatalf("the new unit is the new talk: %q", unit.Text)
		}
	}
}

func TestTheCodexTypeKeepsOnlyWhatWasSaid(t *testing.T) {
	folder := filepath.Join(t.TempDir(), ".codex")
	writeFiles(t, folder, map[string]string{
		"AGENTS.md":                   "Answer briefly.\n",
		"memories/topics/building.md": "The build uses a remote cache.\n",
		"sessions/2026/08/15/rollout-2026-08-15T10-00-00-session-three.jsonl": codexSession,
		"sessions/2026/08/15/rollout-2026-08-15T10-05-00-session-four.jsonl": `{"timestamp":"2026-08-15T10:05:00Z","type":"session_meta","payload":{"id":"session-four","cwd":"/work/garden-app","source":{"subagent":"review"}}}` + "\n" +
			`{"timestamp":"2026-08-15T10:05:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"SUBAGENTTASK read the log"}]}}` + "\n",
		"sessions/2026/08/15/rollout-2026-08-15T10-06-00-session-five.jsonl": `{"timestamp":"2026-08-15T10:06:00Z","type":"session_meta","payload":{"id":"session-five","cwd":"/work/garden-app","source":"exec"}}` + "\n" +
			`{"timestamp":"2026-08-15T10:06:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"SCRIPTED summarize the log"}]}}` + "\n",
		// Never named.
		"sessions/2026/08/16/rollout-2026-08-16T09-00-00-session-six.jsonl": `{"timestamp":"2026-08-16T09:00:00Z","type":"session_meta","payload":{"id":"session-six","cwd":"/work/garden-app","source":"cli"}}` + "\n" +
			`{"timestamp":"2026-08-16T09:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Add a  changelog\nentry for the cache fix and nothing else, please, before the release goes out"}]}}` + "\n",
		"config.toml": "model = \"invented\"\n",
		// Named, then renamed: the later line is the name.
		"session_index.jsonl": `{"id":"session-three","thread_name":"Slow build","updated_at":"2026-08-15T10:00:00Z"}` + "\n" +
			`{"id":"session-three","thread_name":"Why the build is slow","updated_at":"2026-08-15T11:00:00Z"}` + "\n",
	})
	pages, units := entriesByKind(scanRegistryType(t, "codex", folder, nil, "codex01"))
	if len(pages) != 2 {
		t.Fatalf("AGENTS.md and the memory file are pages: %+v", pages)
	}
	if len(units) != 2 {
		t.Fatalf("two conversations; a subagent's session and a script's are not: %+v", units)
	}
	var unit ScanEntry
	for _, each := range units {
		if strings.Contains(each.ExternalID, "session-six") {
			if each.Metadata["channel"] != "Add a changelog entry for the cache fix and nothing else, pl…" {
				t.Fatalf("an unnamed session is named by its first words, cut to a line: %+v", each.Metadata)
			}
		} else {
			unit = each
		}
	}
	for _, said := range []string{"Why is the build slow?", "The cache is cleared on every run."} {
		if !strings.Contains(unit.Text, said) {
			t.Fatalf("what was said is kept (%q): %q", said, unit.Text)
		}
	}
	for _, dropped := range []string{"DEVELOPER", "INSTRUCTIONS", "ENVIRONMENT", "REASONING", "FUNCTIONCALL", "FUNCTIONOUTPUT", "EVENTMESSAGE", "SUBAGENTTASK", "SCRIPTED"} {
		if strings.Contains(unit.Text, dropped) {
			t.Fatalf("%s is left out: %q", dropped, unit.Text)
		}
	}
	if unit.Metadata["channel"] != "Why the build is slow" || unit.Metadata["branch"] != "faster-build" {
		t.Fatalf("a session is named by its latest name: %+v", unit.Metadata)
	}
}
