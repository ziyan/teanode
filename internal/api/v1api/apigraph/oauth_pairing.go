package apigraph

import (
	"context"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Approving a pairing: the other half of internal/api/v1api/apioauth/device.go.
// A device, the TeaNode phone app, shows a code; the person approves it here,
// from the dashboard's /connect page, `teanode agent phone approve`, or their
// agent, and the device collects a token as them.

type ReadOAuthDevicePairingArguments struct {
	// UserCode the device shows, such as KQ7M-4TXD; the dash and case do
	// not matter
	UserCode string `json:"userCode"`
}

type DecideOAuthDevicePairingArguments struct {
	// UserCode the device shows, such as KQ7M-4TXD
	UserCode string `json:"userCode"`

	// IsApproved signs the device in as you when true, and refuses it when
	// false
	IsApproved bool `json:"isApproved"`
}

// OAuthDevicePairing is a device waiting for its code to be approved.
type OAuthDevicePairing struct {
	// UserCode as the device shows it
	UserCode string `json:"userCode"`

	// ClientName is what the program calls itself: claimed, not verified,
	// since anybody may register under any name
	ClientName string `json:"clientName"`

	// DeviceName is what the device calls itself, such as "Ana's iPhone":
	// claimed too
	DeviceName string `json:"deviceName"`

	// Username is the account the device would act as
	Username string `json:"username"`

	// IsFullAccess says whether the device would be able to do everything
	// the account can, rather than use the agent tools alone
	IsFullAccess bool `json:"isFullAccess"`

	// IsApproved and IsDenied say what was decided, when something was
	IsApproved bool `json:"isApproved"`
	IsDenied   bool `json:"isDenied"`

	// ExpiresAt is when the code stops working
	ExpiresAt time.Time `json:"expiresAt"`
}

func (self *graph) ReadOAuthDevicePairing(ctx context.Context, arguments ReadOAuthDevicePairingArguments) (*OAuthDevicePairing, error) {
	principal, err := self.requirePairingApprover(ctx)
	if err != nil {
		return nil, err
	}
	pairing, err := self.database.GetOAuthDeviceAuthorizationByUserCode(api.NormalizePairingCode(arguments.UserCode), time.Now())
	if err != nil {
		return nil, err
	}
	if pairing == nil || pairing.IsDecided() {
		return nil, errNoPairing
	}
	return self.devicePairingOf(pairing, principal.User.Username)
}

func (self *graph) DecideOAuthDevicePairing(ctx context.Context, arguments DecideOAuthDevicePairingArguments) (*OAuthDevicePairing, error) {
	principal, err := self.requirePairingApprover(ctx)
	if err != nil {
		return nil, err
	}
	pairing, err := self.database.DecideOAuthDeviceAuthorization(
		api.NormalizePairingCode(arguments.UserCode), principal.User.ID, arguments.IsApproved, time.Now())
	if err != nil {
		return nil, err
	}
	if pairing == nil {
		return nil, errNoPairing
	}
	if arguments.IsApproved {
		log.Noticef("%s approved the pairing %s for the client %s (%q)",
			principal.User.Username, pairing.ID, pairing.ClientID, pairing.DeviceName)
	} else {
		log.Noticef("%s refused the pairing %s for the client %s", principal.User.Username, pairing.ID, pairing.ClientID)
	}
	return self.devicePairingOf(pairing, principal.User.Username)
}

// errNoPairing is the one answer for a code that is wrong, expired or already
// decided: telling them apart would tell somebody guessing which of their
// guesses was once a code.
var errNoPairing = fmt.Errorf("%w: no device is waiting for that code; it may have expired or been answered already", api.ErrNotFound)

// requirePairingApprover is the person approving, counted against the login
// limiter.
//
// A pairing code is short so a person can read and type it, and approving
// one signs a device in, which is what guessing would be after. Every look
// and every decision counts as a sign-in attempt from where it came from, so
// a few dozen guesses are all anybody gets in a code's ten minutes.
func (self *graph) requirePairingApprover(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, fmt.Errorf("%w: a device is paired to an account", api.ErrInvalidArguments)
	}
	if self.authenticator != nil && !self.authenticator.AllowLoginAttempt(api.ContextRequest(ctx)) {
		return nil, fmt.Errorf("%w: too many pairing codes tried; wait a few minutes", api.ErrTooManyRequests)
	}
	return principal, nil
}

func (self *graph) devicePairingOf(pairing *models.OAuthDeviceAuthorization, username string) (*OAuthDevicePairing, error) {
	clientName := ""
	if client, err := self.database.GetOAuthClient(pairing.ClientID); err != nil {
		return nil, err
	} else if client != nil {
		clientName = client.Name
	}
	return &OAuthDevicePairing{
		UserCode:     api.FormatPairingCode(pairing.UserCode),
		ClientName:   clientName,
		DeviceName:   pairing.DeviceName,
		Username:     username,
		IsFullAccess: pairing.Resource == "",
		IsApproved:   pairing.UserID != "" && !pairing.IsDenied,
		IsDenied:     pairing.IsDenied,
		ExpiresAt:    pairing.ExpiresAt,
	}, nil
}
