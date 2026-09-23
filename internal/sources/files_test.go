package sources

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// A chat archive a tool keeps on disk: brought up to date first, listed
// by its files, read a file at a time, with people and stored files
// looked up by identifier, and a channel that is mostly an integration
// left out.
func TestAnArchiveIsReadThroughItsFiles(t *testing.T) {
	archive := t.TempDir()
	writeTree(t, archive, map[string]string{
		"users.json":          `[{"id": "u1", "username": "river"}, {"id": "u2", "username": "stone"}]`,
		"channels.json":       `[{"id": "c1", "type": "O", "purpose": "planning"}, {"id": "c2", "type": "D"}]`,
		"files/f1__notes.txt": "the notes",
		"posts/garden/planning.jsonl": `{"id": "p1", "channel_id": "c1", "user_id": "u1", "create_at": 1700000000123, "message": "first", "reply_count": 1, "file_ids": ["f1", "gone"]}
{"id": "p2", "channel_id": "c1", "user_id": "u2", "create_at": 1700000001000, "root_id": "p1", "reply_count": 1, "message": "a reply"}
{"id": "p3", "channel_id": "c1", "user_id": "u1", "create_at": 1700000002000, "type": "system_join_channel", "message": "joined"}
{"id": "p4", "channel_id": "c1", "user_id": "u9", "create_at": 1700000003000, "message": "   "}
`,
		"posts/direct/stone.jsonl": `{"id": "d1", "channel_id": "c2", "user_id": "u9", "create_at": 1700000000000, "message": "hello"}
`,
		"posts/garden/alerts.jsonl": `{"id": "a1", "channel_id": "c1", "user_id": "u1", "type": "slack_attachment", "message": "alert"}
{"id": "a2", "channel_id": "c1", "user_id": "u1", "type": "slack_attachment", "message": "alert"}
{"id": "a3", "channel_id": "c1", "user_id": "u1", "message": "anybody?"}
`,
		".hidden/posts/x/y.jsonl": "",
	})
	kind := mustParse(t, `
name: chat-archive
description: an invented chat archive
settings:
  - {name: archive, type: path, default: ""}
  - {name: sync, type: boolean, default: true}
refresh:
  - when: "{{settings.sync}}"
    command: [chat, sync, "{{settings.archive}}"]
lookups:
  users: {file: "{{settings.archive}}/users.json", parse: json, key: "{{item.id}}", value: "{{item.username}}"}
  channels: {file: "{{settings.archive}}/channels.json", parse: json, key: "{{item.id}}"}
  stored: {files: {in: "{{settings.archive}}/files", match: "*"}, key: "{{item.name | before '__'}}", value: "{{item.name}}"}
  missing: {file: "{{settings.archive}}/nothing.json", parse: json, key: "{{item.id}}"}
containers:
  - files: {in: "{{settings.archive}}", match: "posts/*/*.jsonl"}
    name: "{{item.path}}"
    fields: {path: "{{item.path}}", team: "{{item.directory | after 'posts/'}}", channel: "{{item.stem}}"}
records:
  - file: "{{settings.archive}}/{{container.path}}"
    parse: jsonl
    dropWhenMostly: {items: "{{item.type}} == slack_attachment", share: 0.5}
    skip: "{{item.type}} matches ^system_ || {{item.type}} == slack_attachment || {{item.message | empty}}"
    record:
      id: "{{item.id}}"
      kind: chat
      channel: "{{container.channel}}"
      thread: "{{item.root_id}}{{item.id | if item.reply_count | unless item.root_id}}"
      at: "{{item.create_at | epoch-ms | local-time}}"
      author: "{{lookup.users[item.user_id] | or 'somebody'}}"
      private: "{{lookup.channels[item.channel_id].type}} in [P, D, G]"
      text: "{{item.message}}"
    metadata:
      team: "{{container.team}}"
      purpose: "{{lookup.channels[item.channel_id].purpose}}"
    attachments:
      each: item.file_ids
      path: "{{settings.archive}}/files/{{lookup.stored[each]}}"
      name: "{{lookup.stored[each] | after '__'}}"
`)
	executor := &fakeExecutor{commands: map[string]string{"chat sync " + archive: ""}}
	runner := &Runner{Type: kind, Settings: map[string]any{"archive": archive}, Executor: executor, State: t.TempDir()}
	containers, err := runner.List(context.Background())
	if err != nil {
		t.Fatalf("List: %s", err)
	}
	if !reflect.DeepEqual(executor.calls, []string{"chat sync " + archive}) {
		t.Fatalf("the refresh ran %q", executor.calls)
	}
	var names []string
	for _, container := range containers {
		names = append(names, container.Name)
	}
	if want := []string{"posts/direct/stone.jsonl", "posts/garden/alerts.jsonl", "posts/garden/planning.jsonl"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("listed %q, want %q", names, want)
	}

	records, err := runner.Read(context.Background(), containers[2])
	if err != nil {
		t.Fatalf("Read: %s", err)
	}
	if got := ids(records); !reflect.DeepEqual(got, []string{"p1", "p2"}) {
		t.Fatalf("read %q", got)
	}
	first, reply := records[0], records[1]
	local := time.UnixMilli(1700000000123).Local().Format("2006-01-02T15:04:05.000-07:00")
	if first["at"] != local || first["author"] != "river" || first["thread"] != "p1" || first["private"] != false || first["channel"] != "planning" {
		t.Fatalf("the first post is %v", first)
	}
	if reply["thread"] != "p1" || reply["author"] != "stone" {
		t.Fatalf("the reply is %v", reply)
	}
	if metadata := first["metadata"].(map[string]any); metadata["team"] != "garden" || metadata["purpose"] != "planning" {
		t.Fatalf("the metadata is %v", metadata)
	}
	attachments, _ := first["attachments"].([]map[string]any)
	if len(attachments) != 1 || attachments[0]["name"] != "notes.txt" || attachments[0]["path"] != filepath.Join(archive, "files", "f1__notes.txt") {
		t.Fatalf("the attachments are %v", first["attachments"])
	}

	direct, err := runner.Read(context.Background(), containers[0])
	if err != nil || len(direct) != 1 || direct[0]["author"] != "somebody" || direct[0]["private"] != true || direct[0]["thread"] != nil {
		t.Fatalf("the direct channel read %v, %v", direct, err)
	}
	alerts, err := runner.Read(context.Background(), containers[1])
	if err != nil || len(alerts) != 0 {
		t.Fatalf("a channel mostly of alerts read %v, %v", alerts, err)
	}

	quiet := &Runner{Type: kind, Settings: map[string]any{"archive": archive, "sync": false}, Executor: &fakeExecutor{}, State: t.TempDir()}
	if _, err := quiet.List(context.Background()); err != nil {
		t.Fatalf("a source that does not sync still lists: %s", err)
	}
}

