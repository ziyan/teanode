package sources

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Executor runs what a type says: a command on the computer the source
// runs on, or a web request from wherever it runs.
type Executor interface {
	Command(ctx context.Context, words []string) ([]byte, error)
	Request(ctx context.Context, request *PreparedRequest) (int, []byte, error)
}

// PreparedRequest is a request with every template filled in.
type PreparedRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

// CommandError is a command that ended badly, with the end of what it
// said about why.
type CommandError struct {
	Words    []string
	ExitCode int
	Said     string
}

func (self *CommandError) Error() string {
	name := ""
	if len(self.Words) > 0 {
		name = self.Words[0]
	}
	if self.Said != "" {
		return fmt.Sprintf("%s failed (exit %d): %s", name, self.ExitCode, self.Said)
	}
	return fmt.Sprintf("%s failed (exit %d)", name, self.ExitCode)
}

// ErrUnfinished is a reading that ran out of time part way. What it read
// is kept and sent; the pass it belongs to must not delete anything, and
// the next pass goes on from where this one stopped.
var ErrUnfinished = errors.New("sources: the reading ran out of time and goes on next pass")

// Container is one thing a source holds, as the records it is read into
// are named. Several members can share a name, and are then read into the
// same file: the spaces of a wiki whose pages were always kept together.
type Container struct {
	Name    string           `json:"name"`
	Members []map[string]any `json:"members"`
}

// Runner reads one source of one type.
type Runner struct {
	Type     *Type
	Settings map[string]any
	Secrets  map[string]string
	Executor Executor

	// State is the directory the runner keeps what it knows between
	// passes: when each container was last read, the records a reading
	// that reads only what changed has collected, fetched text and files.
	State string

	// Deadline is when a reading should stop and say it is unfinished
	// rather than keep a page waiting. The zero time is no deadline.
	Deadline time.Time

	// Now is the clock, for tests.
	Now func() time.Time

	// Sleep waits, for tests; time.Sleep by default.
	Sleep func(time.Duration)

	templates  map[string]compiled
	conditions map[string]condition
	lastCall   time.Time
}

// retryPauses are the waits before each retry of a call a service turned
// away for coming too often.
var retryPauses = []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second, 3 * time.Minute}

// busyAnswer is how a service says it is being asked too often, or is
// briefly unable to answer.
var busyAnswer = regexp.MustCompile(`(?i)\b429\b|\b503\b|rate ?limit|quota|too many requests|try again later|temporarily unavailable`)

func (self *Runner) sleep(duration time.Duration) {
	if self.Sleep != nil {
		self.Sleep(duration)
		return
	}
	time.Sleep(duration)
}

// paced waits out the type's pace since the last call.
func (self *Runner) paced() {
	if self.Type.Pace == "" {
		return
	}
	pace, err := parseDuration(self.Type.Pace)
	if err != nil || pace <= 0 {
		return
	}
	if wait := pace - self.now().Sub(self.lastCall); wait > 0 && !self.lastCall.IsZero() {
		self.sleep(wait)
	}
	self.lastCall = self.now()
}

// isBusy says a call failed because the service was asked too often, which
// waiting answers.
func isBusy(err error) bool {
	var failed *CommandError
	if errors.As(err, &failed) {
		return busyAnswer.MatchString(failed.Said)
	}
	var status *statusError
	if errors.As(err, &status) {
		return status.code == 429 || status.code == 503
	}
	return false
}

// Record is one record, in the shape the records reader files.
type Record map[string]any

func (self *Runner) now() time.Time {
	if self.Now != nil {
		return self.Now()
	}
	return time.Now()
}

func (self *Runner) scope() Scope {
	settings := map[string]any{}
	for _, setting := range self.Type.Settings {
		if value, ok := self.Settings[setting.Name]; ok {
			settings[setting.Name] = value
		} else {
			settings[setting.Name] = setting.Default
		}
	}
	return Scope{Values: map[string]any{"settings": settings}, Secrets: self.Secrets}
}

func (self *Runner) template(text string) (compiled, error) {
	if self.templates == nil {
		self.templates = map[string]compiled{}
	}
	if found, ok := self.templates[text]; ok {
		return found, nil
	}
	parsed, err := compileTemplate(text)
	if err != nil {
		return nil, err
	}
	self.templates[text] = parsed
	return parsed, nil
}

