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
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/passwords"
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
	// mostBackgroundKept is how many are remembered, running or ended; the
	// oldest that ended is forgotten first.
	mostBackgroundKept = 64

	// backgroundKeptAfterEnd is how long an ended one can still be read.
	backgroundKeptAfterEnd = 24 * time.Hour

	// backgroundStopWait is how long a stopped one is given to end on the
	// polite signal before the whole group is killed.
	backgroundStopWait = 3 * time.Second

	// endedTailBytes is how much of each stream the ending notice carries,
	// which is what the agent is woken with.
	endedTailBytes = 4 << 10

	// What an answer carries of a stream that wrote more than outputBytes:
	// the first part, which says what the command set out to do, and the
	// last, which says how it is doing or how it ended. The rest is read by
	// offset.
	outputHeadBytes = 64 << 10
	outputTailBytes = outputBytes - outputHeadBytes
)

// Why a background command was ended by somebody other than itself:
// stopped when asked (by the person, or by the agent, which then needs no
// word of it), or with this program as it shut down.
const (
	BackgroundStopReasonStopped  = "stopped"
	BackgroundStopReasonShutdown = "shutdown"
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

	// StopReason says it did not end on its own: stopped, or ended with the
	// program. Empty when it ended by itself or is still running.
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

	// When one stream was read by offset (BackgroundReadArguments.Stream):
	// which, where this read began, and where the next begins. Stdout or
	// Stderr then holds just that part; IsStdoutTruncated and
	// IsStderrTruncated say whether more follows it.
	Stream           string `json:"stream,omitempty"`
	StreamOffset     int64  `json:"streamOffset,omitempty"`
	StreamNextOffset int64  `json:"streamNextOffset,omitempty"`
}

// BackgroundReadArguments ask for one background command with its output:
// the last tailBytes of each stream, or what text gives when that is zero.
// With Stream (stdout or stderr), the bytes of that stream from Offset
// instead, LengthBytes of them or readBytesDefault when that is zero; the
// whole output is there to be read this way.
type BackgroundReadArguments struct {
	ID          string `json:"id"`
	TailBytes   int    `json:"tailBytes,omitempty"`
	Stream      string `json:"stream,omitempty"`
	Offset      int64  `json:"offset,omitempty"`
	LengthBytes int    `json:"lengthBytes,omitempty"`
}

// readBytesDefault is how much of a stream one read by offset gives when it
// does not say.
const readBytesDefault = 32 << 10

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

	// stop ends the command's context, which kills the whole group.
	stop context.CancelFunc
	done chan struct{}

	// Set when it ends, before done closes.
	endedAt  time.Time
	exitCode int

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
// until it ends or is stopped. A running one is never refused; room is made
// by forgetting an ended one.
func (self *BackgroundCommands) adopt(held *backgroundCommand) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.forgetLocked()
	self.commands[held.id] = held
	go func() {
		<-held.done
		self.ended(held)
	}()
}

// keepEnded takes one that has already ended into the registry, so that its
// output can be read by id; nobody is told about it, since the call that
// ran it has its ending.
func (self *BackgroundCommands) keepEnded(held *backgroundCommand) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.forgetLocked()
	held.isAcknowledged = true
	self.commands[held.id] = held
}