// An export of Markdown files with a header: each file one record, the
// file in scope beside what it says.
func TestAnExportOfMarkdownFilesIsRead(t *testing.T) {
	export := t.TempDir()
	writeTree(t, export, map[string]string{
		"notes/garden/1-beds.md":   "---\ntitle: \"Beds: where they go\"\nauthor: river\ncreated: 2024-03-01\n---\n\n# Beds\n\nFour of them.\n",
		"notes/garden/2-plain.md":  "No header at all.\n",
		"notes/garden/deep/3.md":   "---\ntitle: deeper\n---\nbelow\n",
		"notes/garden/readme.txt":  "not markdown",
		"notes/orchard/1-trees.md": "---\ntitle: trees\n---\napples\n",
	})
	kind := mustParse(t, `
name: notes-export
description: an invented export of notes
settings:
  - {name: export, type: path, default: ""}
containers:
  - files: {in: "{{settings.export}}", match: "notes/**/*.md"}
    name: "{{item.directory | after 'notes/'}}"
    fields: {project: "{{item.directory | after 'notes/'}}"}
records:
  - files: {in: "{{settings.export}}", match: "notes/{{container.project}}/*.md"}
    parse: markdown
    record:
      id: "{{file.directory}}/{{file.stem}}"
      title: "{{item.title | or file.stem}}"
      author: "{{item.author}}"
      at: "{{item.created}}"
      text: "{{item.body}}"
`)
	runner := &Runner{Type: kind, Settings: map[string]any{"export": export}, Executor: &fakeExecutor{}, State: t.TempDir()}
	containers, err := runner.List(context.Background())
	if err != nil {
		t.Fatalf("List: %s", err)
	}
	if len(containers) != 3 || containers[0].Name != "garden" || len(containers[0].Members) != 1 {
		t.Fatalf("listed %v", containers)
	}
	records, err := runner.Read(context.Background(), containers[0])
	if err != nil {
		t.Fatalf("Read: %s", err)
	}
	if got := ids(records); !reflect.DeepEqual(got, []string{"notes/garden/1-beds", "notes/garden/2-plain"}) {
		t.Fatalf("read %q", got)
	}
	if records[0]["title"] != "Beds: where they go" || records[0]["text"] != "# Beds\n\nFour of them." || records[0]["author"] != "river" {
		t.Fatalf("the first file is %v", records[0])
	}
	if records[1]["title"] != "2-plain" || records[1]["text"] != "No header at all." {
		t.Fatalf("the file with no header is %v", records[1])
	}
}