func (self *Runner) render(text string, scope Scope) (string, error) {
	parsed, err := self.template(text)
	if err != nil {
		return "", err
	}
	return parsed.render(scope)
}

func (self *Runner) value(text string, scope Scope) (any, error) {
	parsed, err := self.template(text)
	if err != nil {
		return nil, err
	}
	return parsed.value(scope)
}

func (self *Runner) holds(text string, scope Scope) (bool, error) {
	if strings.TrimSpace(text) == "" {
		return false, nil
	}
	if self.conditions == nil {
		self.conditions = map[string]condition{}
	}
	parsed, ok := self.conditions[text]
	if !ok {
		var err error
		if parsed, err = compileCondition(text); err != nil {
			return false, err
		}
		self.conditions[text] = parsed
	}
	return parsed.holds(scope)
}

// List lists the containers the source holds. Every listing runs; one
// that fails fails the whole list, since a list with a gap in it is a
// pass that deletes what was in the gap.
func (self *Runner) List(ctx context.Context) ([]Container, error) {
	base := self.scope()
	byListing := map[string][]map[string]any{}
	var order []string
	containers := map[string]*Container{}
	add := func(listing Listing, index int, name string, fields map[string]any) {
		key := listing.ID
		if key == "" {
			key = "#" + strconv.Itoa(index)
		}
		byListing[key] = append(byListing[key], fields)
		if listing.Only == "parents" || name == "" {
			return
		}
		existing, ok := containers[name]
		if !ok {
			existing = &Container{Name: name}
			containers[name] = existing
			order = append(order, name)
		}
		existing.Members = append(existing.Members, fields)
	}
	for index, listing := range self.Type.Containers {
		if listing.When != "" {
			held, err := self.holds(listing.When, base)
			if err != nil {
				return nil, err
			}
			if !held {
				continue
			}
		}
		var contexts []Scope
		switch {
		case listing.Each != "":
			values, err := self.value("{{"+listing.Each+"}}", base)
			if err != nil {
				return nil, err
			}
			for _, each := range asList(values) {
				contexts = append(contexts, base.with("each", each))
			}
		case listing.Over != "":
			for _, parent := range byListing[listing.Over] {
				contexts = append(contexts, base.with("parent", parent))
			}
		default:
			contexts = []Scope{base}
		}
		for _, scope := range contexts {
			if listing.Walk != nil {
				if err := self.walk(ctx, listing, scope, func(name string, fields map[string]any) { add(listing, index, name, fields) }); err != nil {
					return nil, err
				}
				continue
			}
			var fetched []fetchedItem
			if listing.Fixed != nil {
				for _, item := range listing.Fixed {
					fetched = append(fetched, fetchedItem{item: item})
				}
			} else {
				var err error
				if fetched, err = self.fetch(ctx, listing.Command, listing.Request, listing.Parse, listing.Paging, "", scope); err != nil {
					return nil, err
				}
			}
			for _, each := range fetched {
				itemScope := scope.with("item", each.item).with("response", each.response)
				if skip, err := self.holds(listing.Skip, itemScope); err != nil {
					return nil, err
				} else if skip {
					continue
				}
				name, fields, err := self.container(listing, itemScope)
				if err != nil {
					return nil, err
				}
				add(listing, index, name, fields)
			}
		}
	}
	listed := make([]Container, 0, len(order))
	for _, name := range order {
		listed = append(listed, *containers[name])
	}
	return listed, nil
}

// container is a listed item as a container: its name and its fields.
func (self *Runner) container(listing Listing, scope Scope) (string, map[string]any, error) {
	name, err := self.render(listing.Name, scope)
	if err != nil {
		return "", nil, err
	}
	fields := map[string]any{"name": name}
	for key, template := range listing.Fields {
		value, err := self.value(template, scope)
		if err != nil {
			return "", nil, err
		}
		fields[key] = value
	}
	return name, fields, nil
}

// walkLimit is the most places one walk lists, so a tree that loops
// through a shortcut ends.
const walkLimit = 100000

