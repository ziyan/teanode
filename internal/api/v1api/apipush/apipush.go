// Package apipush serves the push relay: what a TeaNode server does when the
// operator holds the TeaNode app's Apple push key and turns
// agent.push.relay.enabled on.
//
// Two endpoints, neither needing a credential, because the relay serves
// phones and servers that have no account here:
//
//   - POST /api/v1/push/registrations: a phone registers its device token and
//     gets back a sealed registration, which it hands its own server.
//   - POST /api/v1/push/notifications: a server sends a notification with a
//     phone's sealed registration, and the relay passes it to Apple.
//
// The relay stores nothing and cannot read what it passes on (see
// internal/push). What it guards is its own capacity and its standing with
// Apple: requests are limited per network, per phone and overall.
package apipush

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/push"
	"github.com/ziyan/teanode/internal/util/ratelimit"
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("apipush")

// requestBytesMost bounds a request body: a notification's payload is at most
// 4 KB, and the rest is a registration and a few fields.
const requestBytesMost = 16 << 10

type relayComponent struct {
	configuration config.Store

	// Per calling network (addressKey): registering is rare, delivering
	// is not.
	registrationsByAddress *ratelimit.Registry
	notificationsByAddress *ratelimit.Registry
	// Per phone, keyed on the device token inside the registration rather
	// than on the registration's text, which has more than one spelling.
	// So one phone cannot be flooded by a server that holds its
	// registration.
	notificationsByPhone *ratelimit.Registry
	// Overall, so callers spread across many networks still cannot make
	// Apple see a flood under the relay's key.
	registrationsOverall *ratelimit.Registry
	notificationsOverall *ratelimit.Registry

	// buildAPNs makes the client that reaches Apple; a test points it at a
	// server of its own.
	buildAPNs func(keyID, teamID, topic, keyPEM string) (*push.APNs, error)

	mutex sync.Mutex
	// relay is built from the configuration it was built for, and rebuilt
	// when the key changes.
	relay    *push.Relay
	builtFor string
}

// New builds the relay component. Its routes are always registered and
// answer 404 while the relay is off, so turning it on takes no restart.
func New(configuration config.Store) (web.Component, error) {
	return &relayComponent{
		configuration:          configuration,
		registrationsByAddress: ratelimit.NewRegistry(10.0/60, 20, 100000, 10*time.Minute),
		notificationsByAddress: ratelimit.NewRegistry(600.0/60, 1200, 100000, 10*time.Minute),
		notificationsByPhone:   ratelimit.NewRegistry(60.0/60, 120, 500000, 10*time.Minute),
		registrationsOverall:   ratelimit.NewRegistry(20, 200, 1, 10*time.Minute),
		notificationsOverall:   ratelimit.NewRegistry(200, 2000, 1, 10*time.Minute),
		buildAPNs:              push.NewAPNs,
	}, nil
}

func (self *relayComponent) AddRoutes(router *mux.Router) error {
	router.Path(push.PathRegistrations).Methods(http.MethodPost).HandlerFunc(self.registerView)
	router.Path(push.PathNotifications).Methods(http.MethodPost).HandlerFunc(self.deliverView)
	return nil
}

// currentRelay is the relay as the configuration now says, or nil while it
// is off or has no key.
func (self *relayComponent) currentRelay() (*push.Relay, error) {
	current := self.configuration.Current()
	settings := current.Agent.Push.Relay
	if !settings.Enabled || !settings.HasKey() {
		return nil, nil
	}
	sum := sha256.Sum256([]byte(settings.APNSKeyID + "\x00" + settings.APNSTeamID + "\x00" + settings.ResolvedTopic() + "\x00" + settings.APNSKey))
	fingerprint := hex.EncodeToString(sum[:])

	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.relay != nil && self.builtFor == fingerprint {
		return self.relay, nil
	}
	apns, err := self.buildAPNs(settings.APNSKeyID, settings.APNSTeamID, settings.ResolvedTopic(), settings.APNSKey)
	if err != nil {
		return nil, err
	}
	relay, err := push.NewRelay(apns)
	if err != nil {
		return nil, err
	}
	self.relay, self.builtFor = relay, fingerprint
	return self.relay, nil
}

