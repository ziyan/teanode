package computer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// The program's side of the relay. It says hello with the person's token,
// then answers the agent's requests one by one: a command through the
// shell, a file read or written, a directory listed or searched — as the
// person, anywhere on the machine, the way a terminal of theirs would. A
// command that runs too long is stopped.

// Protocol is the version of the exchange this program speaks; the server
// refuses another.
const Protocol = 1

// The bounds of one request.
const (
	defaultTimeout   = 120 * time.Second
	longestTimeout   = 600 * time.Second
	outputBytes      = 256 << 10 // per stream
	readBytes        = 4 << 20   // the largest file read at once
	readLines        = 2000      // lines given when no limit is asked
	listEntries      = 500
	searchEntries    = 200
	grepMatches      = 200
	grepLineChars    = 300
	pingEvery        = 30 * time.Second
	welcomeWait      = 15 * time.Second
	concurrentAtMost = 4
)

// Options say what the program offers.
type Options struct {
	// Token is the person's, from `teanode auth login`.
	Token string
	// Name is what the computer is called to the agent; the host name by
	// default. System is the operating system, for the agent to know what
	// commands it may use.
	Name   string
	System string
	// Home is where a command runs unless a directory is given, and what
	// ~ and a relative path are from; the person's home directory by
	// default.
	Home string
}

// Connection is what Serve needs of the websocket: JSON in, JSON out.
type Connection interface {
	ReadJSON(value any) error
	WriteJSON(value any) error
}

