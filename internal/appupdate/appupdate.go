// Package appupdate says what the newest TeaNode app is on each of the
// channels it reaches people by: TestFlight and the App Store for the iPhone
// app, and the Android channel for the Android app.
//
// The app asks its own server rather than Apple or GitHub, so that the only
// machine it talks to is the one its person chose. The server asks a public
// address and sends nothing about the deployment, as the check for its own
// releases does, and an operator who turned that check off turns this off too.
package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// ChannelTestFlight is a build installed from TestFlight.
	ChannelTestFlight = "testflight"

	// ChannelAppStore is a version installed from the App Store.
	ChannelAppStore = "appstore"

	// ChannelAndroid is a build of the Android app installed from the
	// tester distribution link.
	ChannelAndroid = "android"

	// testFlightEndpoint is what the workflow ios-update-channel.yml keeps
	// up to date: the newest build testers can install now, which is not
	// the newest uploaded, since Apple has to process and review each one.
	testFlightEndpoint = "https://github.com/ziyan/teanode/releases/download/ios-update-channel/testflight.json"

	// androidEndpoint is a file the Android distribution script publishes
	// as a release asset on the same release tag scheme: the newest build
	// testers can install now.
	androidEndpoint = "https://github.com/ziyan/teanode/releases/download/android-update-channel/android.json"

	// appStoreEndpoint is Apple's public lookup. It answers no results until
	// the app is on the store.
	appStoreEndpoint = "https://itunes.apple.com/lookup?bundleId=com.teanode.app"

	// keptFor is how long an answer is used before asking again. Every phone
	// asks when it comes to the front, and the answer changes a few times a
	// day at most.
	keptFor = 30 * time.Minute

	// askWithin bounds a request. A phone's question runs inside a database
	// transaction, so a server whose outbound HTTPS is blocked, an ordinary
	// way to run a mail server, must not hold one for long; a failure is
	// kept like an answer, so that this is paid once in keptFor.
	askWithin = 3 * time.Second

	// answerBytesMost bounds what is read of an answer.
	answerBytesMost = 1 << 20

	// fieldCharactersMost bounds a version or a build number.
	fieldCharactersMost = 32
)

// ErrUnknownChannel is a channel other than the three.
var ErrUnknownChannel = errors.New("appupdate: the channel is testflight, appstore or android")

// Update is the newest app on a channel.
type Update struct {
	UpdateChannel string `json:"updateChannel"`

	// Version is what the App Store shows, such as 1.2.0, or the Android
	// app's version name.
	Version string `json:"version"`

	// BuildNumber tells builds of one version apart. TestFlight and Android
	// only: the App Store shows people a version and nothing else.
	BuildNumber string `json:"buildNumber"`

	// InstallURL opens where the update is installed: TestFlight, the app's
	// page on the App Store, or the Android testers' invitation page.
	InstallURL string `json:"installUrl"`
}

type kept struct {
	update  *Update
	askedAt time.Time
}

// Checker asks the channels and keeps their answers for a while.
type Checker struct {
	client             *http.Client
	testFlightEndpoint string
	appStoreEndpoint   string
	androidEndpoint    string
	now                func() time.Time

	mutex sync.Mutex
	kept  map[string]kept
}

// New is a Checker asking the real addresses.
func New() *Checker {
	return &Checker{
		client:             &http.Client{},
		testFlightEndpoint: testFlightEndpoint,
		appStoreEndpoint:   appStoreEndpoint,
		androidEndpoint:    androidEndpoint,
		now:                time.Now,
		kept:               map[string]kept{},
	}
}

// Latest is the newest app on a channel, nil when the channel has none yet.
// A channel that cannot be asked answers what it said last, since an old
// answer does no harm and an error here reaches nobody who can act on it.
func (self *Checker) Latest(ctx context.Context, updateChannel string) (*Update, error) {
	if updateChannel != ChannelTestFlight && updateChannel != ChannelAppStore && updateChannel != ChannelAndroid {
		return nil, ErrUnknownChannel
	}
	self.mutex.Lock()
	known, isKnown := self.kept[updateChannel]
	self.mutex.Unlock()
	if isKnown && self.now().Sub(known.askedAt) < keptFor {
		return known.update, nil
	}
	ctx, cancel := context.WithTimeout(ctx, askWithin)
	defer cancel()
	var update *Update
	var err error
	switch updateChannel {
	case ChannelTestFlight:
		update, err = self.askTestFlight(ctx)
	case ChannelAndroid:
		update, err = self.askAndroid(ctx)
	default:
		update, err = self.askAppStore(ctx)
	}
	if err != nil {
		update = known.update
	}
	self.mutex.Lock()
	self.kept[updateChannel] = kept{update: update, askedAt: self.now()}
	self.mutex.Unlock()
	if err != nil && !isKnown {
		return nil, err
	}
	return update, nil
}

