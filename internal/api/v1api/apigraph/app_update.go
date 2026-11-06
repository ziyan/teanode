package apigraph

import (
	"context"
	"errors"
	"fmt"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/appupdate"
)

type AppUpdateQuery interface {
	// The newest iPhone app on a channel, testflight or appstore, for the
	// app to compare with what it is. Nothing when the channel has none yet,
	// or when this server does not look for releases
	GetAppUpdate(ctx context.Context, arguments GetAppUpdateArguments) (*appupdate.Update, error)
}

type GetAppUpdateArguments struct {
	UpdateChannel string `json:"updateChannel"`
}

// appUpdates is shared by every request, since what it keeps is the same
// for everybody.
var appUpdates = appupdate.New()

func (self *graph) GetAppUpdate(ctx context.Context, arguments GetAppUpdateArguments) (*appupdate.Update, error) {
	if _, err := self.requireAccount(ctx); err != nil {
		return nil, err
	}
	// The operator who turned off looking for this server's own releases
	// asked for it not to reach out, and this is the same kind of request.
	if !self.config.Current().Upgrade.Enabled {
		return nil, nil
	}
	update, err := appUpdates.Latest(ctx, arguments.UpdateChannel)
	if errors.Is(err, appupdate.ErrUnknownChannel) {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	// Nobody can act on this failing: the phone says nothing, as it does
	// when there is no update.
	if err != nil {
		return nil, nil
	}
	return update, nil
}
