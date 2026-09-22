package apioauth

import (
	"encoding/json"
	"net/http"

	"github.com/ziyan/teanode/internal/api"
)

// The one scope this server publishes.
//
// TeaNode's permissions live on accounts and groups, and a token acts as the
// account it belongs to. A vocabulary of scope names invented here would be a
// second answer to what a caller may do, sitting beside the real one, and the
// two would eventually disagree. One name, meaning the tools this account can
// already reach, is the truth about what the token grants.
const scopeMCP = "mcp"

// ProtectedResourceMetadata describes the thing being guarded: what it is,
// who issues tokens for it, and how a token travels.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// AuthorizationServerMetadata names the endpoints a program uses to turn a
// person's approval into a token.
//
// The field names are the ones the specification fixes, so they stay as they
// are; the Go names beside them follow this repository's rules.
type AuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

// originOf is the address this server is being reached at, which is what both
// documents have to be written in terms of.
//
// Taken from the request rather than from a setting. A conforming client
// checks that the issuer it reads back matches the address it asked, and
// refuses the document when they differ -- sameIssuer in
// internal/mcp/oauth.go is this server's own copy of that check. A configured
// address would be one more thing to set correctly and one more way for a
// server answering on two addresses to be wrong on one of them; the address
// in hand is right by construction.
func (self *oauth) originOf(request *http.Request) string {
	scheme := "http"
	if api.IsSecure(request, self.config.Current().Server.TrustedProxies) {
		scheme = "https"
	}
	return scheme + "://" + request.Host
}

func (self *oauth) protectedResourceView(response http.ResponseWriter, request *http.Request) {
	origin := self.originOf(request)
	writeMetadata(response, &ProtectedResourceMetadata{
		Resource:             origin + api.PathAgentMCP,
		AuthorizationServers: []string{origin},
		ScopesSupported:      []string{scopeMCP},
		// In a header, never a query parameter. A token in an address is a
		// token in a proxy log and in a browser's history.
		BearerMethodsSupported: []string{"header"},
	})
}

func (self *oauth) authorizationServerView(response http.ResponseWriter, request *http.Request) {
	origin := self.originOf(request)
	writeMetadata(response, &AuthorizationServerMetadata{
		Issuer:                 origin,
		AuthorizationEndpoint:  origin + api.PathOAuthAuthorize,
		TokenEndpoint:          origin + api.PathOAuthToken,
		RegistrationEndpoint:   origin + api.PathOAuthRegister,
		RevocationEndpoint:     origin + api.PathOAuthRevoke,
		ResponseTypesSupported: []string{"code"},
		GrantTypesSupported:    []string{"authorization_code", "refresh_token"},
		// S256 alone. Offering "plain" beside it lets a client choose the
		// method that hashes nothing, which is the same as not asking.
		CodeChallengeMethodsSupported: []string{"S256"},
		// A program on somebody's laptop cannot keep a secret, so it is not
		// issued one and does not authenticate at the token endpoint. The
		// proof of possession is the PKCE verifier instead.
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported:                   []string{scopeMCP},
	})
}

// writeMetadata sends a discovery document.
//
// Both are public, unchanging for a given address, and read by programs that
// will ask again on the next connection; letting them be cached briefly saves
// a round trip without making a change take long to appear.
func writeMetadata(response http.ResponseWriter, document any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "public, max-age=3600")
	if err := json.NewEncoder(response).Encode(document); err != nil {
		log.Errorf("failed to write a discovery document: %s", err)
	}
}
