package apigraph

import (
	"context"
	"fmt"
	"github.com/ziyan/teanode/internal/models"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/version"
)

type ServerQuery interface {
	// Get what this instance is and how it is running: which build, how long
	// it has been up, and whether anything has changed that it will not pick
	// up until it restarts.
	GetServerStatus(ctx context.Context) (*ServerStatus, error)
}

type ServerMutation interface {
	// Restart this instance. The process exits and whatever supervises it is
	// expected to start a new one, which is the only way to apply a change to
	// the listeners, TLS, storage or the optional integrations.
	RestartServer(ctx context.Context) (*RestartServerResult, error)
}

// ServerStatus describes the running instance rather than the configuration.
//
// Deliberately per-instance. Everything else the API returns is shared through
// the database and the same on every instance; this is the one thing that is
// not, and an operator looking at a restart button needs to know which process
// they are about to end.
type ServerStatus struct {
	// Which instance this is, as it appears in the usage counters
	Instance string `json:"instance"`

	// The release and the commit it was built from
	Version string `json:"version"`
	Commit  string `json:"commit"`

	// When this process started, and how long ago that was in seconds
	StartedAt     time.Time `json:"startedAt"`
	UptimeSeconds int64     `json:"uptimeSeconds"`

	// Settings that have changed since this process started and that it is
	// not using, because they are read once at startup. Empty when there is
	// nothing a restart would pick up.
	PendingRestart []string `json:"pendingRestart"`

	// What is expected to start this process again after it exits:
	// "container", "systemd", or "unknown" when nothing recognisable did.
	//
	// A guess. Neither a container's restart policy nor a unit's Restart= can
	// be read from inside the process, so this says which supervisor is in
	// charge rather than promising it will come back.
	Supervision string `json:"supervision"`

	// Whether a restart has already been asked for and is under way
	Restarting bool `json:"restarting"`
}

// RestartServerResult is what the dashboard needs in order to say what is
// happening and when to start looking for the server again.
type RestartServerResult struct {
	// Whether this call is what started the restart. False when one was
	// already under way, which is not an error.
	Started bool `json:"started"`

	// The instance that is going away
	Instance string `json:"instance"`

	// What is expected to bring it back, as in ServerStatus
	Supervision string `json:"supervision"`
}

func (self *graph) GetServerStatus(ctx context.Context) (*ServerStatus, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	return self.describeServer(), nil
}

func (self *graph) describeServer() *ServerStatus {
	status := &ServerStatus{
		Instance:      self.settings.BackendID,
		Version:       version.Version(),
		Commit:        version.Commit(),
		StartedAt:     self.started,
		UptimeSeconds: int64(time.Since(self.started).Seconds()),
		Supervision:   string(api.SupervisionUnknown),
	}
	if restarter := self.settings.Restarter; restarter != nil {
		status.PendingRestart = restarter.Pending()
		status.Supervision = string(restarter.Supervision())
		status.Restarting = restarter.Requested()
	}
	return status
}

func (self *graph) RestartServer(ctx context.Context) (*RestartServerResult, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	restarter := self.settings.Restarter
	if restarter == nil {
		return nil, fmt.Errorf("%w: it was not started in a way that allows it", api.ErrRestartUnavailable)
	}

	started := restarter.Request()
	if started {
		// Logged at warning, because somebody looking at why mail stopped for
		// a few seconds should find the reason without having to be told.
		log.Warningf("%s asked this instance to restart; shutting down",
			api.ContextAuthenticatedUsername(ctx))
	}

	return &RestartServerResult{
		Started:     started,
		Instance:    self.settings.BackendID,
		Supervision: string(restarter.Supervision()),
	}, nil
}
