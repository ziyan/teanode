package sources

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// An ordering with nothing on one side holds for nothing, so a time a tool
// left out never makes a reading look unchanged.
func TestAnOrderingWithAnEmptySideDoesNotHold(t *testing.T) {
	scope := Scope{Values: map[string]any{"container": map[string]any{"last": ""}, "pass": map[string]any{"since": "2000-01-01T00:00:00Z"}}}
	for _, text := range []string{"{{container.last}} <= {{pass.since}}", "{{container.last}} > {{pass.since}}"} {
		parsed, err := compileCondition(text)
		if err != nil {
			t.Fatal(err)
		}
		if held, err := parsed.holds(scope); err != nil || held {
			t.Errorf("%s held: %v %v", text, held, err)
		}
	}
}

// A lone quote is refused when the type is read, not a panic when it runs.
func TestALoneQuoteIsRefused(t *testing.T) {
	for _, text := range []string{`{{'}}`, `{{"}}`, `{{item.x | or '}}`} {
		if _, err := compileTemplate(text); err == nil {
			t.Errorf("%s was accepted", text)
		}
	}
}

// A listing whose answer has nothing at the items path fails rather than
// listing nothing, which would make the pass delete every container.
func TestAListingThatDoesNotUnderstandItsAnswerFails(t *testing.T) {
	kind := mustParse(t, `
name: invented-host
description: an invented host
containers:
  - command: [host, repositories]
    parse: {json: {items: repositories}}
    name: "{{item.name}}.jsonl"
records:
  - command: [host, read, "{{container.name}}"]
    parse: jsonl
    record: {id: "{{item.id}}"}
`)
	runner := &Runner{Type: kind, Executor: &fakeExecutor{commands: map[string]string{"host repositories": `{"message": "Bad credentials"}`}}, State: t.TempDir()}
	if _, err := runner.List(context.Background()); err == nil || !strings.Contains(err.Error(), "repositories") {
		t.Fatalf("an answer with no list was taken as an empty one: %v", err)
	}
}

// A credential goes only to a host the type settles.
func TestACredentialGoesOnlyWhereTheTypeSays(t *testing.T) {
	base := `
name: invented-api
description: an invented API
settings:
  - {name: host, type: string, default: "api.example.com"}
secrets:
  - {key: token, scope: operator}
authenticationProfiles:
  api: {type: bearer, token: "{{secret:token}}"}
containers:
  - fixed: [{}]
    name: all.jsonl
records:
  - request: {url: "%s", auth: api%s}
    parse: {json: {items: items}}
    record: {id: "{{item.id}}"}
`
	for _, refused := range []struct{ url, extra string }{
		{"https://{{settings.host}}/items", ""},
		{"https://{{item.next}}/items", ""},
		{"http://api.example.com/items", ""},
		{"https://api.example.com/items", `, headers: {X-Token: "{{secret:token}}"}`},
	} {
		if _, err := Parse([]byte("---\n" + strings.Replace(strings.Replace(base, "%s", refused.url, 1), "%s", refused.extra, 1) + "---\n")); err == nil {
			t.Errorf("%s %s was accepted", refused.url, refused.extra)
		}
	}
	if _, err := Parse([]byte("---\n" + strings.Replace(strings.Replace(base, "%s", "https://api.example.com/{{settings.host}}", 1), "%s", "", 1) + "---\n")); err != nil {
		t.Errorf("a settled host with a setting in its path was refused: %s", err)
	}
}

// A detail that is gone leaves its item out rather than failing the whole
// container, and a reading kept across passes saves what it read before
// it moves its mark.
func TestAGoneDetailLeavesOnlyItsItemOut(t *testing.T) {
	kind := mustParse(t, `
name: invented-mail
description: an invented mailbox
containers:
  - fixed: [{}]
    name: threads.jsonl
records:
  - command: [mail, search]
    parse: jsonl
    record: {id: "{{item.id}}", version: "{{item.id}}", text: "{{detail.text}}"}
    detail: {command: [mail, thread, "{{item.id}}"], text: "{{detail.text}}"}
`)
	executor := &fakeExecutor{
		commands: map[string]string{"mail search": "{\"id\": \"a\"}\n{\"id\": \"b\"}\n", "mail thread a": "hello"},
		failing:  map[string]*CommandError{"mail thread b": {ExitCode: 1, Said: "HTTP 404: thread gone"}},
	}
	runner := &Runner{Type: kind, Executor: executor, State: t.TempDir()}
	records, err := runner.Read(context.Background(), Container{Name: "threads.jsonl", Members: []map[string]any{{}}})
	if err != nil || len(records) != 1 || records[0]["id"] != "a" {
		t.Fatalf("read %v, %v", records, err)
	}
	if errors.Is(err, ErrUnfinished) {
		t.Fatalf("a gone item made the reading unfinished")
	}
}

