package sources

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeExecutor answers commands from a table keyed by the words joined
// with spaces, and requests by address, and counts every call.
type fakeExecutor struct {
	commands map[string]string
	failing  map[string]*CommandError
	requests map[string]string
	calls    []string
	headers  []map[string]string
}

func (self *fakeExecutor) Command(ctx context.Context, words []string) ([]byte, error) {
	key := strings.Join(words, " ")
	self.calls = append(self.calls, key)
	if failed, ok := self.failing[key]; ok {
		return nil, failed
	}
	answer, ok := self.commands[key]
	if !ok {
		return nil, fmt.Errorf("the fake has no answer for %q", key)
	}
	return []byte(answer), nil
}

func (self *fakeExecutor) Request(ctx context.Context, request *PreparedRequest) (int, []byte, error) {
	self.calls = append(self.calls, request.Method+" "+request.URL)
	self.headers = append(self.headers, request.Headers)
	answer, ok := self.requests[request.URL]
	if !ok {
		return 404, []byte("not here"), nil
	}
	return 200, []byte(answer), nil
}

func mustParse(t *testing.T, header string) *Type {
	t.Helper()
	parsed, err := Parse([]byte("---\n" + header + "\n---\nFor people.\n"))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	return parsed
}

func ids(records []Record) []string {
	var found []string
	for _, record := range records {
		found = append(found, text(record["id"]))
	}
	return found
}

// A listing that pages by token, runs once for each value of a setting,
// and is listed over; a reading that pages by itself, with a README that
// may not be there.
func TestListingPagesEachOverAndMissing(t *testing.T) {
	kind := mustParse(t, `
name: hosted-code
description: repositories on an invented code host
settings:
  - {name: organizations, type: array, default: []}
  - {name: profile, type: string, default: ""}
containers:
  - id: owners
    each: settings.organizations
    command: [host, --profile, "{{settings.profile}}", owners, "{{each}}"]
    parse: {json: {items: owners}}
    paging: {token: {field: next, flag: --page}}
    name: "owner {{item.login}}"
    fields: {login: "{{item.login}}"}
    only: parents
  - over: owners
    command: [host, repositories, "{{parent.login}}"]
    parse: jsonl
    paging: all
    skip: "{{item.fork}}"
    name: "{{item.full_name | replace \"/\" \" \"}}.jsonl"
    fields: {repository: "{{item.full_name}}"}
records:
  - command: [host, readme, "{{container.repository}}"]
    parse: text
    missing: empty
    record: {id: "{{container.repository}}:readme", text: "{{item.text}}", kind: page}
  - command: [host, issues, "{{container.repository}}"]
    parse: jsonl
    paging: all
    record:
      id: "{{container.repository}}#{{item.number}}"
      kind: post
      text: "{{item.title}}. {{item.labels.*.name | join \", \"}}"
      private: "{{item.secret}}"
`)
	fake := &fakeExecutor{
		commands: map[string]string{
			"host owners north":               `{"owners":[{"login":"north"}],"next":"page-2"}`,
			"host owners north --page page-2": `{"owners":[{"login":"north-labs"}]}`,
			"host repositories north":         `{"full_name":"north/alpha","fork":false}` + "\n" + `{"full_name":"north/fork","fork":true}`,
			"host repositories north-labs":    `{"full_name":"north-labs/beta","fork":false}`,
			"host readme north/alpha":         "# Alpha",
			"host issues north/alpha":         `{"number":1,"title":"Broken","labels":[{"name":"bug"},{"name":"urgent"}],"secret":false}`,
			"host issues north-labs/beta":     ``,
		},
		failing: map[string]*CommandError{"host readme north-labs/beta": {ExitCode: 1, Said: "HTTP 404: Not Found"}},
	}
	runner := &Runner{Type: kind, Settings: map[string]any{"organizations": []any{"north"}}, Executor: fake, State: t.TempDir()}
	containers, err := runner.List(context.Background())
	if err != nil {
		t.Fatalf("List: %s", err)
	}
	var names []string
	for _, each := range containers {
		names = append(names, each.Name)
	}
	if strings.Join(names, ",") != "north alpha.jsonl,north-labs beta.jsonl" {
		t.Fatalf("the containers were %v: the parents are not read, the fork is skipped", names)
	}
	// The empty profile left no empty argument and no dangling flag.
	if fake.calls[0] != "host owners north" {
		t.Errorf("the first call was %q", fake.calls[0])
	}
	records, err := runner.Read(context.Background(), containers[0])
	if err != nil {
		t.Fatalf("Read: %s", err)
	}
	if strings.Join(ids(records), ",") != "north/alpha:readme,north/alpha#1" {
		t.Fatalf("the records were %v", ids(records))
	}
	if records[1]["text"] != "Broken. bug, urgent" || records[1]["private"] != false || records[1]["kind"] != "post" {
		t.Errorf("the issue was %v", records[1])
	}
	// A README that is not there is nothing to read, not a failure.
	if records, err := runner.Read(context.Background(), containers[1]); err != nil || len(records) != 0 {
		t.Errorf("the repository with no README: %v, %v", ids(records), err)
	}
}