// message is what goes over the relay, either way.
type message struct {
	Type     string          `json:"type"`
	Protocol int             `json:"protocol,omitempty"`
	Token    string          `json:"token,omitempty"`
	Name     string          `json:"name,omitempty"`
	System   string          `json:"system,omitempty"`
	Home     string          `json:"home,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Username string          `json:"username,omitempty"`
	ID       int64           `json:"id,omitempty"`
	Action   string          `json:"action,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
	OK       bool            `json:"ok,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// RefusedError is the server turning the program away in words: a token it
// does not take, a protocol it does not speak. Not worth reconnecting for.
type RefusedError struct {
	Reason string
}

func (self *RefusedError) Error() string {
	return "the server refused: " + self.Reason
}

// Serve says hello, then answers requests until the connection ends or
// the context is done. It returns what ended it; a RefusedError means the
// server said no.
func Serve(ctx context.Context, connection Connection, options *Options) error {
	options = withDefaults(options)
	writes := &sync.Mutex{}
	write := func(value any) error {
		writes.Lock()
		defer writes.Unlock()
		return connection.WriteJSON(value)
	}
	if err := write(message{Type: "hello", Protocol: Protocol, Token: options.Token, Name: options.Name, System: options.System, Home: options.Home}); err != nil {
		return err
	}
	welcome := make(chan message, 1)
	readErrors := make(chan error, 1)
	requests := make(chan message, 16)
	go func() {
		for {
			var received message
			if err := connection.ReadJSON(&received); err != nil {
				readErrors <- err
				return
			}
			switch received.Type {
			case "welcome", "refused":
				welcome <- received
			case "act":
				requests <- received
			}
		}
	}()
	select {
	case received := <-welcome:
		if received.Type == "refused" {
			return &RefusedError{Reason: received.Reason}
		}
	case err := <-readErrors:
		return err
	case <-time.After(welcomeWait):
		return errors.New("the server did not answer the hello")
	case <-ctx.Done():
		return ctx.Err()
	}
	pings := time.NewTicker(pingEvery)
	defer pings.Stop()
	slots := make(chan struct{}, concurrentAtMost)
	for {
		select {
		case request := <-requests:
			// A few requests run at once; one more than that is answered
			// at once rather than queued behind them, so the loop goes on
			// pinging and reading whatever the requests do.
			select {
			case slots <- struct{}{}:
				go func() {
					defer func() { <-slots }()
					data, err := handle(ctx, options, request.Action, request.Args)
					answer := message{Type: "result", ID: request.ID, OK: err == nil, Data: data}
					if err != nil {
						answer.Error = err.Error()
					}
					_ = write(answer)
				}()
			default:
				_ = write(message{Type: "result", ID: request.ID, OK: false, Error: fmt.Sprintf("%d requests are still running on this computer; wait for one to finish", concurrentAtMost)})
			}
		case <-pings.C:
			if err := write(message{Type: "ping"}); err != nil {
				return err
			}
		case err := <-readErrors:
			return err
		case <-ctx.Done():
			_ = write(message{Type: "bye"})
			return ctx.Err()
		}
	}
}

func withDefaults(options *Options) *Options {
	filled := &Options{}
	if options != nil {
		*filled = *options
	}
	if filled.Name == "" {
		filled.Name, _ = os.Hostname()
		if filled.Name == "" {
			filled.Name = "computer"
		}
	}
	if filled.System == "" {
		filled.System = runtime.GOOS + "/" + runtime.GOARCH
	}
	if filled.Home == "" {
		filled.Home, _ = os.UserHomeDir()
	}
	filled.Home = filepath.Clean(filled.Home)
	return filled
}

// handle does one request and returns its answer as JSON.
func handle(ctx context.Context, options *Options, action string, args json.RawMessage) (json.RawMessage, error) {
	var result any
	var err error
	switch action {
	case "shell":
		var arguments ShellArguments
		if err := json.Unmarshal(args, &arguments); err != nil {
			return nil, fmt.Errorf("the request is not readable: %w", err)
		}
		result, err = RunShell(ctx, options, &arguments)
	case "filesystem":
		var arguments FilesystemArguments
		if err := json.Unmarshal(args, &arguments); err != nil {
			return nil, fmt.Errorf("the request is not readable: %w", err)
		}
		result, err = RunFilesystem(options, &arguments)
	default:
		return nil, fmt.Errorf("%q is not something this program does", action)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// ShellArguments are a command to run.
type ShellArguments struct {
	Command     string            `json:"command"`
	Directory   string            `json:"directory,omitempty"`
	Timeout     int               `json:"timeout,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

// ShellResult is what a command did.
type ShellResult struct {
	Stdout          string  `json:"stdout"`
	Stderr          string  `json:"stderr"`
	ExitCode        int     `json:"exitCode"`
	StdoutTruncated bool    `json:"stdoutTruncated,omitempty"`
	StderrTruncated bool    `json:"stderrTruncated,omitempty"`
	TimedOut        bool    `json:"timedOut,omitempty"`
	Seconds         float64 `json:"seconds"`
}

// RunShell runs a command through the person's shell, in a directory of
// theirs, for at most the timeout, and reports what it printed.
func RunShell(ctx context.Context, options *Options, arguments *ShellArguments) (*ShellResult, error) {
	options = withDefaults(options)
	if strings.TrimSpace(arguments.Command) == "" {
		return nil, errors.New("there is no command")
	}
	timeout := defaultTimeout
	if arguments.Timeout > 0 {
		timeout = min(time.Duration(arguments.Timeout)*time.Second, longestTimeout)
	}
	directory := options.Home
	if arguments.Directory != "" {
		directory = resolve(options.Home, arguments.Directory)
	}
	callContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(callContext, "cmd", "/C", arguments.Command)
	} else {
		command = exec.CommandContext(callContext, "/bin/sh", "-c", arguments.Command)
	}
	command.Dir = directory
	command.Env = os.Environ()
	// When its time is up the command is ended, and whatever it left
	// running is not waited for beyond a moment.
	command.WaitDelay = 2 * time.Second
	prepare(command)
	for key, value := range arguments.Environment {
		command.Env = append(command.Env, key+"="+value)
	}
	stdout := &boundedBuffer{limit: outputBytes}
	stderr := &boundedBuffer{limit: outputBytes}
	command.Stdout, command.Stderr = stdout, stderr
	started := time.Now()
	err := command.Run()
	result := &ShellResult{
		Stdout: stdout.String(), Stderr: stderr.String(),
		StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
		TimedOut: errors.Is(callContext.Err(), context.DeadlineExceeded),
		Seconds:  time.Since(started).Seconds(),
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		result.ExitCode = exit.ExitCode()
	case result.TimedOut:
		result.ExitCode = -1
	default:
		return nil, fmt.Errorf("the command could not be started: %w", err)
	}
	if result.TimedOut {
		result.ExitCode = -1
	}
	return result, nil
}

