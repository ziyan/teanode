package apioauth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// What a program may claim about itself, and how much of it is kept.
//
// Registration is open, so every one of these is a stranger's text. The name
// is shown to the person approving and is capped at something a line can
// hold; the addresses are checked rather than trusted.
const (
	clientNameLongest    = 128
	clientRedirectsMost  = 8
	clientRedirectLength = 512
	registerRequestBytes = 1 << 16
)

// registrationRequest is the part of RFC 7591 this server reads. A program
// may send a good deal more, and the rest is accepted and ignored: refusing a
// registration for carrying a field we have no use for would turn a program
// that works into one that does not, for no gain.
type registrationRequest struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

// registrationResponse is what a program gets back.
//
// No secret, and it is told so: token_endpoint_auth_method is "none". A
// program on somebody's machine cannot keep a secret, and issuing one it
// cannot protect would only make the exchange look safer than it is. What
// protects it is the PKCE challenge.
type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
}

// usableRedirect says whether an approval may be sent to an address.
//
// This is the one check in registration that is worth getting exactly right,
// because it is what somebody attacking this would aim at: an approval sent
// to an address of their choosing is the approval stolen. Three rules, and
// each one closes a door.
//
// HTTPS, or HTTP only on a loopback address. A program on somebody's own
// machine receives the answer on a loopback port and cannot have a
// certificate for it, which is why that exception exists and why it is
// limited to loopback.
//
// No fragment. The part after a "#" never reaches a server, so a fragment in
// a registered address is either a misunderstanding or an attempt to make two
// addresses compare as one.
//
// Nothing relative, and no other scheme. An address that is not absolute has
// no host to check, and a scheme this server does not understand is a scheme
// whose rules it cannot enforce.
func usableRedirect(address string) bool {
	if address == "" || len(address) > clientRedirectLength {
		return false
	}
	parsed, err := url.Parse(address)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Fragment != "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return true
	case "http":
		return api.IsLoopbackHost(parsed.Hostname())
	}
	return false
}

func (self *oauth) registerView(response http.ResponseWriter, request *http.Request) {
	// Registration writes a row on behalf of somebody who has not proved
	// anything, which is the whole point of it and also the reason it needs a
	// limit. The login limiter counts per address and is already the answer
	// to this question elsewhere in this server.
	if !self.authenticator.AllowLoginAttempt(request) {
		writeOAuthError(response, http.StatusTooManyRequests, "temporarily_unavailable",
			"too many registrations from this address")
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, registerRequestBytes)
	var asked registrationRequest
	if err := json.NewDecoder(request.Body).Decode(&asked); err != nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_client_metadata",
			"the body is not a registration request")
		return
	}

	if len(asked.RedirectURIs) == 0 {
		writeOAuthError(response, http.StatusBadRequest, "invalid_redirect_uri",
			"a registration has to say where an approval may be sent")
		return
	}
	if len(asked.RedirectURIs) > clientRedirectsMost {
		writeOAuthError(response, http.StatusBadRequest, "invalid_redirect_uri",
			"too many redirect addresses")
		return
	}
	for _, address := range asked.RedirectURIs {
		if !usableRedirect(address) {
			writeOAuthError(response, http.StatusBadRequest, "invalid_redirect_uri",
				"an approval may only be sent to an https address, or to http on a loopback address, and never to one carrying a fragment")
			return
		}
	}

	name := strings.TrimSpace(asked.ClientName)
	if len(name) > clientNameLongest {
		name = name[:clientNameLongest]
	}
	// A name is shown to a person on the approval page, so a line break in it
	// would be a line break in what they read. Replaced rather than refused:
	// this is presentation, not security.
	name = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' {
			return ' '
		}
		return character
	}, name)

	client, err := self.database.CreateOAuthClient(&models.OAuthClient{
		ID:           security.NewULID(),
		Name:         name,
		RedirectURIs: asked.RedirectURIs,
	})
	if err != nil {
		log.Errorf("could not register a client: %s", err)
		writeOAuthError(response, http.StatusInternalServerError, "server_error",
			"the registration could not be stored")
		return
	}

	log.Noticef("registered the client %s (%q)", client.ID, client.Name)
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(response).Encode(&registrationResponse{
		ClientID:                client.ID,
		ClientName:              client.Name,
		RedirectURIs:            client.RedirectURIs,
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientIDIssuedAt:        client.CreatedAt.Unix(),
	}); err != nil {
		log.Errorf("failed to answer a registration: %s", err)
	}
}