// walk lists a tree: the start, then every place the branch condition
// picks out of a listing, each a container of its own.
func (self *Runner) walk(ctx context.Context, listing Listing, scope Scope, add func(string, map[string]any)) error {
	start := map[string]any{}
	for key, template := range listing.Walk.Start {
		value, err := self.value(template, scope)
		if err != nil {
			return err
		}
		start[key] = value
	}
	queue := []map[string]any{start}
	visited := map[string]bool{}
	for len(queue) > 0 {
		if len(visited) >= walkLimit {
			return fmt.Errorf("the walk passed %d places and stopped; a tree that large wants a narrower start", walkLimit)
		}
		folder := queue[0]
		queue = queue[1:]
		identity := text(folder["id"])
		if visited[identity] {
			continue
		}
		visited[identity] = true
		folderScope := scope.with("folder", folder)
		name, fields, err := self.container(listing, folderScope)
		if err != nil {
			return err
		}
		add(name, fields)
		fetched, err := self.fetch(ctx, listing.Command, listing.Request, listing.Parse, listing.Paging, "", folderScope)
		if err != nil {
			return err
		}
		for _, each := range fetched {
			itemScope := folderScope.with("item", each.item).with("response", each.response)
			branch, err := self.holds(listing.Walk.Branch, itemScope)
			if err != nil {
				return err
			}
			if !branch {
				continue
			}
			child := map[string]any{}
			for key, template := range listing.Walk.Child {
				value, err := self.value(template, itemScope)
				if err != nil {
					return err
				}
				child[key] = value
			}
			queue = append(queue, child)
		}
	}
	return nil
}

// Read reads one container's records. A reading that reads only what
// changed, or keeps what it no longer lists, is answered from what it has
// collected, with what it read this time folded in. ErrUnfinished comes
// back with the records read so far when the deadline passed part way.
func (self *Runner) Read(ctx context.Context, container Container) ([]Record, error) {
	base := self.scope()
	collected := map[string]Record{}
	var order []string
	keep := func(record Record) {
		id := text(record["id"])
		if _, seen := collected[id]; !seen {
			order = append(order, id)
		}
		collected[id] = record
	}
	unfinished := false
	for readingIndex, reading := range self.Type.Records {
		keeping := reading.Since != nil || reading.Unseen == "keep"
		var store *recordStore
		if keeping {
			var err error
			if store, err = self.openStore(container.Name, readingIndex); err != nil {
				return nil, err
			}
		}
		for _, member := range container.Members {
			memberScope := base.with("container", member)
			eaches, err := self.eachOf(reading.Each, memberScope)
			if err != nil {
				return nil, err
			}
			for _, each := range eaches {
				scope := memberScope
				if each != nil {
					scope = scope.with("each", each)
				}
				records, err := self.readOnce(ctx, container, readingIndex, reading, scope, store)
				if errors.Is(err, ErrUnfinished) {
					unfinished = true
				} else if err != nil {
					return nil, err
				}
				if !keeping {
					for _, record := range records {
						keep(record)
					}
				}
				if unfinished {
					break
				}
			}
			if unfinished {
				break
			}
		}
		if store != nil {
			if err := store.save(); err != nil {
				return nil, err
			}
			for _, record := range store.records() {
				keep(record)
			}
		}
		if unfinished {
			break
		}
	}
	records := make([]Record, 0, len(order))
	for _, id := range order {
		records = append(records, collected[id])
	}
	if unfinished {
		return records, ErrUnfinished
	}
	return records, nil
}

func (self *Runner) eachOf(each any, scope Scope) ([]any, error) {
	switch typed := each.(type) {
	case nil:
		return []any{nil}, nil
	case []any:
		return typed, nil
	case string:
		values, err := self.value("{{"+typed+"}}", scope)
		if err != nil {
			return nil, err
		}
		return asList(values), nil
	}
	return nil, fmt.Errorf("each is a list or a path to one")
}