// boundedBuffer keeps the first limit bytes and says when more came.
type boundedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (self *boundedBuffer) Write(data []byte) (int, error) {
	room := self.limit - self.Len()
	if room <= 0 {
		self.truncated = true
		return len(data), nil
	}
	if len(data) > room {
		self.truncated = true
		_, _ = self.Buffer.Write(data[:room])
		return len(data), nil
	}
	return self.Buffer.Write(data)
}

// FilesystemArguments are one thing to do with the files.
type FilesystemArguments struct {
	Action      string `json:"action"`
	Path        string `json:"path"`
	Content     string `json:"content,omitempty"`
	Destination string `json:"destination,omitempty"`
	Pattern     string `json:"pattern,omitempty"`
	Find        string `json:"find,omitempty"`
	Replace     string `json:"replace,omitempty"`
	All         bool   `json:"all,omitempty"`
	Offset      int    `json:"offset,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Recursive   bool   `json:"recursive,omitempty"`
}

// Entry is one file or directory as listed.
type Entry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path,omitempty"`
	Kind     string    `json:"kind"` // file, directory, link, other
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// resolve is a path of the person's as an absolute one: ~ is their home,
// a relative path is from it, and an absolute path is where it says.
func resolve(home, path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		path = filepath.Join(home, path[1:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(home, path)
	}
	return filepath.Clean(path)
}

// RunFilesystem does one thing with the files and reports it.
func RunFilesystem(options *Options, arguments *FilesystemArguments) (any, error) {
	options = withDefaults(options)
	if strings.TrimSpace(arguments.Path) == "" {
		return nil, errors.New("a path is needed")
	}
	path := resolve(options.Home, arguments.Path)
	var err error
	switch arguments.Action {
	case "read":
		return readFile(path, arguments.Offset, arguments.Limit)
	case "write":
		// The directories on the way are made: a file is written where
		// it is wanted, not where a directory happens to be.
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(arguments.Content), 0o644); err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "bytes": len(arguments.Content)}, nil
	case "append":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, err
		}
		_, err = file.WriteString(arguments.Content)
		if closeError := file.Close(); err == nil {
			err = closeError
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "appended": len(arguments.Content)}, nil
	case "edit":
		return edit(path, arguments.Find, arguments.Replace, arguments.All)
	case "copy":
		if strings.TrimSpace(arguments.Destination) == "" {
			return nil, errors.New("copy needs a destination")
		}
		destination := resolve(options.Home, arguments.Destination)
		if err := copyFile(path, destination); err != nil {
			return nil, err
		}
		return map[string]any{"from": path, "to": destination}, nil
	case "list":
		return listDirectory(path, arguments.Limit)
	case "info":
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		return entryOf(path, info, true), nil
	case "mkdir":
		if arguments.Recursive {
			err = os.MkdirAll(path, 0o755)
		} else {
			err = os.Mkdir(path, 0o755)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "made": true}, nil
	case "delete":
		if arguments.Recursive {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "deleted": true}, nil
	case "move":
		if strings.TrimSpace(arguments.Destination) == "" {
			return nil, errors.New("move needs a destination")
		}
		destination := resolve(options.Home, arguments.Destination)
		if err := os.Rename(path, destination); err != nil {
			return nil, err
		}
		return map[string]any{"from": path, "to": destination}, nil
	case "search":
		return search(path, arguments.Pattern, arguments.Limit)
	case "grep":
		return grep(path, arguments.Pattern, arguments.Limit)
	}
	return nil, fmt.Errorf("%q is not something the filesystem does", arguments.Action)
}

func readFile(path string, offset, limit int) (any, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory; list it", path)
	}
	if info.Size() > readBytes {
		return nil, fmt.Errorf("%s is %d bytes, more than is read at once; use the shell to read part of it", path, info.Size())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return map[string]any{"path": path, "bytes": len(content), "binary": true}, nil
	}
	lines := strings.Split(string(content), "\n")
	total := len(lines)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = readLines
	}
	end := min(offset+limit, total)
	return map[string]any{"path": path, "lines": total, "offset": offset, "content": strings.Join(lines[offset:end], "\n"), "more": end < total}, nil
}