// forgetLocked drops what has ended long ago, and the oldest ended ones
// past the bound. Called with the lock held.
func (self *BackgroundCommands) forgetLocked() {
	now := time.Now()
	for id, held := range self.commands {
		if held.hasEnded() && now.Sub(held.endedAt) > backgroundKeptAfterEnd {
			delete(self.commands, id)
			held.discard()
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
		held.discard()
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
	stream := strings.ToLower(strings.TrimSpace(arguments.Stream))
	if stream == "" {
		return held.statusLocked(max(arguments.TailBytes, 0)), nil
	}
	if stream != "stdout" && stream != "stderr" {
		return nil, fmt.Errorf("%q is not stdout or stderr", arguments.Stream)
	}
	status := held.statusLocked(-1)
	buffer := held.stdout
	if stream == "stderr" {
		buffer = held.stderr
	}
	length := arguments.LengthBytes
	if length <= 0 {
		length = readBytesDefault
	}
	// The buffer has its own lock, taken inside the registry's.
	chunk, next := buffer.readFrom(arguments.Offset, length)
	isMore := next < buffer.byteCount()
	status.Stream, status.StreamOffset, status.StreamNextOffset = stream, min(max(arguments.Offset, 0), buffer.byteCount()), next
	if stream == "stdout" {
		status.Stdout, status.IsStdoutTruncated = chunk, isMore
	} else {
		status.Stderr, status.IsStderrTruncated = chunk, isMore
	}
	return status, nil
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
			held.stopReason = BackgroundStopReasonShutdown
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
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, held := range self.commands {
		held.discard()
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

// discard lets go of the output held on disk.
func (self *backgroundCommand) discard() {
	self.stdout.discard()
	self.stderr.discard()
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
	lifetime, stop := context.WithCancel(context.Background())
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(lifetime, "cmd", "/C", arguments.Command)
	} else {
		command = exec.CommandContext(lifetime, "/bin/sh", "-c", arguments.Command)
	}
	command.Dir = directory
	command.Env = agentEnvironment(arguments.Environment)
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
		stop()
		close(held.done)
	}()
	return held, nil
}

// outputBuffer keeps everything a stream wrote, so that all of it can be
// read back by offset. It holds the first outputBytes in memory, which is all
// a command that prints little ever needs, and moves to a temporary file when
// there is more. Safe to read while the command writes.
type outputBuffer struct {
	mutex   sync.Mutex
	memory  []byte
	file    *os.File
	written int64
}

func (self *outputBuffer) Write(data []byte) (int, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.file == nil && len(self.memory)+len(data) > outputBytes {
		// Without a file to spill to, it stays in memory: losing output is
		// the worse of the two.
		if file, err := os.CreateTemp("", "teanode-output-*"); err == nil {
			if _, err := file.Write(self.memory); err == nil {
				self.file, self.memory = file, nil
			} else {
				_ = file.Close()
				_ = os.Remove(file.Name())
			}
		}
	}
	if self.file != nil {
		if _, err := self.file.Write(data); err != nil {
			return 0, err
		}
	} else {
		self.memory = append(self.memory, data...)
	}
	self.written += int64(len(data))
	return len(data), nil
}

func (self *outputBuffer) byteCount() int64 {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.written
}

// readLocked is up to length bytes from offset. Called with the lock held.
func (self *outputBuffer) readLocked(offset int64, length int) []byte {
	offset = min(max(offset, 0), self.written)
	length = int(min(int64(max(length, 0)), self.written-offset))
	if self.file == nil {
		return append([]byte{}, self.memory[offset:offset+int64(length)]...)
	}
	read := make([]byte, length)
	count, _ := self.file.ReadAt(read, offset)
	return read[:count]
}

// readFrom is up to length bytes from offset, ending on a character
// boundary, and the offset to read on from.
func (self *outputBuffer) readFrom(offset int64, length int) (string, int64) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	offset = min(max(offset, 0), self.written)
	read := self.readLocked(offset, length)
	if offset+int64(len(read)) < self.written {
		// Not at the end: do not end in the middle of a character.
		for cut := len(read) - 1; cut >= 0 && cut >= len(read)-utf8.UTFMax; cut-- {
			if utf8.RuneStart(read[cut]) {
				if !utf8.FullRune(read[cut:]) {
					read = read[:cut]
				}
				break
			}
		}
	}
	return strings.ToValidUTF8(string(read), "\uFFFD"), offset + int64(len(read))
}

// text is what an answer carries: all of it when it is short, and else the
// first part and the last, with a line saying how many bytes lie between
// (readable by offset), and whether anything was left out.
func (self *outputBuffer) text() (string, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.written <= outputBytes {
		return string(self.readLocked(0, int(self.written))), false
	}
	head := self.readLocked(0, outputHeadBytes)
	tail := self.readLocked(self.written-outputTailBytes, outputTailBytes)
	return fmt.Sprintf("%s\n[%d bytes left out here, from byte %d; the command can be read by its id with an offset]\n%s",
		head, self.written-int64(len(head))-int64(len(tail)), len(head), tail), true
}

// tail is the last most bytes written, or what text gives when most is zero,
// and whether that is less than everything.
func (self *outputBuffer) tail(most int) (string, bool) {
	if most == 0 {
		return self.text()
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	start := max(self.written-int64(most), 0)
	return string(self.readLocked(start, most)), start > 0
}

// discard lets go of the file the output moved to.
func (self *outputBuffer) discard() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.file != nil {
		_ = self.file.Close()
		_ = os.Remove(self.file.Name())
		self.file = nil
	}
}

// agentEnvironment is the environment of a command the agent runs: this
// program's own, then what the call added, then the mark that says the agent
// ran it (passwords.AgentCommandEnvironmentName), which comes last so that the
// call cannot unset it. The Passwords commands refuse to open Passwords when
// they see the mark.
func agentEnvironment(added map[string]string) []string {
	environment := os.Environ()
	for key, value := range added {
		environment = append(environment, key+"="+value)
	}
	return append(environment, passwords.AgentCommandEnvironmentName+"=1")
}