// readOnce is one reading of one member of a container, with one each.
func (self *Runner) readOnce(ctx context.Context, container Container, readingIndex int, reading Reading, scope Scope, store *recordStore) ([]Record, error) {
	if reading.Since == nil {
		records, err := self.readItems(ctx, container, reading, scope)
		if err != nil {
			return nil, err
		}
		if store != nil {
			store.merge(records)
		}
		return records, nil
	}

	key := sinceKey(container.Name, readingIndex, scope)
	state, err := self.loadSince()
	if err != nil {
		return nil, err
	}
	since, ok := asTime(state[key])
	if !ok {
		since, _ = asTime(reading.Since.First)
	}
	passScope := scope.with("pass", map[string]any{"since": since})
	if reading.Since.UnchangedWhen != "" {
		unchanged, err := self.holds(reading.Since.UnchangedWhen, passScope)
		if err != nil {
			return nil, err
		}
		if unchanged {
			return nil, nil
		}
	}
	began := self.now()
	if reading.Since.Window == "" {
		records, err := self.readItems(ctx, container, reading, passScope)
		if err != nil {
			return nil, err
		}
		store.merge(records)
		// A little before the read began, so that what changed while it
		// was running is read again next time rather than missed.
		return records, self.saveSince(key, began.Add(-sinceOverlap))
	}
	window, err := parseDuration(reading.Since.Window)
	if err != nil {
		return nil, err
	}
	var records []Record
	for start := since; start.Before(began); {
		if !self.Deadline.IsZero() && self.now().After(self.Deadline) {
			return records, ErrUnfinished
		}
		end := start.Add(window)
		if end.After(began) {
			end = began
		}
		windowScope := scope.with("pass", map[string]any{"since": since, "windowStart": start, "windowEnd": end})
		read, err := self.readItems(ctx, container, reading, windowScope)
		if err != nil {
			return records, err
		}
		store.merge(read)
		if err := store.save(); err != nil {
			return records, err
		}
		records = append(records, read...)
		next := end
		if end.Equal(began) {
			next = began.Add(-sinceOverlap)
		}
		if err := self.saveSince(key, next); err != nil {
			return records, err
		}
		start = end
	}
	return records, nil
}

// sinceOverlap is how far before a read began the next read of what
// changed starts.
const sinceOverlap = 2 * time.Minute

func sinceKey(container string, readingIndex int, scope Scope) string {
	member, _ := json.Marshal(scope.Values["container"])
	each, _ := json.Marshal(scope.Values["each"])
	sum := sha256.Sum256([]byte(container + "\x00" + strconv.Itoa(readingIndex) + "\x00" + string(member) + "\x00" + string(each)))
	return hex.EncodeToString(sum[:12])
}

// readItems runs a reading's call and makes a record of each item.
func (self *Runner) readItems(ctx context.Context, container Container, reading Reading, scope Scope) ([]Record, error) {
	fetched, err := self.fetch(ctx, reading.Command, reading.Request, reading.Parse, reading.Paging, reading.Missing, scope)
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(fetched))
	for _, each := range fetched {
		itemScope := scope.with("item", each.item).with("response", each.response)
		skip, err := self.holds(reading.Skip, itemScope)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		record, err := self.record(ctx, container, reading, itemScope)
		if err != nil {
			return nil, err
		}
		if record != nil {
			records = append(records, record)
		}
	}
	return records, nil
}

// recordFields are the fields of a record and whether each is a flag.
var recordFields = []string{"id", "kind", "title", "url", "at", "modifiedAt", "author", "channel", "thread", "text"}

func (self *Runner) record(ctx context.Context, container Container, reading Reading, scope Scope) (Record, error) {
	id, err := self.render(reading.Record["id"], scope)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		// An item with nothing to call it by cannot be filed, and cannot
		// be told from the next one.
		return nil, nil
	}
	scope = scope.with("record", map[string]any{"id": id})
	version := id
	if template, ok := reading.Record["version"]; ok {
		if version, err = self.render(template, scope); err != nil {
			return nil, err
		}
	}
	if reading.Detail != nil {
		detail, err := self.detail(ctx, container, reading.Detail, scope, id, version)
		if err != nil {
			return nil, err
		}
		scope = scope.with("detail", detail)
	}
	record := Record{"id": id}
	for _, field := range recordFields[1:] {
		template, ok := reading.Record[field]
		if !ok {
			continue
		}
		value, err := self.render(template, scope)
		if err != nil {
			return nil, err
		}
		if value != "" {
			record[field] = value
		}
	}
	if _, ok := record["text"]; !ok && reading.Detail != nil {
		if detailText := text(lookup(scope.Values, []step{{name: "detail"}, {name: "text"}}, scope)); detailText != "" {
			record["text"] = detailText
		}
	}
	if _, ok := record["kind"]; !ok {
		record["kind"] = "page"
	}
	private := true
	if template, ok := reading.Record["private"]; ok {
		value, err := self.value(template, scope)
		if err != nil {
			return nil, err
		}
		private = truthy(value)
	}
	record["private"] = private
	if len(reading.Attachments) > 0 {
		attachments, err := self.attachments(ctx, container, reading, scope, id, version)
		if err != nil {
			return nil, err
		}
		if len(attachments) > 0 {
			record["attachments"] = attachments
		}
	}
	return record, nil
}

