package computer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/util/security"
)

// Commands that go on running after the call that started them returned.
//
// A command either runs and ends inside its call, or it goes on in the
// background: started that way on purpose (a loop that watches for
// something, a build left to itself), or moved there when the call's wait ran
// out rather than killed, because an hour of a build thrown away at the
// second minute helped nobody. A background command is read while it runs,
// stopped when asked, and when it ends this program says so to the server
// without being asked, so that the agent that started it can be woken.
//
// They are held by the program, not by the connection: a network that drops
// for a minute, or a server that restarts, does not kill a build. What the
// server has not yet acknowledged hearing about is said again when it is
// back, so an ending is not lost in the gap. When the program itself stops,
// they stop with it.

// The bounds on background commands.
const (
	// mostBackgroundRunning is how many run at once. Each is a process
	// group on the person's machine.
	mostBackgroundRunning = 16

	// mostBackgroundKept is how many are remembered, running or ended; the
	// oldest that ended is forgotten first.
	mostBackgroundKept = 64

	// backgroundLifetime is how long one may run before it is stopped. A
	// loop that watches for something that never comes should not outlive
	// the day it was started in.
	backgroundLifetime = 24 * time.Hour

	// backgroundKeptAfterEnd is how long an ended one can still be read.
	backgroundKeptAfterEnd = 24 * time.Hour

	// backgroundStopWait is how long a stopped one is given to end on the
	// polite signal before the whole group is killed.
	backgroundStopWait = 3 * time.Second

	// endedTailBytes is how much of each stream the ending notice carries,
	// which is what the agent is woken with.
	endedTailBytes = 4 << 10

	// Output kept per stream: the first part, which says what the command
	// set out to do, and the last, which says how it is doing or how it
	// ended.
	outputHeadBytes = 64 << 10
	outputTailBytes = outputBytes - outputHeadBytes
)

// Why a background command was ended by somebody other than itself.
const (
	BackgroundStopReasonStopped  = "stopped"
	BackgroundStopReasonLifetime = "lifetime"
)

// BackgroundStatus is one background command as the server is told about it.
type BackgroundStatus struct {
	ID        string          `json:"id"`
	Command   string          `json:"command"`
	Directory string          `json:"directory"`
	Origin    json.RawMessage `json:"origin,omitempty"`
	StartedAt time.Time       `json:"startedAt"`
	EndedAt   *time.Time      `json:"endedAt,omitempty"`
	IsRunning bool            `json:"isRunning"`
	ExitCode  int             `json:"exitCode"`

	// StopReason says it did not end on its own: stopped, or out of
	// lifetime. Empty when it ended by itself or is still running.
	StopReason string `json:"stopReason,omitempty"`

	// IsAcknowledged says the server has heard that it ended.
	IsAcknowledged bool `json:"isAcknowledged"`

	// Output, when it was asked for: what is kept of each stream, and how
	// much each has written in all.
	Stdout            string `json:"stdout,omitempty"`
	Stderr            string `json:"stderr,omitempty"`
	IsStdoutTruncated bool   `json:"isStdoutTruncated,omitempty"`
	IsStderrTruncated bool   `json:"isStderrTruncated,omitempty"`
	StdoutByteCount   int64  `json:"stdoutByteCount"`
	StderrByteCount   int64  `json:"stderrByteCount"`
}

// BackgroundReadArguments ask for one background command with its output:
// the last tailBytes of each stream, or all that is kept when that is zero.
type BackgroundReadArguments struct {
	ID        string `json:"id"`
	TailBytes int    `json:"tailBytes,omitempty"`
}

// BackgroundStopArguments stop one. IsAcknowledged says whoever stopped it
// needs no notice that it ended: the agent stopping its own command knows.
type BackgroundStopArguments struct {
	ID             string `json:"id"`
	IsAcknowledged bool   `json:"isAcknowledged,omitempty"`
}

// BackgroundAcknowledgeArguments say the server has heard these ended.
type BackgroundAcknowledgeArguments struct {
	IDs []string `json:"ids"`
}

