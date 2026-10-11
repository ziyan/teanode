package apigraph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/dav"
	"github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/ceremony"
	"github.com/ziyan/teanode/internal/util/qrcode"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// Setup profiles: one file an iPhone or a Mac installs to read a mailbox in
// its own Mail, Calendar, Contacts and Reminders. It carries an app password
// made for the device, so it waits sealed in the ceremony store for one
// download and is never written anywhere else. That store is a map on one
// instance and Redis on several, so the download works whichever instance
// it reaches, including the command line's second request.

type MailboxSetupProfileMutation interface {
	// Make an app password for a device and a setup profile holding it; the profile downloads once, within five minutes
	CreateMailboxSetupProfile(ctx context.Context, arguments CreateMailboxSetupProfileArguments) (*CreatedSetupProfile, error)
}

type CreateMailboxSetupProfileArguments struct {
	MailboxID string `json:"mailboxId"`

	// What the device is called: "Phone", "Laptop"; the app password is named for it
	DeviceName string `json:"deviceName"`

	// True when the caller downloads the file itself through the API rather than opening the link on the device; the link may then point at this server's own loopback address
	IsDownloadOnly *bool `json:"isDownloadOnly" graphapi:"nullable"`
}

// CreatedSetupProfile says where a setup profile can be downloaded, once.
type CreatedSetupProfile struct {
	// The path to download it from, on this server; it works once
	DownloadPath string `json:"downloadPath"`

	// The whole address to download it from, to open on the device
	DownloadLink string `json:"downloadLink"`

	// DownloadLink as a QR code, an SVG image, to scan with the device's camera
	QRCodeSVG string `json:"qrCodeSvg"`

	// When the download address stops working
	ExpiresAt time.Time `json:"expiresAt"`

	// The app password made for it, which revoking turns the device off
	AppPassword *models.MailboxAppPassword `json:"appPassword"`

	// Whether it adds calendars and contacts as well as mail; false when this server has no name they are reached at
	HasCalendarsAndContacts bool `json:"hasCalendarsAndContacts"`
}

// setupProfileLabel binds the key a parked profile is sealed with to this
// one use of the server secret.
const setupProfileLabel = "teanode mailbox: setup profiles waiting to download"

// setupProfileBox seals and opens parked profiles.
func (self *graph) setupProfileBox() (*secretbox.Box, error) {
	return secretbox.New(self.settings.Secret, setupProfileLabel)
}

// setupProfileOrigin is the scheme and host the download link starts with:
// one the device can open.
//
// The name the dashboard was opened at, when it is not this machine's own:
// it reached this server, over https, and carries its port. Otherwise the
// server's configured name, as in every other link a person opens from
// outside. The command line on the server talks to 127.0.0.1, which the
// phone cannot open, so without a configured name the link is refused before
// an app password is spent on it, unless the caller downloads the file
// itself and never shows the link.
func (self *graph) setupProfileOrigin(request *http.Request, isDownloadOnly bool) (string, error) {
	if !api.IsLoopbackHost(hostOnly(request.Host)) {
		return self.originOf(request), nil
	}
	if base := self.config.Current().DashboardBase(); base != "" {
		return base, nil
	}
	if isDownloadOnly {
		return self.originOf(request), nil
	}
	return "", fmt.Errorf("%w: this server has no name a device can reach it at, so the link would point at this machine itself; set server.name, or save the profile to a file (teanode mailbox profile --output) and move it to the device", api.ErrInvalidArguments)
}