func (self *relayComponent) registerView(response http.ResponseWriter, request *http.Request) {
	relay, ok := self.relayOrRefuse(response)
	if !ok {
		return
	}
	if !self.registrationsByAddress.Allow(self.addressKey(request)) {
		writeError(response, http.StatusTooManyRequests, push.ErrorCodeLimited, "too many registrations from this address")
		return
	}
	if !self.registrationsOverall.Allow("") {
		writeError(response, http.StatusTooManyRequests, push.ErrorCodeLimited, "the relay is taking too many registrations; try again shortly")
		return
	}
	var asked push.RegisterRequest
	if err := json.NewDecoder(http.MaxBytesReader(response, request.Body, requestBytesMost)).Decode(&asked); err != nil {
		writeError(response, http.StatusBadRequest, push.ErrorCodeRefused, "the body is not a registration")
		return
	}
	sealed, err := relay.Register(asked.DeviceToken, asked.IsSandbox, asked.PushType, time.Now())
	if err != nil {
		writeError(response, http.StatusBadRequest, push.ErrorCodeRefused, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, &push.RegisterResponse{PushRegistration: sealed})
}

func (self *relayComponent) deliverView(response http.ResponseWriter, request *http.Request) {
	relay, ok := self.relayOrRefuse(response)
	if !ok {
		return
	}
	if !self.notificationsByAddress.Allow(self.addressKey(request)) {
		writeError(response, http.StatusTooManyRequests, push.ErrorCodeLimited, "too many notifications from this address")
		return
	}
	if !self.notificationsOverall.Allow("") {
		writeError(response, http.StatusTooManyRequests, push.ErrorCodeLimited, "the relay is sending too many notifications; try again shortly")
		return
	}
	var asked push.DeliverRequest
	if err := json.NewDecoder(http.MaxBytesReader(response, request.Body, requestBytesMost)).Decode(&asked); err != nil || asked.Delivery == nil {
		writeError(response, http.StatusBadRequest, push.ErrorCodeRefused, "the body is not a notification")
		return
	}
	now := time.Now()
	registration, err := relay.Open(asked.PushRegistration, now)
	if err == nil {
		phoneSum := sha256.Sum256([]byte(registration.DeviceToken))
		if !self.notificationsByPhone.Allow(hex.EncodeToString(phoneSum[:])) {
			writeError(response, http.StatusTooManyRequests, push.ErrorCodeLimited, "too many notifications for this phone")
			return
		}
		err = relay.DeliverTo(request.Context(), registration, asked.Delivery, now)
	}
	switch {
	case err == nil:
		writeJSON(response, http.StatusOK, struct{}{})
	case errors.Is(err, push.ErrGone):
		writeError(response, http.StatusGone, push.ErrorCodeGone, "the phone is no longer registered for notifications; it registers again when the app next starts")
	case errors.Is(err, push.ErrTooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, push.ErrorCodeTooLarge, "the notification is too large")
	case errors.Is(err, push.ErrRefused):
		writeError(response, http.StatusBadRequest, push.ErrorCodeRefused, err.Error())
	default:
		// The kind of failure, never a registration, a token or what was
		// sent; the caller is told only that Apple was not reached.
		log.Warningf("could not pass a notification to Apple: %s", err)
		writeError(response, http.StatusBadGateway, push.ErrorCodeUnreached, "Apple could not be reached; try again later")
	}
}

// addressKey is who a request is limited as: its address, or for IPv6 the
// /64 it is in, which is what one network is usually given. Counting each
// IPv6 address apart would give a caller holding a /64 more budgets than
// anybody could count.
func (self *relayComponent) addressKey(request *http.Request) string {
	address := api.RemoteAddress(request, self.configuration.Current().Server.TrustedProxies)
	parsed, err := netip.ParseAddr(address)
	if err != nil || parsed.Is4() || parsed.Is4In6() {
		return address
	}
	network, err := parsed.Prefix(64)
	if err != nil {
		return address
	}
	return network.String()
}

// relayOrRefuse is the relay, or a 404 written when it is off.
func (self *relayComponent) relayOrRefuse(response http.ResponseWriter) (*push.Relay, bool) {
	relay, err := self.currentRelay()
	if err != nil {
		log.Errorf("the push relay is on but cannot be built: %s", err)
		writeError(response, http.StatusServiceUnavailable, push.ErrorCodeUnreached, "this server's push relay is misconfigured")
		return nil, false
	}
	if relay == nil {
		writeError(response, http.StatusNotFound, push.ErrorCodeRefused, "this server is not a push relay")
		return nil, false
	}
	return relay, true
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		log.Errorf("failed to answer: %s", err)
	}
}

func writeError(response http.ResponseWriter, status int, errorCode, errorMessage string) {
	writeJSON(response, status, &push.ErrorResponse{ErrorCode: errorCode, ErrorMessage: errorMessage})
}