// BackgroundCommands are the ones this program holds.
type BackgroundCommands struct {
	mutex    sync.Mutex
	commands map[string]*backgroundCommand

	// notify tells the connected server that one ended; nil while no
	// server is connected. listener names which connection set it, so a
	// connection that ends does not clear its successor's.
	notify   func(*BackgroundStatus)
	listener int64
}

type backgroundCommand struct {
	id        string
	command   string
	directory string
	origin    json.RawMessage
	startedAt time.Time
	process   *exec.Cmd
	stdout    *outputBuffer
	stderr    *outputBuffer

	// stop ends the lifetime context, which kills the whole group.
	stop context.CancelFunc
	done chan struct{}

	// Set when it ends, before done closes.
	endedAt         time.Time
	exitCode        int
	isOutOfLifetime bool

	// Set under the registry's lock.
	stopReason     string
	isAcknowledged bool
}

// NewBackgroundCommands is an empty registry, for a program that keeps one
// across its connections.
func NewBackgroundCommands() *BackgroundCommands {
	return &BackgroundCommands{commands: map[string]*backgroundCommand{}}
}

// listen makes notify the way endings are told, and says again every ending
// not yet acknowledged. It returns what undoes it.
func (self *BackgroundCommands) listen(notify func(*BackgroundStatus)) func() {
	self.mutex.Lock()
	self.listener++
	listener := self.listener
	self.notify = notify
	var unheard []*BackgroundStatus
	for _, held := range self.sortedLocked() {
		if held.hasEnded() && !held.isAcknowledged {
			unheard = append(unheard, held.statusLocked(endedTailBytes))
		}
	}
	self.mutex.Unlock()
	for _, status := range unheard {
		notify(status)
	}
	return func() {
		self.mutex.Lock()
		if self.listener == listener {
			self.notify = nil
		}
		self.mutex.Unlock()
	}
}

// adopt takes a started process into the registry: from here on it runs
// until it ends, is stopped, or its lifetime is up.
func (self *BackgroundCommands) adopt(held *backgroundCommand) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	runningCount := 0
	for _, other := range self.commands {
		if !other.hasEnded() {
			runningCount++
		}
	}
	if runningCount >= mostBackgroundRunning {
		return fmt.Errorf("%d commands are already running in the background on this computer; stop one first", mostBackgroundRunning)
	}
	self.forgetLocked()
	self.commands[held.id] = held
	go func() {
		<-held.done
		self.ended(held)
	}()
	return nil
}

// forgetLocked drops what has ended long ago, and the oldest ended ones
// past the bound. Called with the lock held.
func (self *BackgroundCommands) forgetLocked() {
	now := time.Now()
	for id, held := range self.commands {
		if held.hasEnded() && now.Sub(held.endedAt) > backgroundKeptAfterEnd {
			delete(self.commands, id)
		}
	}
	if len(self.commands) < mostBackgroundKept {
		return
	}
	var ended []*backgroundCommand
	for _, held := range self.commands {
		if held.hasEnded() {
			ended = append(ended, held)
		}
	}
	// Acknowledged ones first, then by when they ended: an ending nobody
	// has heard about yet is the last thing to lose.
	sort.Slice(ended, func(left, right int) bool {
		if ended[left].isAcknowledged != ended[right].isAcknowledged {
			return ended[left].isAcknowledged
		}
		return ended[left].endedAt.Before(ended[right].endedAt)
	})
	for _, held := range ended {
		if len(self.commands) < mostBackgroundKept {
			break
		}
		delete(self.commands, held.id)
	}
}

// ended records how one ended and tells the server, when one is connected.
func (self *BackgroundCommands) ended(held *backgroundCommand) {
	self.mutex.Lock()
	notify := self.notify
	status := held.statusLocked(endedTailBytes)
	self.mutex.Unlock()
	if notify != nil && !status.IsAcknowledged {
		notify(status)
	}
}

func (self *BackgroundCommands) find(id string) (*backgroundCommand, error) {
	held := self.commands[strings.TrimSpace(id)]
	if held == nil {
		return nil, fmt.Errorf("there is no background command %q on this computer", id)
	}
	return held, nil
}