func (self *graph) CreateMailboxSetupProfile(ctx context.Context, arguments CreateMailboxSetupProfileArguments) (*CreatedSetupProfile, error) {
	found, err := self.requireMailbox(ctx, models.PermissionMailboxManage, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	deviceName := strings.TrimSpace(arguments.DeviceName)
	if deviceName == "" {
		return nil, fmt.Errorf("%w: say what the device is called", api.ErrInvalidArguments)
	}
	// The profile holds a password, and its address is how it is fetched:
	// over plain http, both could be read on the way.
	request := api.ContextRequest(ctx)
	if request == nil {
		return nil, fmt.Errorf("%w: a setup profile is made over the API", api.ErrInvalidArguments)
	}
	if !api.IsSecure(request, self.config.Current().Server.TrustedProxies) && !api.IsLocalRequest(request) {
		return nil, fmt.Errorf("%w: open the dashboard over https to make a setup profile; one fetched over plain http could be read on the way", api.ErrInvalidArguments)
	}
	origin, err := self.setupProfileOrigin(request, arguments.IsDownloadOnly != nil && *arguments.IsDownloadOnly)
	if err != nil {
		return nil, err
	}
	settings, err := self.GetMailProgramSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings.IMAPHost == "" || settings.SubmissionHost == "" {
		return nil, fmt.Errorf("%w: this server has no mail host for a device to reach", api.ErrInvalidArguments)
	}
	imapPort := settings.IMAPSPort
	if imapPort == 0 {
		imapPort = settings.IMAPPort
	}
	if imapPort == 0 {
		return nil, fmt.Errorf("%w: this server does not serve IMAP", api.ErrInvalidArguments)
	}
	box, err := self.setupProfileBox()
	if err != nil {
		return nil, err
	}
	created, err := self.CreateMailboxAppPassword(ctx, CreateMailboxAppPasswordArguments{MailboxID: found.ID, Name: deviceName})
	if err != nil {
		return nil, err
	}
	profile := &mailbox.SetupProfile{
		DeviceName:     deviceName,
		MailboxName:    found.Name,
		Address:        created.Username,
		Password:       created.Password,
		IMAPHost:       settings.IMAPHost,
		IMAPPort:       imapPort,
		SubmissionHost: settings.SubmissionHost,
		SubmissionPort: settings.SubmissionPort,
		DAVHost:        settings.DAVHost,
		PrincipalPath:  dav.Prefix + "/" + found.UserID + "/",
		Identifier:     "com.teanode.profile." + found.ID,
	}
	data, err := profile.Plist()
	if err != nil {
		return nil, err
	}
	sealed, err := box.Seal(data)
	if err != nil {
		return nil, err
	}
	// Taken before parking, so the time shown is never later than the one
	// the store keeps.
	expiresAt := time.Now().Add(ceremony.Lifetime)
	token, err := self.ceremonies.Park(ctx, &ceremony.Ceremony{Kind: ceremony.KindSetupProfile, SealedSetupProfile: sealed})
	if err != nil {
		return nil, err
	}
	path := strings.Replace(api.PathMailboxSetupProfile, "{token}", token, 1)
	link := origin + path
	drawn, err := qrcode.SVG(link)
	if err != nil {
		return nil, err
	}
	log.Noticef("%s made a setup profile for mailbox %q on %q", operatorName(ctx), found.ID, deviceName)
	return &CreatedSetupProfile{
		DownloadPath:            path,
		DownloadLink:            link,
		QRCodeSVG:               drawn,
		ExpiresAt:               expiresAt,
		AppPassword:             created.AppPassword,
		HasCalendarsAndContacts: settings.DAVHost != "",
	}, nil
}

// setupProfileView hands a setup profile to the device installing it, once.
// Anything else parked under the token, a passkey challenge, is refused
// the same way as a token that was never made.
func (self *graph) setupProfileView(response http.ResponseWriter, request *http.Request) {
	data, err := self.takeSetupProfile(request.Context(), mux.Vars(request)["token"])
	if err != nil {
		log.Errorf("cannot take a setup profile: %s", err)
		http.Error(response, "The setup profile could not be read. Make another.", http.StatusInternalServerError)
		return
	}
	if data == nil {
		http.Error(response, "This setup profile was downloaded already or has expired. Make another.", http.StatusNotFound)
		return
	}
	response.Header().Set("Content-Type", "application/x-apple-aspen-config")
	response.Header().Set("Content-Disposition", `attachment; filename="TeaNode.mobileconfig"`)
	response.Header().Set("Cache-Control", "no-store")
	_, _ = response.Write(data)
}

// takeSetupProfile reclaims a parked profile and opens it: nil when there is
// no profile under the token, because it was never made, was downloaded
// already, its time is up, or what is there is not a profile.
func (self *graph) takeSetupProfile(ctx context.Context, token string) ([]byte, error) {
	found, err := self.ceremonies.Take(ctx, token)
	if errors.Is(err, ceremony.ErrNoCeremonyInProgress) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if found.Kind != ceremony.KindSetupProfile || found.SealedSetupProfile == "" {
		return nil, nil
	}
	box, err := self.setupProfileBox()
	if err != nil {
		return nil, err
	}
	data, err := box.Open(found.SealedSetupProfile)
	if errors.Is(err, secretbox.ErrCipher) {
		// Sealed under a server secret that has since changed.
		return nil, nil
	}
	return data, err
}
