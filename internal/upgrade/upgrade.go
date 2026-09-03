package upgrade

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/periodic"
	"github.com/ziyan/teanode/internal/version"
)

var log = logging.MustGetLogger("upgrade")

// ErrNotApplicable is returned when this deployment cannot upgrade itself.
// The reason is on the Status, because a refusal that does not say why reads
// as a bug.
var ErrNotApplicable = errors.New("upgrade: this deployment cannot upgrade itself")

// Status is what is running, what is available, and whether anything can be
// done about the difference.
type Status struct {
	// Current is the running version, and Latest the newest release. Latest
	// is empty until a check has succeeded.
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`

	// Available says Latest is newer than Current and is a release this
	// server would install.
	Available bool `json:"available"`

	// Notes are the release notes for Latest, as written in the changelog.
	Notes string `json:"notes,omitempty"`

	// CheckedAt is when the release list was last read successfully, and
	// Error why the last attempt did not. Both are shown: "it has not managed
	// to check since Tuesday" is the thing an operator needs to be told, and
	// an error that is only logged is an error nobody sees.
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	Error     string     `json:"error,omitempty"`

	// Applicable says whether an upgrade could be applied here at all, and
	// Reason says what stands in the way when it cannot: a container, whose
	// image is the thing to replace, or a process nothing would start again.
	Applicable bool   `json:"applicable"`
	Reason     string `json:"reason,omitempty"`

	// Automatic says upgrades are installed without being asked.
	Automatic bool `json:"automatic"`

	// Upgrading says one is running now. It stays true through the restart,
	// because the process that would set it back does not survive one.
	Upgrading bool `json:"upgrading"`
}

// Manager knows what has been released and can replace this server with it.
type Manager interface {
	// Status is what is known now, without asking anybody.
	Status() Status

	// Check asks the release list and returns what it found.
	Check(ctx context.Context) (Status, error)

	// CheckSoon asks in the background and returns at once, for a caller that
	// must not wait — a GraphQL request, which runs inside a database
	// transaction that a thirty-second call to somebody else's endpoint has
	// no business holding open.
	CheckSoon()

	// Apply downloads the newest release, verifies it, replaces this binary
	// and restarts. It returns when the restart has been requested, not when
	// the new process is running: there is no new process to hear from here.
	Apply(ctx context.Context) error

	// Start begins an upgrade in the background and returns as soon as it has
	// been accepted, with whatever is known now.
	//
	// It exists because the API request that asks for one is wrapped in a
	// database transaction, and a forty-five megabyte download is not
	// something to hold a transaction open for: on a deployment with
	// idle_in_transaction_session_timeout the session is killed part way
	// through and the caller is told the upgrade failed, after the binary has
	// already been replaced.
	Start() (Status, error)

	Close() error
}

type manager struct {
	config     config.Store
	restarter  *api.Restarter
	repository string
	client     *http.Client

	// executable is the path to replace, resolved once at startup. Resolved
	// then rather than at upgrade time because a process whose binary has
	// been moved under it should fail the same way every time.
	executable string

	// checkInterval is how often the release list may actually be asked,
	// which is not how often the loop wakes.
	checkInterval time.Duration

	// endpoint is where the release list is read from, and applicable decides
	// whether this deployment may replace itself. Both are fields rather than
	// constants so that a test can point them elsewhere: everything else in
	// here can be exercised offline, and these two are the reason it could
	// not be.
	endpoint   string
	applicable func() (bool, string)

	mutex  sync.RWMutex
	status Status

	// lastAttempt is when the release list was last asked, successfully or
	// not, and failures and nextAttempt hold off an upgrade that keeps
	// failing.
	lastAttempt time.Time
	failures    int
	nextAttempt time.Time
	refusalSaid string

	// checking is held while a background check is in flight, so that a
	// dashboard being clicked repeatedly asks once.
	checking sync.Mutex

	// applying is held for the whole of an upgrade, download included. Two
	// at once would each swap the binary, and the second would keep the
	// first's new binary as the rollback copy — losing the one an operator
	// would actually want back. Two operators pressing the button, or the
	// schedule firing while somebody presses it, is the ordinary way that
	// happens.
	applying sync.Mutex

	waitGroup sync.WaitGroup
	loop      periodic.Periodic
	ctx       context.Context
	cancel    context.CancelFunc
}

// newClient is the one used for the release list and the download.
//
// No Timeout: the deadline belongs to each request's context, and a client
// timeout silently wins over it — five minutes here quietly cut a
// forty-megabyte download on any link slower than about 150 KB/s, for ever,
// with a re-download every cycle.
//
// Redirects stay on https. A release asset is served from a redirect, and the
// thing at the end of it is executed as the user that receives mail: one
// cleartext hop is enough for somebody on the path to hand over both the
// binary and the checksums that would have caught it.
func newClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" {
				return fmt.Errorf("upgrade: refusing a redirect to %s", request.URL.Scheme)
			}
			if len(via) >= 10 {
				return fmt.Errorf("upgrade: too many redirects")
			}
			return nil
		},
	}
}

// New builds the manager. It does not reach the network; the first check
// happens on the loop.
func New(configuration config.Store, restarter *api.Restarter) (Manager, error) {
	executable, err := os.Executable()
	if err != nil {
		// Not fatal. A server that cannot find its own binary can still say
		// what has been released, which is most of the value here.
		log.Warningf("cannot find this executable, so upgrades will be refused: %s", err)
		executable = ""
	} else if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		// The path somebody would replace, not the symlink pointing at it.
		executable = resolved
	}

	self := &manager{
		config:     configuration,
		restarter:  restarter,
		repository: Repository,
		endpoint:   fmt.Sprintf(releaseEndpoint, Repository),
		client:     newClient(),
		executable: executable,
		status: Status{
			Current: version.Version(),
		},
	}
	self.ctx, self.cancel = context.WithCancel(context.Background())

	self.applicable = self.checkApplicable

	applicable, reason := self.applicableNow()
	self.status.Applicable = applicable
	self.status.Reason = reason

	settings := configuration.Current().Upgrade
	if !settings.Enabled {
		log.Noticef("not checking for releases: upgrade.enabled is off")
		return self, nil
	}

	self.checkInterval = settings.CheckInterval.Duration()

	// The loop wakes far more often than it asks anybody anything, and the
	// two are separate on purpose. A check every six hours happens four times
	// a day at whatever times the process started at; an upgrade window two
	// hours wide would then be hit or missed depending on that phase, and
	// missing it means automatic upgrades silently never happen. Waking every
	// few minutes and deciding each time — with the release list asked only
	// when checkInterval has passed — costs nothing and honours the window.
	interval := self.checkInterval
	if interval > tick {
		interval = tick
	}
	self.loop = periodic.New(self.ctx, &self.waitGroup, self.spinOnce, &periodic.Settings{
		Interval: interval,
		Name:     "upgrade",
	})
	self.loop.Start()
	return self, nil
}

func (self *manager) Close() error {
	if self.loop != nil {
		self.loop.Stop()
	}
	self.cancel()
	self.waitGroup.Wait()
	return nil
}

// currentVersion is the running version, read without the configuration:
// Apply needs it, and the settings it would otherwise pull in have nothing to
// do with which version this is.
func (self *manager) currentVersion() string {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.status.Current
}

func (self *manager) Status() Status {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.describe()
}

// describe is the status as a caller should see it, with the settings folded
// in. Held by the caller's lock.
//
// One function rather than two, because there were two: Check returned the
// stored status directly and therefore always said automatic upgrades were
// off, while Status said the truth. The dashboard hid it by re-reading after
// a check; the command line did not.
func (self *manager) describe() Status {
	status := self.status
	if self.config != nil {
		status.Automatic = self.config.Current().Upgrade.Automatic
	}
	return status
}

// tick is how often the loop wakes when a check is not due. Short enough that
// the narrowest window anybody would write is hit, long enough to be nothing.
const tick = 5 * time.Minute

// attemptBackoff is how long to wait after an automatic upgrade fails,
// doubling to attemptBackoffMax. The same shape as the certificate manager's,
// and for the same reason: the ordinary failure is one that will keep
// failing, and the loop wakes far more often than it is worth retrying.
const (
	attemptBackoff    = 5 * time.Minute
	attemptBackoffMax = 24 * time.Hour
)

// spinOnce is the scheduled half: look if a look is due, and install if that
// is what this deployment was told to do.
// The context is ignored on purpose: periodic hands its handler a background
// context, so a stop would otherwise wait out a download that is minutes from
// finishing. The manager's own context is the one Close cancels.
func (self *manager) spinOnce(_ context.Context) error {
	ctx := self.ctx
	status := self.Status()

	if self.checkDue() {
		var err error
		status, err = self.Check(ctx)
		if err != nil {
			// Said once at warning, not once a cycle at error: a server
			// behind a firewall that blocks this would otherwise fill a log
			// with something nobody can act on.
			log.Warningf("could not check for a release: %s", err)
			return nil
		}
		if status.Available {
			log.Noticef("version %s is available; this server is running %s", status.Latest, status.Current)
		}
	}

	if !status.Available {
		return nil
	}

	settings := self.config.Current().Upgrade
	if !settings.Automatic {
		return nil
	}
	if !status.Applicable {
		// Once, not once a tick. A container with automatic upgrades turned
		// on is an easy thing to have configured before discovering that a
		// container is refused, and it would otherwise say so every five
		// minutes for the life of the process.
		self.sayOnce(fmt.Sprintf("not installing %s automatically: %s", status.Latest, status.Reason))
		return nil
	}
	if !withinWindow(settings.Window, time.Now()) {
		// At debug, because this is the ordinary answer for most of the day
		// once a window is set: an hourly notice saying "not yet" is a log
		// nobody reads.
		log.Debugf("not installing %s yet: outside upgrade.window %q", status.Latest, settings.Window)
		return nil
	}

	// A restart has been asked for, so this process is on its way out and the
	// upgrade that asked for it has already swapped the binary. Doing it
	// again would link the rollback copy to the binary just installed, which
	// is the copy nobody wants back.
	if self.restarter != nil && self.restarter.Requested() {
		return nil
	}

	if wait, ok := self.attemptTooSoon(); !ok {
		log.Debugf("not retrying the upgrade to %s for another %s", status.Latest, wait.Round(time.Minute))
		return nil
	}

	log.Noticef("installing %s automatically", status.Latest)
	if err := self.Apply(ctx); err != nil {
		// Backed off, because the loop wakes every five minutes and the
		// ordinary failure here is one that will keep failing: a checksum
		// that does not match, a release with no asset for this platform, a
		// link that keeps dropping. Without this it downloads forty-five
		// megabytes, throws it away, and does it again — three hundred times
		// a day, for ever.
		self.failed()
		log.Errorf("automatic upgrade to %s failed: %s", status.Latest, err)
		return nil
	}
	self.succeeded()
	return nil
}

// Start begins an upgrade and returns immediately.
func (self *manager) Start() (Status, error) {
	if applicable, reason := self.applicableNow(); !applicable {
		return self.Status(), fmt.Errorf("%w: %s", ErrNotApplicable, reason)
	}

	// The lock is taken here and released by the goroutine, so it is a
	// reservation rather than a question. Taking it and letting it go before
	// the goroutine starts let two requests both pass — and the loser then
	// wrote "an upgrade is already running" onto the status while the winner
	// was downloading, so the dashboard reported a failure for an upgrade
	// that was about to restart the server.
	if !self.applying.TryLock() {
		return self.Status(), fmt.Errorf("upgrade: an upgrade is already running")
	}

	// Marked here as well as in apply, so that the status says so before this
	// request is answered rather than whenever the goroutine gets to run.
	self.mutex.Lock()
	self.status.Upgrading = true
	self.status.Error = ""
	self.mutex.Unlock()

	go func() {
		defer self.applying.Unlock()

		// The manager's context, not the request's: the request is answered
		// before this finishes, and its context is cancelled the moment it
		// is. apply puts the status back if it fails; a success is followed
		// by the restart, which this process does not return from.
		if err := self.apply(self.ctx); err != nil {
			log.Errorf("upgrade failed: %s", err)
		}
	}()

	return self.Status(), nil
}

// attemptTooSoon reports whether an automatic attempt may run now, and how
// long is left if not.
func (self *manager) attemptTooSoon() (time.Duration, bool) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	if self.failures == 0 || time.Now().After(self.nextAttempt) {
		return 0, true
	}
	return time.Until(self.nextAttempt), false
}

// failed records an automatic attempt that did not work, and puts the next one
// off: five minutes, doubling to a day.
func (self *manager) failed() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.failures++
	wait := attemptBackoff << min(self.failures-1, 16)
	if wait > attemptBackoffMax {
		wait = attemptBackoffMax
	}
	self.nextAttempt = time.Now().Add(wait)
	log.Warningf("not retrying the upgrade for %s (%d failures)", wait, self.failures)
}

func (self *manager) succeeded() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.failures = 0
	self.nextAttempt = time.Time{}
}

// sayOnce logs a message the first time it is said, and not again until it
// changes. For the things the loop notices every tick and that stay true.
func (self *manager) sayOnce(message string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.refusalSaid == message {
		return
	}
	self.refusalSaid = message
	log.Warning(message)
}

// CheckSoon asks in the background, once at a time.
func (self *manager) CheckSoon() {
	if !self.checking.TryLock() {
		return
	}
	go func() {
		defer self.checking.Unlock()
		if _, err := self.Check(self.ctx); err != nil {
			log.Warningf("could not check for a release: %s", err)
		}
	}()
}

// checkDue reports whether enough time has passed to ask again.
//
// Measured from the last attempt, not the last success. Measuring from
// success meant that a server which cannot reach the release list at all —
// outbound HTTPS blocked, which is an ordinary way to run a mail server —
// asked again every five minutes and logged a warning every time.
func (self *manager) checkDue() bool {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	if self.lastAttempt.IsZero() {
		return true
	}
	return time.Since(self.lastAttempt) >= self.checkInterval
}

// Check reads the release list and remembers what it found.
func (self *manager) Check(ctx context.Context) (Status, error) {
	found, err := latestRelease(ctx, self.client, self.endpoint)

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.lastAttempt = time.Now()

	// Whether this deployment could apply one is asked again here rather than
	// only at startup: a directory that was briefly unwritable at boot should
	// not hide the button for ever, and one that has since been remounted
	// read-only should not keep offering it.
	applicable, reason := self.applicableNow()
	self.status.Applicable = applicable
	self.status.Reason = reason

	if err != nil {
		self.status.Error = err.Error()
		return self.describe(), err
	}

	now := time.Now()
	self.status.Error = ""
	self.status.CheckedAt = &now
	self.status.Latest = found.version()
	self.status.Notes = found.Notes
	self.status.Available = isUpgrade(self.status.Current, self.status.Latest)
	return self.describe(), nil
}

// applicableNow asks the question through whatever seam is in place, and
// answers it directly when there is none. A function field that is nil until a
// constructor fills it in is a panic waiting for the first caller who builds
// the struct another way — which is what happened, in a test.
func (self *manager) applicableNow() (bool, string) {
	if self.applicable != nil {
		return self.applicable()
	}
	return self.checkApplicable()
}

// checkApplicable answers whether an upgrade could be applied at all, and why
// not.
//
// Asked at startup and shown on the page, so that the button is absent with a
// reason beside it rather than present and disappointing.
func (self *manager) checkApplicable() (bool, string) {
	// Nothing to ask for a restart means nothing that could finish an
	// upgrade. api.Settings documents this as possible, so it is answered
	// rather than assumed.
	if self.restarter == nil {
		return false, "this server was started without anything that can restart it, " +
			"so it cannot finish an upgrade. Upgrade it by hand: download the release, " +
			"replace the binary, and start it again"
	}

	// Every reason says what to do instead. A refusal that only explains
	// itself leaves somebody knowing they are out of date and not knowing
	// where to go next, which is the same place they started.
	switch self.restarter.Supervision() {
	case api.SupervisionContainer:
		return false, "the binary is inside a container image, so replacing it here would be undone " +
			"the next time the container starts. Upgrade the image instead: " +
			"\"docker compose pull && docker compose up -d\" where this is run by compose, " +
			"or pull ghcr.io/ziyan/teanode and recreate the container"
	case api.SupervisionUnknown:
		return false, "nothing recognisable would start this process again, so replacing the binary " +
			"and exiting would leave the server down. Run it under systemd or a container with a " +
			"restart policy and this button appears; until then, upgrade it by hand: download the " +
			"release, replace the binary, and start it again"
	}
	if self.executable == "" {
		return false, "this server cannot find its own executable, so there is nothing to replace. " +
			"Upgrade it by hand: download the release and put it where this one is"
	}
	// Written beside the current binary and renamed over it, so the directory
	// is what has to be writable, not the file. Asked by writing, because the
	// permission bits do not answer it: this process's user, its groups, the
	// mount's options and whatever the filesystem thinks all have a say.
	directory := filepath.Dir(self.executable)
	probe, err := os.CreateTemp(directory, ".teanode-upgrade-probe-*")
	if err != nil {
		return false, fmt.Sprintf("%s is not writable by this process, and the new binary has to be "+
			"written there before it can replace the old one. Either make it writable by the user "+
			"this runs as, or upgrade by hand: download the release and put it there yourself", directory)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return true, ""
}

// withinWindow reports whether now falls inside "HH:MM-HH:MM" in local time.
// An empty window is any time, and a window that cannot be read is any time
// as well — with a warning, because refusing to upgrade for ever over a typo
// is worse than upgrading at the wrong hour.
func withinWindow(window string, now time.Time) bool {
	if window == "" {
		return true
	}
	start, end, err := parseWindow(window)
	if err != nil {
		log.Warningf("cannot read upgrade.window %q, so any time will do: %s", window, err)
		return true
	}

	minutes := now.Hour()*60 + now.Minute()
	if start <= end {
		return minutes >= start && minutes < end
	}
	// A window that crosses midnight: 22:00-02:00.
	return minutes >= start || minutes < end
}

// parseWindow reads "HH:MM-HH:MM" into minutes past midnight.
func parseWindow(window string) (int, int, error) {
	halves := strings.SplitN(window, "-", 2)
	if len(halves) != 2 {
		return 0, 0, fmt.Errorf("upgrade: a window looks like 02:00-04:00")
	}
	start, err := parseClock(halves[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := parseClock(halves[1])
	if err != nil {
		return 0, 0, err
	}
	if start == end {
		return 0, 0, fmt.Errorf("upgrade: a window that starts and ends at the same minute is not a window")
	}
	return start, end, nil
}

func parseClock(text string) (int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("upgrade: %q is not a time of day", strings.TrimSpace(text))
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}
