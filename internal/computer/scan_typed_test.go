package computer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/sources"
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

// What a source's directory learned under one set of settings is
// forgotten under another, and kept while they stay the same.
func TestATypedSourceForgetsWhatOtherSettingsLearned(t *testing.T) {
	root := t.TempDir()
	write := func() {
		for _, name := range []string{"containers.json", "since.json", "store/one.jsonl", "files/kept/file.txt"} {
			path := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	write()
	if err := forgetOtherSettings(root, "name: invented-mail", map[string]any{"account": "someone@example.com"}); err != nil {
		t.Fatal(err)
	}
	if !exists("containers.json") || !exists("store/one.jsonl") {
		t.Fatalf("a directory from before settings were written down was cleared")
	}
	if err := forgetOtherSettings(root, "name: invented-mail", map[string]any{"account": "someone@example.com"}); err != nil {
		t.Fatal(err)
	}
	if !exists("since.json") {
		t.Fatalf("the same settings cleared the directory")
	}
	if err := forgetOtherSettings(root, "name: invented-mail", map[string]any{"account": "another@example.com"}); err != nil {
		t.Fatal(err)
	}
	if exists("containers.json") || exists("since.json") || exists("store") {
		t.Fatalf("other settings kept what the first ones learned")
	}
	if !exists("files/kept/file.txt") {
		t.Fatalf("fetched files were thrown away")
	}
}

// A type's request follows a redirect only on its own host, so the
// credential it carries never reaches another, and a failure does not
// repeat the address, which may say more than the host.
func TestATypedRequestKeepsItsCredentialOnItsHost(t *testing.T) {
	reached := false
	elsewhere := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		reached = request.Header.Get("X-Api-Key") != ""
	}))
	defer elsewhere.Close()
	here := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, elsewhere.URL+"/steal", http.StatusFound)
	}))
	defer here.Close()
	// Two test servers share 127.0.0.1 and differ by port, which is a
	// different host all the same.
	_, _, err := DoRequest(context.Background(), &sources.PreparedRequest{Method: "GET", URL: here.URL + "/items?page=secret-looking", Headers: map[string]string{"X-Api-Key": "the-key"}})
	if err == nil || reached {
		t.Fatalf("the redirect was followed with the key: %v, reached %v", err, reached)
	}
	if strings.Contains(err.Error(), "secret-looking") {
		t.Fatalf("the failure repeats the address: %s", err)
	}
}