// A listing that fails fails the whole list.
func TestAFailureIsNeverAPartialList(t *testing.T) {
	kind := mustParse(t, `
name: two-scopes
description: two listings, one of which fails
settings: []
containers:
  - {command: [tool, one], parse: jsonl, name: "{{item.name}}.jsonl"}
  - {command: [tool, two], parse: jsonl, name: "{{item.name}}.jsonl"}
records:
  - {command: [tool, read, "{{container.name}}"], parse: jsonl, record: {id: "{{item.id}}"}}
`)
	fake := &fakeExecutor{
		commands: map[string]string{"tool one": `{"name":"kept"}`},
		failing:  map[string]*CommandError{"tool two": {ExitCode: 2, Said: "timed out"}},
	}
	runner := &Runner{Type: kind, Executor: fake, State: t.TempDir()}
	if containers, err := runner.List(context.Background()); err == nil {
		t.Fatalf("a listing that failed gave %v and no error", containers)
	}
}

// Lines read from text; several spaces share one file; a reading of what
// changed reads windows oldest first, keeps what it read, and asks for a
// page's detail only once for each version.
func TestWindowsDetailAndKeptRecords(t *testing.T) {
	kind := mustParse(t, `
name: wiki
description: an invented wiki tool that cannot page
settings: []
containers:
  - command: [wiki, spaces]
    parse: {lines: {pattern: "^(?P<key>[^ ]+) - (?P<name>.+)$"}}
    name: pages.jsonl
    fields: {space: "{{item.key}}"}
records:
  - command: [wiki, search, "{{container.space}}", "{{pass.windowStart | date}}", "{{pass.windowEnd | date}}"]
    parse: {lines: {pattern: "^\\d+\\. (?P<title>.+) \\(ID: (?P<id>\\d+)\\)$"}}
    paging: {limit: {size: 3}}
    since: {first: "2026-01-01", window: 10d}
    record: {id: "page:{{item.id}}", title: "{{item.title}}"}
    detail: {command: [wiki, read, "{{item.id}}"], text: "{{detail.text}}"}
`)
	fake := &fakeExecutor{commands: map[string]string{
		"wiki spaces":                         "Available spaces:\nA - Alpha\nB - Beta\n",
		"wiki search A 2026-01-01 2026-01-11": "1. First (ID: 1)\n",
		"wiki search A 2026-01-11 2026-01-15": "",
		"wiki search B 2026-01-01 2026-01-11": "1. Second (ID: 2)\n2. Third (ID: 3)\n",
		"wiki search B 2026-01-11 2026-01-15": "",
		"wiki read 1":                         "first page",
		"wiki read 2":                         "second page",
		"wiki read 3":                         "third page",
	}}
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	runner := &Runner{Type: kind, Executor: fake, State: t.TempDir(), Now: func() time.Time { return now }}
	containers, err := runner.List(context.Background())
	if err != nil || len(containers) != 1 || len(containers[0].Members) != 2 {
		t.Fatalf("both spaces are one file: %+v, %v", containers, err)
	}
	records, err := runner.Read(context.Background(), containers[0])
	if err != nil {
		t.Fatalf("Read: %s", err)
	}
	if strings.Join(ids(records), ",") != "page:1,page:2,page:3" || records[0]["text"] != "first page" {
		t.Fatalf("the records were %v", records)
	}

	// The next pass: only what changed since, and nothing did. What was
	// read is still answered, and no page is read again.
	now = now.Add(time.Hour)
	fake.calls = nil
	fake.commands["wiki search A 2026-01-14 2026-01-15"] = ""
	fake.commands["wiki search B 2026-01-14 2026-01-15"] = ""
	records, err = runner.Read(context.Background(), containers[0])
	if err != nil {
		t.Fatalf("Read again: %s", err)
	}
	if len(records) != 3 {
		t.Errorf("what was read before is kept: %v", ids(records))
	}
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "wiki read") {
			t.Errorf("a page was read again: %s", call)
		}
	}

	// A window that comes back full fails rather than being trusted. The
	// last pass began at one o'clock, so this one reads from a little
	// before it.
	fake.commands["wiki search A 2026-01-15 2026-01-15"] = "1. a (ID: 7)\n2. b (ID: 8)\n3. c (ID: 9)\n"
	if _, err := runner.Read(context.Background(), containers[0]); err == nil || !strings.Contains(err.Error(), "full") {
		t.Errorf("a full window: %v", err)
	}
}

