package computer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// A session is a program that stays open.
//
// Everything else this program does ends: a command runs and returns, a file
// is read and that is that, and the protocol carries one numbered request to
// one answer because that is all it needed to. A program that stays open --
// a language server, a connected server spoken to over its standard input, a
// shell somebody is working in -- needs the other two things: output arriving
// when the program feels like it rather than when it was asked for, and input
// written to something started earlier.
//
// So a session has an identifier of its own, and messages about it carry that
// rather than a request number. The server keeps what arrives; this side owns
// the process.

// SessionStartArguments start one.
type SessionStartArguments struct {
	// Session is the identifier the server chose. It names this session in
	// everything after, in both directions.
	Session string `json:"session"`

	// Kind is "stdio" for a program spoken to over its standard streams,
	// or "pty" for one run in a terminal and read as a screen.
	Kind string `json:"kind"`

	// Command and Arguments are what to run; Directory where; Environment
	// what to add to this program's own.
	Command     string            `json:"command"`
	Arguments   []string          `json:"arguments,omitempty"`
	Directory   string            `json:"directory,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`

	// Columns and Rows size a terminal; a stdio session ignores them.
	Columns int `json:"columns,omitempty"`
	Rows    int `json:"rows,omitempty"`
}

// SessionStartResult says it started, and what it is.
type SessionStartResult struct {
	Session string `json:"session"`
	PID     int    `json:"pid"`
}

// SessionWriteArguments write to one's input. Data is base64, because what
// goes to a program's standard input is bytes and not necessarily text.
type SessionWriteArguments struct {
	Session string `json:"session"`
	Data    string `json:"data"`
}

// SessionSignalArguments send a signal: "int", "term", "kill", or "hup".
type SessionSignalArguments struct {
	Session string `json:"session"`
	Signal  string `json:"signal"`
}

// SessionCloseArguments end one. It is asked to stop, and killed if it does
// not; a session left open by a turn that ended is not the person's problem
// to notice.
type SessionCloseArguments struct {
	Session string `json:"session"`
}

// SessionResult is the answer to writing, signalling or closing.
type SessionResult struct {
	Session string `json:"session"`
	OK      bool   `json:"ok"`
}

// sessionEndWait is how long a closing session is given to stop before it is
// killed.
const sessionEndWait = 3 * time.Second

// mostSessions is how many one computer will hold open at once. A bound
// rather than a policy: each one is a process, and something that can ask
// for a process without limit is a way to fill a machine.
const mostSessions = 16

// pushOutput is how a session says something arrived, and pushEnded that it
// finished. Both are unsolicited, which is what the session layer exists for.
type pushOutput func(session, stream, data string)
type pushEnded func(session string, code int)

// sessions are the ones this program has open.
type sessions struct {
	mutex sync.Mutex
	open  map[string]*session
}

type session struct {
	id      string
	kind    string
	command *exec.Cmd
	stdin   io.WriteCloser

	// For a terminal: the pty the program runs in, and the screen it is
	// drawing, kept here so that reading it is one request and none of the
	// redrawing ever crosses the network.
	pty    *os.File
	screen *screen

	// Set when the process ends. The session stays until it is closed, so
	// that the last screen and the exit code can still be read.
	ended bool
	code  int
	done  chan struct{}
}

func newSessions() *sessions {
	return &sessions{open: map[string]*session{}}
}

