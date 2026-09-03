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
	ApplyUpgrade(ctx context.Context) (*Upgrade, error)
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

	// The release notes for Latest, as markdown
	Notes string `json:"notes,omitempty"`

	// When the release list was last read, and why the last attempt failed.
	// Both are shown: a check that has not succeeded since Tuesday is the
	// thing worth saying, and an error only in a log is an error nobody sees.
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	Error     string     `json:"error,omitempty"`

	// Whether this deployment can install an upgrade at all, and what stands
	// in the way when it cannot: a container, whose image is the thing to
	// replace, or a process nothing would start again after it exits.
	Applicable bool   `json:"applicable"`
	Reason     string `json:"reason,omitempty"`

	// Whether releases are installed without being asked
	Automatic bool `json:"automatic"`

	// Whether one is running now, download included
	Upgrading bool `json:"upgrading"`
}

type GetUpgradeArguments struct {
	// Ask the release list now rather than answering from the last scheduled
	// check. What the dashboard does when somebody presses "check again".
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

	if arguments.Check != nil && *arguments.Check {
		// The error is on the status rather than returned: "it could not
		// reach the release list" is an answer to the question, not a failure
		// of the request that asked it.
		status, _ := self.upgrade.Check(ctx)
		return describeUpgrade(status), nil
	}
	return describeUpgrade(self.upgrade.Status()), nil
}

func (self *graph) ApplyUpgrade(ctx context.Context) (*Upgrade, error) {
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
	status, err := self.upgrade.Start()
	if err != nil {
		if errors.Is(err, upgrade.ErrNotApplicable) {
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
		CheckedAt:  status.CheckedAt,
		Error:      status.Error,
		Applicable: status.Applicable,
		Reason:     status.Reason,
		Automatic:  status.Automatic,
		Upgrading:  status.Upgrading,
	}
}
