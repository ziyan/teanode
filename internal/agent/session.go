package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/util/security"
)

// Sessions: a program on a device that stays open.
//
// Everything else a device does is one request to one answer, which is all
// the shell and the filesystem ever needed. A program that stays open needs
// two things that shape does not have -- output arriving when the program
// feels like it, and input written to something started earlier -- so the
// messages about a session carry its own identifier rather than a request
// number, and arrive unasked.
//
// What is kept here is what arrived: a bounded buffer per session, read by
// whoever is driving it. Bounded because a program that prints forever must
// not be a way to fill this server's memory from somebody's own machine.

const (
	// sessionBuffer is how much of one session's output is kept. What a
	// reader has taken is dropped, so this bounds what nobody has read yet,
	// not the total a session may produce.
	sessionBuffer = 256 << 10

	// sessionIdle is how long a session with nobody reading it is left
	// before it is closed. A turn that ended without closing one should not
	// leave a process running on somebody's machine.
	sessionIdle = 30 * time.Minute

	// sessionsPerDevice bounds how many one device holds at once, matching
	// what the device itself will accept.
	sessionsPerDevice = 16
)

// deviceSession is one open program, as this side sees it.
type deviceSession struct {
	id   string
	kind string

	mutex   sync.Mutex
	output  []byte
	dropped int
	ended   bool
	code    int
	touched time.Time
	waiting chan struct{}
}

// take returns what has arrived since the last take, and empties it.
func (self *deviceSession) take() ([]byte, int, bool, int) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	taken, dropped := self.output, self.dropped
	self.output, self.dropped = nil, 0
	return taken, dropped, self.ended, self.code
}

// arrived adds output, dropping the oldest when the buffer is full: what a
// program said most recently is what somebody driving it wants.
func (self *deviceSession) arrived(data []byte) {
	self.mutex.Lock()
	self.touched = time.Now()
	self.output = append(self.output, data...)
	if excess := len(self.output) - sessionBuffer; excess > 0 {
		self.output = self.output[excess:]
		self.dropped += excess
	}
	self.wake()
	self.mutex.Unlock()
}

func (self *deviceSession) finished(code int) {
	self.mutex.Lock()
	self.ended, self.code, self.touched = true, code, time.Now()
	self.wake()
	self.mutex.Unlock()
}

// wake releases anybody waiting for something to happen. Called with the
// lock held.
func (self *deviceSession) wake() {
	if self.waiting != nil {
		close(self.waiting)
		self.waiting = nil
	}
}

// settled waits until something arrives or the wait is over, so that a
// reader need not poll.
func (self *deviceSession) settled(ctx context.Context, wait time.Duration) {
	self.mutex.Lock()
	if len(self.output) > 0 || self.ended {
		self.mutex.Unlock()
		return
	}
	if self.waiting == nil {
		self.waiting = make(chan struct{})
	}
	channel := self.waiting
	self.mutex.Unlock()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-channel:
	case <-timer.C:
	case <-ctx.Done():
	}
}

// StartSession opens a program on the device and returns its identifier.
func (self *deviceLink) StartSession(ctx context.Context, kind, command string, arguments []string,
	directory string, environment map[string]string) (*deviceSession, error) {
	self.mutex.Lock()
	open := len(self.sessions)
	self.mutex.Unlock()
	if open >= sessionsPerDevice {
		return nil, fmt.Errorf("%s already has %d sessions open", self.what, open)
	}

	id := security.NewULID()
	held := &deviceSession{id: id, kind: kind, touched: time.Now()}
	// Kept before the request goes, because output can arrive before the
	// answer to the request that started it does -- a program that greets
	// is faster than a round trip.
	self.mutex.Lock()
	if self.sessions == nil {
		self.sessions = map[string]*deviceSession{}
	}
	self.sessions[id] = held
	self.mutex.Unlock()

	if _, err := self.Ask(ctx, "session_start", &computer.SessionStartArguments{
		Session: id, Kind: kind, Command: command, Arguments: arguments,
		Directory: directory, Environment: environment,
	}); err != nil {
		self.forgetSession(id)
		return nil, err
	}
	return held, nil
}

// WriteSession writes to an open session's input.
func (self *deviceLink) WriteSession(ctx context.Context, id string, data []byte) error {
	_, err := self.Ask(ctx, "session_write", &computer.SessionWriteArguments{
		Session: id, Data: base64.StdEncoding.EncodeToString(data),
	})
	return err
}

// SignalSession sends a signal to one.
func (self *deviceLink) SignalSession(ctx context.Context, id, signal string) error {
	_, err := self.Ask(ctx, "session_signal", &computer.SessionSignalArguments{Session: id, Signal: signal})
	return err
}

// CloseSession ends one and forgets it.
func (self *deviceLink) CloseSession(ctx context.Context, id string) error {
	defer self.forgetSession(id)
	_, err := self.Ask(ctx, "session_close", &computer.SessionCloseArguments{Session: id})
	return err
}

// Session is an open one by its identifier.
func (self *deviceLink) Session(id string) *deviceSession {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.sessions[id]
}

func (self *deviceLink) forgetSession(id string) {
	self.mutex.Lock()
	delete(self.sessions, id)
	self.mutex.Unlock()
}

// sessionSaid is the device saying something about a session without being
// asked: output, or that it finished.
func (self *deviceLink) sessionSaid(id, event, stream, data string, code int) {
	self.mutex.Lock()
	held := self.sessions[id]
	self.mutex.Unlock()
	if held == nil {
		// A session this side has forgotten -- closed, or belonging to a
		// connection that went. Nothing to keep it in.
		return
	}
	switch event {
	case "output":
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(data))
		if err != nil {
			log.Warningf("unreadable output from session %s: %s", id, err)
			return
		}
		held.arrived(decoded)
	case "ended":
		held.finished(code)
	}
}

// ComputerSessionSaid routes what a computer said about a session.
func (self *Agent) ComputerSessionSaid(agentId string, connection DeviceConnection, id, event, stream, data string, code int) {
	self.computersMutex.Lock()
	var found *attachedComputer
	for _, computer := range self.computers[agentId] {
		if computer.connection == connection {
			found = computer
			break
		}
	}
	self.computersMutex.Unlock()
	if found != nil {
		found.sessionSaid(id, event, stream, data, code)
	}
}

// sweepSessions closes the ones nobody has touched in a while.
func (self *Agent) sweepSessions() {
	self.computersMutex.Lock()
	var stale []struct {
		computer *attachedComputer
		id       string
	}
	for _, computers := range self.computers {
		for _, computer := range computers {
			computer.mutex.Lock()
			for id, held := range computer.sessions {
				held.mutex.Lock()
				idle := time.Since(held.touched) > sessionIdle
				held.mutex.Unlock()
				if idle {
					stale = append(stale, struct {
						computer *attachedComputer
						id       string
					}{computer, id})
				}
			}
			computer.mutex.Unlock()
		}
	}
	self.computersMutex.Unlock()

	for _, one := range stale {
		log.Noticef("closing session %s: nothing has touched it in %s", one.id, sessionIdle)
		ctx, cancel := context.WithTimeout(context.Background(), deviceAnswerWait)
		_ = one.computer.CloseSession(ctx, one.id)
		cancel()
	}
}

// DecodedSessionData is what a session message carries, for the websocket
// handler that has it as raw JSON.
func DecodedSessionData(data json.RawMessage) string {
	var text string
	if len(data) == 0 {
		return ""
	}
	if err := json.Unmarshal(data, &text); err != nil {
		return ""
	}
	return text
}