// A walk that names each place by its path tells two children of one
// name apart, and a detail written to a file is found under the name the
// tool chose for it.
func TestAWalkNamesPlacesByPathAndDetailIsWritten(t *testing.T) {
	kind := mustParse(t, `
name: invented-drive
description: an invented drive
containers:
  - walk:
      start: {id: root, path: Top}
      branch: "{{item.folder}}"
      child: {id: "{{item.id}}"}
      step: "{{item.name | path-step}}"
      distinct: "{{item.id | first 3}}"
    command: [drive, ls, "{{folder.id}}"]
    parse: {json: {items: files}}
    name: "{{folder.path}}"
    fields: {folder: "{{folder.id}}"}
records:
  - command: [drive, ls, "{{container.folder}}"]
    parse: {json: {items: files}}
    skip: "{{item.folder}}"
    detail:
      when: "{{item.kind}} == document"
      command: [drive, export, "{{item.id}}", --out, "{{output}}"]
      text: "{{detail.text | newlines | trim}}"
    record:
      id: "{{item.id}}"
      kind: "{{'page' | if detail.text | or 'file'}}"
      version: "{{item.id}}"
      text: "{{item.name}} — {{item.mime | file-kind item.name}}{{', ' | if item.size}}{{item.size | size}}.\n\n{{detail.text}}"
`)
	executor := &writingExecutor{fakeExecutor: fakeExecutor{commands: map[string]string{
		"drive ls root": `{"files": [{"id": "aaa1", "name": "Work/Old", "folder": true}, {"id": "bbb2", "name": "Work/Old", "folder": true}, {"id": "ccc3", "name": " Trips. ", "folder": true}, {"id": "doc1", "name": "plan", "kind": "document"}, {"id": "pic1", "name": "photo.jpg", "size": "1536"}]}`,
		"drive ls aaa1": `{"files": []}`,
		"drive ls bbb2": `{"files": []}`,
		"drive ls ccc3": `{"files": []}`,
	}}, written: map[string]string{"doc1": "\ufeff line one\r\nline two\r\n"}}
	runner := &Runner{Type: kind, Executor: executor, State: t.TempDir()}
	containers, err := runner.List(context.Background())
	if err != nil {
		t.Fatalf("List: %s", err)
	}
	var names []string
	for _, container := range containers {
		names = append(names, container.Name)
	}
	if want := []string{"Top", "Top/Work-Old (aaa)", "Top/Work-Old (bbb)", "Top/Trips"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("listed %q, want %q", names, want)
	}
	records, err := runner.Read(context.Background(), containers[0])
	if err != nil {
		t.Fatalf("Read: %s", err)
	}
	if len(records) != 2 {
		t.Fatalf("read %v", records)
	}
	if records[0]["kind"] != "page" || records[0]["text"] != "plan — a file.\n\nline one\nline two" {
		t.Fatalf("the document is %q", records[0])
	}
	if records[1]["kind"] != "file" || records[1]["text"] != "photo.jpg — a picture, 1.5 KB.\n\n" {
		t.Fatalf("the picture is %q", records[1])
	}
}

