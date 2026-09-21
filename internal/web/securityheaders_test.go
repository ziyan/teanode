package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/web"
)

// The page the command line client opens to sign in posts the token it
// obtained to a listener on the reader's own machine. That is the one page
// allowed to connect to a loopback address; everywhere else keeps the policy
// that stops a message from reaching anything but this server.
func TestSecurityPolicyAllowsLoopbackOnlyForTheCommandLinePage(t *testing.T) {
	t.Parallel()

	handler := web.MakeSecurityHeadersMiddleware([]string{"'sha256-abc'"}, nil)(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.WriteHeader(http.StatusOK)
		}))

	policyFor := func(path string) string {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder.Header().Get("Content-Security-Policy")
	}

	for _, path := range []string{"/", "/mail", "/cli/", "/settings/tokens", "/clip"} {
		policy := policyFor(path)
		if !strings.Contains(policy, "connect-src 'self';") {
			t.Errorf("%s: connect-src should be 'self' alone, got %q", path, policy)
		}
		if strings.Contains(policy, "127.0.0.1") {
			t.Errorf("%s may reach a loopback address: %q", path, policy)
		}
	}

	policy := policyFor(web.CommandLinePagePath + "?port=1234&state=abc")
	if !strings.Contains(policy, "connect-src 'self' http://127.0.0.1:* http://localhost:*;") {
		t.Errorf("the command line page cannot reach the client's listener: %q", policy)
	}
	// The rest of the policy is unchanged: the script hashes and the frame
	// restrictions are what keep a message from acting on the reader.
	for _, directive := range []string{"script-src 'self' 'sha256-abc'", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(policy, directive) {
			t.Errorf("the command line page lost %q: %q", directive, policy)
		}
	}
}

// A browser that reached the server over TLS is told to keep to it, so a
// hostname typed later does not go to the plain listener first. The header
// is meaningless on a plain response and is not sent there.
func TestStrictTransportSecurityIsSentOverTLSOnly(t *testing.T) {
	t.Parallel()

	handler := web.MakeSecurityHeadersMiddleware(nil, nil)(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.WriteHeader(http.StatusOK)
		}))

	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := plain.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("a plain response carried %q", got)
	}

	secure := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://mail.example.com/", nil)
	handler.ServeHTTP(secure, request)
	if got := secure.Header().Get("Strict-Transport-Security"); !strings.HasPrefix(got, "max-age=") {
		t.Errorf("a TLS response carried %q, want a max-age", got)
	}
}

// The header that says "keep using TLS" is sent when the person reached this
// server over TLS, including when something in front of it ended the TLS.
//
// Asked as "is request.TLS set", it was silently never sent on the ordinary
// deployment: a proxy or a CDN terminates, and the connection here is plain.
// The same question the session cookie asks -- which believes a proxy only
// when the operator listed it -- is the one to ask.
func TestTheTLSHeaderSurvivesAProxy(t *testing.T) {
	t.Parallel()

	trusted := func() []string { return []string{"10.0.0.1"} }
	handler := web.MakeSecurityHeadersMiddleware(nil, trusted)(
		http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusOK)
		}))

	// Plain, straight to this server: nothing to promise.
	plain := httptest.NewRequest(http.MethodGet, "/", nil)
	plain.RemoteAddr = "203.0.113.5:5000"
	recorded := httptest.NewRecorder()
	handler.ServeHTTP(recorded, plain)
	if got := recorded.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("a plain request is told nothing: %q", got)
	}

	// Through a proxy the operator listed, which says the person's own
	// connection was TLS.
	forwarded := httptest.NewRequest(http.MethodGet, "/", nil)
	forwarded.RemoteAddr = "10.0.0.1:5000"
	forwarded.Header.Set("X-Forwarded-Proto", "https")
	recorded = httptest.NewRecorder()
	handler.ServeHTTP(recorded, forwarded)
	if got := recorded.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("the header is sent when a trusted proxy says the person is on TLS")
	}

	// And a stranger claiming the same thing is not believed.
	claimed := httptest.NewRequest(http.MethodGet, "/", nil)
	claimed.RemoteAddr = "203.0.113.9:5000"
	claimed.Header.Set("X-Forwarded-Proto", "https")
	recorded = httptest.NewRecorder()
	handler.ServeHTTP(recorded, claimed)
	if got := recorded.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("anybody may set that header, so it counts only from a listed proxy: %q", got)
	}
}