// read is one answer, nil when there is none to read yet.
func (self *Checker) read(ctx context.Context, endpoint string, into any) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := self.client.Do(request)
	if err != nil {
		return false, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("appupdate: %s answered %d", endpoint, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, answerBytesMost))
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(body, into)
}

func (self *Checker) askTestFlight(ctx context.Context) (*Update, error) {
	var answer struct {
		Version     string `json:"version"`
		BuildNumber string `json:"buildNumber"`
		InstallURL  string `json:"installUrl"`
	}
	if isFound, err := self.read(ctx, self.testFlightEndpoint, &answer); err != nil || !isFound {
		return nil, err
	}
	return checked(&Update{UpdateChannel: ChannelTestFlight, Version: answer.Version, BuildNumber: answer.BuildNumber, InstallURL: answer.InstallURL})
}

// buildNumberText is a build number as the file writes it: text, or a bare
// number, which is how a version code is naturally written.
type buildNumberText string

func (self *buildNumberText) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		*self = buildNumberText(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return err
	}
	*self = buildNumberText(number.String())
	return nil
}

func (self *Checker) askAndroid(ctx context.Context) (*Update, error) {
	var answer struct {
		Version    string          `json:"version"`
		Build      buildNumberText `json:"build"`
		InstallURL string          `json:"installUrl"`
	}
	if isFound, err := self.read(ctx, self.androidEndpoint, &answer); err != nil || !isFound {
		return nil, err
	}
	return checked(&Update{UpdateChannel: ChannelAndroid, Version: answer.Version, BuildNumber: string(answer.Build), InstallURL: answer.InstallURL})
}

func (self *Checker) askAppStore(ctx context.Context) (*Update, error) {
	var answer struct {
		Results []struct {
			Version      string `json:"version"`
			TrackViewURL string `json:"trackViewUrl"`
		} `json:"results"`
	}
	if isFound, err := self.read(ctx, self.appStoreEndpoint, &answer); err != nil || !isFound || len(answer.Results) == 0 {
		return nil, err
	}
	return checked(&Update{UpdateChannel: ChannelAppStore, Version: answer.Results[0].Version, InstallURL: answer.Results[0].TrackViewURL})
}

// checked refuses an answer the app should not act on. The phone opens
// InstallURL, so it has to lead to Apple, or on Android to Firebase's tester
// distribution or Google Play, whatever the file said.
func checked(update *Update) (*Update, error) {
	if !isVersionShaped(update.Version) || len(update.BuildNumber) > fieldCharactersMost {
		return nil, fmt.Errorf("appupdate: %q is not a version", update.Version)
	}
	if !isInstallURL(update.InstallURL, update.UpdateChannel) {
		return nil, fmt.Errorf("appupdate: %q is not where an update is installed", update.InstallURL)
	}
	return update, nil
}

// isVersionShaped says the text is digits and dots.
func isVersionShaped(version string) bool {
	if version == "" || len(version) > fieldCharactersMost {
		return false
	}
	return strings.Trim(version, "0123456789.") == ""
}

func isInstallURL(text, updateChannel string) bool {
	parsed, err := url.Parse(text)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "itms-beta":
		return updateChannel != ChannelAndroid && parsed.Host == "beta.itunes.apple.com"
	case "https":
		if updateChannel == ChannelAndroid {
			return parsed.Host == "appdistribution.firebase.google.com" || parsed.Host == "appdistribution.firebase.dev" || parsed.Host == "play.google.com"
		}
		return parsed.Host == "apps.apple.com" || parsed.Host == "beta.itunes.apple.com" || parsed.Host == "testflight.apple.com"
	}
	return false
}
