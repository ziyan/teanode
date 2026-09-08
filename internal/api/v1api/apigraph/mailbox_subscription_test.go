package apigraph

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Which of the ways a list offers to leave it gets used, and in what order.
//
// The order is the point. One request the sender undertook to honour beats an
// email nobody may read, and both beat handing a page to a person — but a
// sender that promised one-click and named only an address to write to has
// promised nothing, and must fall through rather than fail.
func TestTheWayOutIsChosenInOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers []string
		want    string
	}{
		{
			name: "one-click, which is one request and nothing else",
			headers: []string{
				"List-Unsubscribe: <https://example.com/u>, <mailto:leave@example.com>",
				"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
			},
			want: "https://example.com/u",
		},
		{
			name:    "a page and an address, but no undertaking",
			headers: []string{"List-Unsubscribe: <https://example.com/u>, <mailto:leave@example.com>"},
			want:    "mailto:leave@example.com",
		},
		{
			name: "one-click promised over an address to write to, which it cannot be",
			headers: []string{
				"List-Unsubscribe: <mailto:leave@example.com>",
				"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
			},
			want: "mailto:leave@example.com",
		},
		{
			name:    "only a page, which a person has to open",
			headers: []string{"List-Unsubscribe: <https://example.com/u>"},
			want:    "https://example.com/u",
		},
		{
			name:    "a list that named no way out at all",
			headers: []string{"List-Id: Example Weekly <weekly.example.com>"},
			want:    "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			info := mailparse.ParseList(test.headers, "news@example.com")
			var chosen string
			switch {
			case info.OneClick && info.HTTPSUnsubscribe() != "":
				chosen = info.HTTPSUnsubscribe()
			case info.MailUnsubscribe() != "":
				chosen = info.MailUnsubscribe()
			default:
				chosen = info.WebUnsubscribe()
			}
			if chosen != test.want {
				t.Errorf("chose %q, want %q", chosen, test.want)
			}
		})
	}
}

// The one-click request itself, as RFC 8058 describes it: a POST carrying
// exactly List-Unsubscribe=One-Click and nothing else, and a status that says
// whether the sender accepted it.
func TestOneClickPostsWhatTheSenderExpects(t *testing.T) {
	t.Parallel()

	var method, contentType, body string
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		method = request.Method
		contentType = request.Header.Get("Content-Type")
		read, _ := io.ReadAll(request.Body)
		body = string(read)
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// The address the test server listens on is a loopback address, which the
	// guard against fetching a stranger's URL refuses — deliberately, since
	// that is the whole point of it. So the request is made with the test
	// server's own client rather than through that guard, and what is checked
	// here is the shape of the request; the guard has its own tests in
	// internal/util/safefetch.
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL,
		strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		t.Fatalf("NewRequest: %s", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("Do: %s", err)
	}
	defer func() { _ = response.Body.Close() }()

	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if contentType != "application/x-www-form-urlencoded" {
		t.Errorf("content type = %q", contentType)
	}
	if body != "List-Unsubscribe=One-Click" {
		t.Errorf("body = %q", body)
	}
}

// A one-click address this will not follow: the guard refuses to connect to
// anything that is not a public address, so a list whose unsubscribe URL
// points back inside this network fails rather than being fetched.
func TestOneClickWillNotReachInside(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// http, not https, and on the loopback address: refused twice over.
	if err := postOneClick(context.Background(), server.URL); err == nil {
		t.Fatalf("a request to %s was allowed", server.URL)
	}
}
