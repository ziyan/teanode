package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
		if !FromThisServer(from(origin)) {
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
		if FromThisServer(from(origin)) {
			t.Errorf("%q is not this server and must not open a socket with somebody's cookie", origin)
		}
	}

	// No Origin at all is not a browser, so nothing attached a cookie by
	// itself. It is let through the handshake and has to prove who it is
	// with a token in the first message.
	if !FromThisServer(from("")) {
		t.Error("a client that is not a browser is not doing this to anybody")
	}
}
