package api

import (
	"errors"
	"os"
	"sort"
	"sync"
)

// ErrRestartUnavailable is returned when the API is asked to restart a server
// that has nothing to start it again.
var ErrRestartUnavailable = errors.New("api: this server cannot restart itself")

// Supervision is what is expected to happen when the process exits.
type Supervision string

const (
	// SupervisionContainer means the process is the entry point of a
	// container. Whether it comes back is the container's restart policy,
	// which cannot be read from in here — but a mail server in a container
	// almost always has one.
	SupervisionContainer Supervision = "container"

	// SupervisionSystemd means systemd started this. Whether it comes back is
	// the unit's Restart=, which cannot be read from in here either.
	SupervisionSystemd Supervision = "systemd"

	// SupervisionUnknown means nothing recognisable started this, so exiting
	// most likely leaves the server down. A development server run from a
	// shell looks like this.
	SupervisionUnknown Supervision = "unknown"
)

// Restarter ends the process so that whatever supervises it starts a new one.
//
// There is no way to restart in place: the point of restarting is to build
// everything again from configuration that has changed, and the parts that
// have to be rebuilt are the listeners, the certificates and the connections
// to the object store and the scanners. Exiting is the honest version of that.
type Restarter struct {
	mutex     sync.Mutex
	requested bool
	trigger   func()

	// pending names the settings that have changed since this process
	// started and that it will not pick up until it does.
	pending []string
}

// NewRestarter returns one that ends the process by calling trigger, which is
// expected to begin a graceful shutdown.
func NewRestarter(trigger func()) *Restarter {
	return &Restarter{trigger: trigger}
}

// Supervision guesses what will happen when this process exits.
//
// A guess, and named as one. Neither a container's restart policy nor a
// systemd unit's Restart= can be read from inside the process, so the most
// this can say is which supervisor is in charge. It exists so the dashboard
// can warn the one operator for whom restarting means staying down: somebody
// running the server from a shell.
//
// It errs towards "unknown", which is the direction that warns rather than
// reassures. Being told a restart might not come back when it would is a
// moment's hesitation; the opposite is a mail server that is down and nobody
// expecting it to be.
func (self *Restarter) Supervision() Supervision {
	// PID 1 outside a container is init, which is not running a mail server.
	// Inside one it is the entry point, which is what this is.
	if os.Getpid() == 1 {
		return SupervisionContainer
	}
	// A container that runs an init process — tini, dumb-init, s6 — leaves
	// this somewhere other than PID 1, but the file is still there.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return SupervisionContainer
	}

	// INVOCATION_ID alone is not enough. systemd sets it for every unit, and
	// it is inherited: a terminal is itself a unit, so every command typed
	// into one has it, including a server run by hand for an afternoon.
	// Requiring the parent to be PID 1 as well is what separates a service
	// systemd started from a process that merely descends from one.
	if _, ok := os.LookupEnv("INVOCATION_ID"); ok && os.Getppid() == 1 {
		return SupervisionSystemd
	}
	return SupervisionUnknown
}

// Request begins a restart, and reports whether this call was the one that
// started it.
//
// Repeat calls are not an error. Two operators pressing the button at the same
// moment should get the same answer as one of them pressing it twice, rather
// than one of them getting a failure for a restart that is happening.
func (self *Restarter) Request() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.requested {
		return false
	}
	self.requested = true
	go self.trigger()
	return true
}

// Requested reports whether a restart is already under way.
func (self *Restarter) Requested() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.requested
}

// AddPending records settings that have changed and that this process will
// not pick up until it restarts. Called by whatever watches the configuration.
//
// They accumulate rather than replace. Two changes made an hour apart are two
// reasons to restart, and reporting only the second would have the dashboard
// understate what is out of date.
func (self *Restarter) AddPending(names ...string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, name := range names {
		if !contains(self.pending, name) {
			self.pending = append(self.pending, name)
		}
	}
	sort.Strings(self.pending)
}

func contains(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

// Pending returns those names, so the dashboard can say why it is offering a
// restart rather than leaving the operator to wonder.
func (self *Restarter) Pending() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string(nil), self.pending...)
}