// A deadline part way through the windows stops the reading and says so,
// with what was read kept for the next pass.
func TestADeadlineLeavesTheReadingUnfinished(t *testing.T) {
	kind := mustParse(t, `
name: slow-wiki
description: a wiki read one window at a time
settings: []
containers:
  - {fixed: [{}], name: pages.jsonl}
records:
  - command: [wiki, search, "{{pass.windowStart | date}}"]
    parse: jsonl
    since: {first: "2026-01-01", window: 1d}
    record: {id: "{{item.id}}"}
`)
	start := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)
	fake := &fakeExecutor{commands: map[string]string{
		"wiki search 2026-01-01": `{"id":"one"}`,
		"wiki search 2026-01-02": `{"id":"two"}`,
		"wiki search 2026-01-03": `{"id":"three"}`,
	}}
	clock := start
	runner := &Runner{Type: kind, Executor: fake, State: t.TempDir(), Deadline: start.Add(90 * time.Second),
		Now: func() time.Time { return clock }}
	// Each window takes a minute.
	runner.Executor = &tickingExecutor{inner: fake, tick: func() { clock = clock.Add(time.Minute) }}
	containers, _ := runner.List(context.Background())
	records, err := runner.Read(context.Background(), containers[0])
	if !errors.Is(err, ErrUnfinished) {
		t.Fatalf("the reading was not unfinished: %v, %v", ids(records), err)
	}
	if len(records) == 0 || len(records) == 3 {
		t.Errorf("part of it was read: %v", ids(records))
	}
}

type tickingExecutor struct {
	inner Executor
	tick  func()
}

func (self *tickingExecutor) Command(ctx context.Context, words []string) ([]byte, error) {
	self.tick()
	return self.inner.Command(ctx, words)
}

func (self *tickingExecutor) Request(ctx context.Context, request *PreparedRequest) (int, []byte, error) {
	self.tick()
	return self.inner.Request(ctx, request)
}

// A walk lists every folder under the start, each a container named by
// its path.
func TestAWalkListsEveryFolder(t *testing.T) {
	kind := mustParse(t, `
name: drive
description: an invented drive
settings: []
containers:
  - walk:
      start: {id: root, path: "My Drive"}
      branch: "{{item.type}} == folder"
      child: {id: "{{item.id}}", path: "{{folder.path}}/{{item.name}}"}
    command: [drive, ls, "{{folder.id}}"]
    parse: {json: {items: files}}
    name: "{{folder.path}}.jsonl"
    fields: {folder: "{{folder.id}}"}
records:
  - {command: [drive, ls, "{{container.folder}}"], parse: {json: {items: files}}, skip: "{{item.type}} == folder", record: {id: "{{item.id}}"}}
`)
	fake := &fakeExecutor{commands: map[string]string{
		"drive ls root": `{"files":[{"id":"a","name":"Work","type":"folder"},{"id":"f1","name":"note","type":"file"}]}`,
		"drive ls a":    `{"files":[{"id":"b","name":"Old","type":"folder"}]}`,
		"drive ls b":    `{"files":[]}`,
	}}
	runner := &Runner{Type: kind, Executor: fake, State: t.TempDir()}
	containers, err := runner.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, each := range containers {
		names = append(names, each.Name)
	}
	if strings.Join(names, ",") != "My Drive.jsonl,My Drive/Work.jsonl,My Drive/Work/Old.jsonl" {
		t.Errorf("the folders were %v", names)
	}
}

