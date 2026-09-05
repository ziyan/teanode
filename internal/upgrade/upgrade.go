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

// ErrAlreadyRunning is returned when one is already going. Not a failure: the
// scheduled loop meets it whenever somebody has pressed the button, and
// treating it as one would put the schedule off for hours over nothing.
var ErrAlreadyRunning = errors.New("upgrade: an upgrade is already running")

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

	// Notes are the release notes for Latest, as written in the changelog,
	// and URL is the release's own page — for somebody who would rather read
	// it there, or wants the diff and the assets beside it.
	Notes string `json:"notes,omitempty"`
	URL   string `json:"url,omitempty"`

	// CheckedAt is when the release list was last read successfully, and
	// CheckError why the last attempt did not. Both are shown: "it has not
	// managed to check since Tuesday" is the thing an operator needs to be
	// told, and an error that is only logged is an error nobody sees.
	CheckedAt  *time.Time `json:"checkedAt,omitempty"`
	CheckError string     `json:"checkError,omitempty"`

	// AttemptedAt is when the release list was last asked, successfully or
	// not, and is what tells a caller waiting on a check that it has
	// finished.
	//
	// It was here once, used to guess whether asking again would achieve
	// anything, and that guess was wrong. This is the other job and the right
	// one: CheckedAt only moves when a check succeeds and CheckError only
	// changes when the reason changes, so a check that failed the same way
	// twice — outbound HTTPS blocked, which is an ordinary way to run a mail
	// server — moved nothing at all, and a page waiting on those two waited
	// its full deadline for something that had already happened.
	AttemptedAt *time.Time `json:"attemptedAt,omitempty"`

	// Error is why the last upgrade failed, which is a different sentence in
	// a different place on the page. They were one field, so a checksum that
	// did not match was shown as though the release list could not be read,
	// and pressing Upgrade erased a genuine "cannot reach the release list"
	// that somebody needed to see.
	Error string `json:"error,omitempty"`

	// Applicable says whether an upgrade could be applied here at all, and
	// Reason says what stands in the way when it cannot: a container, whose
	// image is the thing to replace, or a process nothing would start again.
	Applicable bool   `json:"applicable"`
	Reason     string `json:"reason,omitempty"`

	// Window is the hours an automatic upgrade may run in, as it is stored.
	// Settable through the API, so it has to be readable through it: every
	// other setting has a matching read, and one that can only be written is
	// one nobody can check.
	Window string `json:"window,omitempty"`

	// Automatic says upgrades are installed without being asked, and Enabled
	// that the release list is consulted at all. Turning checking off means
	// off: the button on the page stops asking too, rather than the setting
	// applying only to the schedule.
	Automatic bool `json:"automatic"`
	Enabled   bool `json:"enabled"`

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

	// CheckSoon asks in the background and returns whether it started one,
	// for a caller that must not wait — a GraphQL request, which runs inside
	// a database transaction that a thirty-second call to somebody else's
	// endpoint has no business holding open.
	//
	// The answer is what a caller waiting for the result needs: no check
	// started means nothing is going to change, and waiting is waiting for
	// nothing.
	CheckSoon() bool

	// Apply downloads the newest release, verifies it, replaces this binary
	// and restarts. It returns when the restart has been requested, not when
	// the new process is running: there is no new process to hear from here.
	//
	// An empty expected takes whatever is newest; anything else has to match
	// the version found or nothing is installed.
	Apply(ctx context.Context, expected string) error

	// ExecTarget is the binary this process should replace itself with once it
	// has finished shutting down, or empty when no upgrade happened. Read by
	// the server after everything is drained, which is the only safe moment.
	ExecTarget() string

	// Start begins an upgrade in the background and returns as soon as it has
	// been accepted, with whatever is known now. When expected is not empty,
	// it refuses anything else: the dashboard's confirmation names a version,
	// and a tab left open across a release should not install a different one
	// than the one somebody agreed to.
	//
	// It exists because the API request that asks for one is wrapped in a
	// database transaction, and a forty-five megabyte download is not
	// something to hold a transaction open for: on a deployment with
	// idle_in_transaction_session_timeout the session is killed part way
	// through and the caller is told the upgrade failed, after the binary has
	// already been replaced.
	Start(expected string) (Status, error)

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

	// upgradeDirectory is where a binary is staged when the running one
	// cannot be replaced in place. It comes from the environment rather than
	// from the configuration, because the next start has to find it before it
	// opens the database. Empty when the environment did not name one.
	upgradeDirectory string

	// containerized says the executable belongs to an image layer, which a
	// recreate throws away. Such a deployment always stages, whatever the
	// permissions on the image's own directories happen to allow.
	containerized bool

	// execTarget is the binary this process should become once it has shut
	// down, set by a successful upgrade and read by the server at the end.
	execTarget string

	// endpoint is where the release list is read from, and applicable decides
	// whether this deployment may replace itself. Both are fields rather than
	// constants so that a test can point them elsewhere: everything else in
	// here can be exercised offline, and these two are the reason it could
	// not be.
	endpoint   string
	applicable func() (bool, string)

	mutex  sync.RWMutex
	status Status

	// lastManualCheck is when somebody last asked for a check by hand, which
	// is a different clock from lastAttempt: see mayCheckByHand.
	lastManualCheck time.Time

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
func New(configuration config.Store, restarter *api.Restarter, upgradeDirectory string) (Manager, error) {
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
		config:           configuration,
		restarter:        restarter,
		repository:       Repository,
		upgradeDirectory: upgradeDirectory,
		containerized:    restarter != nil && restarter.Supervision() == api.SupervisionContainer,
		endpoint:         fmt.Sprintf(releaseEndpoint, Repository),
		client:           newClient(),
		executable:       executable,
		status: Status{
			Current: version.Version(),
		},
	}
	self.ctx, self.cancel = context.WithCancel(context.Background())

	self.applicable = self.checkApplicable

	applicable, reason := self.applicableNow()
	self.status.Applicable = applicable
	self.status.Reason = reason

	settings := self.settings()
	if !settings.Enabled {
		log.Noticef("not checking for releases: upgrade.enabled is off")
	}
	self.checkInterval = settings.CheckInterval.Duration()

	// The loop is built whether or not checking is on, and asks whether it is
	// on every time it wakes. Building it only when it was on at startup made
	// upgrade.enabled a setting that took effect immediately in one direction
	// and needed a restart in the other — and the dashboard, which cannot know
	// that, asked for a restart after turning it off and then went on asking
	// for ever, because the list of pending restarts is never cleared.
	//
	// A loop that wakes every few minutes and returns immediately costs
	// nothing. checkInterval is still read once here, and is still the one
	// part of this section that a restart is needed for.

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
	// And never zero or less, because periodic waits on time.After and that
	// returns immediately. A stored configuration with an interval of zero —
	// which validation used to permit while checking was off — pinned a core
	// for the life of the process, waking as fast as the scheduler allowed to
	// do nothing. Validation refuses it now; this is here because a busy loop
	// in a mail server is not a thing to leave one guard away from.
	if interval <= 0 {
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

// ExecTarget is what to become, once everything is closed.
func (self *manager) ExecTarget() string {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.execTarget
}

// settings is the upgrade section, or its zero value when there is no store.
//
// A manager built by hand — which the tests do, and which is the shape that
// has produced a nil dereference here twice — has no configuration. describe
// already guarded for that and three other readers did not, which is the kind
// of inconsistency that says the invariant is not really an invariant.
func (self *manager) settings() config.Upgrade {
	if self.config == nil {
		return config.Upgrade{}
	}
	return self.config.Current().Upgrade
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
	settings := self.settings()
	status.Automatic = settings.Automatic
	status.Enabled = settings.Enabled
	status.Window = settings.Window
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

	// Turned off while running: the loop was built at startup and would
	// otherwise keep asking until a restart, which is not what somebody who
	// turned it off meant.
	if !self.settings().Enabled {
		return nil
	}

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

	settings := self.settings()
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
	if err := self.Apply(ctx, ""); err != nil {
		// Backed off, because the loop wakes every five minutes and the
		// ordinary failure here is one that will keep failing: a checksum
		// that does not match, a release with no asset for this platform, a
		// link that keeps dropping. Without this it downloads forty-five
		// megabytes, throws it away, and does it again — three hundred times
		// a day, for ever.
		//
		// A refusal is not one of those. An upgrade somebody started from the
		// dashboard holds the lock for the length of its download, and the
		// tick that lands during it must not spend the allowance on a
		// refusal it was never going to get past: that pushed automatic
		// upgrades hours into the future for a reason that never failed.
		if !errors.Is(err, ErrNotApplicable) && !errors.Is(err, ErrAlreadyRunning) {
			self.failed()
		}
		log.Errorf("automatic upgrade to %s failed: %s", status.Latest, err)
		return nil
	}
	self.succeeded()
	return nil
}

// Start begins an upgrade and returns immediately.
func (self *manager) Start(expected string) (Status, error) {
	if !self.settings().Enabled {
		// Enforced, not hidden. The button disappears because nothing is
		// known to be available, but the API is reachable from the command
		// line and from any other client, and "checking is off" should mean
		// this server does not fetch and run a binary from the internet.
		return self.Status(), fmt.Errorf("%w: upgrade.enabled is off", ErrNotApplicable)
	}
	if applicable, reason := self.applicableNow(); !applicable {
		return self.Status(), fmt.Errorf("%w: %s", ErrNotApplicable, reason)
	}
	// The same guard the loop has: a restart already asked for means an
	// upgrade has already swapped the binary, and this process is on its way
	// out. A second one in that window — another tab, another operator, the
	// command line — would link the rollback copy to the binary just
	// installed.
	if self.restarter != nil && self.restarter.Requested() {
		return self.Status(), fmt.Errorf("%w: a restart is already under way", ErrAlreadyRunning)
	}

	// The lock is taken here and released by the goroutine, so it is a
	// reservation rather than a question. Taking it and letting it go before
	// the goroutine starts let two requests both pass — and the loser then
	// wrote "an upgrade is already running" onto the status while the winner
	// was downloading, so the dashboard reported a failure for an upgrade
	// that was about to restart the server.
	if !self.applying.TryLock() {
		return self.Status(), ErrAlreadyRunning
	}

	// Marked here as well as in apply, so that the status says so before this
	// request is answered rather than whenever the goroutine gets to run.
	self.markUpgrading()

	// Not after Close has begun: an Add that lands once Wait has been entered
	// is the misuse that panics, and both of the ways in here are reachable
	// from a request that a shutdown can race.
	select {
	case <-self.ctx.Done():
		self.clearUpgrading()
		self.applying.Unlock()
		return self.Status(), fmt.Errorf("upgrade: this server is shutting down")
	default:
	}

	// Counted, so that Close does not return while this is between chmod and
	// rename. Cancelling the context stops the download; it does not stop a
	// swap that has started.
	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		defer self.applying.Unlock()

		// The manager's context, not the request's: the request is answered
		// before this finishes, and its context is cancelled the moment it
		// is. apply puts the status back if it fails; a success is followed
		// by the restart, which this process does not return from.
		if err := self.apply(self.ctx, expected); err != nil {
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

// manualCheckInterval is the least time between checks somebody asks for.
//
// Single-flighting is not a rate limit: a loop calling GetUpgrade(check: true)
// finishes each request in a few hundred milliseconds and would spend the
// endpoint's sixty-an-hour allowance in a minute — and then the scheduled
// check fails too, for everybody on that address.
const manualCheckInterval = time.Minute

// bootstrapPrefix is on the front of every environment variable this program
// reads. Repeated here rather than imported: internal/bootstrap reads the
// configuration this package is configured by, and the dependency would go the
// wrong way round.
const bootstrapPrefix = "TEANODE_"

// CheckSoon asks in the background, once at a time and not too often, and
// says whether it actually started one.
//
// The answer matters to the caller. A page that asks for a check and then
// waits for the recorded time to move will wait for ever when no check ran —
// checking is off, one is already in flight, or the last one somebody asked
// for was less than a minute ago — and the page cannot work any of that out
// for itself. It guessed twice, from two different timestamps, and both
// guesses were wrong in a case the server knew about all along.
func (self *manager) CheckSoon() bool {
	if !self.settings().Enabled {
		return false
	}
	if !self.mayCheckByHand() {
		return false
	}
	if !self.checking.TryLock() {
		// One is already running, and this caller will see its answer. The
		// allowance is not spent on it: a request that started no check must
		// not be the reason the next one is turned away.
		return false
	}
	self.recordCheckByHand()

	// Not after Close has begun. Close cancels and then waits, and an Add
	// that lands after the counter has reached zero and Wait has been entered
	// is the misuse that panics.
	select {
	case <-self.ctx.Done():
		self.checking.Unlock()
		return false
	default:
	}

	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		defer self.checking.Unlock()
		if _, err := self.Check(self.ctx); err != nil {
			log.Warningf("could not check for a release: %s", err)
		}
	}()
	return true
}

// mayCheckByHand reports whether a check somebody asked for may run: not
// within a minute of the last one somebody asked for.
//
// Measured against the last manual check rather than the last check of any
// kind. Sharing the clock with the scheduled loop meant that "Check now" did
// nothing for the first minute after a start — the loop checks immediately —
// and the dashboard, which waits for the recorded time to move, waited forty
// seconds for something that was never going to happen and then gave up
// without a word.
func (self *manager) mayCheckByHand() bool {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	return self.lastManualCheck.IsZero() || time.Since(self.lastManualCheck) >= manualCheckInterval
}

// recordCheckByHand spends the allowance, and is called only once a check is
// actually about to run.
func (self *manager) recordCheckByHand() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.lastManualCheck = time.Now()
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

	// Whether this deployment could apply one is asked again here rather than
	// only at startup: a directory that was briefly unwritable at boot should
	// not hide the button for ever, and one that has since been remounted
	// read-only should not keep offering it.
	//
	// Asked before the lock, because it writes a file to find out: on a slow
	// mount that would block every dashboard read behind it, including the
	// two-second poll during an upgrade.
	applicable, reason := self.applicableNow()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	// A fresh value rather than the address of the field: Status copies the
	// struct and the caller reads it after the lock is gone.
	attempted := time.Now()
	self.lastAttempt = attempted
	self.status.AttemptedAt = &attempted
	self.status.Applicable = applicable
	self.status.Reason = reason

	if err != nil {
		self.status.CheckError = err.Error()
		return self.describe(), err
	}

	now := time.Now()
	self.status.CheckError = ""
	self.status.CheckedAt = &now
	self.status.Latest = found.version()
	self.status.Notes = found.Notes
	self.status.URL = found.URL
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
	if self.executable == "" {
		return false, "this server cannot find its own executable, so there is nothing to replace. " +
			"Upgrade it by hand: download the release and put it where this one is"
	}

	// Nothing here ends the process by itself. The upgrade puts the new binary
	// in place and the serve loop, when it is asked to stop, execs it — so
	// without the thing that asks, the swap would happen and nothing would
	// come of it. A manager built without a restarter is a manager that can
	// report and cannot apply, and it says so rather than finding out after
	// the download.
	if self.restarter == nil {
		return false, "this server has no way to restart itself into a new binary, so an upgrade would " +
			"replace the file and change nothing until the next restart"
	}

	// Somewhere to write it is the rest of the question. Replacing this
	// process afterwards does not need a supervisor — exec does it — so the
	// old refusal for a process nobody started is gone. A container's refusal
	// is gone too, but only because it has somewhere else to write: see
	// target.
	if path, _ := self.target(); path == "" {
		if self.containerized {
			return false, fmt.Sprintf("this is a container and %s, so there is nowhere to put a new "+
				"binary that the next start would still find. Mount a writable volume and name it in "+
				"%sUPGRADE_DIRECTORY, or upgrade the image: docker compose pull && docker compose up -d",
				self.stagingProblem(), bootstrapPrefix)
		}
		return false, fmt.Sprintf("neither %s nor %s can be written by this process, and the new binary "+
			"has to go in one of them. Make one writable by the user this runs as, or upgrade by hand: "+
			"download the release and put it in place yourself",
			filepath.Dir(self.executable), self.describeStagingDirectory())
	}
	return true, ""
}

// describeStagingDirectory names the staging directory for a message, without
// pretending there is one when there is not.
func (self *manager) describeStagingDirectory() string {
	if self.upgradeDirectory == "" {
		return fmt.Sprintf("the directory %sUPGRADE_DIRECTORY would name", bootstrapPrefix)
	}
	return self.upgradeDirectory
}

// target is where a new binary can be written, and therefore what this process
// will exec afterwards. Empty when there is nowhere.
//
// Beside the running binary when that directory can be written, because
// replacing it in place is what an operator expects and what survives without
// any of the machinery below. Otherwise the data directory, which is the one
// path a container has as a writable volume — the image's own layer is read
// only, and a binary written there would be gone at the next start.
// target is where a new binary can be written, and whether that place is the
// staging directory. Empty when there is nowhere.
//
// Beside the running binary when that directory can be written, because
// replacing it in place is what an operator expects and what survives without
// any of the machinery below. Otherwise the staging directory, which is the
// one path a container has that outlives it — the image's own layer is thrown
// away by the next recreate, and a binary written there would be gone.
//
// It returns the second value rather than leaving callers to work it out by
// comparing paths. They did, and it was wrong twice: once because a process
// exec'd out of the staging directory has the staged path as its executable,
// and once, after that was fixed, because one side of the comparison was
// resolved through its symlinks and the other was not. The function that
// chooses knows which it chose.
func (self *manager) target() (string, bool) {
	staged := Staged(self.upgradeDirectory)

	// Not when the executable is the staged binary, whatever the two paths
	// look like written down: a process that was exec'd out of the staging
	// directory is upgrading the staged binary again, and writing it any
	// other way leaves the directory describing the release before last.
	if !self.containerized && !sameFile(self.executable, staged) &&
		writable(filepath.Dir(self.executable)) {
		return self.executable, false
	}

	if self.stagingProblem() != "" {
		return "", false
	}
	return staged, true
}

// stagingProblem says why the staging directory cannot be used, or nothing.
//
// It asks the same question the next start asks before running what it finds
// there, and it asks it now. An upgrade that stages into a directory the next
// start will refuse is the worst kind of success: the binary is written, the
// process execs it, the page says it worked, and then a recreate quietly puts
// the old version back with no refusal recorded anywhere. A volume mounted
// dir_mode=0777 is all it takes.
//
// It reads and does not write. Every caller here is answering a question —
// what the page should say, whether the button belongs — and a question that
// creates a directory on a deployment with upgrades turned off, or resets a
// mode an operator chose on purpose every time the loop wakes, is doing
// something nobody asked for. Creating it is stage's job, once.
func (self *manager) stagingProblem() string {
	if self.upgradeDirectory == "" {
		return "no upgrade directory is configured"
	}

	// Not being there yet is not a problem: it is made, private to this user,
	// at the moment something is staged.
	//
	// The directory above it is deliberately not judged. It is the data
	// directory, which already holds the signing keys and the spool — its
	// permissions are a question for the whole deployment and not for this
	// feature, and refusing upgrades over them would be this feature
	// answering somebody else's question badly.
	info, err := os.Stat(self.upgradeDirectory)
	if os.IsNotExist(err) {
		// Whether it could be made, which is the question at this point.
		// Answering "nothing is known to be wrong" without asking would let
		// the page offer a button to a deployment whose volume is mounted
		// read-only, and the operator would find out by pressing it.
		//
		// A probe file, created and removed: that is what writable does
		// everywhere else here, and it is a different thing from leaving a
		// directory behind on a deployment that will never stage anything.
		if !writable(filepath.Dir(self.upgradeDirectory)) {
			return fmt.Sprintf("%s cannot be created, because this process cannot write %s",
				self.upgradeDirectory, filepath.Dir(self.upgradeDirectory))
		}
		return ""
	}
	if err != nil {
		return err.Error()
	}
	if !info.IsDir() {
		return fmt.Sprintf("%s is not a directory", self.upgradeDirectory)
	}

	if why := UnsafeDirectory(self.upgradeDirectory); why != "" {
		return fmt.Sprintf("%s — a staged binary there would be refused at the next start; chmod 700 it",
			why)
	}
	if !writable(self.upgradeDirectory) {
		return fmt.Sprintf("%s cannot be written by this process", self.upgradeDirectory)
	}
	return ""
}

// writable reports whether this process can create a file in a directory.
//
// By writing one, because the permission bits do not answer it: the user, its
// groups, the mount's options and whatever the filesystem thinks all have a
// say. The running binary itself is never opened for writing — Linux answers
// ETXTBSY for an executable that is in use — which is why the question is
// about the directory and the swap is a rename.
func writable(directory string) bool {
	probe, err := os.CreateTemp(directory, ".teanode-upgrade-probe-*")
	if err != nil {
		return false
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return true
}

// withinWindow reports whether now falls inside "HH:MM-HH:MM" in local time.
// An empty window is any time, and a window that cannot be read is any time
// as well — with a warning, because refusing to upgrade for ever over a typo
// is worse than upgrading at the wrong hour.
func withinWindow(window string, now time.Time) bool {
	window = strings.TrimSpace(window)
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

// clearUpgrading takes the mark off again, for the one path that puts it on
// and then finds the server is shutting down. Its own function so the unlock
// is a defer; the applying mutex released after it is a different lock with a
// different lifetime, which is why the two are not one deferred pair.
func (self *manager) clearUpgrading() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.status.Upgrading = false
}
