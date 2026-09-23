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