// detail is an item's text from its detail call, fetched once for each
// version of the item and kept.
func (self *Runner) detail(ctx context.Context, container Container, detail *Detail, scope Scope, id, version string) (map[string]any, error) {
	path := filepath.Join(self.State, "detail", cacheName(self.Type.Name, container.Name, id, version)+".txt")
	if cached, err := os.ReadFile(path); err == nil {
		return map[string]any{"text": string(cached)}, nil
	}
	shape := detail.Parse
	if shape.Kind == "" {
		shape.Kind = "text"
	}
	fetched, err := self.fetch(ctx, detail.Command, detail.Request, shape, Paging{Kind: "none"}, "", scope)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if len(fetched) > 0 {
		result = fetched[0].item
	}
	if detail.Text != "" {
		value, err := self.render(detail.Text, scope.with("detail", result))
		if err != nil {
			return nil, err
		}
		result = map[string]any{"text": value}
	}
	if err := writeFile(path, []byte(text(result["text"]))); err != nil {
		return nil, err
	}
	return result, nil
}

// defaultAttachmentBytes is the largest file an attachment command keeps
// where the type says nothing.
const defaultAttachmentBytes = 25 << 20

var fileNameUnsafe = regexp.MustCompile(`[/\\\x00]+`)

// attachments fetches an item's files, each once for each version, into
// the runner's state, and answers where each is.
func (self *Runner) attachments(ctx context.Context, container Container, reading Reading, scope Scope, id, version string) ([]map[string]any, error) {
	var found []map[string]any
	for _, attachment := range reading.Attachments {
		if attachment.When != "" {
			held, err := self.holds(attachment.When, scope)
			if err != nil {
				return nil, err
			}
			if !held {
				continue
			}
		}
		eaches := []any{nil}
		if attachment.Each != "" {
			values, err := self.value("{{"+attachment.Each+"}}", scope)
			if err != nil {
				return nil, err
			}
			eaches = asList(values)
		}
		most := attachment.MaxBytes
		if most == 0 {
			most = reading.MaxBytes
		}
		if most == 0 {
			most = defaultAttachmentBytes
		}
		for _, each := range eaches {
			attachmentScope := scope
			if each != nil {
				attachmentScope = scope.with("each", each)
			}
			attachmentVersion := version
			if attachment.Version != "" {
				rendered, err := self.render(attachment.Version, attachmentScope)
				if err != nil {
					return nil, err
				}
				attachmentVersion = rendered
			}
			name := text(each)
			if attachment.Name != "" {
				rendered, err := self.render(attachment.Name, attachmentScope)
				if err != nil {
					return nil, err
				}
				name = rendered
			}
			name = fileNameUnsafe.ReplaceAllString(strings.TrimSpace(name), "_")
			if name == "" || name == "." || name == ".." {
				name = "file"
			}
			directory := filepath.Join(self.State, "files", cacheName(self.Type.Name, container.Name, id, text(each), attachmentVersion))
			path := filepath.Join(directory, name)
			if information, err := os.Stat(path); err != nil || information.Size() == 0 {
				if err := os.MkdirAll(directory, 0o700); err != nil {
					return nil, err
				}
				words, err := self.words(attachment.Command, attachmentScope.with("output", path))
				if err != nil {
					return nil, err
				}
				self.paced()
				if _, err := self.Executor.Command(ctx, words); err != nil {
					// One file that will not come is one file left out,
					// not a container that cannot be read.
					_ = os.RemoveAll(directory)
					continue
				}
			}
			information, err := os.Stat(path)
			if err != nil {
				continue
			}
			if information.Size() > most {
				_ = os.RemoveAll(directory)
				continue
			}
			found = append(found, map[string]any{"path": path, "name": name})
		}
	}
	return found, nil
}

