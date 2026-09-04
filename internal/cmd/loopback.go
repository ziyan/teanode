package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/ziyan/teanode/internal/client"
)

// Signing in from a browser.
//
// The client listens on a random loopback port and sends the browser to the
// dashboard's /cli page with that port and a nonce. The reader signs in there
// if they are not already, and presses Authorize; the page asks the server
// for a token — the same CreateToken the dashboard's own settings page uses —
// and posts it to this listener. The nonce is what ties the two ends
// together: the page echoes it, and a post without it is refused, so a page
// that was not opened by this very command cannot hand a token to it.
//
// Nothing travels through the clipboard or the shell history. When the
// browser cannot reach the loopback — a remote desktop, a locked-down browser
// — the page shows the complete "auth login --token" command instead, and
// the reader pastes that.

// loginTimeout bounds how long the client waits for the browser. Long enough
// to sign in, find a passkey and read the page; short enough that a forgotten
// terminal does not hold a port for the afternoon.
const loginTimeout = 5 * time.Minute

// CommandLinePagePath is where the dashboard's authorisation page lives.
// Named in both the client and the server, so the two cannot drift.
const CommandLinePagePath = "/cli"

// loginResult is what the page posts back.
type loginResult struct {
	State    string `json:"state"`
	Token    string `json:"token"`
	TokenID  string `json:"tokenId"`
	Username string `json:"username"`

	// Error is set by the page instead of a token when the server refused
	// to issue one, so the client can say why rather than time out.
	Error string `json:"error,omitempty"`
}

// loopback is one listener, waiting for one result.
type loopback struct {
	listener net.Listener
	server   *http.Server
	state    string
	origin   string
	results  chan loginResult
}

// newLoopback starts listening. The origin is the server's, and is the only
// one the browser is told may post here.
func newLoopback(ctx context.Context, serverUrl string) (*loopback, error) {
	parsed, err := url.Parse(client.NormalizeURL(serverUrl))
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("%q is not a server URL", serverUrl)
	}

	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("cannot listen on the loopback interface: %w; pass --token instead", err)
	}

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		_ = listener.Close()
		return nil, err
	}

	self := &loopback{
		listener: listener,
		state:    hex.EncodeToString(nonce),
		origin:   parsed.Scheme + "://" + parsed.Host,
		results:  make(chan loginResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", self.callback)
	self.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		_ = self.server.Serve(listener)
	}()
	return self, nil
}

// Port is the loopback port the page is told to post to.
func (self *loopback) Port() int {
	return self.listener.Addr().(*net.TCPAddr).Port
}

// AuthorizeURL is the page to open: the dashboard's /cli with what it needs
// to find its way back here.
func (self *loopback) AuthorizeURL(serverUrl, name, lifetime string) string {
	query := url.Values{
		"port":  {strconv.Itoa(self.Port())},
		"state": {self.state},
	}
	if name != "" {
		query.Set("name", name)
	}
	if lifetime != "" {
		query.Set("lifetime", lifetime)
	}
	return client.NormalizeURL(serverUrl) + CommandLinePagePath + "?" + query.Encode()
}

// callback receives the page's post.
//
// The headers are what a browser requires before it will let a page on a
// public origin post to a loopback address: the origin has to be allowed
// explicitly, and a request from a public site into a private network gets a
// preflight that has to be acknowledged as intended.
func (self *loopback) callback(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Access-Control-Allow-Origin", self.origin)
	response.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	response.Header().Set("Access-Control-Allow-Private-Network", "true")
	response.Header().Set("Access-Control-Allow-Local-Network", "true")
	response.Header().Set("Vary", "Origin")

	switch request.Method {
	case http.MethodOptions:
		response.WriteHeader(http.StatusNoContent)
		return
	case http.MethodPost:
	default:
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var posted loginResult
	if err := json.NewDecoder(io.LimitReader(request.Body, 1<<20)).Decode(&posted); err != nil {
		http.Error(response, "not a login result", http.StatusBadRequest)
		return
	}
	if posted.State != self.state {
		// Not this command's page. Refused, and not delivered, so a stray or
		// malicious post cannot hand this terminal a token it did not ask for.
		http.Error(response, "state mismatch", http.StatusBadRequest)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	_, _ = response.Write([]byte(`{"ok":true}` + "\n"))
	select {
	case self.results <- posted:
	default:
		// A second post after the first was taken. The first one won.
	}
}

// Wait blocks until the page posts, the context ends, or the timeout passes.
func (self *loopback) Wait(ctx context.Context, timeout time.Duration) (*loginResult, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-self.results:
		if result.Error != "" {
			return nil, fmt.Errorf("the server did not issue a token: %s", result.Error)
		}
		if result.Token == "" {
			return nil, fmt.Errorf("the page posted no token")
		}
		return &result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("cancelled")
	case <-timer.C:
		return nil, fmt.Errorf("gave up after %s waiting for the browser; run it again, or pass --token", timeout)
	}
}

// Close stops listening.
func (self *loopback) Close() {
	_ = self.server.Close()
}

// openBrowser asks the desktop to open a URL. Detached from the command's
// context on purpose: the browser should outlive the command that opened it.
func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}
