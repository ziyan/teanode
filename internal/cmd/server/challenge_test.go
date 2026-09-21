package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The plain listener answers ACME challenges and, when this process serves
// HTTPS itself, sends everything else there. It used to serve the dashboard
// on port 80 as well, which is where a typed hostname lands first, so a
// reader who signed in there sent their password in the clear and got a
// cookie a browser would keep sending that way.
func TestThePlainListenerRedirectsToTLSWhenServingIt(t *testing.T) {
	t.Parallel()

	dashboard := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusOK)
	})

	redirecting := withChallengeHandler(dashboard, nil, true)
	recorder := httptest.NewRecorder()
	redirecting.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://mail.example.com/mail?page=2", nil))
	if recorder.Code != http.StatusPermanentRedirect {
		t.Fatalf("got %d, want a permanent redirect", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "https://mail.example.com/mail?page=2" {
		t.Errorf("redirected to %q", location)
	}

	// With TLS ended at a proxy in front there is no HTTPS listener here,
	// and the plain listener is the dashboard.
	serving := withChallengeHandler(dashboard, nil, false)
	recorder = httptest.NewRecorder()
	serving.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://mail.example.com/mail", nil))
	if recorder.Code != http.StatusOK {
		t.Errorf("got %d, want the dashboard", recorder.Code)
	}
}