func entryOf(path string, info fs.FileInfo, withPath bool) Entry {
	entry := Entry{Name: info.Name(), Kind: "file", Size: info.Size(), Modified: info.ModTime()}
	switch {
	case info.IsDir():
		entry.Kind = "directory"
	case info.Mode()&fs.ModeSymlink != 0:
		entry.Kind = "link"
	case !info.Mode().IsRegular():
		entry.Kind = "other"
	}
	if withPath {
		entry.Path = path
	}
	return entry
}

func listDirectory(path string, limit int) (any, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = listEntries
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	listed := make([]Entry, 0, min(len(entries), limit))
	for _, entry := range entries {
		if len(listed) >= limit {
			break
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		listed = append(listed, entryOf("", info, false))
	}
	return map[string]any{"path": path, "entries": listed, "total": len(entries), "more": len(entries) > len(listed)}, nil
}

func search(root, pattern string, limit int) (any, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errors.New("a pattern is needed")
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("the pattern is not one: %w", err)
	}
	if limit <= 0 {
		limit = searchEntries
	}
	var found []Entry
	more := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && path != root && skipped(entry.Name()) {
			return filepath.SkipDir
		}
		if matched, _ := filepath.Match(pattern, entry.Name()); !matched {
			return nil
		}
		if len(found) >= limit {
			more = true
			return filepath.SkipAll
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		found = append(found, entryOf(path, info, true))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"root": root, "pattern": pattern, "entries": found, "more": more}, nil
}

// Match is one line of a file that matched.
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// skipped are the directories a walk leaves alone: what a tool made, not
// what the person wrote.
func skipped(name string) bool {
	return name == ".git" || name == "node_modules" || name == "vendor" || name == ".cache"
}

// grep finds the lines under a path — a file, or a directory walked —
// that match a regular expression, up to limit of them; binary files and
// very large ones are left out.
func grep(root, pattern string, limit int) (any, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errors.New("a pattern is needed")
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("the pattern is not a regular expression: %w", err)
	}
	if limit <= 0 {
		limit = grepMatches
	}
	var found []Match
	more := false
	searched := 0
	scan := func(path string) error {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > readBytes {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer func() { _ = file.Close() }()
		head := make([]byte, 512)
		read, _ := file.Read(head)
		if bytes.IndexByte(head[:read], 0) >= 0 {
			return nil
		}
		if _, err := file.Seek(0, 0); err != nil {
			return nil
		}
		searched++
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for number := 1; scanner.Scan(); number++ {
			line := scanner.Text()
			if !expression.MatchString(line) {
				continue
			}
			if len(found) >= limit {
				more = true
				return filepath.SkipAll
			}
			if len(line) > grepLineChars {
				line = line[:grepLineChars] + "…"
			}
			found = append(found, Match{Path: path, Line: number, Text: line})
		}
		return nil
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && skipped(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		return scan(path)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"root": root, "pattern": pattern, "matches": found, "files": searched, "more": more}, nil
}

// edit replaces text in a file: the one place it occurs, or every place
// when all is asked. A find that occurs nowhere, or in more than one place
// when only one was asked for, changes nothing and says so, so that the
// wrong line is never the one changed.
func edit(path, find, replace string, all bool) (any, error) {
	if find == "" {
		return nil, errors.New("edit needs find: the text to replace, exactly as it is in the file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(content)
	count := strings.Count(text, find)
	switch {
	case count == 0:
		return nil, fmt.Errorf("%s does not contain the text to find; read it and copy the text exactly, spaces and all", path)
	case count > 1 && !all:
		return nil, fmt.Errorf("%s contains the text to find %d times; include more of its surroundings so that it occurs once, or set all", path, count)
	}
	replaced := count
	if !all {
		replaced = 1
	}
	text = strings.Replace(text, find, replace, replaced)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(text), info.Mode().Perm()); err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "replaced": replaced}, nil
}

// copyFile copies one file, mode and all, making the directories on the
// way.
func copyFile(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; copy the files in it, or use the shell", from)
	}
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	target, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := target.ReadFrom(source); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
}
