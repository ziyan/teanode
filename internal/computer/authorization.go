package computer

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

// An authorization that comes back to this computer.
//
// Some services send an authorization only to a loopback address, the flow
// meant for a program on somebody's own machine, and refuse the server's own
// address. This computer is somebody's own machine. Asked, it listens on its
// loopback interface for one redirect and sends the browser that brings it on
// to the address the server named, with what came back in its query, so the
// dashboard finishes the connection the way it finishes any other.
//
// The code only ever passes through the browser that is already carrying it.
// Nothing here reads it, keeps it or sends it anywhere itself.

// FeatureAuthorizationForward is this program listening for an
// authorization on its loopback interface when asked.
const FeatureAuthorizationForward = "authorizationForward"

// AuthorizationForwardArguments are where to send the browser on to.
type AuthorizationForwardArguments struct {
	// ForwardURL is the dashboard's address that finishes a connection.
	ForwardURL string `json:"forwardUrl"`

	// LifetimeSeconds is how long to wait for the redirect before the
	// listener closes.
	LifetimeSeconds int `json:"lifetimeSeconds,omitempty"`
}

// AuthorizationForwardResult is the address to give the service.
type AuthorizationForwardResult struct {
	RedirectURL string `json:"redirectUrl"`
}

// Bounds on the listeners: a person signing in and proving who they are can
// take a while, and a listener nobody came back to costs a port and nothing
// else, but a program that is asked again and again should not collect them.
const (
	authorizationLifetimeDefault = 15 * time.Minute
	authorizationLifetimeLongest = time.Hour
	authorizationListenersAtMost = 4
)

// The parameters an authorization comes back with, per OAuth 2.0 and the
// issuer identification it added later. Nothing else is carried on.
var authorizationParameters = []string{"code", "state", "error", "error_description", "error_uri", "iss"}

var (
	authorizationListenersMutex sync.Mutex
	authorizationListenerCount  int
)

// RunAuthorizationForward opens one listener and answers with its address.
// It outlives the request, and the connection too: a network that drops while
// the person is in the middle of signing in should not lose the redirect.
func RunAuthorizationForward(arguments *AuthorizationForwardArguments) (*AuthorizationForwardResult, error) {
	forward, err := url.Parse(strings.TrimSpace(arguments.ForwardURL))
	if err != nil || forward.Host == "" || (forward.Scheme != "http" && forward.Scheme != "https") {
		return nil, fmt.Errorf("%q is not an http or https address to send the authorization on to", arguments.ForwardURL)
	}
	lifetime := authorizationLifetimeDefault
	if arguments.LifetimeSeconds > 0 {
		lifetime = min(time.Duration(arguments.LifetimeSeconds)*time.Second, authorizationLifetimeLongest)
	}

	authorizationListenersMutex.Lock()
	if authorizationListenerCount >= authorizationListenersAtMost {
		authorizationListenersMutex.Unlock()
		return nil, fmt.Errorf("%d authorizations are already waiting on this computer; finish one or wait for it to expire", authorizationListenersAtMost)
	}
	authorizationListenerCount++
	authorizationListenersMutex.Unlock()
	release := func() {
		authorizationListenersMutex.Lock()
		authorizationListenerCount--
		authorizationListenersMutex.Unlock()
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		release()
		return nil, fmt.Errorf("cannot listen on the loopback interface: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	// The address given out names localhost, which is what the services
	// that take only a loopback address expect, and a browser may take
	// localhost to mean the IPv6 loopback first. The same port there too,
	// when this computer has one; without it the browser falls back to the
	// IPv4 one.
	listeners := []net.Listener{listener}
	if sixth, err := net.Listen("tcp", fmt.Sprintf("[::1]:%d", port)); err == nil {
		listeners = append(listeners, sixth)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lifetime)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(response http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("code") == "" && query.Get("error") == "" {
			response.Header().Set("Content-Type", "text/plain; charset=utf-8")
			response.WriteHeader(http.StatusBadRequest)
			_, _ = response.Write([]byte("This address is where an authorization comes back to. There is nothing here.\n"))
			return
		}
		http.Redirect(response, request, forwardedURL(forward, query), http.StatusFound)
		// One redirect is what this was opened for.
		cancel()
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	for _, each := range listeners {
		go func() {
			defer deferutil.Recover()
			_ = server.Serve(each)
		}()
	}
	go func() {
		defer deferutil.Recover()
		defer release()
		<-ctx.Done()
		// A moment for the redirect just written to reach the browser.
		shutdown, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelShutdown()
		_ = server.Shutdown(shutdown)
	}()
	return &AuthorizationForwardResult{RedirectURL: fmt.Sprintf("http://localhost:%d/callback", port)}, nil
}

// forwardedURL is the forward address with the authorization's parameters
// added to whatever query it already has.
func forwardedURL(forward *url.URL, query url.Values) string {
	target := *forward
	values := target.Query()
	for _, name := range authorizationParameters {
		if value := query.Get(name); value != "" {
			values.Set(name, value)
		}
	}
	target.RawQuery = values.Encode()
	return target.String()
}
