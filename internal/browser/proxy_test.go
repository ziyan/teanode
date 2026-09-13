package browser

import (
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// through makes a request through the proxy, as Chrome would.
func through(t *testing.T, proxy *guardedProxy, address string, password string) (*http.Response, error) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		t.Fatalf("the request: %s", err)
	}
	if password != "" {
		request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(proxy.username+":"+password)))
	}
	transport := &http.Transport{
		Proxy:             func(*http.Request) (*url.URL, error) { return url.Parse(proxy.URL()) },
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	return client.Do(request)
}

// The proxy is on a network something else may be on -- in the compose file
// Chrome is a container of its own -- so finding the port is not the same as
// being able to use it.
func TestTheProxyIsNotUsedWithoutThePassword(t *testing.T) {
	t.Parallel()

	proxy, err := newGuardedProxy("127.0.0.1:0", "127.0.0.1", map[string]bool{}, nil)
	if err != nil {
		t.Fatalf("starting it: %s", err)
	}
	defer proxy.Close()

	answered, err := through(t, proxy, "http://example.com/", "")
	if err != nil {
		t.Fatalf("asking: %s", err)
	}
	defer func() { _ = answered.Body.Close() }()
	if answered.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("without the password: %d", answered.StatusCode)
	}
	if answered.Header.Get("Proxy-Authenticate") == "" {
		t.Fatal("and it says how to give one")
	}

	wrong, err := through(t, proxy, "http://example.com/", "not-the-password")
	if err != nil {
		t.Fatalf("asking again: %s", err)
	}
	defer func() { _ = wrong.Body.Close() }()
	if wrong.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("with the wrong one: %d", wrong.StatusCode)
	}
}

// The check runs on the address the connection is made to, which is the only
// place it cannot be raced: the name is resolved once, here, and the socket
// goes to the address that came back.
func TestTheProxyRefusesWhatTheNameResolvesTo(t *testing.T) {
	t.Parallel()

	// A server on this machine, which is exactly what a page must never be
	// able to read: it is reached by a name, and the name is public-looking.
	inside := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "the metadata service")
	}))
	defer inside.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(inside.URL, "http://"))
	if err != nil {
		t.Fatalf("the server's address: %s", err)
	}
	address := "http://localhost:" + port + "/"

	refusing, err := newGuardedProxy("127.0.0.1:0", "127.0.0.1", map[string]bool{}, nil)
	if err != nil {
		t.Fatalf("starting it: %s", err)
	}
	defer refusing.Close()
	answered, err := through(t, refusing, address, refusing.password)
	if err != nil {
		t.Fatalf("asking: %s", err)
	}
	defer func() { _ = answered.Body.Close() }()
	if answered.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(answered.Body)
		t.Fatalf("a name resolving to this machine is refused: %d %q", answered.StatusCode, body)
	}

	// Unless the operator said so. Then it is reached, because they meant it
	// to be -- and they named the host, not an address it might resolve to.
	allowing, err := newGuardedProxy("127.0.0.1:0", "127.0.0.1", map[string]bool{"localhost": true}, nil)
	if err != nil {
		t.Fatalf("starting it: %s", err)
	}
	defer allowing.Close()
	allowed, err := through(t, allowing, address, allowing.password)
	if err != nil {
		t.Fatalf("asking: %s", err)
	}
	defer func() { _ = allowed.Body.Close() }()
	body, _ := io.ReadAll(allowed.Body)
	if allowed.StatusCode != http.StatusOK || !strings.Contains(string(body), "the metadata service") {
		t.Fatalf("the operator's own host: %d %q", allowed.StatusCode, body)
	}
}

// A proxy that connects anywhere is a way to reach a port the page could not
// otherwise speak to. This one connects to the web.
func TestTheProxyOnlyReachesTheWebsPorts(t *testing.T) {
	t.Parallel()

	proxy, err := newGuardedProxy("127.0.0.1:0", "127.0.0.1", map[string]bool{"localhost": true}, nil)
	if err != nil {
		t.Fatalf("starting it: %s", err)
	}
	defer proxy.Close()

	connection, err := net.DialTimeout("tcp", proxy.listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dialling the proxy: %s", err)
	}
	defer func() { _ = connection.Close() }()
	credentials := base64.StdEncoding.EncodeToString([]byte(proxy.username + ":" + proxy.password))
	request := "CONNECT localhost:5432 HTTP/1.1\r\nHost: localhost:5432\r\nProxy-Authorization: Basic " + credentials + "\r\n\r\n"
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatalf("asking: %s", err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	answer := make([]byte, 64)
	read, err := connection.Read(answer)
	if err != nil && read == 0 {
		t.Fatalf("reading the answer: %s", err)
	}
	if !strings.Contains(string(answer[:read]), "403") {
		t.Fatalf("a database port is not the web: %q", answer[:read])
	}
}

// Where Chrome is told to find the proxy: the address its own connection to
// this server came from, so that it works whether Chrome is on this machine,
// in a container beside it, or somewhere else.
func TestTheProxyAnnouncesItselfWhereChromeCanReachIt(t *testing.T) {
	t.Parallel()

	if got := proxyAnnounceHost(&net.TCPAddr{IP: net.ParseIP("172.18.0.4"), Port: 51234}, "127.0.0.1"); got != "172.18.0.4" {
		t.Fatalf("the address Chrome connected to: %q", got)
	}
	if got := proxyAnnounceHost(&net.TCPAddr{IP: net.IPv4zero, Port: 1}, "127.0.0.1"); got != "127.0.0.1" {
		t.Fatalf("and something usable when there is none: %q", got)
	}
	if got := proxyListenAddress("", "172.18.0.4"); got != "172.18.0.4:0" {
		t.Fatalf("by default it binds the one address Chrome can reach it at: %q", got)
	}
	if got := proxyListenAddress(" 127.0.0.1:9000 ", "172.18.0.4"); got != "127.0.0.1:9000" {
		t.Fatalf("and the operator may pin it: %q", got)
	}
}
