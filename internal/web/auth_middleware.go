package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ziyan/teanode/internal/api"
)

// writeJson sends a JSON body with a status code.
func writeJson(response http.ResponseWriter, statusCode int, body any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(statusCode)
	if err := json.NewEncoder(response).Encode(body); err != nil {
		log.Errorf("failed to encode response: %s", err)
	}
}

// MakeAuthenticationMiddleware refuses anything under /api/ that the caller is
// not entitled to, and hands the authenticated username to the handlers behind
// it.
//
// The paths in api.PublicPaths and under api.PublicPrefixes are always allowed
// through, and so is the ACME challenge, because a certificate authority
// cannot log in.
func MakeAuthenticationMiddleware(authenticator Authenticator, challengePath string, trustedProxies func() []string) Middleware {
	public := make(map[string]bool, len(api.PublicPaths()))
	for _, path := range api.PublicPaths() {
		public[path] = true
	}
	isPublic := func(path string) bool {
		if public[path] {
			return true
		}
		for _, prefix := range api.PublicPrefixes() {
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
		return false
	}

	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			// Never trust this header from outside; it is ours to set.
			request.Header.Del(api.AuthenticatedUsernameHeader)
			request.Header.Del(api.ImpersonatorUsernameHeader)

			path := request.URL.Path

			// The certificate authority solving a challenge is not a caller
			// with an identity, and asking who it is would only get in the
			// way.
			if challengePath != "" && strings.HasPrefix(path, challengePath) {
				handler.ServeHTTP(response, request)
				return
			}

			// Establish who the caller is first, and decide whether to refuse
			// them second. These are separate questions, and running them
			// together is how a public path ended up anonymous even when the
			// caller had a perfectly good session: the early return skipped
			// authentication as well as the refusal, so every resolver behind
			// the GraphQL endpoint was told nobody was there.
			identity, ok := authenticator.AuthenticateIdentity(request)
			username := identity.Username
			if ok && username != "" {
				request.Header.Set(api.AuthenticatedUsernameHeader, username)
				if identity.ImpersonatorUsername != "" {
					request.Header.Set(api.ImpersonatorUsernameHeader, identity.ImpersonatorUsername)
					// Signed in as somebody else is looking, not acting:
					// outside the GraphQL endpoint, which checks each
					// operation itself, only reads go through, and the
					// agent's device sockets not at all. Refused by default,
					// so an endpoint added later is covered without knowing.
					if strings.HasPrefix(path, "/api/") && !isViewOnlyRequest(request) {
						http.Error(response, "not while signed in as somebody else", http.StatusForbidden)
						return
					}
				}
			}

			if !ok && !isPublic(path) {
				// Only the API is refused outright. Everything else is the
				// dashboard itself, which has to load in order to show a
				// login form.
				if strings.HasPrefix(path, "/api/") {
					// Say where to go, not merely no. A program reading
					// this header discovers the authorization server and
					// gets itself a token; without it the only honest
					// report it can make is that the server wants a login
					// it has no way to perform.
					//
					// Only on the endpoint programs speak to. The
					// dashboard fetches GraphQL, mail and media from a
					// browser, and a browser meeting Bearer in this header
					// on a fetch behaves in ways the dashboard does not
					// want.
					if path == api.PathAgentMCP {
						response.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+resourceMetadataURL(request, trustedProxies)+`"`)
					}
					writeJson(response, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
					return
				}
			}
			handler.ServeHTTP(response, request)
		})
	}
}

// resourceMetadataURL is where the refusal points: the document naming this
// resource and the server that authorizes it.
//
// Built from the address in hand for the same reason the documents
// themselves are, in internal/api/v1api/apioauth/metadata.go: a client
// checks that what it reads back matches what it asked for.
func resourceMetadataURL(request *http.Request, trustedProxies func() []string) string {
	var proxies []string
	if trustedProxies != nil {
		proxies = trustedProxies()
	}
	scheme := "http"
	if api.IsSecure(request, proxies) {
		scheme = "https"
	}
	return scheme + "://" + request.Host + api.PathOAuthProtectedResource
}

// isViewOnlyRequest is what an impersonation may send outside GraphQL: a read
// of something the dashboard shows, such as an attachment or a picture. Not
// the sockets the agent drives a browser tab or a computer through, which
// would leave the person's agent attached to the operator's machine.
func isViewOnlyRequest(request *http.Request) bool {
	path := request.URL.Path
	if path == api.PathGraphQL {
		return true
	}
	if path == api.PathAgentTab || path == api.PathAgentComputer {
		return false
	}
	return request.Method == http.MethodGet || request.Method == http.MethodHead
}