// list is every one kept, newest first, without output.
func (self *BackgroundCommands) list() []*BackgroundStatus {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	listed := []*BackgroundStatus{}
	for _, held := range self.sortedLocked() {
		listed = append(listed, held.statusLocked(-1))
	}
	return listed
}

// read is one, with its output.
func (self *BackgroundCommands) read(arguments *BackgroundReadArguments) (*BackgroundStatus, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	held, err := self.find(arguments.ID)
	if err != nil {
		return nil, err
	}
	return held.statusLocked(max(arguments.TailBytes, 0)), nil
}

// stopOne asks one to end, and kills its group when it will not.
func (self *BackgroundCommands) stopOne(arguments *BackgroundStopArguments) (*BackgroundStatus, error) {
	self.mutex.Lock()
	held, err := self.find(arguments.ID)
	if err != nil {
		self.mutex.Unlock()
		return nil, err
	}
	if arguments.IsAcknowledged {
		held.isAcknowledged = true
	}
	if held.hasEnded() {
		status := held.statusLocked(-1)
		self.mutex.Unlock()
		return status, nil
	}
	held.stopReason = BackgroundStopReasonStopped
	self.mutex.Unlock()

	terminate(held.process)
	select {
	case <-held.done:
	case <-time.After(backgroundStopWait):
		held.stop()
		<-held.done
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return held.statusLocked(-1), nil
}

// acknowledge marks endings the server has heard about.
func (self *BackgroundCommands) acknowledge(arguments *BackgroundAcknowledgeArguments) (*SessionResult, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, id := range arguments.IDs {
		if held := self.commands[id]; held != nil && held.hasEnded() {
			held.isAcknowledged = true
		}
	}
	return &SessionResult{OK: true}, nil
}

// Close kills every one still running, for the program ending.
func (self *BackgroundCommands) Close() {
	self.mutex.Lock()
	var running []*backgroundCommand
	for _, held := range self.commands {
		if !held.hasEnded() {
			held.stopReason = BackgroundStopReasonStopped
			running = append(running, held)
		}
	}
	self.mutex.Unlock()
	for _, held := range running {
		held.stop()
	}
	for _, held := range running {
		select {
		case <-held.done:
		case <-time.After(backgroundStopWait):
		}
	}
}

// sortedLocked is every one, newest first. Called with the lock held.
func (self *BackgroundCommands) sortedLocked() []*backgroundCommand {
	sorted := make([]*backgroundCommand, 0, len(self.commands))
	for _, held := range self.commands {
		sorted = append(sorted, held)
	}
	sort.Slice(sorted, func(left, right int) bool { return sorted[left].startedAt.After(sorted[right].startedAt) })
	return sorted
}

func (self *backgroundCommand) hasEnded() bool {
	select {
	case <-self.done:
		return true
	default:
		return false
	}
}

// statusLocked is the command as the server is told it. tailBytes is how
// much of each stream to include: negative for none, zero for all that is
// kept. Called with the registry's lock held.
func (self *backgroundCommand) statusLocked(tailBytes int) *BackgroundStatus {
	status := &BackgroundStatus{
		ID: self.id, Command: self.command, Directory: self.directory, Origin: self.origin,
		StartedAt: self.startedAt, IsRunning: !self.hasEnded(), StopReason: self.stopReason,
		IsAcknowledged:  self.isAcknowledged,
		StdoutByteCount: self.stdout.byteCount(), StderrByteCount: self.stderr.byteCount(),
	}
	if !status.IsRunning {
		endedAt := self.endedAt
		status.EndedAt, status.ExitCode = &endedAt, self.exitCode
		if status.StopReason == "" && self.isOutOfLifetime {
			status.StopReason = BackgroundStopReasonLifetime
		}
	}
	if tailBytes >= 0 {
		status.Stdout, status.IsStdoutTruncated = self.stdout.tail(tailBytes)
		status.Stderr, status.IsStderrTruncated = self.stderr.tail(tailBytes)
	}
	return status
}

// startCommand runs a command through the person's shell. Its lifetime is
// its own, not the call's: the call decides later whether to wait it out,
// kill it, or let it go on.
func startCommand(options *Options, arguments *ShellArguments) (*backgroundCommand, error) {
	directory := options.Home
	if arguments.Directory != "" {
		directory = resolve(options.Home, arguments.Directory)
	}
	lifetime, stop := context.WithTimeout(context.Background(), backgroundLifetime)
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(lifetime, "cmd", "/C", arguments.Command)
	} else {
		command = exec.CommandContext(lifetime, "/bin/sh", "-c", arguments.Command)
	}
	command.Dir = directory
	command.Env = os.Environ()
	for key, value := range arguments.Environment {
		command.Env = append(command.Env, key+"="+value)
	}
	// When it is killed, whatever it left running is not waited for beyond
	// a moment.
	command.WaitDelay = 2 * time.Second
	prepare(command)
	held := &backgroundCommand{
		id: security.NewULID(), command: arguments.Command, directory: directory, origin: arguments.Origin,
		process: command, stop: stop, done: make(chan struct{}),
		stdout: &outputBuffer{}, stderr: &outputBuffer{},
	}
	command.Stdout, command.Stderr = held.stdout, held.stderr
	held.startedAt = time.Now()
	if err := command.Start(); err != nil {
		stop()
		return nil, fmt.Errorf("the command could not be started: %w", err)
	}
	go func() {
		err := command.Wait()
		code := 0
		var exit *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exit):
			code = exit.ExitCode()
		default:
			code = -1
		}
		// Written before done closes, and read only after: that is what
		// makes them safe to read without a lock.
		held.endedAt, held.exitCode = time.Now(), code
		held.isOutOfLifetime = errors.Is(lifetime.Err(), context.DeadlineExceeded)
		stop()
		close(held.done)
	}()
	return held, nil
}