// start runs the program and streams what it says back through push.
func (self *sessions) start(ctx context.Context, options *Options, arguments *SessionStartArguments,
	output pushOutput, ended pushEnded) (*SessionStartResult, error) {
	if strings.TrimSpace(arguments.Session) == "" {
		return nil, fmt.Errorf("a session needs an identifier")
	}
	if strings.TrimSpace(arguments.Command) == "" {
		return nil, fmt.Errorf("a session needs a command to run")
	}
	kind := strings.TrimSpace(arguments.Kind)
	if kind == "" {
		kind = "stdio"
	}
	if kind != "stdio" && kind != "pty" {
		return nil, fmt.Errorf("%q is not a kind of session this program starts", kind)
	}

	// The name is taken and the slot counted before the process starts,
	// under one lock, so that two starts at once cannot both pass the
	// check and end up one over the bound or two under one name. A start
	// that fails after this gives the reservation back.
	self.mutex.Lock()
	if _, taken := self.open[arguments.Session]; taken {
		self.mutex.Unlock()
		return nil, fmt.Errorf("a session called %q is already open", arguments.Session)
	}
	if len(self.open) >= mostSessions {
		self.mutex.Unlock()
		return nil, fmt.Errorf("this computer already has %d sessions open", mostSessions)
	}
	reserved := &session{id: arguments.Session, kind: kind, done: make(chan struct{})}
	self.open[arguments.Session] = reserved
	self.mutex.Unlock()
	release := func() {
		self.mutex.Lock()
		if self.open[arguments.Session] == reserved {
			delete(self.open, arguments.Session)
		}
		self.mutex.Unlock()
	}

	// The same directory rule the shell tool follows, so that a session is
	// not a way around it.
	directory := options.Home
	if arguments.Directory != "" {
		directory = resolve(options.Home, arguments.Directory)
	}
	command := exec.Command(arguments.Command, arguments.Arguments...)
	command.Dir = directory
	command.Env = os.Environ()
	for key, value := range arguments.Environment {
		command.Env = append(command.Env, key+"="+value)
	}

	if kind == "pty" {
		return self.startTerminal(reserved, release, command, arguments, ended)
	}

	stdin, err := command.StdinPipe()
	if err != nil {
		release()
		return nil, fmt.Errorf("cannot write to %s: %w", arguments.Command, err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		release()
		return nil, fmt.Errorf("cannot read %s: %w", arguments.Command, err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		release()
		return nil, fmt.Errorf("cannot read %s: %w", arguments.Command, err)
	}
	if err := command.Start(); err != nil {
		release()
		return nil, fmt.Errorf("cannot start %s: %w", arguments.Command, err)
	}

	// The reservation becomes the running session.
	self.mutex.Lock()
	reserved.command, reserved.stdin = command, stdin
	self.mutex.Unlock()

	go self.pump(arguments.Session, "stdout", stdout, output)
	go self.pump(arguments.Session, "stderr", stderr, output)
	go func() {
		err := command.Wait()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			} else {
				code = -1
			}
		}
		self.finish(reserved, code)
		ended(arguments.Session, code)
	}()

	return &SessionStartResult{Session: arguments.Session, PID: command.Process.Pid}, nil
}

// pump reads one stream until it ends, sending what it reads across as it
// arrives rather than at the end -- which is the whole point of a session.
func (self *sessions) pump(id, stream string, reader io.Reader, output pushOutput) {
	buffer := make([]byte, 32<<10)
	for {
		read, err := reader.Read(buffer)
		if read > 0 {
			output(id, stream, base64.StdEncoding.EncodeToString(buffer[:read]))
		}
		if err != nil {
			return
		}
	}
}

func (self *sessions) find(id string) (*session, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	held, found := self.open[id]
	if !found || held.command == nil {
		return nil, fmt.Errorf("there is no session called %q here", id)
	}
	return held, nil
}

func (self *sessions) write(arguments *SessionWriteArguments) (*SessionResult, error) {
	held, err := self.find(arguments.Session)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(arguments.Data)
	if err != nil {
		return nil, fmt.Errorf("what was to be written is not base64: %w", err)
	}
	if held.hasEnded() {
		return nil, fmt.Errorf("the session has ended; nothing reads what is written to it")
	}
	var writer io.Writer = held.stdin
	if held.pty != nil {
		writer = held.pty
	}
	if _, err := writer.Write(decoded); err != nil {
		return nil, fmt.Errorf("cannot write to the session: %w", err)
	}
	return &SessionResult{Session: arguments.Session, OK: true}, nil
}

