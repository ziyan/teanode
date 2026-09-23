package computer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A typed source is read end to end by running its type's commands here,
// through the records reader, and its state is kept under the person's
// cache rather than anywhere a request names.
func TestATypedSourceIsReadByRunningItsType(t *testing.T) {
	home := t.TempDir()
	kind := `---
name: printed
description: records a command prints
containers:
  - command: [printf, '{"name":"alpha"}\n{"name":"beta"}\n']
    parse: jsonl
    name: "{{item.name}}.jsonl"
    fields: {label: "{{item.name}}"}
records:
  - command: [printf, '{"id":"one","text":"hello from %s"}\n', "{{container.label}}"]
    parse: jsonl
    record: {id: "{{item.id}}", text: "{{item.text}}", kind: page, title: "{{container.label}} one"}
---
For people.
`
	arguments := &ScanArguments{
		Root: "/somewhere/else", Format: FormatTyped, SourceType: kind, SourceKey: "source01",
		Known: map[string]string{}, KnownID: "pass-1",
	}
	result, err := RunScan(context.Background(), &Options{Home: home}, arguments)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	var found []string
	for _, entry := range result.Entries {
		found = append(found, entry.ExternalID+"="+entry.Text)
	}
	if strings.Join(found, ",") != "alpha.jsonl#one=hello from alpha,beta.jsonl#one=hello from beta" {
		t.Fatalf("the entries were %v", found)
	}
	if _, err := os.Stat(filepath.Join(home, ".cache", "teanode", "sources", "source01", "containers.json")); err != nil {
		t.Errorf("the listing is kept under the person's cache: %s", err)
	}
	if _, err := os.Stat("/somewhere/else"); err == nil {
		t.Errorf("the root the request named was used")
	}

	// A later page reads what the first page listed.
	arguments.After = "beta.jsonl"
	arguments.Known = nil
	result, err = RunScan(context.Background(), &Options{Home: home}, arguments)
	if err != nil || len(result.Entries) != 1 || result.Entries[0].ExternalID != "beta.jsonl#one" {
		t.Errorf("the second page: %+v, %v", result, err)
	}
}

// A type that needs a tool this computer lacks says so, rather than
// failing on whatever it ran first.
func TestATypedSourceSaysWhatToolIsMissing(t *testing.T) {
	kind := "---\nname: missing\ndescription: a tool nobody has\nrequires: [no-such-tool-anywhere]\ncontainers:\n  - {command: [no-such-tool-anywhere], parse: jsonl, name: x}\nrecords:\n  - {command: [no-such-tool-anywhere], parse: jsonl, record: {id: \"{{item.id}}\"}}\n---\n"
	_, err := RunScan(context.Background(), &Options{Home: t.TempDir()}, &ScanArguments{
		Format: FormatTyped, SourceType: kind, SourceKey: "source02", Known: map[string]string{}, KnownID: "pass-1",
	})
	if err == nil || !strings.Contains(err.Error(), "no-such-tool-anywhere, which is not installed") {
		t.Errorf("the error was %v", err)
	}
}