// outputBuffer keeps the first and the last of what a stream wrote, and how
// much it wrote in all. Safe to read while the command writes.
type outputBuffer struct {
	mutex    sync.Mutex
	head     []byte
	tailRing []byte
	written  int64
}

func (self *outputBuffer) Write(data []byte) (int, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.written += int64(len(data))
	rest := data
	if room := outputHeadBytes - len(self.head); room > 0 {
		taken := min(room, len(rest))
		self.head = append(self.head, rest[:taken]...)
		rest = rest[taken:]
	}
	if len(rest) == 0 {
		return len(data), nil
	}
	// Only the last of one long write can survive it, so a write of any
	// size costs at most twice the tail.
	if len(rest) > outputTailBytes {
		rest = rest[len(rest)-outputTailBytes:]
	}
	self.tailRing = append(self.tailRing, rest...)
	if excess := len(self.tailRing) - outputTailBytes; excess > 0 {
		// Copied rather than resliced, so the dropped bytes are let go.
		kept := make([]byte, outputTailBytes, 2*outputTailBytes)
		copy(kept, self.tailRing[excess:])
		self.tailRing = kept
	}
	return len(data), nil
}

func (self *outputBuffer) byteCount() int64 {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.written
}

// text is all that is kept, with a line where the middle was left out, and
// whether anything was.
func (self *outputBuffer) text() (string, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	kept := int64(len(self.head) + len(self.tailRing))
	if kept == self.written {
		return string(self.head) + string(self.tailRing), false
	}
	return fmt.Sprintf("%s\n[%d bytes left out here]\n%s", self.head, self.written-kept, self.tailRing), true
}

// tail is the last most bytes written, or all that is kept when most is
// zero, and whether that is less than everything.
func (self *outputBuffer) tail(most int) (string, bool) {
	if most == 0 {
		return self.text()
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	joined := append(append([]byte{}, self.head...), self.tailRing...)
	isTruncated := int64(len(joined)) < self.written
	if len(self.tailRing) > 0 && int64(len(joined)) < self.written {
		// The head and the tail are not contiguous; only the tail is the
		// end of the stream.
		joined = self.tailRing
	}
	if len(joined) > most {
		joined = joined[len(joined)-most:]
		isTruncated = true
	}
	return string(joined), isTruncated
}
