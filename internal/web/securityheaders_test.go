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

	handler := web.MakeSecurityHeadersMiddleware([]string{"'sha256-abc'"})(
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
