package autoacme

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
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
// the response is held in memory only for as long as the order is open.
//
// The handler must be mounted on a listener reachable on port 80 from the
// internet. The server mounts it on the same HTTP listener that serves the
// dashboard, so no extra port is needed. Requests to it are answered before
// any authentication middleware runs, because the authority cannot log in.
type http01Solver struct {
	mutex     sync.RWMutex
	responses map[string]string
}

func newHTTP01Solver() *http01Solver {
	return &http01Solver{
		responses: make(map[string]string),
	}
}

func (self *http01Solver) Type() string {
	return "http-01"
}

func (self *http01Solver) Present(ctx context.Context, client acmeClient, challenges []Challenge) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, challenge := range challenges {
		response, err := client.HTTP01ChallengeResponse(challenge.Challenge.Token)
		if err != nil {
			return fmt.Errorf("autoacme: cannot build the http-01 response for %q: %w", challenge.Domain, err)
		}
		self.responses[challenge.Challenge.Token] = response
		log.Debugf("serving http-01 challenge for %q at %s%s", challenge.Domain, challengePath, challenge.Challenge.Token)
	}
	return nil
}

func (self *http01Solver) CleanUp(ctx context.Context, challenges []Challenge) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, challenge := range challenges {
		delete(self.responses, challenge.Challenge.Token)
	}
	return nil
}

func (self *http01Solver) responseFor(token string) (string, bool) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	body, ok := self.responses[token]
	return body, ok
}

// Handler serves the challenge responses. Mount it at challengePath.
func (self *http01Solver) Handler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		token := strings.TrimPrefix(request.URL.Path, challengePath)
		if token == "" || strings.Contains(token, "/") {
			http.NotFound(response, request)
			return
		}

		body, ok := self.responseFor(token)

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
