package browser

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ziyan/teanode/internal/util/safefetch"
	"github.com/ziyan/teanode/internal/util/security"
)

// The guarded proxy: the only way out of a page this server opens.
//
// The address guard used to work by resolving the name here and then telling
// Chrome to go ahead, which is a race and not a guard. Between the two, the
// name is resolved a second time -- by Chrome, over its own resolver, on its
// own schedule -- and a record with a one-second lifetime answers the first
// with a public address and the second with 127.0.0.1. The window is the
// whole point of the attack, and nothing in the shape of "check, then ask
// somebody else to connect" can close it.
//
// So nothing resolves names for the browser but this. Chrome is given a proxy
// for the context's lifetime and sends it the host name, unresolved; the
// proxy resolves it, and the check runs on the address the socket is actually
// opened to, in the dialler's own Control function -- after the address is
// chosen and before the connection is made, where there is no gap to race.
// It is the same primitive the fetch tool uses.
//
// The proxy asks for a password. It has to be reachable by Chrome, which in
// the compose file is a container of its own, so it cannot simply listen on
// the loopback address; a password means that something else on that network
// finding the port has nothing but a proxy to the public internet that will
// not talk to it.
type guardedProxy struct {
	listener net.Listener
	server   *http.Server

	// What Chrome is told to connect to: the address on this machine that
	// Chrome's own connection to us came from, which is reachable from
	// wherever Chrome is.
	address string

	username string
	password string

	// allowedHosts and allowed are the operator's exceptions: a name they
	// named may resolve wherever it likes, and an address inside a network
	// they named is permitted although it is private.
	allowedHosts map[string]bool
	allowed      []*net.IPNet

	closeOnce sync.Once
}

// proxyDialTimeout is how long the proxy waits for the far end.
const proxyDialTimeout = 20 * time.Second

// newGuardedProxy starts the proxy on listen, announcing itself at
// announceHost -- the address Chrome reaches this server on.
func newGuardedProxy(listen, announceHost string, allowedHosts map[string]bool, allowed []*net.IPNet) (*guardedProxy, error) {
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("browser: the guarded proxy cannot listen on %s: %w", listen, err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	self := &guardedProxy{
		listener:     listener,
		address:      net.JoinHostPort(announceHost, port),
		username:     "teanode",
		password:     security.GenerateRandomString(32, security.AlphaNumeric),
		allowedHosts: allowedHosts,
		allowed:      allowed,
	}
	self.server = &http.Server{Handler: self, ReadHeaderTimeout: proxyDialTimeout}
	go func() { _ = self.server.Serve(listener) }()
	return self, nil
}

// Close stops the proxy.
func (self *guardedProxy) Close() {
	self.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = self.server.Shutdown(ctx)
		_ = self.listener.Close()
	})
}

// URL is what Chrome is told to use.
func (self *guardedProxy) URL() string {
	return "http://" + self.address
}

func (self *guardedProxy) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if !self.authorized(request) {
		response.Header().Set("Proxy-Authenticate", `Basic realm="teanode"`)
		http.Error(response, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if request.Method == http.MethodConnect {
		self.connect(response, request)
		return
	}
	self.forward(response, request)
}

// authorized is the password Chrome was given, compared without leaking how
// much of it was right.
func (self *guardedProxy) authorized(request *http.Request) bool {
	header := request.Header.Get("Proxy-Authorization")
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(decoded, []byte(self.username+":"+self.password)) == 1
}

// connect is the tunnel every https page goes through. The name arrives
// unresolved, which is the whole point.
func (self *guardedProxy) connect(response http.ResponseWriter, request *http.Request) {
	host, port, err := net.SplitHostPort(request.Host)
	if err != nil {
		http.Error(response, "a proxy connects to host:port", http.StatusBadRequest)
		return
	}
	if port != "80" && port != "443" {
		http.Error(response, "this proxy connects to the web and nothing else", http.StatusForbidden)
		return
	}
	hijacker, ok := response.(http.Hijacker)
	if !ok {
		http.Error(response, "cannot tunnel", http.StatusInternalServerError)
		return
	}
	far, err := self.dial(request.Context(), host, port)
	if err != nil {
		http.Error(response, "refused", http.StatusForbidden)
		return
	}
	defer func() { _ = far.Close() }()
	near, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer func() { _ = near.Close() }()
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := buffered.Flush(); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(far, buffered); done <- struct{}{} }()
	go func() { _, _ = io.Copy(near, far); done <- struct{}{} }()
	<-done
}

// forward is a plain http request, which a proxy is asked to make itself.
func (self *guardedProxy) forward(response http.ResponseWriter, request *http.Request) {
	if request.URL == nil || !request.URL.IsAbs() {
		http.Error(response, "a proxy is asked for an absolute address", http.StatusBadRequest)
		return
	}
	if request.URL.Scheme != "http" {
		http.Error(response, "this proxy speaks http and tunnels https", http.StatusForbidden)
		return
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			return self.dial(ctx, host, port)
		},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: proxyDialTimeout,
	}
	outgoing := request.Clone(request.Context())
	outgoing.RequestURI = ""
	for _, hop := range []string{"Proxy-Authorization", "Proxy-Connection", "Connection", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		outgoing.Header.Del(hop)
	}
	answered, err := transport.RoundTrip(outgoing)
	if err != nil {
		http.Error(response, "refused", http.StatusForbidden)
		return
	}
	defer func() { _ = answered.Body.Close() }()
	for name, values := range answered.Header {
		for _, value := range values {
			response.Header().Add(name, value)
		}
	}
	response.WriteHeader(answered.StatusCode)
	_, _ = io.Copy(response, answered.Body)
}

// dial opens the connection, checking the address it is actually opening it
// to rather than a name that resolved a moment ago.
func (self *guardedProxy) dial(ctx context.Context, host, port string) (net.Conn, error) {
	byName := self.allowedHosts[strings.TrimSpace(strings.ToLower(host))]
	dialer := &net.Dialer{
		Timeout: proxyDialTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			if byName {
				// The operator named this host: wherever it lives is where
				// they meant it to live.
				return nil
			}
			return self.allowAddress(address)
		},
	}
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
}

// allowAddress is the fetch guard, with the operator's own networks added.
func (self *guardedProxy) allowAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("browser: not an address: %s", host)
	}
	for _, network := range self.allowed {
		if network.Contains(ip) {
			return nil
		}
	}
	return safefetch.AllowAddress(address)
}

// proxyAnnounceHost is the address on this machine that Chrome's own
// connection to us came from: whatever Chrome can reach us at, whether it is
// on this host, in a container beside it, or on another machine.
func proxyAnnounceHost(local net.Addr, fallback string) string {
	if host, _, err := net.SplitHostPort(local.String()); err == nil && host != "" {
		if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
			return host
		}
	}
	return fallback
}

// proxyListenAddress is where the proxy binds: the operator's choice, or an
// unused port on the one address Chrome reaches this server at.
//
// That address rather than all of them. Chrome may be a container beside this
// one, so the loopback address is not always enough; binding everything to
// make that case work would also put the proxy on every other network this
// machine is on, which is more than was asked for.
func proxyListenAddress(configured, announceHost string) string {
	if strings.TrimSpace(configured) != "" {
		return strings.TrimSpace(configured)
	}
	return net.JoinHostPort(announceHost, "0")
}