// An item's author is looked up in the rest of the answer, times given in
// milliseconds become times, and a reply carries its thread.
func TestLookupsTimesAndThreads(t *testing.T) {
	kind := mustParse(t, `
name: chat
description: an invented chat server
settings: []
containers:
  - {fixed: [{channel: c1}], name: "posts/team/general.jsonl", fields: {channel: "{{item.channel}}"}}
records:
  - command: [chat, posts, "{{container.channel}}"]
    parse: {json: {items: "posts.*"}}
    skip: "{{item.type}} != ''"
    record:
      id: "{{item.id}}"
      kind: chat
      author: "{{response.users[item.user_id].username}}"
      at: "{{item.create_at | epoch-ms}}"
      thread: "{{item.root_id | or item.id}}"
      text: "{{item.message}}"
`)
	fake := &fakeExecutor{commands: map[string]string{"chat posts c1": `{
		"posts": {
			"p1": {"id":"p1","user_id":"u1","create_at":1767225600000,"root_id":"","type":"","message":"hello"},
			"p2": {"id":"p2","user_id":"u2","create_at":1767225660000,"root_id":"p1","type":"","message":"hi"},
			"p3": {"id":"p3","user_id":"u1","create_at":1767225720000,"root_id":"","type":"system_join","message":"joined"}
		},
		"users": {"u1":{"username":"ada"},"u2":{"username":"bo"}}
	}`}}
	runner := &Runner{Type: kind, Executor: fake, State: t.TempDir()}
	containers, _ := runner.List(context.Background())
	records, err := runner.Read(context.Background(), containers[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("the system message is skipped: %v", ids(records))
	}
	if records[0]["author"] != "ada" || records[0]["at"] != "2026-01-01T00:00:00Z" || records[0]["thread"] != "p1" {
		t.Errorf("the first post was %v", records[0])
	}
	if records[1]["author"] != "bo" || records[1]["thread"] != "p1" {
		t.Errorf("the reply was %v", records[1])
	}
}

// A feed is read with a request, as XML; a token left unset sends no
// credential, and one that is set is sent.
func TestAFeedIsReadWithARequest(t *testing.T) {
	kind := mustParse(t, `
name: feed
description: an invented feed
runs: [server, computer]
settings:
  - {name: url, type: string}
secrets:
  - {key: token, description: a token, scope: person}
authenticationProfiles:
  feed: {type: bearer, token: "{{secret:token}}"}
containers:
  - {fixed: [{}], name: feed.jsonl}
records:
  - request: {url: "{{settings.url}}", auth: feed}
    parse: {xml: {items: "rss.channel.item | feed.entry"}}
    unseen: keep
    record:
      id: "{{item.guid | or item.id}}"
      title: "{{item.title}}"
      url: "{{item.link.href | or item.link}}"
      at: "{{item.pubDate | or item.updated | time}}"
      text: "{{item.description | or item.summary | html-text}}"
`)
	feed := `<?xml version="1.0"?><rss><channel>
		<item><guid>g1</guid><title>First</title><link>https://news.example/1</link><pubDate>Thu, 01 Jan 2026 10:00:00 +0000</pubDate><description>&lt;p&gt;Hello &amp;amp; welcome&lt;/p&gt;</description></item>
		<item><guid>g2</guid><title>Second</title><link>https://news.example/2</link><description>Plain</description></item>
	</channel></rss>`
	fake := &fakeExecutor{requests: map[string]string{"https://news.example/feed": feed}}
	runner := &Runner{Type: kind, Settings: map[string]any{"url": "https://news.example/feed"}, Executor: fake, State: t.TempDir()}
	containers, _ := runner.List(context.Background())
	records, err := runner.Read(context.Background(), containers[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids(records), ",") != "g1,g2" {
		t.Fatalf("the entries were %v", ids(records))
	}
	if records[0]["url"] != "https://news.example/1" || records[0]["at"] != "2026-01-01T10:00:00Z" || records[0]["text"] != "Hello & welcome" {
		t.Errorf("the first entry was %v", records[0])
	}
	if _, sent := fake.headers[0]["Authorization"]; sent {
		t.Errorf("an unset token was sent: %v", fake.headers[0])
	}
	runner.Secrets = map[string]string{"token": "secret-value"}
	if _, err := runner.Read(context.Background(), containers[0]); err != nil {
		t.Fatal(err)
	}
	if fake.headers[1]["Authorization"] != "Bearer secret-value" {
		t.Errorf("a set token was not sent: %v", fake.headers[1])
	}
}

// A service that says it is asked too often is asked again after a wait,
// and a type's pace spaces its calls out.
func TestABusyServiceIsAskedAgain(t *testing.T) {
	kind := mustParse(t, `
name: busy
description: a service that turns the first call away
pace: 50ms
settings: []
containers:
  - {fixed: [{}], name: one.jsonl}
records:
  - {command: [service, list], parse: jsonl, record: {id: "{{item.id}}"}}
`)
	calls := 0
	busy := &scriptedExecutor{answer: func(words []string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, &CommandError{Words: words, ExitCode: 7, Said: "Google API error (403 rateLimitExceeded): Quota exceeded"}
		}
		return []byte(`{"id":"one"}`), nil
	}}
	var waited []time.Duration
	runner := &Runner{Type: kind, Executor: busy, State: t.TempDir(), Sleep: func(duration time.Duration) { waited = append(waited, duration) }}
	containers, _ := runner.List(context.Background())
	records, err := runner.Read(context.Background(), containers[0])
	if err != nil || len(records) != 1 {
		t.Fatalf("the busy service was not asked again: %v, %v", ids(records), err)
	}
	// The retry's wait, then the pace before the call it retries.
	if len(waited) != 2 || waited[0] != retryPauses[0] || waited[1] <= 0 || waited[1] > 50*time.Millisecond {
		t.Errorf("the waits were %v", waited)
	}
}

type scriptedExecutor struct {
	answer func([]string) ([]byte, error)
}

func (self *scriptedExecutor) Command(ctx context.Context, words []string) ([]byte, error) {
	return self.answer(words)
}

func (self *scriptedExecutor) Request(ctx context.Context, request *PreparedRequest) (int, []byte, error) {
	return 404, nil, nil
}
