package apigraph

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/dav"
	"github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/qrcode"
	"github.com/ziyan/teanode/internal/util/security"
)

// Setup profiles: one file an iPhone or a Mac installs to read a mailbox in
// its own Mail, Calendar, Contacts and Reminders. It carries an app password
// made for the device, so it is kept in memory for one download and never
// written anywhere.

type MailboxSetupProfileMutation interface {
	// Make an app password for a device and a setup profile holding it; the profile downloads once, within ten minutes
	CreateMailboxSetupProfile(ctx context.Context, arguments CreateMailboxSetupProfileArguments) (*CreatedSetupProfile, error)
}

type CreateMailboxSetupProfileArguments struct {
	MailboxID string `json:"mailboxId"`

	// What the device is called: "Phone", "Laptop"; the app password is named for it
	DeviceName string `json:"deviceName"`
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

// setupProfileLifetime is how long a download address works.
const setupProfileLifetime = 10 * time.Minute

// setupProfileTokenAlphabet is what a download token is made of: letters
// and digits, safe in a path.
const setupProfileTokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// setupProfileStore holds the profiles made and not yet downloaded.
type setupProfileStore struct {
	lock     sync.Mutex
	profiles map[string]storedSetupProfile
}

type storedSetupProfile struct {
	data      []byte
	expiresAt time.Time
}

// put keeps a profile and returns its token, dropping the ones whose time
// is up.
func (self *setupProfileStore) put(data []byte, now time.Time) (string, time.Time) {
	self.lock.Lock()
	defer self.lock.Unlock()
	if self.profiles == nil {
		self.profiles = map[string]storedSetupProfile{}
	}
	for token, stored := range self.profiles {
		if !now.Before(stored.expiresAt) {
			delete(self.profiles, token)
		}
	}
	token := security.GenerateRandomString(32, setupProfileTokenAlphabet)
	expiresAt := now.Add(setupProfileLifetime)
	self.profiles[token] = storedSetupProfile{data: data, expiresAt: expiresAt}
	return token, expiresAt
}

// take hands a profile out and forgets it; nil when it was never made, was
// downloaded already, or its time is up.
func (self *setupProfileStore) take(token string, now time.Time) []byte {
	self.lock.Lock()
	defer self.lock.Unlock()
	stored, found := self.profiles[token]
	if !found {
		return nil
	}
	delete(self.profiles, token)
	if !now.Before(stored.expiresAt) {
		return nil
	}
	return stored.data
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
	if !api.IsSecure(request, self.config.Current().Server.TrustedProxies) && !api.IsLoopbackHost(hostOnly(request.Host)) {
		return nil, fmt.Errorf("%w: open the dashboard over https to make a setup profile; one fetched over plain http could be read on the way", api.ErrInvalidArguments)
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
	token, expiresAt := self.setupProfiles.put(data, time.Now())
	path := strings.Replace(api.PathMailboxSetupProfile, "{token}", token, 1)
	link := self.originOf(request) + path
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
func (self *graph) setupProfileView(response http.ResponseWriter, request *http.Request) {
	data := self.setupProfiles.take(mux.Vars(request)["token"], time.Now())
	if data == nil {
		http.Error(response, "This setup profile was downloaded already or has expired. Make another.", http.StatusNotFound)
		return
	}
	response.Header().Set("Content-Type", "application/x-apple-aspen-config")
	response.Header().Set("Content-Disposition", `attachment; filename="TeaNode.mobileconfig"`)
	response.Header().Set("Cache-Control", "no-store")
	_, _ = response.Write(data)
}