func (self *sessions) signal(arguments *SessionSignalArguments) (*SessionResult, error) {
	held, err := self.find(arguments.Session)
	if err != nil {
		return nil, err
	}
	var sent syscall.Signal
	switch strings.ToLower(strings.TrimSpace(arguments.Signal)) {
	case "int", "":
		sent = syscall.SIGINT
	case "term":
		sent = syscall.SIGTERM
	case "kill":
		sent = syscall.SIGKILL
	case "hup":
		sent = syscall.SIGHUP
	default:
		return nil, fmt.Errorf("%q is not a signal this sends", arguments.Signal)
	}
	if held.command.Process == nil {
		return nil, fmt.Errorf("the session has no process")
	}
	if err := held.command.Process.Signal(sent); err != nil {
		return nil, fmt.Errorf("cannot signal the session: %w", err)
	}
	return &SessionResult{Session: arguments.Session, OK: true}, nil
}

// close asks the session to stop, and kills it if it will not.
func (self *sessions) close(arguments *SessionCloseArguments) (*SessionResult, error) {
	held, err := self.find(arguments.Session)
	if err != nil {
		return nil, err
	}
	// Closing its input is what most programs read as "we are done", and it
	// is the only ending that lets one finish on its own terms -- cat, and
	// every connected server spoken to this way, exits cleanly on it. The
	// signals are for the ones that do not, in that order and only after
	// waiting: a program killed the instant it was asked to stop reports as
	// killed, which loses the difference between one that ended badly and
	// one that was not given the chance to end at all.
	if held.stdin != nil {
		_ = held.stdin.Close()
	}
	if held.pty != nil {
		// Closing the terminal is a hangup to everything in it, which is
		// what a shell reads as the person having left.
		_ = held.pty.Close()
	}
	if held.command.Process != nil && !held.hasEnded() {
		go func() {
			if held.waitForEnd(sessionEndWait) {
				return
			}
			_ = held.command.Process.Signal(syscall.SIGTERM)
			if held.waitForEnd(sessionEndWait) {
				return
			}
			_ = held.command.Process.Kill()
		}()
	}
	// Forgotten now: a closed session is not read again, and the name is
	// free for the next one.
	self.mutex.Lock()
	delete(self.open, arguments.Session)
	self.mutex.Unlock()
	return &SessionResult{Session: arguments.Session, OK: true}, nil
}

// finish records that the process ended. The session stays open -- it is
// removed when it is closed -- so that the last screen and the code can be
// read by whoever was driving it.
func (self *sessions) finish(held *session, code int) {
	self.mutex.Lock()
	if !held.ended {
		held.ended, held.code = true, code
		close(held.done)
	}
	self.mutex.Unlock()
}

func (self *session) hasEnded() bool {
	select {
	case <-self.done:
		return true
	default:
		return false
	}
}

// waitForEnd reports whether the process ended within the wait.
func (self *session) waitForEnd(wait time.Duration) bool {
	select {
	case <-self.done:
		return true
	case <-time.After(wait):
		return false
	}
}

// closeAll ends every session, for when the connection goes. A process
// started for a conversation that is over has nobody to answer.
func (self *sessions) closeAll() {
	self.mutex.Lock()
	held := make([]*session, 0, len(self.open))
	for _, one := range self.open {
		held = append(held, one)
	}
	self.mutex.Unlock()
	for _, one := range held {
		if one.command == nil {
			continue // reserved, never started
		}
		if one.stdin != nil {
			_ = one.stdin.Close()
		}
		if one.pty != nil {
			_ = one.pty.Close()
		}
		if one.command.Process != nil {
			_ = one.command.Process.Kill()
		}
	}
}

// quoted is a string as a JSON string, for the Data field a session's output
// travels in. The field is raw JSON because a request's answer is; output is
// base64 already, so this only has to put quotes round it.
func quoted(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}
