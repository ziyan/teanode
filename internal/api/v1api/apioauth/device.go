package apioauth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// Pairing: signing a device in by approving a short code somewhere else
// (RFC 8628, the device authorization grant).
//
// A phone app asks for a pairing and shows the code, "KQ7M-4TXD". The person
// approves it where they are already signed in: the dashboard's /connect page,
// `teanode agent phone approve`, or by telling their agent the code, which
// asks them to confirm. All the while the app asks the token endpoint with the
// long device code only it holds, and once the pairing is approved it is
// handed its token, once. Nobody types a password or a second factor on the
// phone.

const (
	// pairingLifetime is how long a code can be approved in. Long enough to
	// walk to a computer, short enough that a code read off somebody's screen
	// is soon worth nothing.
	pairingLifetime = 10 * time.Minute

	// pairingPollSeconds is how often the device is told it may ask. One
	// asking faster is told to slow down.
	pairingPollSeconds = 5

	// userCodeAlphabet is the consonants RFC 8628 suggests: no vowels, so a
	// code never spells a word, and no 0, O, 1 or I to misread. Eight of them
	// is about 2.5 * 10^10 codes, while ten minutes and the login limiter
	// allow a few dozen guesses.
	userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

	// deviceNameLongest bounds what a device may call itself.
	deviceNameLongest = 64
)

// deviceAuthorizationResponse is the answer RFC 8628 section 3.2 fixes.
type deviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

func (self *oauth) deviceAuthorizationView(response http.ResponseWriter, request *http.Request) {
	// Each request writes a row for somebody who has proved nothing yet, the
	// same as registering, and is limited the same way.
	if !self.authenticator.AllowLoginAttempt(request) {
		writeOAuthError(response, http.StatusTooManyRequests, "slow_down", "too many pairings from this address")
		return
	}
	if err := request.ParseForm(); err != nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_request", "the body is not a form")
		return
	}
	client, err := self.database.GetOAuthClient(clientIdOf(request))
	if err != nil {
		log.Errorf("could not read a client: %s", err)
		writeOAuthError(response, http.StatusInternalServerError, "server_error", "the client could not be read")
		return
	}
	if client == nil {
		writeOAuthError(response, http.StatusUnauthorized, "invalid_client", "no program is registered under that identifier")
		return
	}
	scope := strings.TrimSpace(request.PostForm.Get("scope"))
	resource, ok := resourceOfScope(scope)
	if !ok {
		writeOAuthError(response, http.StatusBadRequest, "invalid_scope", "this server publishes the scopes mcp and api")
		return
	}
	if scope == scopeAPI && !api.IsNativeAppClient(client.RedirectURIs) {
		writeOAuthError(response, http.StatusBadRequest, "invalid_scope",
			"only an app installed on a device may ask for the whole API: register with an app's own address, such as com.example.app:/oauth")
		return
	}

	deviceName := []rune(strings.Join(strings.Fields(request.PostForm.Get("device_name")), " "))
	if len(deviceName) > deviceNameLongest {
		deviceName = deviceName[:deviceNameLongest]
	}

	deviceCode := security.GenerateRandomString(48, security.LowerAlphaNumeric)
	var pairing *models.OAuthDeviceAuthorization
	// A new code clashes with one in use about once in a few billion; asked
	// again rather than refused, and only a few times, so a fault elsewhere
	// still surfaces.
	for attempt := 0; attempt < 3 && pairing == nil; attempt++ {
		pairing, err = self.database.CreateOAuthDeviceAuthorization(&models.OAuthDeviceAuthorization{
			ID:         security.NewULID(),
			ClientID:   client.ID,
			UserCode:   security.GenerateRandomString(api.PairingCodeLength, userCodeAlphabet),
			DeviceName: string(deviceName),
			Resource:   resource,
			ExpiresAt:  time.Now().Add(pairingLifetime),
		}, hashOf(deviceCode))
	}
	if err != nil || pairing == nil {
		log.Errorf("could not start a pairing: %v", err)
		writeOAuthError(response, http.StatusInternalServerError, "server_error", "the pairing could not be started")
		return
	}

	verification := self.originOf(request) + api.PathOAuthConnect
	log.Noticef("the client %s (%q) started a pairing for %q", client.ID, client.Name, pairing.DeviceName)
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(response).Encode(&deviceAuthorizationResponse{
		DeviceCode:              deviceCode,
		UserCode:                api.FormatPairingCode(pairing.UserCode),
		VerificationURI:         verification,
		VerificationURIComplete: verification + "?code=" + pairing.UserCode,
		ExpiresIn:               int64(pairingLifetime / time.Second),
		Interval:                pairingPollSeconds,
	}); err != nil {
		log.Errorf("failed to answer a pairing: %s", err)
	}
}

// collectPairing answers a device asking whether its pairing was approved,
// with its token once it was (RFC 8628 section 3.5).
func (self *oauth) collectPairing(response http.ResponseWriter, request *http.Request) {
	deviceCode := request.PostForm.Get("device_code")
	if deviceCode == "" {
		writeOAuthError(response, http.StatusBadRequest, "invalid_request", "a device code is required")
		return
	}
	now := time.Now()
	pairing, previousPoll, err := self.database.PollOAuthDeviceAuthorization(hashOf(deviceCode), now)
	if err != nil {
		log.Errorf("could not read a pairing: %s", err)
		writeOAuthError(response, http.StatusInternalServerError, "server_error", "the pairing could not be read")
		return
	}
	if pairing == nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant", "that device code was not issued here, or has been used")
		return
	}
	// The program that asked for the pairing, and no other, collects it.
	if pairing.ClientID != clientIdOf(request) {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant", "that device code was issued to a different program")
		return
	}
	if !now.Before(pairing.ExpiresAt) {
		writeOAuthError(response, http.StatusBadRequest, "expired_token", "the code was not approved in time; ask for a new one")
		return
	}
	if pairing.IsDenied {
		writeOAuthError(response, http.StatusBadRequest, "access_denied", "the person refused this pairing")
		return
	}
	if pairing.UserID == "" {
		if !previousPoll.IsZero() && now.Sub(previousPoll) < pairingPollSeconds*time.Second {
			writeOAuthError(response, http.StatusBadRequest, "slow_down", "ask less often")
			return
		}
		writeOAuthError(response, http.StatusBadRequest, "authorization_pending", "the code has not been approved yet")
		return
	}

	// Approved. Taken and deleted in one statement, so two polls arriving
	// together cannot both be handed a token.
	spent, err := self.database.SpendOAuthDeviceAuthorization(hashOf(deviceCode), now)
	if err != nil {
		log.Errorf("could not spend a pairing: %s", err)
		writeOAuthError(response, http.StatusInternalServerError, "server_error", "the pairing could not be read")
		return
	}
	if spent == nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant", "that device code has been used")
		return
	}
	if user := self.authenticator.UserByID(spent.UserID); user == nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant", "that account no longer exists")
		return
	}
	self.issue(response, spent.UserID, pairedTokenName(self.clientName(spent.ClientID), spent.DeviceName), spent.ClientID, spent.Resource)
}

// clientName is what a program calls itself, for naming its tokens.
func (self *oauth) clientName(clientId string) string {
	client, err := self.database.GetOAuthClient(clientId)
	if err == nil && client != nil && client.Name != "" {
		return client.Name
	}
	return "an authorized program"
}

// pairedTokenName is how a paired device's token is listed: the program, and
// the device it is on when it said, so two phones are told apart.
func pairedTokenName(clientName, deviceName string) string {
	if deviceName == "" {
		return clientName
	}
	return clientName + " on " + deviceName
}
