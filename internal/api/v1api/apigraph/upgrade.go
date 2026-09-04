package apigraph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/upgrade"
)

type UpgradeQuery interface {
	// What version this server is running, what has been released since, and
	// whether anything can be done about the difference from here
	GetUpgrade(ctx context.Context, arguments GetUpgradeArguments) (*Upgrade, error)
}

type UpgradeMutation interface {
	// Install the newest release: download it, check it against the release's
	// checksums, replace this binary and restart. It answers as soon as the
	// upgrade has started, not when it has finished — the download takes
	// minutes and this request holds a database transaction open.
	ApplyUpgrade(ctx context.Context, arguments ApplyUpgradeArguments) (*Upgrade, error)
}

// Upgrade is what is running, what is available, and what may be done.
//
// The refusals carry their reason. A button that is absent with a sentence
// beside it is a decision an operator can act on; one that is absent silently
// is a bug they will report.
type Upgrade struct {
	// The running version, and the newest release. Latest is empty until a
	// check has succeeded.
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`

	// Whether Latest is newer than Current
	Available bool `json:"available"`

	// The release notes for Latest, as markdown, and the release's own page
	Notes string `json:"notes,omitempty"`
	URL   string `json:"url,omitempty"`

	// When the release list was last read, and why the last attempt failed.
	// Both are shown: a check that has not succeeded since Tuesday is the
	// thing worth saying, and an error only in a log is an error nobody sees.
	CheckedAt *time.Time `json:"checkedAt,omitempty"`

	// Whether asking for a check actually started one. Only ever true in the
	// reply to a request that asked; false means the answer here is the one
	// already known, and waiting for it to change would be waiting for
	// nothing.
	Checking bool   `json:"checking,omitempty"`
	Error    string `json:"error,omitempty"`

	// Whether this deployment can install an upgrade at all, and what stands
	// in the way when it cannot: a container, whose image is the thing to
	// replace, or a process nothing would start again after it exits.
	Applicable bool   `json:"applicable"`
	Reason     string `json:"reason,omitempty"`

	// Whether releases are installed without being asked
	Automatic bool `json:"automatic"`

	// Whether one is running now, download included
	Upgrading bool `json:"upgrading"`

	// Whether the release list is consulted at all
	Enabled bool `json:"enabled"`

	// The hours an automatic upgrade may run in, as stored
	Window string `json:"window,omitempty"`

	// Why the last check failed, which is a different thing from why the last
	// upgrade did
	CheckError string `json:"checkError,omitempty"`
}

type GetUpgradeArguments struct {
	// Ask the release list again rather than only answering from the last
	// scheduled check. What the dashboard does when somebody presses "check
	// again" — the reply is what is known now, and the fresh answer arrives
	// on a later read.
	//
	// A pointer, so the schema makes it optional: a plain bool becomes
	// Boolean! and then every caller that only wants to know what is already
	// known has to pass an argument to say so.
	Check *bool `json:"check"`
}

func (self *graph) GetUpgrade(ctx context.Context, arguments GetUpgradeArguments) (*Upgrade, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	if self.upgrade == nil {
		return nil, fmt.Errorf("%w: this server was not started with upgrades", api.ErrNotFound)
	}

	answer := describeUpgrade(self.upgrade.Status())
	if arguments.Check != nil && *arguments.Check {
		// Started, not waited for. This request runs inside a database
		// transaction, and a thirty-second call to somebody else's endpoint
		// — which is what it is on a server whose outbound HTTPS is blocked,
		// an ordinary way to run a mail server — has no business holding one
		// open. The answer arrives on the next read; the dashboard polls.
		//
		// Whether one started is part of the reply, because the caller cannot
		// work it out. Checking may be off, one may already be running, or
		// the last check somebody asked for may have been half a minute ago —
		// and a page that polls for a recorded time to move, when nothing is
		// going to move it, waits until its own deadline and then shows the
		// same answer with no explanation. It guessed twice from two
		// different timestamps and both guesses were wrong somewhere.
		answer.Checking = self.upgrade.CheckSoon()
	}
	return answer, nil
}

type ApplyUpgradeArguments struct {
	// The version this was agreed to install, as the page showed it. Omit to
	// take whatever is newest; give it and anything else is refused, so that
	// a tab left open across a release cannot install a version nobody
	// confirmed.
	Version *string `json:"version"`
}

func (self *graph) ApplyUpgrade(ctx context.Context, arguments ApplyUpgradeArguments) (*Upgrade, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	if self.upgrade == nil {
		return nil, fmt.Errorf("%w: this server was not started with upgrades", api.ErrNotFound)
	}

	// Logged before it happens, at warning, because somebody looking at why
	// mail stopped for a few seconds should find the reason without being
	// told where to look.
	log.Warningf("%s asked this instance to upgrade", api.ContextAuthenticatedUsername(ctx))

	// Started rather than done. Every GraphQL request runs inside a database
	// transaction, and a forty-five megabyte download is not something to
	// hold one open for: a deployment with idle_in_transaction_session_timeout
	// would kill the session part way through and answer that the upgrade
	// failed, after the binary had already been replaced.
	expected := ""
	if arguments.Version != nil {
		expected = *arguments.Version
	}
	status, err := self.upgrade.Start(expected)
	if err != nil {
		// Both are answers rather than failures: this deployment cannot
		// upgrade itself, or one is already going. A caller should see the
		// sentence, not a five hundred.
		if errors.Is(err, upgrade.ErrNotApplicable) || errors.Is(err, upgrade.ErrAlreadyRunning) {
			return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		}
		return nil, err
	}
	return describeUpgrade(status), nil
}

func describeUpgrade(status upgrade.Status) *Upgrade {
	return &Upgrade{
		Current:    status.Current,
		Latest:     status.Latest,
		Available:  status.Available,
		Notes:      status.Notes,
		URL:        status.URL,
		CheckedAt:  status.CheckedAt,
		Error:      status.Error,
		Applicable: status.Applicable,
		Reason:     status.Reason,
		Automatic:  status.Automatic,
		Upgrading:  status.Upgrading,
		Enabled:    status.Enabled,
		Window:     status.Window,
		CheckError: status.CheckError,
	}
}
