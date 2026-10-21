package apioauth

import (
	"net/http"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
)

// Connecting a phone: an app presents a one-time connect code and is handed a
// token for the whole API as the person the code was made for.
//
// The code was made where the person is signed in (the dashboard's Connect
// page or `teanode agent phone pair`) and shown as a QR code their phone
// scanned. That direction is the point. The opposite
// one, a device showing a short code for the person to approve (RFC 8628),
// lets anybody start a pairing as "TeaNode on Ana's iPhone", send the person a
// link to approve it on their own server, and collect a full token from one
// press; a code that only ever leaves the person's screen for their own
// phone's camera cannot be phished that way.

// grantConnectCode is the grant type an app exchanges a connect code with.
// Not a registered grant: RFC 6749 section 4.5 lets a server define its own
// as an absolute URI.
const grantConnectCode = "urn:teanode:params:oauth:grant-type:connect-code"

// deviceNameLongest bounds what a device may call itself.
const deviceNameLongest = 64

// collectConnectCode answers an app presenting a connect code with its token.
func (self *oauth) collectConnectCode(response http.ResponseWriter, request *http.Request) {
	code := strings.TrimSpace(request.PostForm.Get("connect_code"))
	if code == "" {
		writeOAuthError(response, http.StatusBadRequest, "invalid_request", "a connect code is required")
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
	// The code itself is the credential, so this keeps nobody out who holds
	// one; it keeps the tokens a code gives to programs registered as apps,
	// which is what the person was told they were connecting.
	if !api.IsNativeAppClient(client.RedirectURIs) {
		writeOAuthError(response, http.StatusBadRequest, "unauthorized_client",
			"only an app installed on a device may connect with a code: register with an app's own address, such as com.example.app:/oauth")
		return
	}

	now := time.Now()
	connected, err := self.database.SpendOAuthConnectCode(hashOf(code), now)
	if err != nil {
		log.Errorf("could not spend a connect code: %s", err)
		writeOAuthError(response, http.StatusInternalServerError, "server_error", "the connect code could not be read")
		return
	}
	if connected == nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant",
			"that connect code has been used, has expired, or was not made here; show a new one")
		return
	}
	user := self.authenticator.UserByID(connected.UserID)
	if user == nil || user.Disabled() {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant", "that account no longer exists")
		return
	}
	// Approved in all but name: a client nobody approved is swept after a
	// day, and this one is in service.
	if err := self.database.TouchOAuthClientApproval(client.ID, now); err != nil {
		log.Warningf("could not record that %s was connected: %s", client.ID, err)
	}

	deviceName := api.DisplayName(request.PostForm.Get("device_name"), deviceNameLongest)
	log.Noticef("%s connected the client %s (%q) on %q", user.Username, client.ID, client.Name, deviceName)
	// The code carries whether it was made where a person was tapping; a
	// code a command line token made yields an app token with no marker. And
	// the mark is kept only for the TeaNode app itself: another app that
	// registered a private-use address of its own can collect a code, and
	// gets an ordinary token.
	clientKind := connected.ClientKind
	if !api.IsTeaNodeAppClient(client.RedirectURIs) {
		clientKind = ""
	}
	self.issue(response, user.ID, connectedTokenName(client.Name, deviceName), client.ID, "", clientKind)
}

// connectedTokenName is how a connected phone's token is listed: the program,
// and the device it is on when it said, so two phones are told apart.
func connectedTokenName(clientName, deviceName string) string {
	if clientName == "" {
		clientName = "an app"
	}
	if deviceName == "" {
		return clientName
	}
	return clientName + " on " + deviceName
}