func cacheName(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// fetchedItem is one item a call answered, with the answer it came in.
type fetchedItem struct {
	item     map[string]any
	response any
}

// pagesLimit is the most pages one listing follows.
const pagesLimit = 10000

// fetch runs a command or request and every page after it, and answers
// every item. A page that comes back full where the type said the call
// cannot page fails, since it may have been cut.
func (self *Runner) fetch(ctx context.Context, command []string, request *Request, shape Parsing, paging Paging, missing string, scope Scope) ([]fetchedItem, error) {
	var all []fetchedItem
	token := ""
	for page := 0; ; page++ {
		if page >= pagesLimit {
			return nil, fmt.Errorf("the listing passed %d pages and stopped", pagesLimit)
		}
		output, err := self.call(ctx, command, request, paging, token, scope)
		if err != nil {
			if missing == "empty" && isMissing(err) {
				return nil, nil
			}
			return nil, err
		}
		result, err := parseOutput(shape, output)
		if err != nil {
			return nil, err
		}
		for _, item := range result.items {
			all = append(all, fetchedItem{item: item, response: result.response})
		}
		switch paging.Kind {
		case "token":
			next, _ := at(result.response, paging.Field)
			token = text(next)
			if strings.TrimSpace(token) == "" {
				return all, nil
			}
		case "limit":
			if paging.Size > 0 && len(result.items) >= paging.Size {
				return nil, fmt.Errorf("the answer came back full (%d), so it may have been cut; read a smaller window", paging.Size)
			}
			return all, nil
		default:
			return all, nil
		}
	}
}

// missingAnswer is what a tool says when what it was asked for is not
// there.
var missingAnswer = regexp.MustCompile(`(?i)\b404\b|not found`)

func isMissing(err error) bool {
	var failed *CommandError
	if errors.As(err, &failed) {
		return missingAnswer.MatchString(failed.Said)
	}
	var status *statusError
	if errors.As(err, &status) {
		return status.code == 404
	}
	return false
}

type statusError struct {
	code int
	body string
}

func (self *statusError) Error() string {
	return fmt.Sprintf("the request was answered %d: %s", self.code, self.body)
}

// call runs one command or request, with the page token where there is
// one, waiting out the type's pace and retrying what a service turned away
// for coming too often.
func (self *Runner) call(ctx context.Context, command []string, request *Request, paging Paging, token string, scope Scope) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		self.paced()
		output, err := self.callOnce(ctx, command, request, paging, token, scope)
		if err == nil || !isBusy(err) || attempt >= len(retryPauses) {
			return output, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		self.sleep(retryPauses[attempt])
	}
}

func (self *Runner) callOnce(ctx context.Context, command []string, request *Request, paging Paging, token string, scope Scope) ([]byte, error) {
	if request != nil {
		prepared, err := self.prepare(request, scope)
		if err != nil {
			return nil, err
		}
		if token != "" {
			parsed, err := url.Parse(prepared.URL)
			if err != nil {
				return nil, err
			}
			query := parsed.Query()
			query.Set(strings.TrimLeft(paging.Flag, "-"), token)
			parsed.RawQuery = query.Encode()
			prepared.URL = parsed.String()
		}
		code, body, err := self.Executor.Request(ctx, prepared)
		if err != nil {
			return nil, err
		}
		if code < 200 || code >= 300 {
			said := string(body)
			if len(said) > 300 {
				said = said[:300]
			}
			return nil, &statusError{code: code, body: said}
		}
		return body, nil
	}
	words, err := self.words(command, scope)
	if err != nil {
		return nil, err
	}
	if token != "" {
		words = append(words, paging.Flag, token)
	}
	return self.Executor.Command(ctx, words)
}

// words fills a command's templates in. A word that is one expression
// and comes to nothing is left out, and so is the flag before it, so an
// optional setting left empty leaves no empty argument and no dangling
// flag behind.
func (self *Runner) words(command []string, scope Scope) ([]string, error) {
	var words []string
	for index, word := range command {
		parsed, err := self.template(word)
		if err != nil {
			return nil, err
		}
		rendered, err := parsed.render(scope)
		if err != nil {
			return nil, err
		}
		alone := len(parsed) == 1 && parsed[0].expression != nil
		if alone && rendered == "" {
			if index > 0 && len(words) > 0 && strings.HasPrefix(words[len(words)-1], "-") && !strings.Contains(command[index-1], "{{") {
				words = words[:len(words)-1]
			}
			continue
		}
		words = append(words, rendered)
	}
	if len(words) == 0 {
		return nil, fmt.Errorf("the command came to nothing")
	}
	return words, nil
}