// writingExecutor writes what an export command is told to, under a name
// of its own choosing, as a tool that puts its own extension on does.
type writingExecutor struct {
	fakeExecutor
	written map[string]string
}

func (self *writingExecutor) Command(ctx context.Context, words []string) ([]byte, error) {
	if len(words) == 5 && words[1] == "export" {
		output := strings.TrimSuffix(words[4], filepath.Ext(words[4])) + ".txt"
		return nil, os.WriteFile(output, []byte(self.written[words[2]]), 0o600)
	}
	return self.fakeExecutor.Command(ctx, words)
}

func TestFilters(t *testing.T) {
	scope := Scope{Values: map[string]any{"item": map[string]any{"name": "a/b\\c. ", "count": "0", "size": "3145728"}}}
	for template, want := range map[string]string{
		"{{item.name | path-step}}":          "a-b-c",
		"{{'' | path-step}}":                 "unnamed",
		"{{item.size | size}}":               "3.0 MB",
		"{{item.count | size}}":              "",
		"{{'x' | if item.count}}":            "",
		"{{'x' | unless item.count}}":        "x",
		"{{'one__two__three' | after '__'}}": "two__three",
		"{{'no separator' | after '__'}}":    "",
		"{{'no separator' | before '__'}}":   "no separator",
		"{{'ABC' | lower}}":                  "abc",
		"{{'abcdef' | first 2}}":             "ab",
		"{{'' | file-kind 'report.pdf'}}":    "a PDF",
		"{{'text/csv' | file-kind}}":         "a spreadsheet",
	} {
		parsed, err := compileTemplate(template)
		if err != nil {
			t.Fatalf("%s: %s", template, err)
		}
		got, err := parsed.render(scope)
		if err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", template, got, err, want)
		}
	}
}

func TestSettingsAreChecked(t *testing.T) {
	kind := mustParse(t, `
name: checked
description: an invented type with every kind of setting
settings:
  - {name: path, type: string, pattern: "^[^-].*$"}
  - {name: labels, type: array, items: {type: string, pattern: "^[a-z]+$"}, default: []}
  - {name: bots, type: boolean, default: false}
  - {name: most, type: integer, minimum: 1, maximum: 10, default: 5}
containers:
  - fixed: [{}]
    name: all.jsonl
records:
  - command: [tool, "{{settings.path}}"]
    parse: jsonl
    record: {id: "{{item.id}}"}
`)
	checked, err := kind.CheckSettings(map[string]any{"path": "~/notes", "labels": "red, blue", "bots": "true", "most": float64(3)})
	if err != nil {
		t.Fatalf("CheckSettings: %s", err)
	}
	if want := map[string]any{"path": "~/notes", "labels": []any{"red", "blue"}, "bots": true, "most": 3}; !reflect.DeepEqual(checked, want) {
		t.Fatalf("checked %v, want %v", checked, want)
	}
	for _, refused := range []map[string]any{
		{},
		{"path": "-x"},
		{"path": "a", "colour": "red"},
		{"path": "a", "labels": []any{"Red"}},
		{"path": "a", "most": float64(11)},
		{"path": "a", "most": 2.5},
		{"path": "a", "bots": "perhaps"},
	} {
		if _, err := kind.CheckSettings(refused); err == nil {
			t.Errorf("%v was accepted", refused)
		}
	}
}
