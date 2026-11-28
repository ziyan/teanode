package websearch

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

type recordingTransport struct{ requested []string }

func (self *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	self.requested = append(self.requested, request.URL.RawQuery)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"web":{"results":[]}}`)), Header: http.Header{}, Request: request}, nil
}

// A later page is asked for with the service's page offset, counted from
// zero, and a page past the service's last is refused with a way on.
func TestSearchAsksForALaterPage(t *testing.T) {
	transport := &recordingTransport{}
	original := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = original }()
	search := config.AgentSearch{Kind: "brave", APIKey: "key-for-a-test"}

	if _, err := SearchPage(t.Context(), search, "kestrels", 20, 3, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := SearchPage(t.Context(), search, "kestrels", 20, 1, ""); err != nil {
		t.Fatal(err)
	}
	if len(transport.requested) != 2 || !strings.Contains(transport.requested[0], "offset=2") || strings.Contains(transport.requested[1], "offset") {
		t.Fatalf("requests: %v", transport.requested)
	}
	if _, err := SearchPage(t.Context(), search, "kestrels", 20, maximumPage+1, ""); err == nil || !strings.Contains(err.Error(), "other words") {
		t.Fatalf("a page past the last: %v", err)
	}
}
