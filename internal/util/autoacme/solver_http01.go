package autoacme

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// challengePath is the well-known prefix the certificate authority fetches
// during an http-01 challenge, fixed by RFC 8555 section 8.3.
const challengePath = "/.well-known/acme-challenge/"

// http01Solver answers the http-01 challenge: the certificate authority makes
// a plain HTTP request to
//
//	http://<domain>/.well-known/acme-challenge/<token>
//
// and expects the key authorization as the body. Nothing is written to disk;
// the response is kept in the challenge store only for as long as the order is
// open. The store is shared by every instance, because the request can reach
// any of them, not only the one that ordered.
//
// The handler must be mounted on a listener reachable on port 80 from the
// internet. The server mounts it on the same HTTP listener that serves the
// dashboard, so no extra port is needed. Requests to it are answered before
// any authentication middleware runs, because the authority cannot log in.
type http01Solver struct {
	store ChallengeStore
}

func newHttp01Solver(store ChallengeStore) *http01Solver {
	return &http01Solver{store: store}
}

func (self *http01Solver) Type() string {
	return "http-01"
}

func (self *http01Solver) Present(ctx context.Context, client acmeClient, challenges []Challenge) error {
	for _, challenge := range challenges {
		response, err := client.HTTP01ChallengeResponse(challenge.Challenge.Token)
		if err != nil {
			return fmt.Errorf("autoacme: cannot build the http-01 response for %q: %w", challenge.Domain, err)
		}
		if err := self.store.Put(ctx, self.Type(), challenge.Challenge.Token, response, time.Now().Add(challengeLifetime)); err != nil {
			return fmt.Errorf("autoacme: cannot store the http-01 response for %q: %w", challenge.Domain, err)
		}
		log.Debugf("serving http-01 challenge for %q at %s%s", challenge.Domain, challengePath, challenge.Challenge.Token)
	}
	return nil
}

func (self *http01Solver) CleanUp(ctx context.Context, challenges []Challenge) error {
	var firstErr error
	for _, challenge := range challenges {
		if err := self.store.Delete(ctx, challenge.Challenge.Token); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (self *http01Solver) responseFor(ctx context.Context, token string) (string, bool, error) {
	return self.store.Find(ctx, self.Type(), token)
}

// Handler serves the challenge responses. Mount it at challengePath.
func (self *http01Solver) Handler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		token := strings.TrimPrefix(request.URL.Path, challengePath)
		if token == "" || strings.Contains(token, "/") {
			http.NotFound(response, request)
			return
		}

		body, ok, err := self.responseFor(request.Context(), token)
		if err != nil {
			log.Warningf("failed to look up the http-01 challenge response for token %q: %s", token, err)
			http.Error(response, "challenge store unavailable", http.StatusServiceUnavailable)
			return
		}
		if !ok {
			// Either the order has already been validated and cleaned up, or
			// somebody is probing. Both are ordinary; do not make noise.
			log.Debugf("no http-01 challenge response for token %q", token)
			http.NotFound(response, request)
			return
		}

		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		if _, err := response.Write([]byte(body)); err != nil {
			log.Warningf("failed to write http-01 challenge response: %s", err)
		}
	})
}
