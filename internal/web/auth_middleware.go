package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ziyan/teanode/internal/api"
)

// writeJSON sends a JSON body with a status code.
func writeJSON(response http.ResponseWriter, statusCode int, body any) {
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
func MakeAuthenticationMiddleware(authenticator Authenticator, challengePath string) Middleware {
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
			username, ok := authenticator.Authenticate(request)
			if ok && username != "" {
				request.Header.Set(api.AuthenticatedUsernameHeader, username)
			}

			if !ok && !isPublic(path) {
				// Only the API is refused outright. Everything else is the
				// dashboard itself, which has to load in order to show a
				// login form.
				if strings.HasPrefix(path, "/api/") {
					writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
					return
				}
			}
			handler.ServeHTTP(response, request)
		})
	}
}
