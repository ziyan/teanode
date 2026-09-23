package computer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A request made from here goes out as given, follows redirects, and says
// where the answer finally came from.
func TestARequestFromHereFollowsRedirectsAndCarriesItsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/moved":
			http.Redirect(response, request, "/landed", http.StatusFound)
		case "/landed":
			response.Header().Set("X-Seen", request.Header.Get("X-Asked"))
			_, _ = io.WriteString(response, "arrived")
		case "/echo":
			body, _ := io.ReadAll(request.Body)
			_, _ = response.Write(body)
		}
	}))
	defer server.Close()

	result, err := RunHTTP(context.Background(), &HTTPArguments{URL: server.URL + "/moved", Header: map[string][]string{"X-Asked": {"yes"}}})
	if err != nil {
		t.Fatalf("RunHTTP: %s", err)
	}
	if result.Status != http.StatusOK || string(result.Body) != "arrived" || !strings.HasSuffix(result.URL, "/landed") {
		t.Errorf("got %d %q from %s", result.Status, result.Body, result.URL)
	}
	if got := http.Header(result.Header).Get("X-Seen"); got != "yes" {
		t.Errorf("the header asked with was %q at the far end", got)
	}

	echoed, err := RunHTTP(context.Background(), &HTTPArguments{Method: "post", URL: server.URL + "/echo", Body: []byte("a body")})
	if err != nil || string(echoed.Body) != "a body" {
		t.Errorf("a posted body came back as %q, %v", echoed.Body, err)
	}
}

// An answer is read only so far, and says so when it was cut.
func TestAnAnswerIsCutAtItsBound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, strings.Repeat("x", 100))
	}))
	defer server.Close()

	result, err := RunHTTP(context.Background(), &HTTPArguments{URL: server.URL, MaximumBytes: 10})
	if err != nil {
		t.Fatalf("RunHTTP: %s", err)
	}
	if len(result.Body) != 10 || !result.Truncated {
		t.Errorf("read %d bytes, truncated %v", len(result.Body), result.Truncated)
	}
}

// Only web addresses: a file path or another scheme is not a request.
func TestOnlyAWebAddressIsFetched(t *testing.T) {
	for _, address := range []string{"file:///etc/passwd", "ftp://example.com/x", "not an address", "http://"} {
		if _, err := RunHTTP(context.Background(), &HTTPArguments{URL: address}); err == nil {
			t.Errorf("%q was fetched", address)
		}
	}
}