// A reader type's defaults reach the reader when the source says nothing,
// and only what the source said is stored.
func TestAReaderTypesDefaultsReachTheReader(t *testing.T) {
	kind := mustParse(t, `
name: invented-site
description: an invented website
reader: web
settings:
  - {name: start, type: string}
  - {name: depth, type: integer, default: 2}
`)
	source := &models.AgentKnowledgeSource{}
	if err := kind.Specify(source, map[string]any{"start": "https://example.com/"}); err != nil {
		t.Fatal(err)
	}
	if source.Specification.Depth != 2 || source.Specification.Start != "https://example.com/" {
		t.Errorf("the reader was given %+v", source.Specification)
	}
	if string(source.Specification.Settings) != `{"start":"https://example.com/"}` {
		t.Errorf("stored %s", source.Specification.Settings)
	}
}

// Offset paging asks for the next page from where the last one ended,
// until a page comes back short.
func TestOffsetPagingReadsEveryPage(t *testing.T) {
	kind := mustParse(t, `
name: invented-wiki
description: an invented wiki search
containers:
  - fixed: [{}]
    name: pages.jsonl
records:
  - command: [wiki, search, --limit, "2", --json]
    parse: {json: {items: results}}
    paging: {offset: {flag: --start, size: 2}}
    record: {id: "{{item.id}}"}
`)
	executor := &fakeExecutor{commands: map[string]string{
		"wiki search --limit 2 --json":           `{"results": [{"id": "1"}, {"id": "2"}]}`,
		"wiki search --limit 2 --json --start 2": `{"results": [{"id": "3"}, {"id": "4"}]}`,
		"wiki search --limit 2 --json --start 4": `{"results": [{"id": "5"}]}`,
	}}
	runner := &Runner{Type: kind, Executor: executor, State: t.TempDir()}
	records, err := runner.Read(context.Background(), Container{Name: "pages.jsonl", Members: []map[string]any{{}}})
	if err != nil || strings.Join(ids(records), ",") != "1,2,3,4,5" {
		t.Fatalf("read %v, %v", ids(records), err)
	}
}

// A tool that ignores where it was asked to start fails the reading rather
// than being paged for ever.
func TestAToolThatRepeatsItsPageFails(t *testing.T) {
	kind := mustParse(t, `
name: invented-wiki
description: an invented wiki search
containers:
  - fixed: [{}]
    name: pages.jsonl
records:
  - command: [wiki, search]
    parse: {json: {items: results}}
    paging: {offset: {flag: --start, size: 2}}
    record: {id: "{{item.id}}"}
`)
	executor := &fakeExecutor{commands: map[string]string{
		"wiki search":           `{"results": [{"id": "1"}, {"id": "2"}]}`,
		"wiki search --start 2": `{"results": [{"id": "1"}, {"id": "2"}]}`,
	}}
	runner := &Runner{Type: kind, Executor: executor, State: t.TempDir()}
	if _, err := runner.Read(context.Background(), Container{Name: "pages.jsonl", Members: []map[string]any{{}}}); err == nil || !strings.Contains(err.Error(), "same page") {
		t.Fatalf("a repeated page was not refused: %v", err)
	}
}

// Link paging follows the address the answer gives for the next page, read
// against the base it gives, and never to another host.
func TestLinkPagingStaysOnItsHost(t *testing.T) {
	kind := mustParse(t, `
name: invented-wiki-api
description: an invented wiki's web API
containers:
  - fixed: [{}]
    name: pages.jsonl
records:
  - request: {url: "https://wiki.example.com/api/search?cql={{'type = page' | query-escape}}"}
    parse: {json: {items: results}}
    paging: {link: {field: _links.next, base: _links.base}}
    record: {id: "{{item.id}}"}
`)
	executor := &fakeExecutor{requests: map[string]string{
		"https://wiki.example.com/api/search?cql=type+%3D+page": `{"results": [{"id": "1"}], "_links": {"base": "https://wiki.example.com", "next": "/api/search?cursor=2"}}`,
		"https://wiki.example.com/api/search?cursor=2":          `{"results": [{"id": "2"}], "_links": {"base": "https://wiki.example.com"}}`,
	}}
	runner := &Runner{Type: kind, Executor: executor, State: t.TempDir()}
	records, err := runner.Read(context.Background(), Container{Name: "pages.jsonl", Members: []map[string]any{{}}})
	if err != nil || strings.Join(ids(records), ",") != "1,2" {
		t.Fatalf("read %v, %v (asked %v)", ids(records), err, executor.calls)
	}

	executor.requests["https://wiki.example.com/api/search?cql=type+%3D+page"] = `{"results": [{"id": "1"}], "_links": {"next": "https://elsewhere.example.net/steal"}}`
	elsewhere := &Runner{Type: kind, Executor: executor, State: t.TempDir()}
	if _, err := elsewhere.Read(context.Background(), Container{Name: "pages.jsonl", Members: []map[string]any{{}}}); err == nil || !strings.Contains(err.Error(), "not followed") {
		t.Fatalf("a next page on another host was followed: %v", err)
	}
}
