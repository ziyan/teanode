package apigraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/qrcode"
	"github.com/ziyan/teanode/internal/util/security"
)

// Connecting a phone: a one-time code, made here for the person signed in and
// shown as a QR code, that the TeaNode phone app scans and exchanges for a
// token as them (internal/api/v1api/apioauth/connect.go). The dashboard's
// Connect page and `teanode agent phone pair` make one.
//
// The code is the whole credential: whoever presents it gets a token for
// everything the person can do. So it is made only for the person themselves
// and only over a connection that keeps it secret.

// connectCodeLifetime is how long a code can be scanned in: long enough to
// fetch a phone and open its camera, short enough that a photo of the screen
// is soon worth nothing.
const connectCodeLifetime = 10 * time.Minute

// OAuthConnectCode is a code for a phone to scan.
type OAuthConnectCode struct {
	// ConnectLink is what the code holds: https://teanode.com/app/connect
	// with this server and the secret after the "#". Opened on a phone it
	// opens the TeaNode app, or the App Store when the app is not there.
	// It signs a phone in as you, so it is shown to you and given to nobody.
	ConnectLink string `json:"connectLink"`

	// QRCodeSVG is ConnectLink as a QR code, an SVG image
	QRCodeSVG string `json:"qrCodeSvg"`

	// ExpiresAt is when the code stops working
	ExpiresAt time.Time `json:"expiresAt"`
}

func (self *graph) CreateOAuthConnectCode(ctx context.Context) (*OAuthConnectCode, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, fmt.Errorf("%w: a phone is connected to an account", api.ErrInvalidArguments)
	}
	// An operator signed in as somebody is not that somebody. A phone they
	// connected would go on acting as the account after the impersonation
	// ended, and nothing would say who connected it.
	if principal.IsImpersonation() {
		return nil, fmt.Errorf("%w: a phone is connected by the person themselves, not while signed in as them", api.ErrPermissionDenied)
	}
	// The link carries the address the phone sends the code to. Plain http
	// would send it, and the token that comes back, where anybody on the way
	// can read them, so it is refused except on this machine.
	request := api.ContextRequest(ctx)
	if !api.IsSecure(request, self.config.Current().Server.TrustedProxies) && !api.IsLoopbackHost(hostOnly(request.Host)) {
		return nil, fmt.Errorf("%w: open the dashboard over https to connect a phone; a code sent over plain http could be read on the way", api.ErrInvalidArguments)
	}
	// What the code signs in is marked as the app's own only when the code was
	// made where a person was tapping. A code made with a command line token
	// could have been made, and scanned, by a program on the person's
	// computer, which would then hold an app's token.
	var clientKind models.TokenClientKind
	if principal.IsPersonsTap() {
		clientKind = models.ClientKindApp
	}
	secret := security.GenerateRandomString(40, security.LowerAlphaNumeric)
	sum := sha256.Sum256([]byte(secret))
	created, err := self.database.CreateOAuthConnectCode(&models.OAuthConnectCode{
		ID:         security.NewULID(),
		UserID:     principal.User.ID,
		ClientKind: clientKind,
		ExpiresAt:  time.Now().Add(connectCodeLifetime),
	}, hex.EncodeToString(sum[:]))
	if err != nil {
		return nil, err
	}

	// After the "#", so the secret never reaches the project's web server
	// should the phone open the link in a browser rather than the app.
	fragment := url.Values{}
	fragment.Set("server", self.originOf(request))
	fragment.Set("code", secret)
	link := api.AppConnectLink + "#" + fragment.Encode()
	drawn, err := qrcode.SVG(link)
	if err != nil {
		return nil, err
	}
	log.Noticef("%s made a code to connect a phone", principal.User.Username)
	return &OAuthConnectCode{ConnectLink: link, QRCodeSVG: drawn, ExpiresAt: created.ExpiresAt}, nil
}

// hostOnly is the host of a Host header, without its port.
func hostOnly(hostAndPort string) string {
	if host, _, err := net.SplitHostPort(hostAndPort); err == nil {
		return host
	}
	return hostAndPort
}
