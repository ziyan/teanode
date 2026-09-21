package apigraph

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ziyan/teanode/internal/web"
)

// A form on another site can post here with the person's cookie attached, and
// the browser sends it without asking this server first as long as the request
// looks like one a form could make. It cannot read the answer, but by then the
// mutation has run. Requiring the JSON type takes that shape away.
func TestACookieAuthenticatedPostHasToBeJSON(t *testing.T) {
	t.Parallel()

	withCookie := func(kind string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)
		request.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: "whatever"})
		if kind != "" {
			request.Header.Set("Content-Type", kind)
		}
		return request
	}

	if err := readableAsJSON(withCookie("application/json")); err != nil {
		t.Fatalf("the dashboard's own request: %s", err)
	}
	if err := readableAsJSON(withCookie("application/json; charset=utf-8")); err != nil {
		t.Fatalf("with a charset: %s", err)
	}
	// The three a cross-origin form can send without being asked about.
	for _, kind := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data", ""} {
		if err := readableAsJSON(withCookie(kind)); err == nil {
			t.Fatalf("%q is what a form can send, and is refused", kind)
		}
	}

	// A token is a credential somebody attached on purpose, so nothing about
	// it is forgeable by a page, and a script may post however it likes.
	tokened := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)
	tokened.Header.Set("Authorization", "Bearer something")
	if err := readableAsJSON(tokened); err != nil {
		t.Fatalf("a token-authenticated request is not a browser's: %s", err)
	}
	// And a request with no credential at all is not worth refusing here.
	if err := readableAsJSON(httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)); err != nil {
		t.Fatalf("an unauthenticated request: %s", err)
	}
}

// The websocket carries the same API as the POST above, and a websocket
// handshake is not asked about across origins the way a fetch is: the browser
// opens it with the reader's cookie attached and hands the page the answers.
// So the page that opened it has to be one of this server's own.
//
// What stood here before compared an "X-CSRFToken" header against a
// "csrftoken" cookie. Nothing in this program sets that cookie, so both were
// empty, they matched, and every connection passed the check.
func TestAWebSocketIsOpenedByAPageFromHere(t *testing.T) {
	t.Parallel()

	from := func(origin string) *http.Request {
		request := httptest.NewRequest(http.MethodGet, "http://mail.example.com/api/v1/graphql", nil)
		request.Host = "mail.example.com"
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		return request
	}

	for _, origin := range []string{"http://mail.example.com", "https://mail.example.com", "https://MAIL.example.com"} {
		if !fromThisServer(from(origin)) {
			t.Errorf("%q is this server", origin)
		}
	}
	for _, origin := range []string{
		"https://evil.example",
		"https://mail.example.com.evil.example",
		"null",
		"https://mail.example.com:8443",
		"::::",
	} {
		if fromThisServer(from(origin)) {
			t.Errorf("%q is not this server and must not open a socket with somebody's cookie", origin)
		}
	}

	// No Origin at all is not a browser, so nothing attached a cookie by
	// itself. It is let through the handshake and has to prove who it is
	// with a token in the first message.
	if !fromThisServer(from("")) {
		t.Error("a client that is not a browser is not doing this to anybody")
	}
}
