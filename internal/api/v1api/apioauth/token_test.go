package apioauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A client may name itself in the body or in an HTTP Basic header.
//
// RFC 6749 allows either, and a client that used the header was refused for
// naming no client at all, since only the body was read. The body wins when
// both are present, because that is where these clients are told to put it.
func TestTheClientIsReadFromTheBodyOrTheBasicHeader(test *testing.T) {
	request := func(body url.Values, basicUser string) *http.Request {
		made := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body.Encode()))
		made.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if basicUser != "" {
			made.SetBasicAuth(url.QueryEscape(basicUser), "")
		}
		if err := made.ParseForm(); err != nil {
			test.Fatalf("ParseForm: %s", err)
		}
		return made
	}

	if got := clientIdOf(request(url.Values{"client_id": {"from-the-body"}}, "")); got != "from-the-body" {
		test.Errorf("from the body: %q", got)
	}
	if got := clientIdOf(request(url.Values{}, "from-the-header")); got != "from-the-header" {
		test.Errorf("from the header: %q", got)
	}
	if got := clientIdOf(request(url.Values{"client_id": {"from-the-body"}}, "from-the-header")); got != "from-the-body" {
		test.Errorf("with both, the body should win: %q", got)
	}
	if got := clientIdOf(request(url.Values{}, "")); got != "" {
		test.Errorf("with neither: %q", got)
	}
}