// prepare fills a request's templates in and adds its credential.
func (self *Runner) prepare(request *Request, scope Scope) (*PreparedRequest, error) {
	prepared := &PreparedRequest{Method: strings.ToUpper(request.Method), Headers: map[string]string{}}
	if prepared.Method == "" {
		prepared.Method = "GET"
	}
	var err error
	if prepared.URL, err = self.render(request.URL, scope); err != nil {
		return nil, err
	}
	for key, template := range request.Headers {
		if prepared.Headers[key], err = self.render(template, scope); err != nil {
			return nil, err
		}
	}
	if prepared.Body, err = self.render(request.Body, scope); err != nil {
		return nil, err
	}
	if request.Auth == "" {
		return prepared, nil
	}
	profile, ok := self.Type.AuthenticationProfiles[request.Auth]
	if !ok {
		return nil, fmt.Errorf("there is no authentication profile %q", request.Auth)
	}
	fill := func(template string) (string, error) { return self.render(template, scope) }
	switch profile.Type {
	case "bearer":
		token, err := fill(profile.Token)
		if err != nil {
			return nil, err
		}
		// An optional secret left unset sends no credential rather than
		// an empty one.
		if token != "" {
			prepared.Headers["Authorization"] = "Bearer " + token
		}
	case "basic":
		username, err := fill(profile.Username)
		if err != nil {
			return nil, err
		}
		password, err := fill(profile.Password)
		if err != nil {
			return nil, err
		}
		if username != "" || password != "" {
			prepared.Headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
		}
	case "apiKey":
		value := profile.Value
		if value == "" {
			value = profile.Key
		}
		key, err := fill(value)
		if err != nil {
			return nil, err
		}
		header := profile.Header
		if header == "" {
			header = "X-API-Key"
		}
		if key != "" {
			prepared.Headers[header] = key
		}
	}
	return prepared, nil
}

// parseDuration reads a duration, with d for days.
func parseDuration(text string) (time.Duration, error) {
	if days, isDays := strings.CutSuffix(strings.TrimSpace(text), "d"); isDays {
		count, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("%q is not a length of time", text)
		}
		return time.Duration(count) * 24 * time.Hour, nil
	}
	return time.ParseDuration(text)
}

// The runner's state on disk.

func (self *Runner) loadSince() (map[string]any, error) {
	state := map[string]any{}
	content, err := os.ReadFile(filepath.Join(self.State, "since.json"))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &state); err != nil {
		return nil, fmt.Errorf("the state of what was last read is not readable: %w", err)
	}
	return state, nil
}

func (self *Runner) saveSince(key string, when time.Time) error {
	state, err := self.loadSince()
	if err != nil {
		return err
	}
	state[key] = when.UTC().Format(time.RFC3339)
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(self.State, "since.json"), encoded)
}

// SaveContainers keeps a pass's listing, so the pages after its first
// read the same containers rather than listing again.
func (self *Runner) SaveContainers(containers []Container) error {
	encoded, err := json.Marshal(containers)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(self.State, "containers.json"), encoded)
}

// LoadContainers is the listing the pass's first page kept.
func (self *Runner) LoadContainers() ([]Container, error) {
	content, err := os.ReadFile(filepath.Join(self.State, "containers.json"))
	if err != nil {
		return nil, err
	}
	var containers []Container
	if err := json.Unmarshal(content, &containers); err != nil {
		return nil, err
	}
	return containers, nil
}

// recordStore is what a reading that keeps records has collected for one
// container, one JSON line a record, by identifier.
type recordStore struct {
	path    string
	byID    map[string]Record
	order   []string
	changed bool
}

func (self *Runner) openStore(container string, readingIndex int) (*recordStore, error) {
	store := &recordStore{
		path: filepath.Join(self.State, "store", cacheName(container, strconv.Itoa(readingIndex))+".jsonl"),
		byID: map[string]Record{},
	}
	file, err := os.Open(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		id := text(record["id"])
		if _, seen := store.byID[id]; !seen {
			store.order = append(store.order, id)
		}
		store.byID[id] = record
	}
	return store, scanner.Err()
}

func (self *recordStore) merge(records []Record) {
	for _, record := range records {
		id := text(record["id"])
		if _, seen := self.byID[id]; !seen {
			self.order = append(self.order, id)
		}
		self.byID[id] = record
		self.changed = true
	}
}

func (self *recordStore) records() []Record {
	records := make([]Record, 0, len(self.order))
	for _, id := range self.order {
		records = append(records, self.byID[id])
	}
	return records
}

func (self *recordStore) save() error {
	if !self.changed {
		return nil
	}
	var built strings.Builder
	for _, record := range self.records() {
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		built.Write(encoded)
		built.WriteByte('\n')
	}
	self.changed = false
	return writeFile(self.path, []byte(built.String()))
}

// writeFile writes a file whole or not at all.
func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".writing"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// sortedKeys is a map's keys in order, for output that does not change
// between runs.
func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
